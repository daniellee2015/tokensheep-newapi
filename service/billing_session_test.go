// Copyright (C) 2023-2026 QuantumNous
//
// Licensed under the GNU Affero General Public License, version 3 or later.
// See LICENSE / NOTICE for the full text.
package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/tokensheep_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resolveBillingPreferenceForCommercial 是 R16-1 修法 B 的核心 helper。
// 这里从纯函数层完整覆盖 hazard 文档 §六 里五个决策点：
//   A. 非商业分组不 coerce (v4 §3.1 原语义不变)
//   B. 商业分组 (retail/wholesale/wholesale-plus) + subscription_only 被 coerce
//   C. 商业分组 + subscription_first 也被 coerce (对称处理)
//   D. 商业分组 + wallet_first / wallet_only 保持不变
//   E. UserGroup 空字符串 (context 未设 / cache miss) 走 fail-open
//
// 这些用例只验证 helper 的 coerce 语义；coerce 之后 dispatch 选资金来源的行为
// 由文件末尾的 TestNewBillingSession_CommercialFundingSource 走 sqlite 端到端覆盖。

func withSavedEconomySetting(t *testing.T) {
	t.Helper()
	previous := tokensheep_setting.EconomySetting2JSONString()
	t.Cleanup(func() {
		if err := tokensheep_setting.UpdateEconomySettingByJSONString(previous); err != nil {
			t.Logf("failed to restore economy setting: %v", err)
		}
	})
}

// stubContext 提供 gin.Context 不为 nil 的最小骨架，让 logger.LogInfo 分支得到
// 覆盖 (helper 里 ctx==nil 走 common.SysLog，ctx!=nil 走 logger.LogInfo)。
func stubContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	return c
}

// Test A: 非商业分组用户 pref=subscription_only 保持不变，dispatch 会走 trySubscription。
// v4 §3.1 明确 subscription_only 对个人用户是硬性要求，不能被 hazard 修法误伤。
func TestResolveBillingPreference_NonCommercialSubscriptionOnly_Unchanged(t *testing.T) {
	withSavedEconomySetting(t)
	c := stubContext()

	got := resolveBillingPreferenceForCommercial(c, 42, "free", "subscription_only")
	require.Equal(t, "subscription_only", got, "non-commercial group must not be coerced")

	// bestie 是贡献档，subscription_only 是 v4 语义里合法且预期的选择
	got = resolveBillingPreferenceForCommercial(c, 42, "bestie", "subscription_only")
	require.Equal(t, "subscription_only", got)
}

// Test B: 商业分组 (三档全覆盖) + subscription_only 被 coerce 成 wallet_first。
// hazard 文档 §一：这是唯一会产生 403 hard-block 的路径，也是修法 B 主要止血目标。
func TestResolveBillingPreference_CommercialSubscriptionOnly_CoercedToWalletFirst(t *testing.T) {
	withSavedEconomySetting(t)
	c := stubContext()

	commercialGroups := []string{"retail", "wholesale", "wholesale-plus"}
	for _, g := range commercialGroups {
		got := resolveBillingPreferenceForCommercial(c, 100, g, "subscription_only")
		require.Equalf(t, "wallet_first", got,
			"commercial group %q with subscription_only should be coerced to wallet_first", g)
	}
}

// Test C: 商业分组 + subscription_first 也被 coerce。虽然现网 dispatch 里
// subscription_first 分支有 HasActiveUserSubscription 兜底不会产生 403，但为了
// 保持前端显示层 (billing-preference-card.tsx isSubscriptionOption) 的对称处理，
// dispatch 层同步 coerce。避免用户 UI 显示"优先钱包"、后端却走 subscription_first
// 分支进 HasActiveUserSubscription 查询这类"不必要的 DB 调用"。
func TestResolveBillingPreference_CommercialSubscriptionFirst_CoercedToWalletFirst(t *testing.T) {
	withSavedEconomySetting(t)
	c := stubContext()

	got := resolveBillingPreferenceForCommercial(c, 100, "retail", "subscription_first")
	require.Equal(t, "wallet_first", got)

	got = resolveBillingPreferenceForCommercial(c, 100, "wholesale", "subscription_first")
	require.Equal(t, "wallet_first", got)
}

