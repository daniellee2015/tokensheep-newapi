package billing_setting

import (
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// GetGroupModelPrice returns a per-request base price for this exact routing
// group and public model. The caller still applies the user's group multiplier.
// An explicit zero means free; absence falls back to global model billing.
func GetGroupModelPrice(group, model string) (float64, bool) {
	if billingSetting.GroupModelPrice == nil {
		return 0, false
	}
	prices, ok := billingSetting.GroupModelPrice.Get(group)
	if !ok {
		return 0, false
	}
	price, ok := prices[model]
	return price, ok
}

func ValidateGroupModelPrice(value string) error {
	var groups map[string]map[string]*float64
	if err := common.UnmarshalJsonStr(value, &groups); err != nil {
		return fmt.Errorf("invalid group model prices: %w", err)
	}
	if groups == nil {
		return fmt.Errorf("group model prices must be an object")
	}
	for group, models := range groups {
		if strings.TrimSpace(group) == "" || models == nil {
			return fmt.Errorf("group model prices require a group name and model prices")
		}
		for model, price := range models {
			if strings.TrimSpace(model) == "" || price == nil || *price < 0 || math.IsNaN(*price) || math.IsInf(*price, 0) {
				return fmt.Errorf("invalid per-request price for group %q model %q", group, model)
			}
		}
	}
	return nil
}
