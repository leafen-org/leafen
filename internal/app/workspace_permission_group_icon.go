package app

import (
	"strings"
)

// A group's icon, as Notion's "아이콘 및 이름" in "새 그룹 생성" and the
// group's window (seen 2026-11-11): an emoji, a built-in icon or a picture's
// link or an uploaded picture (held_icon_files.go) picked from its picker's
// three tabs, kept with the group and shown
// where the group is named — its row in the list, its window's head, a
// person's groups. Empty, the people mark shows.

// normalizeWorkspacePermissionGroupIcon is the icon a group keeps for what
// its owner picked: "" for none, one emoji, a built-in icon's "name:color", a
// picture's web link or an uploaded picture's address (whose file
// checkHeldIconFile checks). ok is false otherwise.
func normalizeWorkspacePermissionGroupIcon(raw string) (string, bool) {
	icon := strings.TrimSpace(raw)
	if _, uploaded := heldIconFileID(icon); validNativeIconValue(icon) || uploaded {
		return icon, true
	}
	return normalizeConnectionIcon(icon, nil)
}