// Test D: 商业分组 + wallet_first / wallet_only 保持不变。coerce 只针对
// subscription_* 输入，不做多余改动，保证幂等 (第二次跑 helper 结果一致)。
func TestResolveBillingPreference_CommercialWalletVariants_Unchanged(t *testing.T) {
	withSavedEconomySetting(t)
	c := stubContext()

	got := resolveBillingPreferenceForCommercial(c, 100, "retail", "wallet_first")
	require.Equal(t, "wallet_first", got)

	got = resolveBillingPreferenceForCommercial(c, 100, "wholesale-plus", "wallet_only")
	require.Equal(t, "wallet_only", got)

	// 幂等：coerce 结果再喂一次应该稳定
	coerced := resolveBillingPreferenceForCommercial(c, 100, "retail", "subscription_only")
	twice := resolveBillingPreferenceForCommercial(c, 100, "retail", coerced)
	require.Equal(t, coerced, twice, "coerce must be idempotent")
}

// Test E: UserGroup 空字符串代表 fail-open — context 未设 / cache miss /
// 上游 middleware 未填 relayInfo.UserGroup 时，商业身份判断走空，helper 不 coerce。
// hazard §六 Q2: 宁可退回原 R16-1 行为 (可能 403)，也不能因为 group lookup 缺失
// 就把普通用户的 subscription_only 全部改成 wallet_first。
func TestResolveBillingPreference_EmptyGroup_FailOpen(t *testing.T) {
	withSavedEconomySetting(t)
	c := stubContext()

	got := resolveBillingPreferenceForCommercial(c, 100, "", "subscription_only")
	require.Equal(t, "subscription_only", got, "empty group must fail-open to original pref")

	got = resolveBillingPreferenceForCommercial(c, 100, "", "subscription_first")
	require.Equal(t, "subscription_first", got)
}

// Test F: helper 从 EconomySetting.CommercialGroups 读取，operator 通过 admin
// 面板临时把某个组从商业集合移出后，coerce 立刻停止对该组生效。这条测试锁定
// R16-2 admin UI 可编辑性和 dispatch 之间的耦合：CommercialGroups 是运行时数据，
// 不是 hard-code 常量。
func TestResolveBillingPreference_ObeysRuntimeCommercialGroupsChange(t *testing.T) {
	withSavedEconomySetting(t)
	c := stubContext()

	// 基线：retail 命中 coerce
	got := resolveBillingPreferenceForCommercial(c, 100, "retail", "subscription_only")
	require.Equal(t, "wallet_first", got)

	// admin 把 retail 移出 commercial_groups 集合。UpdateEconomySettingByJSONString
	// 的 JSON unmarshal 对 map 是 merge 而非 replace (encoding/json 语义)，所以
	// 移除必须显式写 "retail":false 而不是省略 key。这条也是 admin UI 保存商业
	// 组变更时的合约。
	require.NoError(t, tokensheep_setting.UpdateEconomySettingByJSONString(
		`{"commercial_groups":{"retail":false,"wholesale":true,"wholesale-plus":true}}`))

	got = resolveBillingPreferenceForCommercial(c, 100, "retail", "subscription_only")
	require.Equal(t, "subscription_only", got,
		"once retail is removed from commercial_groups, coerce must stop")

	// wholesale 仍在集合里，仍然 coerce
	got = resolveBillingPreferenceForCommercial(c, 100, "wholesale", "subscription_only")
	require.Equal(t, "wallet_first", got)
}

// Test G: gin.Context == nil 走 common.SysLog 分支，helper 不 panic。
// 保护 helper 未来若被非 request path 调用 (例如 cron 里 diagnostic 检查) 时的稳定性。
func TestResolveBillingPreference_NilContext_UsesSysLog(t *testing.T) {
	withSavedEconomySetting(t)

	got := resolveBillingPreferenceForCommercial(nil, 100, "retail", "subscription_only")
	require.Equal(t, "wallet_first", got)
}

