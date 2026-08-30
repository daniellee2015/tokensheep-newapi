package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GroupSpecialUsableGroup 有两条 DB key 共存的历史:
//
//   - 扁平 `GroupSpecialUsableGroup`  — 早期直接命名, updateOptionMap 里没有
//     case, 落进 common.OptionMap 就到头了. 生产上 199 条 value 全空串, 不影响
//     任何逻辑, 只是 legacy DB 行
//   - 点分 `group_ratio_setting.group_special_usable_group` — 现在的权威源,
//     通过 handleConfigUpdate 反射到 ratio_setting.GroupRatioSetting RWMap,
//     service/group.go:17 里 GetUserUsableGroups 读的就是这个
//
// 这两个 test 把 R17-B 澄清结论固化下来:
//  1. 写扁平 key 不会影响 RWMap
//  2. 写点分 key 会影响 RWMap
//
// 如果将来有人以为"这两个是一样的"把 flat 也 wire 进去, 会破 test 1.
// 如果反射链坏了 (config.Register / handleConfigUpdate 挂了), 会破 test 2.

func snapshotAndClearGSU(t *testing.T) map[string]map[string]string {
	t.Helper()
	rw := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup
	snapshot := rw.ReadAll()
	rw.Clear()
	return snapshot
}

func restoreGSU(t *testing.T, snapshot map[string]map[string]string) {
	t.Helper()
	rw := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup
	rw.Clear()
	rw.AddAll(snapshot)
}

func ensureOptionMapInit(t *testing.T) {
	t.Helper()
	common.OptionMapRWMutex.Lock()
	defer common.OptionMapRWMutex.Unlock()
	if common.OptionMap == nil {
		common.OptionMap = map[string]string{}
	}
}

func TestUpdateOptionMap_FlatGroupSpecialUsableGroupKeyIsInert(t *testing.T) {
	// 隔离: 每 test 独占 RWMap 和 OptionMap 里的 flat entry
	ensureOptionMapInit(t)
	original := snapshotAndClearGSU(t)
	t.Cleanup(func() { restoreGSU(t, original) })

	common.OptionMapRWMutex.Lock()
	originalFlat, hadFlat := common.OptionMap["GroupSpecialUsableGroup"]
	delete(common.OptionMap, "GroupSpecialUsableGroup")
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		if hadFlat {
			common.OptionMap["GroupSpecialUsableGroup"] = originalFlat
		} else {
			delete(common.OptionMap, "GroupSpecialUsableGroup")
		}
	})

	// 扁平 key 通过 updateOptionMap 写入 JSON, 期望: RWMap 不变
	payload := `{"vip":{"-:default":""}}`
	require.NoError(t, updateOptionMap("GroupSpecialUsableGroup", payload))

	rw := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup
	_, found := rw.Get("vip")
	assert.False(t, found,
		"flat GroupSpecialUsableGroup key must NOT feed RWMap; "+
			"if this fails, someone wired updateOptionMap to consume the flat key "+
			"(权威源应该是点分 key, 不要新加 case)")

	common.OptionMapRWMutex.Lock()
	stored := common.OptionMap["GroupSpecialUsableGroup"]
	common.OptionMapRWMutex.Unlock()
	assert.Equal(t, payload, stored,
		"flat key does land in OptionMap (for /api/option/ echo & UI 向后兼容), "+
			"just doesn't influence service/group.go behavior")
}

func TestUpdateOptionMap_DottedGroupSpecialUsableGroupIsAuthoritative(t *testing.T) {
	ensureOptionMapInit(t)
	original := snapshotAndClearGSU(t)
	t.Cleanup(func() { restoreGSU(t, original) })

	// 点分 key 通过 updateOptionMap → handleConfigUpdate → 反射进 RWMap
	require.NoError(t, updateOptionMap(
		"group_ratio_setting.group_special_usable_group",
		`{"vip":{"-:default":"隐藏 default"}}`,
	))

	rw := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup
	entry, found := rw.Get("vip")
	require.True(t, found,
		"dotted key is the authoritative source; if this fails, "+
			"config.Register / handleConfigUpdate 反射链坏了")
	assert.Equal(t, "隐藏 default", entry["-:default"])
}
