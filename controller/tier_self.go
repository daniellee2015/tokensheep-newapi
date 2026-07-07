package controller

import (
	"net/http"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/tokensheep_setting"

	"github.com/gin-gonic/gin"
)

// myTierView is the TokenSheep-flavored per-user tier snapshot consumed by
// the profile TierCard / TierLimitsCard and the overview mini cards.
//
// Unlike upstream/100b (spend-triggered tiers with daily gates + rebate),
// TokenSheep tiers are contribution-triggered: total_donated crosses the
// TierThresholds ladder. All monetary values here are station dollars
// (quota / QuotaPerUnit) so the frontend renders "$" directly.
type myTierView struct {
	Group string `json:"group"`

	// Limits for the current tier.
	RPM          int `json:"rpm"`           // requests/min (0 = unlimited)
	SessionLimit int `json:"session_limit"` // concurrent in-flight (0 = unlimited)

	// Wallet dual pool (station $).
	QuotaPaid      float64 `json:"quota_paid"`
	QuotaGift      float64 `json:"quota_gift"`
	GiftPoolCap    float64 `json:"gift_pool_cap"`
	GiftUsedToday  float64 `json:"gift_used_today"`
	GiftDailyLimit float64 `json:"gift_daily_limit"`

	// Daily check-in award for this tier (station $, 0 = not eligible e.g. free).
	DailyGift float64 `json:"daily_gift"`

	// Contribution progress toward the next tier.
	TotalDonated       float64 `json:"total_donated"`
	NextTier           string  `json:"next_tier"`            // "" if already at top
	NextThreshold      float64 `json:"next_threshold"`       // station $, 0 if top
	NextProgress       float64 `json:"next_progress"`        // 0..1
	ToNextContribution float64 `json:"to_next_contribution"` // station $ remaining
}

func quotaToDollar(q int) float64 {
	if common.QuotaPerUnit <= 0 {
		return 0
	}
	return float64(q) / common.QuotaPerUnit
}

// GetMyTier returns the current user's tier snapshot. Always 200 for a logged
// in user; the frontend cards decide their own visibility.
func GetMyTier(c *gin.Context) {
	userId := c.GetInt("id")
	user, err := model.GetUserById(userId, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	group := user.Group

	view := myTierView{
		Group:         group,
		QuotaPaid:     quotaToDollar(user.Quota),
		QuotaGift:     quotaToDollar(user.QuotaGift),
		GiftPoolCap:   quotaToDollar(tokensheep_setting.GiftPoolCap()),
		TotalDonated:  quotaToDollar(user.TotalDonated),
		DailyGift:     quotaToDollar(tokensheep_setting.CheckinAward(group)),
	}

	// today's gift consumption (resets daily; stale date = 0 used today)
	if user.GiftQuotaUsedDate == time.Now().Format("2006-01-02") {
		view.GiftUsedToday = quotaToDollar(user.GiftQuotaUsed)
	}
	view.GiftDailyLimit = quotaToDollar(tokensheep_setting.GiftDailyLimit(group))

	// RPM from the native per-group rate-limit map.
	if total, _, found := setting.GetGroupRateLimit(group); found {
		view.RPM = total
	}
	view.SessionLimit = tokensheep_setting.SessionLimit(group)

	// Next tier: walk the TierThresholds ladder for the first threshold
	// strictly above total_donated.
	thresholds := tokensheep_setting.GetTierThresholdsCopy()
	type tierPair struct {
		name  string
		quota int
	}
	pairs := make([]tierPair, 0, len(thresholds))
	for name, q := range thresholds {
		if q > 0 {
			pairs = append(pairs, tierPair{name, q})
		}
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].quota < pairs[j].quota })

	// current tier floor = highest threshold <= total_donated (0 if none)
	currentFloor := 0
	for _, p := range pairs {
		if user.TotalDonated >= p.quota {
			currentFloor = p.quota
		}
	}
	for _, p := range pairs {
		if p.quota > user.TotalDonated {
			view.NextTier = p.name
			view.NextThreshold = quotaToDollar(p.quota)
			span := p.quota - currentFloor
			if span > 0 {
				prog := float64(user.TotalDonated-currentFloor) / float64(span)
				if prog < 0 {
					prog = 0
				}
				if prog > 1 {
					prog = 1
				}
				view.NextProgress = prog
			}
			view.ToNextContribution = quotaToDollar(p.quota - user.TotalDonated)
			break
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    view,
	})
}
