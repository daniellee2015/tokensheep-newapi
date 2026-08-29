package model

// v4 regression: RechargeWaffo accumulates total_donated and moves user.group
// only when (a) the TradeNo carries the WAFFO_PANCAKE_TIER prefix, (b) the user
// is not in a commercial group, and (c) the user is not inside an active
// subscription. Any deviation from these invariants would either strand
// commercial resellers on the wrong tier or silently upgrade standard-topup
// users into contribution tiers, violating the "does not count toward
// contribution tiers" UI copy on the standard top-up card.
//
// See docs/spec/economy-model-v4.md §八 items B2 / B10 / B12.

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/tokensheep_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// insertUserV4TierTest inserts a user with the given group so RechargeWaffo
// can consult its commercial-group / subscription state without touching
// unrelated columns.
func insertUserV4TierTest(t *testing.T, id int, group string) {
	t.Helper()
	user := &User{
		Id:       id,
		Username: "v4_tier_" + group,
		Status:   common.UserStatusEnabled,
		Group:    group,
		Quota:    0,
	}
	require.NoError(t, DB.Create(user).Error)
}

// insertPendingWaffoTopUp inserts a pending Waffo Pancake top-up with the
// supplied TradeNo (its prefix controls the tier vs standard branch).
func insertPendingWaffoTopUp(t *testing.T, tradeNo string, userID int, amount int64) {
	t.Helper()
	topUp := &TopUp{
		UserId:          userID,
		Amount:          amount,
		Money:           float64(amount),
		TradeNo:         tradeNo,
		PaymentMethod:   PaymentMethodWaffoPancake,
		PaymentProvider: PaymentProviderWaffoPancake,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	require.NoError(t, DB.Create(topUp).Error)
}

// getUserDonationState reads the fields the tier ladder depends on.
func getUserDonationState(t *testing.T, id int) (group string, totalDonated int) {
	t.Helper()
	var user User
	require.NoError(t, DB.Select("`group`, total_donated").
		Where("id = ?", id).First(&user).Error)
	return user.Group, user.TotalDonated
}

// TestRechargeWaffo_StandardTopUpKeepsTierNeutral verifies that a plain
// WAFFO_PANCAKE- top-up credits quota but does NOT touch total_donated or
// user.group. Standard top-ups must remain tier-neutral so the "Add Funds"
// card's UI copy stays truthful (B10).
func TestRechargeWaffo_StandardTopUpKeepsTierNeutral(t *testing.T) {
	truncateTables(t)
	insertUserV4TierTest(t, 501, "free")
	insertPendingWaffoTopUp(t, "WAFFO_PANCAKE-501-1-std", 501, 10)

	require.NoError(t, RechargeWaffoPancake("WAFFO_PANCAKE-501-1-std"))

	group, donated := getUserDonationState(t, 501)
	assert.Equal(t, "free", group, "standard top-up must not change user.group")
	assert.Equal(t, 0, donated, "standard top-up must not accumulate total_donated")
}

// TestRechargeWaffo_TierContributionAccumulatesAndUpgrades verifies that a
// WAFFO_PANCAKE_TIER- top-up bumps total_donated by the credited quota and
// re-runs TierForDonation, promoting the user when a threshold is crossed
// (B10 positive path).
func TestRechargeWaffo_TierContributionAccumulatesAndUpgrades(t *testing.T) {
	truncateTables(t)
	insertUserV4TierTest(t, 502, "free")
	// $10 contribution -> 5,000,000 quota which is the supporter threshold.
	insertPendingWaffoTopUp(t, "WAFFO_PANCAKE_TIER-502-1-sup", 502, 10)

	require.NoError(t, RechargeWaffoPancake("WAFFO_PANCAKE_TIER-502-1-sup"))

	group, donated := getUserDonationState(t, 502)
	assert.Equal(t, 5_000_000, donated, "tier contribution must bump total_donated")
	assert.Equal(t, "supporter", group, "crossing supporter threshold must upgrade group")
}

// TestRechargeWaffo_CommercialUserImmuneToTierUpgrade guards B2: even if a
// TIER-prefixed order somehow lands on a commercial user (via legacy
// TradeNo, admin manual injection, or race with a group change) the
// settlement must NOT accumulate total_donated and must NOT touch the
// commercial group. Otherwise wholesale users could get auto-promoted into
// the free-tier ladder and their donation counter would drift.
func TestRechargeWaffo_CommercialUserImmuneToTierUpgrade(t *testing.T) {
	truncateTables(t)
	// Seed the fresh commercial-groups option value so IsCommercialGroup
	// resolves correctly under the test binary (the package init only wires
	// up the default set).
	require.True(t, tokensheep_setting.IsCommercialGroup("wholesale"),
		"test precondition: wholesale must be recognised as commercial")

	insertUserV4TierTest(t, 503, "wholesale")
	insertPendingWaffoTopUp(t, "WAFFO_PANCAKE_TIER-503-1-mistake", 503, 10)

	require.NoError(t, RechargeWaffoPancake("WAFFO_PANCAKE_TIER-503-1-mistake"))

	group, donated := getUserDonationState(t, 503)
	assert.Equal(t, "wholesale", group, "commercial group must survive tier settlement")
	assert.Equal(t, 0, donated, "commercial user must not accumulate total_donated")
}

// TestRechargeWaffo_SubscriptionActivePreservesGroup guards B12: while a
// user has an active subscription, their group is the subscription's
// UpgradeGroup (e.g. "sub"). A tier contribution during that window must
// still accumulate total_donated (so the eventual downgrade lands them at
// the right tier) but must NOT overwrite the "sub" group, or the expiry
// hook loses its cue to revert to PrevUserGroup.
func TestRechargeWaffo_SubscriptionActivePreservesGroup(t *testing.T) {
	truncateTables(t)
	insertUserV4TierTest(t, 504, "sub")
	// Insert an active subscription for user 504 that ends well into the
	// future — that keeps the RechargeWaffoPancake path from treating this
	// as a lapsed subscription case.
	plan := insertSubscriptionPlanForPaymentGuardTest(t, 4001)
	subscription := &UserSubscription{
		UserId:              504,
		PlanId:              plan.Id,
		AmountTotal:         5_000_000,
		AmountUsed:          0,
		StartTime:           time.Now().Add(-time.Hour).Unix(),
		EndTime:             time.Now().Add(30 * 24 * time.Hour).Unix(),
		Status:              "active",
		UpgradeGroup:        "sub",
		PrevUserGroup:       "free",
		AllowWalletOverflow: true,
	}
	require.NoError(t, DB.Create(subscription).Error)

	insertPendingWaffoTopUp(t, "WAFFO_PANCAKE_TIER-504-1-during-sub", 504, 10)
	require.NoError(t, RechargeWaffoPancake("WAFFO_PANCAKE_TIER-504-1-during-sub"))

	group, donated := getUserDonationState(t, 504)
	assert.Equal(t, "sub", group,
		"tier contribution during an active subscription must not overwrite the sub group")
	assert.Equal(t, 5_000_000, donated,
		"tier contribution during an active subscription must still accumulate total_donated")
}
