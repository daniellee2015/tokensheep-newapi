package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

// GetGroups returns the list of user-facing group names. TokenSheep splits
// its group registry into two overlapping sets: the "ratio" map lives in
// GroupRatio and includes both user tiers (free / supporter / fan / ...)
// and channel-side tags (gpt-supporter / claude-supporter / image / ...).
// The admin user-editor drop-down only wants the tier names, so we filter
// against `UserUsableGroups` — the same registry that /api/user/self/groups
// uses to decide what a caller may switch to. Adding a new tier to that
// map exposes it here automatically; adding a channel tag does not.
func GetGroups(c *gin.Context) {
	usable := setting.GetUserUsableGroupsCopy()
	groupNames := make([]string, 0, len(usable))
	for groupName := range usable {
		groupNames = append(groupNames, groupName)
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    groupNames,
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
