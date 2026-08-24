// Copyright (C) 2023-2026 QuantumNous
//
// Licensed under the GNU Affero General Public License, version 3 or later.
// See LICENSE / NOTICE for the full text.
package tokensheep_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGiftDailyLimitFallsBackForDefaultGroup pins the invariant behind the
// new-user gift-pool fix: users who register into the "default" fallback
// group (common.DefaultUserGroup) must be able to spend welcome-code credit
// from quota_gift. Returning zero here freezes the pool and every request
// 400s with "insufficient quota" even though GetUserQuota reports a positive
// balance.
func TestGiftDailyLimitFallsBackForDefaultGroup(t *testing.T) {
	// Snapshot and restore state so this test doesn't leak into others.
	previous := EconomySetting2JSONString()
	t.Cleanup(func() {
		if err := UpdateEconomySettingByJSONString(previous); err != nil {
			t.Logf("failed to restore economy setting: %v", err)
		}
	})

	require.NoError(t, UpdateEconomySettingByJSONString(`{"checkin_award_by_group":{"vip":1500000}}`))

	// Configured groups keep their explicit allowance.
	require.Equal(t, 1500000, GiftDailyLimit("vip"))
	// Free tier and the tokensheep fallback group both get a small spend
	// allowance so welcome codes are usable out of the box.
	require.Equal(t, 50000, GiftDailyLimit("free"))
	require.Equal(t, 50000, GiftDailyLimit("default"))
	require.Equal(t, 50000, GiftDailyLimit(""))
	// Groups the operator hasn't opted in stay at zero.
	require.Equal(t, 0, GiftDailyLimit("commercial-only"))
}
