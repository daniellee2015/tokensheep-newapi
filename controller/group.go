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
			}
		}
	}
	if _, ok := userUsableGroups["auto"]; ok {
		usableGroups["auto"] = map[string]interface{}{
			"ratio": "自动",
			"desc":  setting.GetUsableGroupDescription("auto"),
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    usableGroups,
	})
}
