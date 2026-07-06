package setting

// Waffo Pancake hosted checkout configuration. Gateway is enabled once
// MerchantID + PrivateKey + ProductID are populated (no separate Enabled
// flag, matching Stripe / Creem). StoreID + ProductID are operator-bound
// via SaveWaffoPancakeConfig.
//
// WaffoPancakeSurchargePercent: extra percentage added on top of the
//   station-side amount before it is charged via Pancake. Covers pancake
//   platform fees (~5%) so the operator doesn't eat them. e.g. a user
//   selecting a $10 pass with SurchargePercent=5 is actually charged
//   $10 * 1.05 = $10.50 by Pancake. Station-side quota credited stays $10.
var (
	WaffoPancakeMerchantID       string
	WaffoPancakePrivateKey       string
	WaffoPancakeReturnURL        string
	WaffoPancakeUnitPrice        float64 = 1.0
	WaffoPancakeMinTopUp         int     = 1
	WaffoPancakeStoreID          string
	WaffoPancakeProductID        string
	WaffoPancakeSurchargePercent float64 = 5.0
)
