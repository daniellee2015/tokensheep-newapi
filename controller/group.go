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

// groupRPM resolves the per-group RPM ceiling used by the API-key picker so
// users can see "GPT-Pro: 1000 rpm" instead of a flat list without a
// number. Returns (rpm, windowMinutes):
//
//   - rpm=0 / windowMinutes=0 → no ceiling (either the global switch is
//     off, or this group is absent from ModelRequestRateLimitGroup and
//     falls back to the site-wide default). The frontend renders that as
//     "unlimited".
//   - rpm>0 / windowMinutes>0 → cap is `rpm` successful requests inside a
//     `windowMinutes`-minute rolling window. When the operator picks a
//     >1-minute window the frontend can divide to display "per minute".
//
// We report limits[1] (the successful-request ceiling). That's the number
// users can plan against — it counts *only* successful requests, so
// clients tuning their throughput care about it. limits[0] (the total
// count including failures) is a safety net against clients hammering
// with bad requests and doesn't belong on a "your RPM is …" card.
func groupRPM(name string) (int, int) {
	if !setting.ModelRequestRateLimitEnabled {
		return 0, 0
	}
	_, successCount, found := setting.GetGroupRateLimit(name)
	if !found {
		return 0, 0
	}
	return successCount, setting.ModelRequestRateLimitDurationMinutes
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
			rpm, windowMinutes := groupRPM(groupName)
			usableGroups[groupName] = map[string]interface{}{
				"ratio": service.GetUserGroupRatio(userGroup, groupName),
				"desc":  desc,
				// R16-5: tier / commercial / channel hint so the API-key
				// group picker can label each option instead of showing a
				// flat list where a user tier and an upstream channel group
				// look identical.
				"kind": groupKind(groupName),
				// R21: expose the per-group RPM ceiling and its window so
				// the API-key picker renders "1000 rpm" instead of leaving
				// the user to guess. 0/0 = no ceiling configured for this
				// group; the frontend treats that as "unlimited".
				"rpm":                rpm,
				"rpm_window_minutes": windowMinutes,
			}
		}
	}
	if _, ok := userUsableGroups["auto"]; ok {
		usableGroups["auto"] = map[string]interface{}{
			"ratio": "自动",
			"desc":  setting.GetUsableGroupDescription("auto"),
			"kind":  "auto",
			// `auto` is a routing pseudo-group; it has no rate-limit row of
			// its own, so return 0/0 alongside the same fields the real
			// groups carry to keep the response shape uniform.
			"rpm":                0,
			"rpm_window_minutes": 0,
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    usableGroups,
	})
}