// 商业分组被 coerce 成 wallet_first 后，资金来源必须是钱包：钱包有余额时按钱包
// 预扣并结算；钱包不足时返回钱包额度不足，不能因为回退到不存在的订阅而报
// "订阅额度不足或未配置订阅"。生产中商业分组用户（DB 残留 subscription_first）
// 钱包欠费期间每个请求都拿到订阅错误，真实原因被掩盖。
// 最后一行是对照：真实持有活跃订阅的 wallet_first 用户仍保留钱包 -> 订阅回退。
func TestNewBillingSession_CommercialFundingSource(t *testing.T) {
	const preConsume = 7_008

	tests := []struct {
		name            string
		group           string
		preference      string
		walletQuota     int
		withActiveSub   bool
		settleQuota     int
		wantSource      string
		wantErrContains string
		wantWallet      int
	}{
		{
			name:        "commercial stale subscription_first with funded wallet bills wallet",
			group:       "wholesale-plus",
			preference:  "subscription_first",
			walletQuota: 1_000_000,
			settleQuota: 10_000,
			wantSource:  BillingSourceWallet,
			wantWallet:  990_000,
		},
		{
			name:            "commercial stale subscription_first with wallet in arrears reports wallet shortfall",
			group:           "wholesale-plus",
			preference:      "subscription_first",
			walletQuota:     -71_852,
			wantErrContains: "用户额度不足",
			wantWallet:      -71_852,
		},
		{
			name:            "commercial stale subscription_only with wallet below pre-consume reports wallet shortfall",
			group:           "retail",
			preference:      "subscription_only",
			walletQuota:     1_000,
			wantErrContains: "预扣费额度失败",
			wantWallet:      1_000,
		},
		{
			name:          "non-commercial wallet_first with active subscription still falls back to subscription",
			group:         "fan",
			preference:    "wallet_first",
			walletQuota:   0,
			withActiveSub: true,
			wantSource:    BillingSourceSubscription,
			wantWallet:    0,
		},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			truncate(t)
			withSavedEconomySetting(t)

			userID := 9_100 + i
			require.NoError(t, model.DB.Create(&model.User{
				Id:       userID,
				Username: "billing_session_user",
				Group:    tc.group,
				Quota:    tc.walletQuota,
				Status:   common.UserStatusEnabled,
			}).Error)
			if tc.withActiveSub {
				planID := 9_100 + i
				require.NoError(t, model.DB.Create(&model.SubscriptionPlan{Id: planID, Title: "test plan", TotalAmount: 1_000_000}).Error)
				require.NoError(t, model.DB.Create(&model.UserSubscription{
					UserId:      userID,
					PlanId:      planID,
					AmountTotal: 1_000_000,
					Status:      "active",
					StartTime:   time.Now().Unix(),
					EndTime:     time.Now().Add(24 * time.Hour).Unix(),
				}).Error)
			}

			relayInfo := &relaycommon.RelayInfo{
				UserId:          userID,
				UserGroup:       tc.group,
				RequestId:       "billing-session-commercial-" + tc.group + "-" + tc.preference,
				OriginModelName: "gpt-test",
				IsPlayground:    true,
				UserSetting:     dto.UserSetting{BillingPreference: tc.preference},
			}

			session, apiErr := NewBillingSession(stubContext(), relayInfo, preConsume)

			if tc.wantErrContains != "" {
				require.NotNil(t, apiErr)
				assert.Nil(t, session)
				assert.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
				assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
				assert.Contains(t, apiErr.Error(), tc.wantErrContains)
				assert.NotContains(t, apiErr.Error(), "订阅")
				assert.NotContains(t, apiErr.Error(), "subscription")
			} else {
				require.Nil(t, apiErr)
				require.NotNil(t, session)
				assert.Equal(t, tc.wantSource, session.funding.Source())
				assert.Equal(t, tc.wantSource, relayInfo.BillingSource)
				if tc.settleQuota > 0 {
					require.NoError(t, session.Settle(tc.settleQuota))
				}
			}

			wallet, err := model.GetUserQuota(userID, true)
			require.NoError(t, err)
			assert.Equal(t, tc.wantWallet, wallet)
		})
	}
}
