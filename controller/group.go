package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/tokensheep_setting"

	"github.com/gin-gonic/gin"
)

// GetGroups returns every key in GroupRatio — the full "channel-side"
// pricing-group registry that channel editors, tag editors and channel
// list filters need to enumerate. Matches the upstream new-api behavior;
// tier-only listing is served by GetTierList on /api/group/tiers so the
// two admin drop-downs (user-editor vs channel-editor) never share a
// namespace by accident.
func GetGroups(c *gin.Context) {
	groupNames := make([]string, 0)
	for groupName := range ratio_setting.GetGroupRatioCopy() {
		groupNames = append(groupNames, groupName)
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    groupNames,
	})
}

// GetTierList returns the list of USER TIER names (free + every key in
// TierThresholds), consumed by the admin user-editor and subscription-plan
// drop-downs where the value written to users.group must be a tier — not
// a channel-side pricing group. Tier names come from
// `tokensheep_economy.tier_thresholds`; `free` is always included as the
// default fallback tier.
func GetTierList(c *gin.Context) {
	names := []string{"free"}
	for tierName := range tokensheep_setting.GetTierThresholdsCopy() {
		if tierName == "free" {
			continue
		}
		names = append(names, tierName)
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    names,
	})
}

// groupKind classifies a selectable group for the API-key picker so the UI
// can tell the operator *what kind of thing* they're binding a key to. The
// three namespaces overlap in one flat list (see v4 spec §八 R3-1), which
// is confusing without a label:
//
//	"tier"       — a contribution-ladder tier the user can be promoted into
//	               (free / supporter / fan / bestie / ...). Listed in
//	               TierThresholds and not marked commercial.
//	"commercial" — a reseller / bulk-contract group (retail / wholesale /
//	               wholesale-plus). Admin-assigned, outside the ladder.
//	"channel"    — an upstream channel group (GPT-Pro, aws-q, claude-max,
//	               ...). Not a user identity at all; routes traffic.
//
// Anything unrecognised falls through to "channel" because that's the
// larger population by far and the safer default for a display hint.
func groupKind(name string) string {
	if tokensheep_setting.IsCommercialGroup(name) {
		return "commercial"
	}
	if _, ok := tokensheep_setting.GetTierThresholdsCopy()[name]; ok {
		return "tier"
	}
	// `free` carries no threshold row (it's the default group, not a
	// purchasable tier) but is still a user identity rather than a channel.
	if name == "free" {
		return "tier"
	}
	return "channel"
}

func GetUserGroups(c *gin.Context) {
	usableGroups := make(map[string]map[string]interface{})
	userGroup := ""
	userId := c.GetInt("id")
	userGroup, _ = model.GetUserGroup(userId, false)
	userUsableGroups := service.GetUserUsableGroups(userGroup)
	for groupName, _ := range ratio_setting.GetGroupRatioCopy() {
		// UserUsableGroups contains the groups that the user can use
		if desc, ok := userUsableGroups[groupName]; ok {
			usableGroups[groupName] = map[string]interface{}{
				"ratio": service.GetUserGroupRatio(userGroup, groupName),
				"desc":  desc,
				// R16-5: tier / commercial / channel hint so the API-key
				// group picker can label each option instead of showing a
				// flat list where a user tier and an upstream channel group
				// look identical.
				"kind": groupKind(groupName),
			}
		}
	}
	if _, ok := userUsableGroups["auto"]; ok {
		usableGroups["auto"] = map[string]interface{}{
			"ratio": "自动",
			"desc":  setting.GetUsableGroupDescription("auto"),
			"kind":  "auto",
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    usableGroups,
	})
}
