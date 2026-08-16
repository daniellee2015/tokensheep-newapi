package claude

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestPatchClaudeMessageDeltaUsageDataPreserveUnknownFields(t *testing.T) {
	originalData := `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":53},"vendor_meta":{"trace_id":"trace_001"}}`
	usage := &dto.ClaudeUsage{
		InputTokens:              100,
		CacheReadInputTokens:     30,
		CacheCreationInputTokens: 50,
	}

	patchedData := patchClaudeMessageDeltaUsageData(originalData, usage)

	require.Equal(t, "message_delta", gjson.Get(patchedData, "type").String())
	require.Equal(t, "end_turn", gjson.Get(patchedData, "delta.stop_reason").String())
	require.Equal(t, "trace_001", gjson.Get(patchedData, "vendor_meta.trace_id").String())
	require.EqualValues(t, 53, gjson.Get(patchedData, "usage.output_tokens").Int())
	require.EqualValues(t, 100, gjson.Get(patchedData, "usage.input_tokens").Int())
	require.EqualValues(t, 30, gjson.Get(patchedData, "usage.cache_read_input_tokens").Int())
	require.EqualValues(t, 50, gjson.Get(patchedData, "usage.cache_creation_input_tokens").Int())
}

func TestPatchClaudeMessageDeltaUsageDataZeroValueChecks(t *testing.T) {
	originalData := `{"type":"message_delta","usage":{"output_tokens":53,"input_tokens":9,"cache_read_input_tokens":0}}`
	usage := &dto.ClaudeUsage{
		InputTokens:              100,
		CacheReadInputTokens:     30,
		CacheCreationInputTokens: 0,
	}

	patchedData := patchClaudeMessageDeltaUsageData(originalData, usage)

	require.EqualValues(t, 9, gjson.Get(patchedData, "usage.input_tokens").Int())
	require.EqualValues(t, 30, gjson.Get(patchedData, "usage.cache_read_input_tokens").Int())
	assert.False(t, gjson.Get(patchedData, "usage.cache_creation_input_tokens").Exists())
}

func TestShouldSkipClaudeMessageDeltaUsagePatch(t *testing.T) {
	originGlobalPassThrough := model_setting.GetGlobalSettings().PassThroughRequestEnabled
	t.Cleanup(func() {
		model_setting.GetGlobalSettings().PassThroughRequestEnabled = originGlobalPassThrough
	})

	model_setting.GetGlobalSettings().PassThroughRequestEnabled = true
	assert.True(t, shouldSkipClaudeMessageDeltaUsagePatch(&relaycommon.RelayInfo{}))

	model_setting.GetGlobalSettings().PassThroughRequestEnabled = false
	assert.True(t, shouldSkipClaudeMessageDeltaUsagePatch(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelSetting: dto.ChannelSettings{PassThroughBodyEnabled: true}},
	}))
	assert.False(t, shouldSkipClaudeMessageDeltaUsagePatch(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelSetting: dto.ChannelSettings{PassThroughBodyEnabled: false}},
	}))
}

