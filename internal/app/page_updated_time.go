package app

import (
	"github.com/pocketbase/pocketbase/tools/types"
)

// A page's "updated" is PocketBase's time, "2006-01-02 15:04:05.000Z", which
// lists such as "최근 편집" order as text (recent_edits.go). Turning a page
// into a database's page and back wrote it as RFC 3339 ("…T…Z") until
// 2026-10-29, which ordered after every later edit; those times are written
// again as PocketBase's.

// pageUpdatedNow is the "updated" a direct change of a page record writes.
func pageUpdatedNow() string {
	return types.NowDateTime().String()
}
