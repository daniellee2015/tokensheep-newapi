package system_setting

import "github.com/QuantumNous/new-api/setting/config"

type LegalSettings struct {
	UserAgreement string `json:"user_agreement"`
	PrivacyPolicy string `json:"privacy_policy"`
	// UsagePolicy is the Acceptable Use Policy (AUP) — required by the Waffo
	// payment channel for AIGC compliance. Chinese default + English variant.
	UsagePolicy   string `json:"usage_policy"`
	UsagePolicyEn string `json:"usage_policy_en"`
}

var defaultLegalSettings = LegalSettings{
	UserAgreement: "",
	PrivacyPolicy: "",
	UsagePolicy:   "",
	UsagePolicyEn: "",
}

func init() {
	config.GlobalConfig.Register("legal", &defaultLegalSettings)
}

func GetLegalSettings() *LegalSettings {
	return &defaultLegalSettings
}