func TestBuildMessageDeltaPatchUsage(t *testing.T) {
	t.Run("merge missing fields from claudeInfo", func(t *testing.T) {
		claudeResponse := &dto.ClaudeResponse{Usage: &dto.ClaudeUsage{OutputTokens: 53}}
		claudeInfo := &ClaudeResponseInfo{
			Usage: &dto.Usage{
				PromptTokens: 100,
				PromptTokensDetails: dto.InputTokenDetails{
					CachedTokens:         30,
					CachedCreationTokens: 50,
				},
				ClaudeCacheCreation5mTokens: 10,
				ClaudeCacheCreation1hTokens: 20,
			},
		}

		usage := buildMessageDeltaPatchUsage(claudeResponse, claudeInfo)
		require.NotNil(t, usage)
		require.EqualValues(t, 100, usage.InputTokens)
		require.EqualValues(t, 30, usage.CacheReadInputTokens)
		require.EqualValues(t, 50, usage.CacheCreationInputTokens)
		require.EqualValues(t, 53, usage.OutputTokens)
		require.NotNil(t, usage.CacheCreation)
		require.EqualValues(t, 30, usage.CacheCreation.Ephemeral5mInputTokens)
		require.EqualValues(t, 20, usage.CacheCreation.Ephemeral1hInputTokens)
	})

	t.Run("keep upstream non-zero values", func(t *testing.T) {
		claudeResponse := &dto.ClaudeResponse{Usage: &dto.ClaudeUsage{
			InputTokens:              9,
			CacheReadInputTokens:     7,
			CacheCreationInputTokens: 6,
		}}
		claudeInfo := &ClaudeResponseInfo{Usage: &dto.Usage{
			PromptTokens: 100,
			PromptTokensDetails: dto.InputTokenDetails{
				CachedTokens:         30,
				CachedCreationTokens: 50,
			},
		}}

		usage := buildMessageDeltaPatchUsage(claudeResponse, claudeInfo)
		require.EqualValues(t, 9, usage.InputTokens)
		require.EqualValues(t, 7, usage.CacheReadInputTokens)
		require.EqualValues(t, 6, usage.CacheCreationInputTokens)
	})

	t.Run("default aggregate cache creation to 5m when split missing", func(t *testing.T) {
		claudeResponse := &dto.ClaudeResponse{Usage: &dto.ClaudeUsage{
			OutputTokens:             53,
			CacheCreationInputTokens: 50,
		}}
		claudeInfo := &ClaudeResponseInfo{Usage: &dto.Usage{
			PromptTokensDetails: dto.InputTokenDetails{
				CachedCreationTokens: 50,
			},
		}}

		usage := buildMessageDeltaPatchUsage(claudeResponse, claudeInfo)
		require.NotNil(t, usage)
		require.NotNil(t, usage.CacheCreation)
		require.EqualValues(t, 50, usage.CacheCreation.Ephemeral5mInputTokens)
		require.EqualValues(t, 0, usage.CacheCreation.Ephemeral1hInputTokens)
	})
}

func TestNormalizeCursorProxyClaudeUsageCapsOnlyAbnormalHiddenPrompt(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://cpa.muxpay.xyz"},
	}
	info.SetEstimatePromptTokens(1000)
	usage := &dto.ClaudeUsage{
		InputTokens:              27098,
		CacheReadInputTokens:     26905,
		CacheCreationInputTokens: 431,
		OutputTokens:             17,
		CacheCreation:            &dto.ClaudeCacheCreationUsage{Ephemeral5mInputTokens: 27096},
	}

	changed := normalizeCursorProxyClaudeUsage(info, usage)

	require.True(t, changed)
	require.Equal(t, 1000, usage.InputTokens)
	require.Equal(t, 1000, usage.CacheReadInputTokens)
	require.Equal(t, 431, usage.CacheCreationInputTokens)
	require.Equal(t, 17, usage.OutputTokens)
	require.NotNil(t, usage.CacheCreation)
	require.Equal(t, 1000, usage.CacheCreation.Ephemeral5mInputTokens)
}

func TestNormalizeCursorProxyClaudeUsageLeavesNormalAnthropicCache(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://api.anthropic.com"},
	}
	info.SetEstimatePromptTokens(1000)
	usage := &dto.ClaudeUsage{
		InputTokens:              1200,
		CacheReadInputTokens:     900,
		CacheCreationInputTokens: 100,
	}

	changed := normalizeCursorProxyClaudeUsage(info, usage)

	require.False(t, changed)
	require.Equal(t, 1200, usage.InputTokens)
	require.Equal(t, 900, usage.CacheReadInputTokens)
	require.Equal(t, 100, usage.CacheCreationInputTokens)
}

func TestPatchNativeClaudeUsageDataOverwritesAbnormalUsage(t *testing.T) {
	data := `{"type":"message_delta","usage":{"input_tokens":27098,"cache_read_input_tokens":26905,"cache_creation_input_tokens":431,"output_tokens":17}}`
	response := &dto.ClaudeResponse{
		Type: "message_delta",
		Usage: &dto.ClaudeUsage{
			InputTokens:              1000,
			CacheReadInputTokens:     1000,
			CacheCreationInputTokens: 431,
			OutputTokens:             17,
		},
	}

	patched := patchNativeClaudeUsageData(data, response)

	require.EqualValues(t, 1000, gjson.Get(patched, "usage.input_tokens").Int())
	require.EqualValues(t, 1000, gjson.Get(patched, "usage.cache_read_input_tokens").Int())
	require.EqualValues(t, 431, gjson.Get(patched, "usage.cache_creation_input_tokens").Int())
	require.EqualValues(t, 17, gjson.Get(patched, "usage.output_tokens").Int())
}
