package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/require"
)

// TestFormatUserLogsStripsQuotaSaturation verifies the admin-only quota
// saturation marker (nested under other.admin_info) is removed for non-admin
// log views, since formatUserLogs strips the whole admin_info object.
func TestFormatUserLogsStripsQuotaSaturation(t *testing.T) {
	other := common.MapToJsonStr(map[string]interface{}{
		"model_price":         0.004,
		"channel_id":          84,
		"channel_name":        "legacy-private-channel",
		"channel_type":        14,
		"upstream_error_code": "legacy-private-code",
		"admin_info": map[string]interface{}{
			"quota_saturation": map[string]interface{}{
				"op":      "QuotaFromDecimal",
				"kind":    "overflow",
				"clamped": common.MaxQuota,
			},
		},
	})
	logs := []*Log{{
		ChannelId:         84,
		ChannelName:       "private-channel",
		UpstreamRequestId: "upstream-request-id",
		Other:             other,
	}}

	formatUserLogs(logs, 0)

	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	_, hasAdminInfo := parsed["admin_info"]
	require.False(t, hasAdminInfo, "admin_info (and nested quota_saturation) must be stripped for non-admin views")
	require.Zero(t, logs[0].ChannelId)
	require.Empty(t, logs[0].ChannelName)
	require.Empty(t, logs[0].UpstreamRequestId)
	require.NotContains(t, parsed, "channel_id")
	require.NotContains(t, parsed, "channel_name")
	require.NotContains(t, parsed, "channel_type")
	require.NotContains(t, parsed, "upstream_error_code")
	// Non-admin billing fields remain visible.
	require.Contains(t, parsed, "model_price")
}
