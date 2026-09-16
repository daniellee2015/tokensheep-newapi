package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelSelectionStopsAfterAllPrioritiesAreTried(t *testing.T) {
	const (
		primaryID  = 980001
		fallbackID = 980002
	)
	modelName := fmt.Sprintf("channel-priority-exhaustion-%d", primaryID)
	primaryPriority := int64(0)
	fallbackPriority := int64(-1)

	for _, channel := range []*Channel{
		{
			Id:       primaryID,
			Type:     constant.ChannelTypeOpenAI,
			Name:     "priority-exhaustion-primary",
			Status:   common.ChannelStatusEnabled,
			Group:    "default",
			Models:   modelName,
			Priority: &primaryPriority,
		},
		{
			Id:       fallbackID,
			Type:     constant.ChannelTypeOpenAI,
			Name:     "priority-exhaustion-fallback",
			Status:   common.ChannelStatusEnabled,
			Group:    "default",
			Models:   modelName,
			Priority: &fallbackPriority,
		},
	} {
		require.NoError(t, DB.Create(channel).Error)
	}
	for channelID, priority := range map[int]*int64{
		primaryID:  &primaryPriority,
		fallbackID: &fallbackPriority,
	} {
		require.NoError(t, DB.Create(&Ability{
			Group:     "default",
			Model:     modelName,
			ChannelId: channelID,
			Enabled:   true,
			Priority:  priority,
		}).Error)
	}

	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	t.Cleanup(func() {
		require.NoError(t, DB.Where("model = ?", modelName).Delete(&Ability{}).Error)
		require.NoError(t, DB.Where("id IN ?", []int{primaryID, fallbackID}).Delete(&Channel{}).Error)
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		InitChannelCache()
	})

	for _, memoryCacheEnabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("memory-cache-%t", memoryCacheEnabled), func(t *testing.T) {
			common.MemoryCacheEnabled = memoryCacheEnabled
			InitChannelCache()

			primary, err := GetRandomSatisfiedChannel("default", modelName, 0, "")
			require.NoError(t, err)
			require.NotNil(t, primary)
			assert.Equal(t, primaryID, primary.Id)

			fallback, err := GetRandomSatisfiedChannel("default", modelName, 1, "")
			require.NoError(t, err)
			require.NotNil(t, fallback)
			assert.Equal(t, fallbackID, fallback.Id)

			exhausted, err := GetRandomSatisfiedChannel("default", modelName, 2, "")
			require.NoError(t, err)
			assert.Nil(t, exhausted)
		})
	}
}

func TestChannelSelectionRetriesMultipleChannelsAtLowestPriority(t *testing.T) {
	const primaryID = 980011
	const fallbackStartID = 980012
	modelName := fmt.Sprintf("channel-lowest-pool-%d", primaryID)
	primaryPriority := int64(0)
	fallbackPriority := int64(-1)

	for _, channel := range []*Channel{
		{Id: primaryID, Type: constant.ChannelTypeOpenAI, Name: "lowest-pool-primary", Status: common.ChannelStatusEnabled, Group: "default", Models: modelName, Priority: &primaryPriority},
		{Id: fallbackStartID, Type: constant.ChannelTypeOpenAI, Name: "lowest-pool-fallback-a", Status: common.ChannelStatusEnabled, Group: "default", Models: modelName, Priority: &fallbackPriority},
		{Id: fallbackStartID + 1, Type: constant.ChannelTypeOpenAI, Name: "lowest-pool-fallback-b", Status: common.ChannelStatusEnabled, Group: "default", Models: modelName, Priority: &fallbackPriority},
	} {
		require.NoError(t, DB.Create(channel).Error)
		require.NoError(t, DB.Create(&Ability{Group: "default", Model: modelName, ChannelId: channel.Id, Enabled: true, Priority: channel.Priority}).Error)
	}
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	t.Cleanup(func() {
		require.NoError(t, DB.Where("model = ?", modelName).Delete(&Ability{}).Error)
		require.NoError(t, DB.Where("id IN ?", []int{primaryID, fallbackStartID, fallbackStartID + 1}).Delete(&Channel{}).Error)
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		InitChannelCache()
	})

	for _, memoryCacheEnabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("memory-cache-%t", memoryCacheEnabled), func(t *testing.T) {
			common.MemoryCacheEnabled = memoryCacheEnabled
			InitChannelCache()
			fallback, err := GetRandomSatisfiedChannel("default", modelName, 2, "")
			require.NoError(t, err)
			require.NotNil(t, fallback)
			assert.Contains(t, []int{fallbackStartID, fallbackStartID + 1}, fallback.Id)
		})
	}
}

