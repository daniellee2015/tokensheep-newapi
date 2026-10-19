package operation_setting

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestIndependentFallbackRequiresExplicitScope(t *testing.T) {
	value := `{"enabled":true,"groups":["gemini-lowprice"],"models":["gemini-3.8-flash"],"primary_channel_ids":[12,140],"reserve_channel_id":146}`
	require.NoError(t, ValidateIndependentChannelFallback(value))
	common.OptionMapRWMutex.Lock()
	old := common.OptionMap
	common.OptionMap = map[string]string{IndependentChannelFallbackOption: value}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() { common.OptionMapRWMutex.Lock(); common.OptionMap = old; common.OptionMapRWMutex.Unlock() })
	require.NotNil(t, MatchIndependentChannelFallback("gemini-lowprice", "gemini-3.8-flash", 12))
	assert.Nil(t, MatchIndependentChannelFallback("other", "gemini-3.8-flash", 12))
	assert.Nil(t, MatchIndependentChannelFallback("gemini-lowprice", "gemini-3.1-pro", 12))
	assert.Nil(t, MatchIndependentChannelFallback("gemini-lowprice", "gemini-3.8-flash", 999))
	for _, invalid := range []string{
		`{"enabled":true}`,
		`{"enabled":true,"groups":["auto"],"models":["m"],"primary_channel_ids":[12],"reserve_channel_id":146}`,
		`{"enabled":true,"groups":["g"],"models":["m"],"primary_channel_ids":[12,140,141],"reserve_channel_id":146}`,
		`{"enabled":true,"groups":["g"],"models":["m"],"primary_channel_ids":[12,146],"reserve_channel_id":146}`,
	} {
		assert.Error(t, ValidateIndependentChannelFallback(invalid))
	}
}

func TestIndependentFallbackMultipleBillingGroups(t *testing.T) {
	value := `[{"enabled":true,"groups":["gemini-sale"],"models":["gemini-3.8-flash"],"primary_channel_ids":[12,140],"reserve_channel_id":146},{"enabled":true,"groups":["gemini-ultra"],"models":["gemini-4-argon"],"primary_channel_ids":[148,161],"reserve_channel_id":159},{"enabled":true,"groups":["gemini-ultra-per-call"],"models":["gemini-4-argon"],"primary_channel_ids":[149,162],"reserve_channel_id":160}]`
	common.OptionMapRWMutex.Lock()
	old := common.OptionMap
	common.OptionMap = map[string]string{IndependentChannelFallbackOption: value}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() { common.OptionMapRWMutex.Lock(); common.OptionMap = old; common.OptionMapRWMutex.Unlock() })
	require.NoError(t, ValidateIndependentChannelFallback(value))
	for _, tc := range []struct {
		group, model     string
		channel, reserve int
	}{
		{"gemini-sale", "gemini-3.8-flash", 12, 146},
		{"gemini-ultra", "gemini-4-argon", 148, 159},
		{"gemini-ultra", "gemini-4-argon", 161, 159},
		{"gemini-ultra-per-call", "gemini-4-argon", 149, 160},
		{"gemini-ultra-per-call", "gemini-4-argon", 162, 160},
	} {
		policy := MatchIndependentChannelFallback(tc.group, tc.model, tc.channel)
		require.NotNil(t, policy)
		assert.Equal(t, tc.reserve, policy.ReserveChannelID)
	}
	assert.Nil(t, MatchIndependentChannelFallback("gemini-ultra-per-call", "gemini-4-argon", 161))
	assert.Nil(t, MatchIndependentChannelFallback("gemini-ultra", "gemini-3.8-flash", 148))
	assert.Nil(t, MatchIndependentChannelFallback("other", "gemini-4-argon", 148))
	assert.Error(t, ValidateIndependentChannelFallback(`[{"enabled":true}]`))
	assert.Error(t, ValidateIndependentChannelFallback(`[{"enabled":true,"groups":["g"],"models":["m"],"primary_channel_ids":[1],"reserve_channel_id":2},{"enabled":true,"groups":["g"],"models":["m"],"primary_channel_ids":[3],"reserve_channel_id":4}]`))
}
