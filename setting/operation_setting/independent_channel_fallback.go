package operation_setting

import (
	"fmt"
	"slices"

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
	var policy IndependentChannelFallback
	if err := common.UnmarshalJsonStr(value, &policy); err != nil {
		return err
	}
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
	if value == "" || ValidateIndependentChannelFallback(value) != nil {
		return nil
	}
	var policy IndependentChannelFallback
	if common.UnmarshalJsonStr(value, &policy) != nil || !policy.Enabled {
		return nil
	}
	if !slices.Contains(policy.Groups, group) || !slices.Contains(policy.Models, model) {
		return nil
	}
	if channelID != policy.ReserveChannelID && !slices.Contains(policy.PrimaryChannelIDs, channelID) {
		return nil
	}
	return &policy
}
