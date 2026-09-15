package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestChannelSelectionExcludesChannelsAlreadyTriedByRequest(t *testing.T) {
	channelIDs := []int{980021, 980022, 980023}
	modelName := fmt.Sprintf("channel-retry-exclusion-%d", channelIDs[0])
	priority := int64(0)

	for _, channelID := range channelIDs {
		channel := &Channel{
			Id:       channelID,
			Type:     constant.ChannelTypeOpenAI,
			Name:     fmt.Sprintf("retry-exclusion-%d", channelID),
			Status:   common.ChannelStatusEnabled,
			Group:    "default",
			Models:   modelName,
			Priority: &priority,
		}
		require.NoError(t, DB.Create(channel).Error)
		require.NoError(t, DB.Create(&Ability{
			Group:     "default",
			Model:     modelName,
			ChannelId: channelID,
			Enabled:   true,
			Priority:  &priority,
		}).Error)
	}

	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	t.Cleanup(func() {
		require.NoError(t, DB.Where("model = ?", modelName).Delete(&Ability{}).Error)
		require.NoError(t, DB.Where("id IN ?", channelIDs).Delete(&Channel{}).Error)
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		InitChannelCache()
	})

	for _, memoryCacheEnabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("memory-cache-%t", memoryCacheEnabled), func(t *testing.T) {
			common.MemoryCacheEnabled = memoryCacheEnabled
			InitChannelCache()

			first, err := GetRandomSatisfiedChannel("default", modelName, 0, "")
			require.NoError(t, err)
			require.NotNil(t, first)

			second, err := GetRandomSatisfiedChannel("default", modelName, 1, "", first.Id)
			require.NoError(t, err)
			require.NotNil(t, second)
			require.NotEqual(t, first.Id, second.Id)

			third, err := GetRandomSatisfiedChannel("default", modelName, 2, "", first.Id, second.Id)
			require.NoError(t, err)
			require.NotNil(t, third)
			require.NotContains(t, []int{first.Id, second.Id}, third.Id)
		})
	}
}
