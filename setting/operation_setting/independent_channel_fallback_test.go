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
