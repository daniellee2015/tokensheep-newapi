package relay

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestShouldPreserveMappedEffortVariantForCPA(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	info.IsModelMapped = true
	info.ChannelBaseUrl = "https://cpa.muxpay.xyz"

	require.True(t, shouldPreserveMappedEffortVariant(info))
}

func TestShouldNotPreserveUnmappedEffortVariant(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	info.IsModelMapped = false
	info.ChannelBaseUrl = "https://api.anthropic.com"

	require.False(t, shouldPreserveMappedEffortVariant(info))
}
