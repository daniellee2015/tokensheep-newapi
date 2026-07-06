// Package tokensheep_setting holds all TokenSheep-specific runtime settings
// that live outside the upstream new-api schema. Keeping them here lets us
// pull upstream cleanly — every field the operator can tweak from the admin
// panel lives in this package rather than being sprinkled through the
// existing setting/ subtrees.
//
// See docs/spec/economy-model.md for the full economic model this module
// implements. All monetary values are quota cents (1 cent = $0.01 × QuotaPerUnit).
package tokensheep_setting

import (
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

// EconomySetting groups every tier-driven knob. All monetary values are
// quota cents; SessionLimits is a simple integer count of in-flight requests.
//
//   - CheckinAwardByGroup: user_group -> quota cents credited on each check-in.
//   - GiftPoolCap:         ceiling for quota_gift per account.
//   - TierThresholds:      cumulative-donation cents that promote a user into
//     each tier. Applied together with the "donation in
//     last 30 days" liveness rule.
//   - GiftPoolInactiveDays: after this many days without an API request the
//     daily zeroing cron wipes quota_gift.
//   - DowngradeInactiveDays: after this many days without a new donation the
//     downgrade cron drops the user back to `free`.
//   - SessionLimits:       max simultaneous in-flight requests per user, keyed
//     by tier. Enforced by the session-concurrency
//     middleware (see middleware/session_concurrency.go).
type EconomySetting struct {
	CheckinAwardByGroup   map[string]int `json:"checkin_award_by_group"`
	GiftPoolCap           int            `json:"gift_pool_cap"`
	TierThresholds        map[string]int `json:"tier_thresholds"`
	GiftPoolInactiveDays  int            `json:"gift_pool_inactive_days"`
	DowngradeInactiveDays int            `json:"downgrade_inactive_days"`
	SessionLimits         map[string]int `json:"session_limits"`
}

var (
	economyMu      sync.RWMutex
	economySetting = EconomySetting{
		CheckinAwardByGroup: map[string]int{
			// See docs/spec/economy-model.md §4.2 — free tier can't check in;
			// paid tiers earn a fixed daily gift into quota_gift.
			"supporter": 50000,   // $0.50
			"fan":       300000,  // $3.00
			"bestie":    500000,  // $5.00
			"vip":       1000000, // $10.00 — highest daily gift
		},
		GiftPoolCap: 5000000, // $50
		TierThresholds: map[string]int{
			// §2.2 — cumulative donation cents to promote into each tier.
			"supporter": 1000000,  // $10
			"fan":       5000000,  // $50
			"bestie":    10000000, // $100
			"vip":       50000000, // $500 — invite-only whale tier
		},
		GiftPoolInactiveDays:  30,
		DowngradeInactiveDays: 30,
		SessionLimits: map[string]int{
			// §2.2 — simultaneous in-flight request ceilings per tier.
			"free":      1,
			"supporter": 3,
			"fan":       5,
			"bestie":    8,
			"vip":       15,
		},
	}
)

// SessionLimit returns the maximum simultaneous in-flight requests a member
// of `group` may hold. Zero means no session-concurrency ceiling for that
// group (unknown/legacy groups fall through to zero on purpose so operators
// notice the miss).
func SessionLimit(group string) int {
	economyMu.RLock()
	defer economyMu.RUnlock()
	if economySetting.SessionLimits == nil {
		return 0
	}
	return economySetting.SessionLimits[group]
}

func init() {
	config.GlobalConfig.Register("tokensheep_economy", &economySetting)
}

// GetEconomySetting returns a snapshot of the current economy config. Values
// are copies so callers can't mutate the underlying maps.
func GetEconomySetting() EconomySetting {
	economyMu.RLock()
	defer economyMu.RUnlock()
	awards := make(map[string]int, len(economySetting.CheckinAwardByGroup))
	for k, v := range economySetting.CheckinAwardByGroup {
		awards[k] = v
	}
	tiers := make(map[string]int, len(economySetting.TierThresholds))
	for k, v := range economySetting.TierThresholds {
		tiers[k] = v
	}
	sessions := make(map[string]int, len(economySetting.SessionLimits))
	for k, v := range economySetting.SessionLimits {
		sessions[k] = v
	}
	return EconomySetting{
		CheckinAwardByGroup:   awards,
		GiftPoolCap:           economySetting.GiftPoolCap,
		TierThresholds:        tiers,
		GiftPoolInactiveDays:  economySetting.GiftPoolInactiveDays,
		DowngradeInactiveDays: economySetting.DowngradeInactiveDays,
		SessionLimits:         sessions,
	}
}

// CheckinAward returns the quota cents credited to a member of `group` on
// their next successful check-in. Returns 0 when the group isn't eligible.
func CheckinAward(group string) int {
	economyMu.RLock()
	defer economyMu.RUnlock()
	if economySetting.CheckinAwardByGroup == nil {
		return 0
	}
	return economySetting.CheckinAwardByGroup[group]
}

// GiftDailyLimit returns the maximum quota_gift that may be spent today by a
// user in `group`. It intentionally uses the same operator map as check-in
// awards. Free users cannot check in, but welcome-code gift credit still needs
// a small daily spend allowance.
func GiftDailyLimit(group string) int {
	economyMu.RLock()
	defer economyMu.RUnlock()
	if economySetting.CheckinAwardByGroup == nil {
		return 0
	}
	if limit := economySetting.CheckinAwardByGroup[group]; limit > 0 {
		return limit
	}
	if group == "free" {
		return 50000
	}
	return 0
}

// GiftPoolCap returns the maximum quota_gift a user may accumulate.
func GiftPoolCap() int {
	economyMu.RLock()
	defer economyMu.RUnlock()
	if economySetting.GiftPoolCap <= 0 {
		return 5000000
	}
	return economySetting.GiftPoolCap
}

// TierForDonation returns the tier a user with `totalDonatedCents` cumulative
// donations *and* recent activity qualifies for. Callers must also check
// "donated within DowngradeInactiveDays" — this function only implements the
// monetary side of the rule.
//
// The tier name and threshold pair is looked up from TierThresholds at call
// time so the operator can add or remove tiers from the admin panel without
// touching this code. Ties on threshold are broken by choosing the
// alphabetically greater name (stable across restarts). Groups with a
// non-positive threshold are treated as `free` fallbacks.
func TierForDonation(totalDonatedCents int) string {
	economyMu.RLock()
	defer economyMu.RUnlock()

	bestName := "free"
	bestThreshold := -1
	for name, threshold := range economySetting.TierThresholds {
		if threshold <= 0 || totalDonatedCents < threshold {
			continue
		}
		if threshold > bestThreshold ||
			(threshold == bestThreshold && name > bestName) {
			bestThreshold = threshold
			bestName = name
		}
	}
	return bestName
}

// GiftPoolInactiveDays is the age threshold (in days) after which a users
// quota_gift is zeroed by the daily cron.
func GiftPoolInactiveDays() int {
	economyMu.RLock()
	defer economyMu.RUnlock()
	if economySetting.GiftPoolInactiveDays <= 0 {
		return 30
	}
	return economySetting.GiftPoolInactiveDays
}

// DowngradeInactiveDays is the age threshold (in days) after which a user is
// dropped back to `free` because they haven't donated recently.
func DowngradeInactiveDays() int {
	economyMu.RLock()
	defer economyMu.RUnlock()
	if economySetting.DowngradeInactiveDays <= 0 {
		return 30
	}
	return economySetting.DowngradeInactiveDays
}

// UpdateEconomySettingByJSONString is invoked by the admin panel when the
// operator saves the economy tab. Only the fields present in the incoming
// JSON are overwritten so partial saves are safe.
func UpdateEconomySettingByJSONString(jsonStr string) error {
	economyMu.Lock()
	defer economyMu.Unlock()
	// Copy so partial overwrites keep defaults for any omitted fields.
	next := economySetting
	if err := common.Unmarshal([]byte(jsonStr), &next); err != nil {
		return err
	}
	economySetting = next
	return nil
}

// EconomySetting2JSONString exposes the current state for the option-map.
func EconomySetting2JSONString() string {
	economyMu.RLock()
	defer economyMu.RUnlock()
	b, err := common.Marshal(&economySetting)
	if err != nil {
		return "{}"
	}
	return string(b)
}
