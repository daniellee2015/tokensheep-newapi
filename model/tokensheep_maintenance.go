// TokenSheep daily maintenance queries.
//
// Split into a separate file so we can keep the SQL close to the schema
// definition without spilling tokensheep-specific concerns into user.go.
// See docs/spec/economy-model.md §4.4 / §4.5.
package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/tokensheep_setting"
)

// RunTokenSheepDailyMaintenance executes the two cron jobs in a single walk
// of the users table:
//
//   - zero-out quota_gift for users whose `last_request_at` is older than
//     `giftIdleCutoff` (unix seconds). Their `quota` (paid) stays intact.
//   - recompute `group` for every user based on their most-recent 30 days of
//     donations vs the total donated. Users who donated in the last window
//     stay at (or move up to) the tier their total qualifies them for; users
//     who didnt fall back to `free`.
//
// Returns the number of downgraded/upgraded users and the number of gift
// pools zeroed. Any query error is bubbled up so the scheduler can log it.
func RunTokenSheepDailyMaintenance(giftIdleCutoff int64, downgradeCutoff int64) (tierMutations int, giftZeroed int, err error) {
	// 1) Gift-pool zeroing. Predicate applied server-side so we dont pull
	//    the whole table over the wire.
	giftIdleExpr := "CASE WHEN last_request_at > 0 THEN last_request_at ELSE created_at END"
	var giftUserIds []int
	if err := DB.Model(&User{}).
		Where("quota_gift > 0 AND "+giftIdleExpr+" < ?", giftIdleCutoff).
		Pluck("id", &giftUserIds).Error; err != nil {
		return 0, 0, err
	}
	resZero := DB.Exec(
		`UPDATE users
		    SET quota_gift = 0
		  WHERE quota_gift > 0
		    AND CASE WHEN last_request_at > 0 THEN last_request_at ELSE created_at END < ?`,
		giftIdleCutoff,
	)
	if resZero.Error != nil {
		return 0, 0, resZero.Error
	}
	giftZeroed = int(resZero.RowsAffected)
	for _, userId := range giftUserIds {
		if err := invalidateUserCache(userId); err != nil {
			common.SysLog("failed to invalidate user cache after gift zeroing: " + err.Error())
		}
	}

	// 2) Tier recomputation. We only need the users whose current group is
	//    a tokensheep tier — legacy `default` / admin groups are left alone.
	//    Load them in batches so the process footprint stays flat.
	var users []struct {
		Id           int
		Group        string `gorm:"column:group"`
		TotalDonated int    `gorm:"column:total_donated"`
	}
	err = DB.Table("users").
		Select("id, "+commonGroupColumn()+", total_donated").
		Where(commonGroupColumn()+" IN ?", []string{"free", "supporter", "fan", "bestie", "vip"}).
		Find(&users).Error
	if err != nil {
		return 0, giftZeroed, err
	}

	// For the "must have donated in the last N days" check, ask top_ups.
	// We do a single grouped query rather than per-user round-trips.
	recentByUser := map[int]bool{}
	{
		var rows []struct {
			UserId int
		}
		err = DB.Table("top_ups").
			Select("user_id").
			Where("status = ? AND complete_time >= ?", common.TopUpStatusSuccess, downgradeCutoff).
			Group("user_id").
			Find(&rows).Error
		if err != nil {
			return 0, giftZeroed, err
		}
		for _, r := range rows {
			recentByUser[r.UserId] = true
		}
	}

	for _, u := range users {
		var newGroup string
		if !recentByUser[u.Id] {
			// No recent donation — drop to free regardless of history.
			newGroup = "free"
		} else {
			newGroup = tokensheep_setting.TierForDonation(u.TotalDonated)
		}
		if newGroup != u.Group {
			if err := DB.Table("users").
				Where("id = ?", u.Id).
				Update("group", newGroup).Error; err != nil {
				return tierMutations, giftZeroed, err
			}
			if err := invalidateUserCache(u.Id); err != nil {
				common.SysLog("failed to invalidate user cache after tier maintenance: " + err.Error())
			}
			tierMutations++
		}
	}

	return tierMutations, giftZeroed, nil
}
