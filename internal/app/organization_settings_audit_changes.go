package app

import (
	"encoding/json"
	"reflect"
	"sort"
)

// organizationSettingsChanges names what one save changed, each setting with
// the value it had and the one it took, so the audit log says "내보내기
// 비활성화: 켬" rather than only that something changed (2026-11-06).
func organizationSettingsChanges(before, after any, skip ...string) []map[string]any {
	read := func(value any) map[string]any {
		encoded, err := json.Marshal(value)
		fields := map[string]any{}
		if err == nil {
			_ = json.Unmarshal(encoded, &fields)
		}
		return fields
	}
	was, now := read(before), read(after)
	skipped := map[string]bool{}
	for _, key := range skip {
		skipped[key] = true
	}
	keys := make([]string, 0, len(now))
	for key := range now {
		if !skipped[key] && !reflect.DeepEqual(was[key], now[key]) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	changes := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		changes = append(changes, map[string]any{"setting": key, "from": was[key], "to": now[key]})
	}
	return changes
}
