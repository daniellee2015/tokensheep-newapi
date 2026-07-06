package model

import (
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTokenSheepEconomyTestState(t *testing.T) {
	t.Helper()
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM users").Error)
	require.NoError(t, DB.Exec("DELETE FROM redemptions").Error)

	oldRedisEnabled := common.RedisEnabled
	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	t.Cleanup(func() {
		common.RedisEnabled = oldRedisEnabled
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
	})
}

func TestGetUserQuotaIncludesGiftPool(t *testing.T) {
	setupTokenSheepEconomyTestState(t)

	user := User{
		Id:        101,
		Username:  "gift-quota-user",
		Password:  "password",
		Status:    common.UserStatusEnabled,
		Group:     "free",
		Quota:     700,
		QuotaGift: 300,
	}
	require.NoError(t, DB.Create(&user).Error)

	quota, err := GetUserQuota(user.Id, true)
	require.NoError(t, err)
	assert.Equal(t, 1000, quota)

	cache := user.ToBaseUser()
	assert.Equal(t, 1000, cache.Quota)
}

func TestDecreaseUserQuotaUsesGiftBeforePaid(t *testing.T) {
	setupTokenSheepEconomyTestState(t)

	user := User{
		Id:        102,
		Username:  "gift-first-user",
		Password:  "password",
		Status:    common.UserStatusEnabled,
		Group:     "supporter",
		Quota:     1000,
		QuotaGift: 600,
	}
	require.NoError(t, DB.Create(&user).Error)

	debit, err := DecreaseUserQuotaDetailed(user.Id, 800, false)
	require.NoError(t, err)
	assert.Equal(t, QuotaDebit{Gift: 600, Paid: 200}, debit)

	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	assert.Equal(t, 800, debit.Total())
	assert.Equal(t, 800, got.Quota)
	assert.Equal(t, 0, got.QuotaGift)
	assert.Equal(t, 600, got.GiftQuotaUsed)
	assert.Equal(t, time.Now().Format("2006-01-02"), got.GiftQuotaUsedDate)
}

func TestDecreaseUserQuotaHonorsDailyGiftLimit(t *testing.T) {
	setupTokenSheepEconomyTestState(t)

	today := time.Now().Format("2006-01-02")
	user := User{
		Id:                103,
		Username:          "gift-limit-user",
		Password:          "password",
		Status:            common.UserStatusEnabled,
		Group:             "supporter",
		Quota:             100000,
		QuotaGift:         100000,
		GiftQuotaUsed:     49000,
		GiftQuotaUsedDate: today,
	}
	require.NoError(t, DB.Create(&user).Error)

	debit, err := DecreaseUserQuotaDetailed(user.Id, 5000, false)
	require.NoError(t, err)
	assert.Equal(t, QuotaDebit{Gift: 1000, Paid: 4000}, debit)

	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	assert.Equal(t, 96000, got.Quota)
	assert.Equal(t, 99000, got.QuotaGift)
	assert.Equal(t, 50000, got.GiftQuotaUsed)
	assert.Equal(t, today, got.GiftQuotaUsedDate)
}

func TestRefundUserQuotaDebitRestoresGiftAndPaid(t *testing.T) {
	setupTokenSheepEconomyTestState(t)

	user := User{
		Id:        104,
		Username:  "refund-user",
		Password:  "password",
		Status:    common.UserStatusEnabled,
		Group:     "supporter",
		Quota:     1000,
		QuotaGift: 600,
	}
	require.NoError(t, DB.Create(&user).Error)

	debit, err := DecreaseUserQuotaDetailed(user.Id, 800, false)
	require.NoError(t, err)
	require.NoError(t, RefundUserQuotaDebit(user.Id, debit))

	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	assert.Equal(t, 1000, got.Quota)
	assert.Equal(t, 600, got.QuotaGift)
	assert.Equal(t, 0, got.GiftQuotaUsed)
}

func TestRedeemCreditsGiftPoolAndOnlyOncePerUser(t *testing.T) {
	setupTokenSheepEconomyTestState(t)

	user := User{
		Id:       105,
		Username: "redeem-user",
		Password: "password",
		Status:   common.UserStatusEnabled,
		Group:    "free",
	}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&Redemption{
		Id:          1,
		UserId:      1,
		Key:         "first-welcome-code",
		Status:      common.RedemptionCodeStatusEnabled,
		Name:        "welcome",
		Quota:       200000,
		CreatedTime: common.GetTimestamp(),
	}).Error)
	require.NoError(t, DB.Create(&Redemption{
		Id:          2,
		UserId:      1,
		Key:         "second-welcome-code",
		Status:      common.RedemptionCodeStatusEnabled,
		Name:        "welcome",
		Quota:       200000,
		CreatedTime: common.GetTimestamp(),
	}).Error)

	quota, err := Redeem("first-welcome-code", user.Id)
	require.NoError(t, err)
	assert.Equal(t, 200000, quota)

	_, err = Redeem("second-welcome-code", user.Id)
	assert.True(t, errors.Is(err, ErrRedeemFailed))

	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	assert.Equal(t, 0, got.Quota)
	assert.Equal(t, 200000, got.QuotaGift)
}

