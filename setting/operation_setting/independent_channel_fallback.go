package operation_setting

import (
	"fmt"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const IndependentChannelFallbackOption = "IndependentChannelFallback"

// IndependentChannelFallback explicitly declares independent capacity domains.
// It is opt-in, limited to two primaries and one reserve, and never changes models.
type IndependentChannelFallback struct {
	Enabled           bool     `json:"enabled"`
	Groups            []string `json:"groups"`
	Models            []string `json:"models"`
	PrimaryChannelIDs []int    `json:"primary_channel_ids"`
	ReserveChannelID  int      `json:"reserve_channel_id"`
}

func ValidateIndependentChannelFallback(value string) error {
	_, err := parseIndependentChannelFallbackPolicies(value)
	return err
}

// parseIndependentChannelFallbackPolicies keeps legacy object configurations
// valid while allowing separate bounded routes for distinct billing groups.
func parseIndependentChannelFallbackPolicies(value string) ([]IndependentChannelFallback, error) {
	var policies []IndependentChannelFallback
	if strings.HasPrefix(strings.TrimSpace(value), "[") {
		if err := common.UnmarshalJsonStr(value, &policies); err != nil {
			return nil, err
		}
	} else {
		var policy IndependentChannelFallback
		if err := common.UnmarshalJsonStr(value, &policy); err != nil {
			return nil, err
		}
		policies = []IndependentChannelFallback{policy}
	}
	seen := make(map[[2]string]bool)
	for _, policy := range policies {
		if err := policy.validate(); err != nil {
			return nil, err
		}
		if !policy.Enabled {
			continue
		}
		for _, group := range policy.Groups {
			for _, model := range policy.Models {
				scope := [2]string{group, model}
				if seen[scope] {
					return nil, fmt.Errorf("independent fallback group/model scopes must not overlap")
				}
				seen[scope] = true
			}
		}
	}
	return policies, nil
}

func (policy IndependentChannelFallback) validate() error {
	if !policy.Enabled {
		return nil
	}
	if len(policy.Groups) == 0 || len(policy.Models) == 0 || len(policy.PrimaryChannelIDs) == 0 || len(policy.PrimaryChannelIDs) > 2 || policy.ReserveChannelID <= 0 {
		return fmt.Errorf("independent fallback requires groups, models, one or two primary channels and one reserve")
	}
	seen := map[int]bool{policy.ReserveChannelID: true}
	for _, id := range policy.PrimaryChannelIDs {
		if id <= 0 || seen[id] {
			return fmt.Errorf("independent fallback channel IDs must be positive and distinct")
		}
		seen[id] = true
	}
	for _, name := range append(slices.Clone(policy.Groups), policy.Models...) {
		if name == "" || name == "auto" || name == "*" {
			return fmt.Errorf("independent fallback requires explicit groups and models")
		}
	}
	return nil
}

// MatchIndependentChannelFallback snapshots the option once per request. Normal
// authentication and model/group permissions have already run in the distributor.
func MatchIndependentChannelFallback(group, model string, channelID int) *IndependentChannelFallback {
	common.OptionMapRWMutex.RLock()
	value := common.OptionMap[IndependentChannelFallbackOption]
	common.OptionMapRWMutex.RUnlock()
	if value == "" {
		return nil
	}
	policies, err := parseIndependentChannelFallbackPolicies(value)
	if err != nil {
		return nil
	}
	for _, policy := range policies {
		if policy.Enabled && slices.Contains(policy.Groups, group) && slices.Contains(policy.Models, model) &&
			(channelID == policy.ReserveChannelID || slices.Contains(policy.PrimaryChannelIDs, channelID)) {
			return &policy
		}
	}
	return nil
}
