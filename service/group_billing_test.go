package service

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupPerCallBillingReservesAndSettlesFixedCharge(t *testing.T) {
	billing := &recordingBillingSettler{preConsumedQuota: 300}
	info := &relaycommon.RelayInfo{
		Billing:         billing,
		OriginModelName: "gemini-4-argon",
		PriceData:       types.PriceData{UsePrice: true, ModelPrice: 1, QuotaToPreConsume: 35000, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.07}},
	}
	ctx, _ := gin.CreateTestContext(nil)
	require.Nil(t, PrepareBillingForSelectedGroup(ctx, info))
	assert.Equal(t, 35000, info.FinalPreConsumedQuota)
	assert.Equal(t, []int{35000}, billing.reserveTargets)
	for _, usage := range []*dto.Usage{{PromptTokens: 6, CompletionTokens: 5}, {PromptTokens: 10000, CompletionTokens: 5000}} {
		assert.Equal(t, 35000, calculateTextQuotaSummary(ctx, info, usage).Quota)
	}
	info.PriceData.FreeModel = true
	info.PriceData.ModelPrice = 0
	info.PriceData.QuotaToPreConsume = 0
	require.Nil(t, PrepareBillingForSelectedGroup(ctx, info))
	assert.False(t, info.PriceData.FreeModel, "retain settlement/refund for the existing reservation")
	assert.Equal(t, 35000, info.FinalPreConsumedQuota)
}

func TestGroupPerCallBillingStartsAfterFreeGroupAndRejectsInsufficientQuota(t *testing.T) {
	for _, balance := range []int{100000, 1000} {
		t.Run("balance="+strconv.Itoa(balance), func(t *testing.T) {
			truncate(t)
			const userID = 740
			seedUser(t, userID, balance)
			info := &relaycommon.RelayInfo{
				UserId: userID, IsPlayground: true, ForcePreConsume: true,
				OriginModelName: "gemini-4-argon",
				UserSetting:     dto.UserSetting{BillingPreference: "wallet_only"},
				PriceData:       types.PriceData{FreeModel: true, UsePrice: true, ModelPrice: 1, QuotaToPreConsume: 35000, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.07}},
			}
			ctx, _ := gin.CreateTestContext(nil)
			err := PrepareBillingForSelectedGroup(ctx, info)
			if balance < 35000 {
				require.NotNil(t, err)
				assert.Nil(t, info.Billing)
			} else {
				require.Nil(t, err)
				require.NotNil(t, info.Billing)
				assert.Equal(t, 35000, info.FinalPreConsumedQuota)
				balance -= 35000
			}
			quota, quotaErr := model.GetUserQuota(userID, false)
			require.NoError(t, quotaErr)
			assert.Equal(t, balance, quota)
		})
	}
}