func TestRedeemCapsGiftPool(t *testing.T) {
	setupTokenSheepEconomyTestState(t)

	user := User{
		Id:        106,
		Username:  "redeem-cap-user",
		Password:  "password",
		Status:    common.UserStatusEnabled,
		Group:     "free",
		QuotaGift: 4999900,
	}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&Redemption{
		Id:          3,
		UserId:      1,
		Key:         "capped-welcome-code",
		Status:      common.RedemptionCodeStatusEnabled,
		Name:        "welcome",
		Quota:       200000,
		CreatedTime: common.GetTimestamp(),
	}).Error)

	quota, err := Redeem("capped-welcome-code", user.Id)
	require.NoError(t, err)
	assert.Equal(t, 100, quota)

	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	assert.Equal(t, 5000000, got.QuotaGift)
}

func TestDailyMaintenanceUsesCreatedAtWhenUserHasNoRequests(t *testing.T) {
	setupTokenSheepEconomyTestState(t)

	now := time.Now().Unix()
	cutoff := now - 30*86400
	users := []User{
		{
			Id:            107,
			Username:      "recent-no-request",
			Password:      "password",
			Status:        common.UserStatusEnabled,
			Group:         "free",
			AffCode:       "recent-no-request",
			QuotaGift:     200000,
			CreatedAt:     now - 3*86400,
			LastRequestAt: 0,
		},
		{
			Id:            108,
			Username:      "old-no-request",
			Password:      "password",
			Status:        common.UserStatusEnabled,
			Group:         "free",
			AffCode:       "old-no-request",
			QuotaGift:     200000,
			CreatedAt:     now - 45*86400,
			LastRequestAt: 0,
		},
		{
			Id:            109,
			Username:      "old-active-request",
			Password:      "password",
			Status:        common.UserStatusEnabled,
			Group:         "free",
			AffCode:       "old-active-request",
			QuotaGift:     200000,
			CreatedAt:     now - 45*86400,
			LastRequestAt: now - 3*86400,
		},
	}
	require.NoError(t, DB.Create(&users).Error)

	_, giftZeroed, err := RunTokenSheepDailyMaintenance(cutoff, cutoff)
	require.NoError(t, err)
	assert.Equal(t, 1, giftZeroed)

	var got []User
	require.NoError(t, DB.Where("id IN ?", []int{107, 108, 109}).Order("id").Find(&got).Error)
	require.Len(t, got, 3)
	assert.Equal(t, 200000, got[0].QuotaGift)
	assert.Equal(t, 0, got[1].QuotaGift)
	assert.Equal(t, 200000, got[2].QuotaGift)
}

func TestReverseWaffoPancakeTopUpRevokesPaidQuotaAndTier(t *testing.T) {
	setupTokenSheepEconomyTestState(t)

	paidQuota := int(10 * common.QuotaPerUnit)
	user := User{
		Id:           110,
		Username:     "refund-topup-user",
		Password:     "password",
		Status:       common.UserStatusEnabled,
		Group:        "supporter",
		AffCode:      "refund-topup-user",
		Quota:        paidQuota / 2,
		QuotaGift:    200000,
		TotalDonated: paidQuota,
	}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&TopUp{
		UserId:          user.Id,
		Amount:          10,
		Money:           10.50,
		TradeNo:         "WAFFO_PANCAKE-refund-test",
		PaymentMethod:   PaymentMethodWaffoPancake,
		PaymentProvider: PaymentProviderWaffoPancake,
		CreateTime:      time.Now().Unix(),
		CompleteTime:    time.Now().Unix(),
		Status:          common.TopUpStatusSuccess,
	}).Error)

	quotaRevoked, err := ReverseWaffoPancakeTopUp("WAFFO_PANCAKE-refund-test", common.TopUpStatusRefunded, "")
	require.NoError(t, err)
	assert.Equal(t, paidQuota, quotaRevoked)

	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	assert.Equal(t, 0, got.Quota)
	assert.Equal(t, 200000, got.QuotaGift)
	assert.Equal(t, 0, got.TotalDonated)
	assert.Equal(t, "free", got.Group)

	var topUp TopUp
	require.NoError(t, DB.Where("trade_no = ?", "WAFFO_PANCAKE-refund-test").First(&topUp).Error)
	assert.Equal(t, common.TopUpStatusRefunded, topUp.Status)

	quotaRevoked, err = ReverseWaffoPancakeTopUp("WAFFO_PANCAKE-refund-test", common.TopUpStatusRefunded, "")
	require.NoError(t, err)
	assert.Equal(t, 0, quotaRevoked)
}
