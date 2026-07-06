package setting

// Waffo Pancake hosted checkout configuration. Gateway is enabled once
// MerchantID + PrivateKey + ProductID are populated (no separate Enabled
// flag, matching Stripe / Creem). StoreID + ProductID are operator-bound
// via SaveWaffoPancakeConfig.
//
// TokenSheep-only additions on top of the upstream fields:
//
//   - WaffoPancakeApplyUSDExchangeRate: when true, the station-side dollar
//     amount is divided by operation_setting.USDExchangeRate before being
//     charged via Pancake. Matches the 100b USD-settlement flow so the
//     in-app "$10" contribution settles to Pancake in USD via the global
//     exchange rate (default 6.8). Station-side quota credited is still $10.
//   - WaffoPancakeSurchargePercent: extra percentage added on top of the
//     Pancake settlement amount to cover the ~0.5% Pancake platform fee so
//     the operator doesn't eat it. Station-side quota credited stays the
//     selected amount; the extra is only visible on the Pancake checkout page.
var (
	WaffoPancakeMerchantID           string
	WaffoPancakePrivateKey           string
	WaffoPancakeReturnURL            string
	WaffoPancakeUnitPrice            float64 = 1.0
	WaffoPancakeMinTopUp             int     = 1
	WaffoPancakeStoreID              string
	WaffoPancakeProductID            string
	WaffoPancakeApplyUSDExchangeRate bool    = true
	WaffoPancakeSurchargePercent     float64 = 0.5
)
