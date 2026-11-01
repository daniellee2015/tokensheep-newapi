package helper

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelPriceHelperSameModelDifferentGroupBilling(t *testing.T) {
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(k, v string) error { saved[k] = v; return nil }))
	savedRatios := ratio_setting.ModelRatio2JSONString()
	savedPrices := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedRatios))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedPrices))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gemini-4-argon":1}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.group_model_price": `{"call":{"gemini-4-argon":1},"free":{"gemini-4-argon":0},"overflow":{"gemini-4-argon":1e20}}`,
		"group_ratio_setting.group_ratio":   `{"usage":0.3,"call":0.07,"free":1,"overflow":1}`,
	}))
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{OriginModelName: "gemini-4-argon", UserGroup: "retail", UsingGroup: "usage"}
	meta := &types.TokenCountMeta{}
	usage, err := ModelPriceHelper(ctx, info, 1000, meta)
	require.NoError(t, err)
	assert.False(t, usage.UsePrice)
	assert.Equal(t, 0.3, usage.GroupRatioInfo.GroupRatio)

	// Automatic routing must resolve the group before looking up its price.
	ctx.Set("auto_group", "call")
	call, err := ModelPriceHelper(ctx, info, 1000, meta)
	require.NoError(t, err)
	assert.True(t, call.UsePrice)
	assert.Equal(t, 1.0, call.ModelPrice)
	assert.Equal(t, 35000, call.QuotaToPreConsume)
	assert.Equal(t, "gemini-4-argon", info.OriginModelName)
	task, err := ModelPriceHelperPerCall(ctx, info)
	require.NoError(t, err)
	assert.True(t, task.UsePrice)
	assert.Equal(t, 35000, task.Quota)
	ctx.Set("auto_group", "usage")
	info.PriceData.GroupRatioInfo = HandleGroupRatio(ctx, info)
	require.NoError(t, RefreshModelPriceForGroup(ctx, info, "call", 1000, meta))
	assert.False(t, info.PriceData.UsePrice, "retry to the usage group must resume token billing")
	assert.Equal(t, 0.3, info.PriceData.GroupRatioInfo.GroupRatio)
	ctx.Set("auto_group", "call")
	info.PriceData.GroupRatioInfo = HandleGroupRatio(ctx, info)
	require.NoError(t, RefreshModelPriceForGroup(ctx, info, "usage", 1000, meta))
	assert.True(t, info.PriceData.UsePrice)
	assert.Equal(t, 35000, info.PriceData.QuotaToPreConsume)

	// A per-call attempt must suspend expression settlement. Returning to an
	// expression group restores the captured contract, even if settings changed.
	snapshot := &billingexpr.BillingSnapshot{BillingMode: "tiered_expr", ExprString: `tier("base", p * 2)`, EstimatedQuotaBeforeGroup: 1000}
	info.TieredBillingSnapshot = snapshot
	require.NoError(t, RefreshModelPriceForGroup(ctx, info, "usage", 1000, meta))
	assert.Nil(t, info.TieredBillingSnapshot)
	ctx.Set("auto_group", "usage")
	info.PriceData.GroupRatioInfo = HandleGroupRatio(ctx, info)
	require.NoError(t, RefreshModelPriceForGroup(ctx, info, "call", 1000, meta))
	assert.Same(t, snapshot, info.TieredBillingSnapshot)
	assert.False(t, info.PriceData.UsePrice)

	ctx.Set("auto_group", "free")
	free, err := ModelPriceHelper(ctx, info, 1000, meta)
	require.NoError(t, err)
	assert.True(t, free.UsePrice, "explicit zero must override token pricing")
	assert.Zero(t, free.QuotaToPreConsume)

	ctx.Set("auto_group", "overflow")
	_, err = ModelPriceHelper(ctx, info, 1000, meta)
	var clamp *common.QuotaClamp
	require.ErrorAs(t, err, &clamp)
}
