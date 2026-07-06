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

// GetGroups returns the list of USER TIER names (free + every key in
// TierThresholds), used by the admin user-editor drop-down.
//
// Upstream new-api merges "user tier" and "channel/pricing group" into a
// single GroupRatio map. TokenSheep separates them: tier names come from
// `tokensheep_economy.tier_thresholds` (supporter / fan / bestie / vip …),
// and channel-side names (gpt-supporter, claude-supporter, image, …) stay
// in GroupRatio only for pricing lookups. The admin user-editor never
// wants a channel name assigned to `users.group`, so we return only the
// tier set here. `free` is always included as the default fallback tier.
func GetGroups(c *gin.Context) {
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
