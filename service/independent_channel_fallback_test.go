package service

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

func TestIndependentFallbackKeepsPrimariesBeforeReserveAndEnforcesEligibility(t *testing.T) {
	for _, memory := range []bool{false, true} {
		t.Run(fmt.Sprintf("memory=%v", memory), func(t *testing.T) {
			db := setupChannelSelectAutoGroupsTest(t)
			common.MemoryCacheEnabled = memory
			const name = "gemini-reserve-test"
			for _, id := range []int{12, 140, 146, 999} {
				createChannelSelectAutoGroupsChannel(t, db, id, "default", name)
				priority := 10
				if id == 146 {
					priority = 0
				}
				if id == 999 {
					priority = 20
				}
				require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", id).Update("priority", priority).Error)
				require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", id).Update("priority", priority).Error)
			}
			model.InitChannelCache()
			policy := &operation_setting.IndependentChannelFallback{Enabled: true, PrimaryChannelIDs: []int{12, 140}, ReserveChannelID: 146}
			for _, first := range []int{12, 140} {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				p := &RetryParam{Ctx: c, TokenGroup: "default", ModelName: name, IndependentFallback: policy}
				p.ExcludeChannel(first)
				second, group, err := CacheGetRandomSatisfiedChannel(p)
				require.NoError(t, err)
				require.NotNil(t, second)
				assert.Equal(t, "default", group)
				assert.Equal(t, 152-first, second.Id)
				p.ExcludeChannel(second.Id)
				reserve, _, err := CacheGetRandomSatisfiedChannel(p)
				require.NoError(t, err)
				require.NotNil(t, reserve)
				assert.Equal(t, 146, reserve.Id)
				p.ExcludeChannel(reserve.Id)
				exhausted, _, err := CacheGetRandomSatisfiedChannel(p)
				require.NoError(t, err)
				assert.Nil(t, exhausted)
			}
			// A reserve outside the current group/model must never be forced in.
			require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", 146).Update("enabled", false).Error)
			require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 146).Update("status", common.ChannelStatusManuallyDisabled).Error)
			model.InitChannelCache()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			p := &RetryParam{Ctx: c, TokenGroup: "default", ModelName: name, IndependentFallback: policy}
			p.ExcludeChannel(12)
			p.ExcludeChannel(140)
			missing, _, err := CacheGetRandomSatisfiedChannel(p)
			require.NoError(t, err)
			assert.Nil(t, missing)
		})
	}
}