func TestChannelSelectionExhaustsUntriedChannelsBeforeLowerPriority(t *testing.T) {
	const (
		primaryID       = 980021
		fallbackStartID = 980022
		lastFallbackID  = 980024
	)
	modelName := fmt.Sprintf("channel-priority-order-%d", primaryID)
	primaryPriority := int64(0)
	fallbackPriority := int64(-1)
	lastPriority := int64(-2)
	channels := []*Channel{
		{Id: primaryID, Type: constant.ChannelTypeOpenAI, Name: "priority-order-primary", Status: common.ChannelStatusEnabled, Group: "default", Models: modelName, Priority: &primaryPriority},
		{Id: fallbackStartID, Type: constant.ChannelTypeOpenAI, Name: "priority-order-fallback-a", Status: common.ChannelStatusEnabled, Group: "default", Models: modelName, Priority: &fallbackPriority},
		{Id: fallbackStartID + 1, Type: constant.ChannelTypeOpenAI, Name: "priority-order-fallback-b", Status: common.ChannelStatusEnabled, Group: "default", Models: modelName, Priority: &fallbackPriority},
		{Id: lastFallbackID, Type: constant.ChannelTypeOpenAI, Name: "priority-order-last", Status: common.ChannelStatusEnabled, Group: "default", Models: modelName, Priority: &lastPriority},
	}
	for _, channel := range channels {
		require.NoError(t, DB.Create(channel).Error)
		require.NoError(t, DB.Create(&Ability{Group: "default", Model: modelName, ChannelId: channel.Id, Enabled: true, Priority: channel.Priority}).Error)
	}

	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	t.Cleanup(func() {
		require.NoError(t, DB.Where("model = ?", modelName).Delete(&Ability{}).Error)
		require.NoError(t, DB.Where("id IN ?", []int{primaryID, fallbackStartID, fallbackStartID + 1, lastFallbackID}).Delete(&Channel{}).Error)
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		InitChannelCache()
	})

	for _, memoryCacheEnabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("memory-cache-%t", memoryCacheEnabled), func(t *testing.T) {
			common.MemoryCacheEnabled = memoryCacheEnabled
			InitChannelCache()

			primary, err := GetRandomSatisfiedChannel("default", modelName, 0, "")
			require.NoError(t, err)
			require.Equal(t, primaryID, primary.Id)

			firstFallback, err := GetRandomSatisfiedChannel("default", modelName, 1, "", primaryID)
			require.NoError(t, err)
			require.Contains(t, []int{fallbackStartID, fallbackStartID + 1}, firstFallback.Id)

			secondFallback, err := GetRandomSatisfiedChannel("default", modelName, 2, "", primaryID, firstFallback.Id)
			require.NoError(t, err)
			require.Contains(t, []int{fallbackStartID, fallbackStartID + 1}, secondFallback.Id)
			require.NotEqual(t, firstFallback.Id, secondFallback.Id)

			lastFallback, err := GetRandomSatisfiedChannel("default", modelName, 3, "", primaryID, firstFallback.Id, secondFallback.Id)
			require.NoError(t, err)
			require.Equal(t, lastFallbackID, lastFallback.Id)
		})
	}
}

func TestChannelSelectionKeepsSinglePrimaryRetryableForUpstreamPool(t *testing.T) {
	const channelID = 980031
	modelName := fmt.Sprintf("channel-single-primary-%d", channelID)
	priority := int64(0)
	channel := &Channel{
		Id:       channelID,
		Type:     constant.ChannelTypeGemini,
		Name:     "single-primary",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   modelName,
		Priority: &priority,
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group: "default", Model: modelName, ChannelId: channelID,
		Enabled: true, Priority: &priority,
	}).Error)

	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	t.Cleanup(func() {
		require.NoError(t, DB.Where("model = ?", modelName).Delete(&Ability{}).Error)
		require.NoError(t, DB.Where("id = ?", channelID).Delete(&Channel{}).Error)
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
			require.Equal(t, channelID, first.Id)

			// Retry 1 is intentionally still the same channel. CPA uses this
			// request to select another credential from its internal pool.
			retry, err := GetRandomSatisfiedChannel("default", modelName, 1, "", first.Id)
			require.NoError(t, err)
			require.NotNil(t, retry)
			require.Equal(t, channelID, retry.Id)

			// The sole channel remains retryable even when recorded as tried; the
			// outer retry limit, rather than channel exclusion, bounds this path.
			excluded, err := GetRandomSatisfiedChannel("default", modelName, 2, "", channelID)
			require.NoError(t, err)
			require.NotNil(t, excluded)
			require.Equal(t, channelID, excluded.Id)
		})
	}
}

func TestChannelSelectionDoesNotRepeatSingleNonPoolChannel(t *testing.T) {
	const channelID = 980041
	modelName := fmt.Sprintf("channel-single-non-pool-%d", channelID)
	priority := int64(0)
	channel := &Channel{
		Id:       channelID,
		Type:     constant.ChannelTypeOpenAI,
		Name:     "single-non-pool",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   modelName,
		Priority: &priority,
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group: "default", Model: modelName, ChannelId: channelID,
		Enabled: true, Priority: &priority,
	}).Error)

	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	t.Cleanup(func() {
		require.NoError(t, DB.Where("model = ?", modelName).Delete(&Ability{}).Error)
		require.NoError(t, DB.Where("id = ?", channelID).Delete(&Channel{}).Error)
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
			require.Equal(t, channelID, first.Id)

			retry, err := GetRandomSatisfiedChannel("default", modelName, 1, "", channelID)
			require.NoError(t, err)
			require.Nil(t, retry)
		})
	}
}
