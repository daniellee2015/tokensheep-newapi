package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/tokensheep_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssignableUserGroupsIncludeCustomRulesWithoutExposingRoutingGroups(t *testing.T) {
	previousEconomy := tokensheep_setting.EconomySetting2JSONString()
	previousRatios := ratio_setting.GroupGroupRatio2JSONString()
	previousGroups := ratio_setting.GroupRatio2JSONString()
	specialUsable := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup
	previousUsable := specialUsable.MarshalJSONString()
	t.Cleanup(func() {
		require.NoError(t, tokensheep_setting.UpdateEconomySettingByJSONString(`{"tier_thresholds":null,"commercial_groups":null}`))
		require.NoError(t, tokensheep_setting.UpdateEconomySettingByJSONString(previousEconomy))
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(previousRatios))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroups))
		require.NoError(t, types.LoadFromJsonString(specialUsable, previousUsable))
	})
	require.NoError(t, tokensheep_setting.UpdateEconomySettingByJSONString(`{"tier_thresholds":null,"commercial_groups":null}`))
	require.NoError(t, tokensheep_setting.UpdateEconomySettingByJSONString(`{"tier_thresholds":{"vip":100},"commercial_groups":{"wholesale-plus":true,"disabled-commercial":false}}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"aws-q":1,"gemini-ultra":2,"channel-only":1}`))
	require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{"vip":{"gemini-ultra":1},"auto":{},"":{},"wholesale-ultra":{"aws-q":0.35,"gemini-ultra":0.35,"gemini-lowprice":0.12},"empty-rules":{}}`))
	require.NoError(t, types.LoadFromJsonString(specialUsable, `{"private-customer":{"+:gemini-ultra":"Gemini"},"wholesale-ultra":{}}`))

	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name    string
		handler gin.HandlerFunc
		want    []string
	}{
		{"user editor", GetAssignableUserGroups, []string{"free", "empty-rules", "private-customer", "vip", "wholesale-plus", "wholesale-ultra"}},
		{"subscription tier catalog", GetTierList, []string{"free", "vip"}},
		{"channel catalog", GetGroups, []string{"aws-q", "gemini-ultra", "channel-only"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			tc.handler(ctx)
			var body struct {
				Success bool     `json:"success"`
				Data    []string `json:"data"`
			}
			require.Equal(t, http.StatusOK, recorder.Code)
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
			require.True(t, body.Success)
			assert.ElementsMatch(t, tc.want, body.Data)
		})
	}
}
