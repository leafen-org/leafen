package app

import "github.com/pocketbase/pocketbase/core"

// markSharedPagesUnderUnseenTrash marks in the trash, for the person reading
// them, the pages shared with them that sit under a page in the trash whose
// page above them they can't open: the trash holds a page and every page
// under it (pageTrashWalkSQL), and their list has no page above it to tell
// it by (outOfTrashIn in the web app), so the page says so itself, with when
// the page holding it went there — its mentions read "휴지통에 있음" and the
// sidebar leaves it out, as for anyone who reads the page above it (Notion,
// seen 2026-11-24 in two tabs: a mention of a page, a database, a database
// page or a data source under a page in the trash is marked as one in it).
// A page whose page above it they open is told by that page, as before.
func markSharedPagesUnderUnseenTrash(app core.App, readerID string, rows []storedBootstrapPage) error {
	ids := []string{}
	for _, row := range rows {
		if !row.InTrash && row.ParentID != "" {
			ids = append(ids, row.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	effects, err := loadPageTrashEffects(app, ids)
	if err != nil || len(effects) == 0 {
		return err
	}
	opens := map[string]bool{}
	for index := range rows {
		effect, trashed := effects[rows[index].ID]
		if !trashed || rows[index].InTrash {
			continue
		}
		parentID := rows[index].ParentID
		open, known := opens[parentID]
		if !known {
			_, err := authorizePage(app, readerID, parentID, permissionWorkspaceRead)
			if err != nil && !isPermissionDenied(err) {
				return err
			}
			open = err == nil
			opens[parentID] = open
		}
		if !open {
			rows[index].InTrash, rows[index].TrashedAt = true, effect.TrashedAt
		}
	}
	return nil
}
