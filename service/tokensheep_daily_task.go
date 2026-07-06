// TokenSheep daily maintenance cron.
//
// Runs once per day (interval configurable via env var below) on the master
// node only. Two jobs are combined so we only walk the users table once:
//
//   1. Downgrade users who havent donated within the last N days back to
//      `free`. Users who *have* donated recently are re-classified based on
//      their `total_donated` against the tier thresholds — this also handles
//      any earlier missed upgrades (e.g. if the webhook briefly failed).
//   2. Zero out `quota_gift` for users idle for N days. Their `quota_paid`
//      (the money they actually put in) is untouched.
//
// See docs/spec/economy-model.md §4.4 and §4.5.
package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/tokensheep_setting"
	"github.com/bytedance/gopkg/util/gopool"
)

var (
	tokenSheepDailyOnce     sync.Once
	tokenSheepDailyRunning  atomic.Bool
	tokenSheepDailyInterval = 6 * time.Hour // check every 6h; individual users only mutate when they cross a boundary
)

// StartTokenSheepDailyTask launches the daily-maintenance goroutine. Safe to
// call multiple times; only the first call boots the worker.
func StartTokenSheepDailyTask() {
	tokenSheepDailyOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			ctx := context.Background()
			logger.LogInfo(ctx, fmt.Sprintf("tokensheep daily task started: tick=%s", tokenSheepDailyInterval))
			ticker := time.NewTicker(tokenSheepDailyInterval)
			defer ticker.Stop()

			runTokenSheepDailyOnce(ctx)
			for range ticker.C {
				runTokenSheepDailyOnce(ctx)
			}
		})
	})
}

// runTokenSheepDailyOnce guards against overlapping runs (in case a previous
// tick is still processing when the next arrives) and walks all users once.
func runTokenSheepDailyOnce(ctx context.Context) {
	if !tokenSheepDailyRunning.CompareAndSwap(false, true) {
		return
	}
	defer tokenSheepDailyRunning.Store(false)

	giftInactiveDays := tokensheep_setting.GiftPoolInactiveDays()
	downgradeInactiveDays := tokensheep_setting.DowngradeInactiveDays()
	now := time.Now().Unix()
	giftIdleCutoff := now - int64(giftInactiveDays)*86400
	downgradeCutoff := now - int64(downgradeInactiveDays)*86400

	downgraded, giftedZeroed, err := model.RunTokenSheepDailyMaintenance(
		giftIdleCutoff,
		downgradeCutoff,
	)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("tokensheep daily task failed: %v", err))
		return
	}
	if downgraded > 0 || giftedZeroed > 0 {
		logger.LogInfo(ctx, fmt.Sprintf(
			"tokensheep daily task done: downgraded=%d gift_zeroed=%d",
			downgraded, giftedZeroed,
		))
	}
}
