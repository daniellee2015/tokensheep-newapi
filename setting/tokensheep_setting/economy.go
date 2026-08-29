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
	"sort"
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
	CheckinAwardByGroup   map[string]int  `json:"checkin_award_by_group"`
	GiftPoolCap           int             `json:"gift_pool_cap"`
	TierThresholds        map[string]int  `json:"tier_thresholds"`
	GiftPoolInactiveDays  int             `json:"gift_pool_inactive_days"`
	DowngradeInactiveDays int             `json:"downgrade_inactive_days"`
	SessionLimits         map[string]int  `json:"session_limits"`
	// CommercialGroups: reseller / bulk-contract groups that live outside the
	// contribution-tier ladder. Members do NOT accumulate total_donated on
	// top-up, do NOT get auto-promoted by TierForDonation, and CANNOT purchase
	// subscription plans. Admin-assigned only. See docs/spec/economy-model-v4.md.
	CommercialGroups map[string]bool `json:"commercial_groups"`
	// DisabledTiers: tier names hidden from the contribution ladder UI and
	// skipped by TierForDonation. Existing users already in a disabled tier
	// keep that group until the next daily maintenance run reassigns them.
	// Used to temporarily close vip without deleting its configuration.
	DisabledTiers map[string]bool `json:"disabled_tiers"`
}

