package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func TestFormatWaffoPancakeAmount_UsesDisplayPriceString(t *testing.T) {
	testCases := []struct {
		name     string
		amount   float64
		expected string
	}{
		{name: "whole amount", amount: 29, expected: "29.00"},
		{name: "decimal amount", amount: 29.9, expected: "29.90"},
		{name: "round half up to cents", amount: 29.999, expected: "30.00"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, formatWaffoPancakeAmount(tc.amount))
		})
	}
}

func TestGetWaffoPancakePayMoney(t *testing.T) {
	originalUnitPrice := setting.WaffoPancakeUnitPrice
	originalSurchargePercent := setting.WaffoPancakeSurchargePercent
	originalApplyUSDExchangeRate := setting.WaffoPancakeApplyUSDExchangeRate
	originalQuotaDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	originalUSDExchangeRate := operation_setting.USDExchangeRate
	originalDiscounts := make(map[int]float64, len(operation_setting.GetPaymentSetting().AmountDiscount))
	for k, v := range operation_setting.GetPaymentSetting().AmountDiscount {
		originalDiscounts[k] = v
	}
	originalTopupGroupRatio := common.TopupGroupRatio2JSONString()

	t.Cleanup(func() {
		setting.WaffoPancakeUnitPrice = originalUnitPrice
		setting.WaffoPancakeSurchargePercent = originalSurchargePercent
		setting.WaffoPancakeApplyUSDExchangeRate = originalApplyUSDExchangeRate
		operation_setting.GetGeneralSetting().QuotaDisplayType = originalQuotaDisplayType
		operation_setting.USDExchangeRate = originalUSDExchangeRate
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscounts
		require.NoError(t, common.UpdateTopupGroupRatioByJSONString(originalTopupGroupRatio))
	})

	setting.WaffoPancakeUnitPrice = 2.5
	setting.WaffoPancakeSurchargePercent = 5
	setting.WaffoPancakeApplyUSDExchangeRate = true
	operation_setting.USDExchangeRate = 6.8
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{
		10:                           0.8,
		int(common.QuotaPerUnit * 3): 0.5,
		20:                           0,
	}
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"default":1,"vip":1.2}`))

	testCases := []struct {
		name             string
		amount           int64
		group            string
		quotaDisplayType string
		applyExchange    bool
		expected         float64
	}{
		{
			name:             "currency display applies unit price group ratio discount exchange rate and surcharge",
			amount:           10,
			group:            "vip",
			quotaDisplayType: operation_setting.QuotaDisplayTypeUSD,
			applyExchange:    true,
			expected:         25.2 / 6.8,
		},
		{
			name:             "tokens display converts quota to display units before pricing and exchange rate",
			amount:           int64(common.QuotaPerUnit * 3),
			group:            "vip",
			quotaDisplayType: operation_setting.QuotaDisplayTypeTokens,
			applyExchange:    true,
			expected:         4.725 / 6.8,
		},
		{
			name:             "non-positive discount falls back to no discount",
			amount:           20,
			group:            "default",
			quotaDisplayType: operation_setting.QuotaDisplayTypeUSD,
			applyExchange:    true,
			expected:         52.5 / 6.8,
		},
		{
			name:             "exchange rate can be disabled",
			amount:           10,
			group:            "vip",
			quotaDisplayType: operation_setting.QuotaDisplayTypeUSD,
			applyExchange:    false,
			expected:         25.2,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			operation_setting.GetGeneralSetting().QuotaDisplayType = tc.quotaDisplayType
			setting.WaffoPancakeApplyUSDExchangeRate = tc.applyExchange
			actual := getWaffoPancakePayMoney(tc.amount, tc.group)
			require.InDelta(t, tc.expected, actual, 0.000001)
		})
	}
}

func TestWaffoPancakeReversalStatus(t *testing.T) {
	testCases := []struct {
		name         string
		eventType    string
		expected     string
		expectedOkay bool
	}{
		{
			name:         "payment refunded",
			eventType:    "payment.refunded",
			expected:     common.TopUpStatusRefunded,
			expectedOkay: true,
		},
		{
			name:         "order disputed",
			eventType:    "order.disputed",
			expected:     common.TopUpStatusDisputed,
			expectedOkay: true,
		},
		{
			name:         "chargeback event",
			eventType:    "payment.chargeback.created",
			expected:     common.TopUpStatusDisputed,
			expectedOkay: true,
		},
		{
			name:         "completed event",
			eventType:    "order.completed",
			expectedOkay: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, ok := waffoPancakeReversalStatus(tc.eventType)
			require.Equal(t, tc.expectedOkay, ok)
			require.Equal(t, tc.expected, actual)
		})
	}
}
