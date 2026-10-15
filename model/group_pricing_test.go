package model

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPricingSameModelHasGroupSpecificBilling(t *testing.T) {
	resetPricingEndpointTestTables(t)
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(k, v string) error { saved[k] = v; return nil }))
	savedRatios := ratio_setting.ModelRatio2JSONString()
	savedPrices := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedRatios))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedPrices))
		InvalidatePricingCache()
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gemini-4-argon":1}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.group_model_price": `{"call":{"gemini-4-argon":1},"free":{"gemini-4-argon":0}}`,
		"group_ratio_setting.group_ratio":   `{"usage":1,"call":1,"free":1}`,
	}))
	insertPricingEndpointChannel(t, 101, constant.ChannelTypeGemini, dto.ChannelOtherSettings{})
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", 101).Updates(map[string]interface{}{
		"group": "usage,call,free", "models": "gemini-4-argon",
	}).Error)
	for _, group := range []string{"usage", "call", "free"} {
		require.NoError(t, DB.Create(&Ability{Group: group, Model: "gemini-4-argon", ChannelId: 101, Enabled: true}).Error)
	}
	InitChannelCache()
	rows := GetPricing()
	require.Len(t, rows, 3)
	byGroup := map[string]Pricing{}
	for _, row := range rows {
		assert.Equal(t, "gemini-4-argon", row.ModelName)
		require.Len(t, row.EnableGroup, 1)
		byGroup[row.EnableGroup[0]] = row
	}
	assert.Equal(t, 0, byGroup["usage"].QuotaType)
	assert.Equal(t, 1.0, byGroup["usage"].ModelRatio)
	assert.Equal(t, 1, byGroup["call"].QuotaType)
	assert.Equal(t, 1.0, byGroup["call"].ModelPrice)
	assert.Nil(t, byGroup["call"].CacheRatio)
	assert.Empty(t, byGroup["call"].BillingExpr)
	assert.Equal(t, 1, byGroup["free"].QuotaType)
	assert.Zero(t, byGroup["free"].ModelPrice)
	assert.ElementsMatch(t, []string{"usage", "call", "free"}, GetModelEnableGroups("gemini-4-argon"))
	assert.ElementsMatch(t, []int{0, 1}, GetModelQuotaTypes("gemini-4-argon"))
}