var (
	economyMu      sync.RWMutex
	economySetting = EconomySetting{
		// All monetary values in this struct are in quota units, where
		// QuotaPerUnit = 500,000 quota per station dollar (see
		// common/constants.go). So $10 = 5,000,000 quota. The seed values
		// below match docs/spec/economy-model.md §2.2 & §4.2.
		CheckinAwardByGroup: map[string]int{
			// See docs/spec/economy-model.md §4.2 — free tier can't check in;
			// paid tiers earn a fixed daily gift into quota_gift.
			"supporter": 250_000,   // $0.50
			"fan":       1_500_000, // $3.00
			"bestie":    2_500_000, // $5.00
			"vip":       5_000_000, // $10.00 — highest daily gift
		},
		GiftPoolCap: 25_000_000, // $50
		TierThresholds: map[string]int{
			// §2.2 — cumulative donation quota to promote into each tier.
			"supporter": 5_000_000,   // $10
			"fan":       25_000_000,  // $50
			"bestie":    50_000_000,  // $100
			"vip":       250_000_000, // $500 — invite-only whale tier
		},
		GiftPoolInactiveDays:  30,
		DowngradeInactiveDays: 30,
		SessionLimits: map[string]int{
			// §2.2 — simultaneous in-flight request ceilings per tier.
			// v4 §八 B15: `default` is the pre-append_free landing spot for
			// brand-new signups and must have its own ceiling, otherwise a
			// signup burst before the append_free hook fires can slip
			// through the system-wide 5000/min fallback. Mirror `free`.
			// v4 R2.1: `promo` needs a low limit so leaked promo codes
			// can't be weaponized into a concurrency abuse channel.
			// Seed only applies on fresh installs (options table empty);
			// production values are the ones already in the DB.
			"default":   1,
			"free":      1,
			"supporter": 3,
			"fan":       5,
			"bestie":    8,
			"vip":       15,
			"promo":     2,
		},
		// v4: reseller / bulk-contract groups. Members buy quota outright and
		// are admin-assigned only, so they are excluded from every contribution-
		// tier code path (donation accounting, ladder UI, downgrade cron,
		// subscription purchase).
		CommercialGroups: map[string]bool{
			"retail":         true,
			"wholesale":      true,
			"wholesale-plus": true,
		},
		// v4: DisabledTiers is empty by default. Add "vip": true here (or via
		// admin panel) to hide vip from the ladder and stop TierForDonation
		// from returning it. Existing vip users survive until daily cron picks
		// them up and reassigns to bestie.
		DisabledTiers: map[string]bool{},
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

// TierCard is the wallet-facing shape for a single tier upgrade option.
// Amount is expressed in station dollars (unit = 1 = $1), converted from
// the TierThresholds map's quota-cent values.
type TierCard struct {
	Tier   string `json:"tier"`
	Amount int    `json:"amount"`
}

// IsCommercialGroup reports whether `group` is a reseller / bulk-contract
// group. Members are excluded from the contribution-tier ladder (donation
// accounting skips them, TierForDonation never returns them, the ladder UI
// hides them, the downgrade cron doesn't touch them, and they cannot buy
// subscription plans). Admin-assigned only.
//
// v4: seeded from EconomySetting.CommercialGroups (option
// tokensheep_economy.commercial_groups) so operators can edit the set from
// the admin panel without a code change.
func IsCommercialGroup(group string) bool {
	economyMu.RLock()
	defer economyMu.RUnlock()
	if economySetting.CommercialGroups == nil {
		return false
	}
	return economySetting.CommercialGroups[group]
}

// IsTierDisabled reports whether `tier` should be hidden from the contribution
// ladder and skipped by TierForDonation. Used to temporarily close a tier
// (e.g. vip) without deleting its configuration.
func IsTierDisabled(tier string) bool {
	economyMu.RLock()
	defer economyMu.RUnlock()
	if economySetting.DisabledTiers == nil {
		return false
	}
	return economySetting.DisabledTiers[tier]
}

// CommercialGroups is retained as a compatibility export for callers that
// iterate the set (rather than checking a single group). It reflects the
// current EconomySetting.CommercialGroups snapshot at call time.
//
// Deprecated: prefer IsCommercialGroup. Retained until callers migrate.
func CommercialGroups() map[string]bool {
	economyMu.RLock()
	defer economyMu.RUnlock()
	out := make(map[string]bool, len(economySetting.CommercialGroups))
	for k, v := range economySetting.CommercialGroups {
		if v {
			out[k] = true
		}
	}
	return out
}

// IsTierContribution reports whether (tier, amountDollars) is a legitimate
// contribution-card click: the tier name exists in TierThresholds and its
// configured dollar amount equals amountDollars. Used to exempt tier-card
// payments from the standard MinTopUp floor (tier amounts are fixed by config,
// e.g. supporter=$10, and must be payable even when MinTopUp is higher). The
// amount must match the server-side config, so a client can't bypass MinTopUp
// by sending an arbitrary tier name.
func IsTierContribution(tier string, amountDollars int64) bool {
	if tier == "" {
		return false
	}
	economyMu.RLock()
	defer economyMu.RUnlock()
	// Commercial groups carry a sentinel threshold to keep them selectable in
	// the admin dropdown, but their $-amount must never match a contribution
	// card. Disabled tiers similarly can't be purchased through the ladder UI.
	if economySetting.CommercialGroups != nil && economySetting.CommercialGroups[tier] {
		return false
	}
	if economySetting.DisabledTiers != nil && economySetting.DisabledTiers[tier] {
		return false
	}
	const quotaPerDollar = 500_000
	quota, ok := economySetting.TierThresholds[tier]
	if !ok || quota <= 0 {
		return false
	}
	return int64(quota/quotaPerDollar) == amountDollars
}

// GetTierThresholdsCopy returns a shallow copy of the tier threshold map.
// Used by the admin user-editor to enumerate valid tier names — see
// controller/group.go for the "tier vs pricing group" split.
func GetTierThresholdsCopy() map[string]int {
	economyMu.RLock()
	defer economyMu.RUnlock()
	out := make(map[string]int, len(economySetting.TierThresholds))
	for k, v := range economySetting.TierThresholds {
		out[k] = v
	}
	return out
}

// TierCardsSorted materializes the currently-configured TierThresholds map
// as a slice sorted by amount (ascending). Threshold values live in quota
// units where 500,000 quota = $1 (matches common.QuotaPerUnit), so we
// divide to convert the map into station dollars for the wallet UI.
//
// Thresholds <= 0 are excluded (they signify "free tier" or admin-cleared
// rows), as are CommercialGroups (their thresholds are unreachable sentinels,
// not purchasable tiers) and DisabledTiers (temporarily hidden without
// deleting configuration). This keeps the wallet Tier row responsive to admin
// panel edits with no code change.
func TierCardsSorted() []TierCard {
	economyMu.RLock()
	rawThresholds := make(map[string]int, len(economySetting.TierThresholds))
	for k, v := range economySetting.TierThresholds {
		rawThresholds[k] = v
	}
	commercial := economySetting.CommercialGroups
	disabled := economySetting.DisabledTiers
	economyMu.RUnlock()

	// Kept as a local constant to avoid importing common from setting/.
	// If common.QuotaPerUnit is ever changed this needs to move with it.
	const quotaPerDollar = 500_000

	cards := make([]TierCard, 0, len(rawThresholds))
	for tier, quota := range rawThresholds {
		if quota <= 0 {
			continue
		}
		if commercial != nil && commercial[tier] {
			continue
		}
		if disabled != nil && disabled[tier] {
			continue
		}
		cards = append(cards, TierCard{
			Tier:   tier,
			Amount: quota / quotaPerDollar,
		})
	}
	sort.Slice(cards, func(i, j int) bool {
		return cards[i].Amount < cards[j].Amount
	})
	return cards
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
//
// Empty and "default" groups fall back to the free allowance. New users land
// in `common.DefaultUserGroup = "default"` until an admin promotes them, and
// returning zero here freezes their gift pool: GetUserQuota reports a
// positive balance from welcome codes while every request 400s on
// "insufficient quota". This is exactly the trap the tokensheep economy is
// designed to prevent.
func GiftDailyLimit(group string) int {
	economyMu.RLock()
	defer economyMu.RUnlock()
	if limit, ok := economySetting.CheckinAwardByGroup[group]; ok && limit > 0 {
		return limit
	}
	if group == "" || group == "free" || group == "default" {
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
		// Skip commercial groups (their thresholds are unreachable sentinels
		// used only to keep the admin dropdown populated) and any tier the
		// operator has explicitly disabled (v4 "disabled_tiers" switch).
		if economySetting.CommercialGroups != nil && economySetting.CommercialGroups[name] {
			continue
		}
		if economySetting.DisabledTiers != nil && economySetting.DisabledTiers[name] {
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
