package app

import (
	"encoding/json"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A search checks each match the reader may see in full
// (authorizePageAccessRole), and for a team member most of a big
// workspace's matches are others' private pages: checking those one by one
// was half of a busy server's time (2026-10-15). What is certainly hidden
// from the reader is left out before the full check, which still decides
// every other match: someone else's private page with no share on it or
// above it. Where nothing in the workspace is shared with the reader
// (pageSharesExist), the shares on such pages, someone else's, open nothing
// to them, as the full check finds (pageShareRoleOn): they are not looked
// for (2026-11-13: 1,500 items shared with one member went one full check
// at a time for every other member's search, two to four seconds on a NAS).

type workspaceSearchHiddenReader struct {
	workspaceID string
	principalID string
	// shares is whether anything in the workspace is shared with the reader.
	shares bool
}

// newWorkspaceSearchHiddenReader reads what a reader may be kept from in a
// workspace; for one who came in by a web link that gives more than
// viewing it hides nothing. Someone else's private page is closed to the
// workspace's owner too (privatePageAccessRole): its search went through
// 3,000 of a member's private pages one full check at a time, two seconds
// on a NAS (2026-11-13).
func newWorkspaceSearchHiddenReader(app core.App, workspaceID, principalID string) (*workspaceSearchHiddenReader, error) {
	reader := &workspaceSearchHiddenReader{workspaceID: workspaceID, principalID: principalID}
	var linked int
	if err := app.DB().NewQuery(`
		SELECT EXISTS (SELECT 1 FROM {{leafen_page_link_visitors}} [[visitor]]
			JOIN {{leafen_public_share_links}} [[link]] ON [[link]].[[id]] = [[visitor]].[[link_id]]
			WHERE [[visitor]].[[workspace_id]] = {:workspaceId} AND [[visitor]].[[user_id]] = {:principalId}
				AND [[link]].[[web_enabled]] = 1 AND [[link]].[[role]] IN ('comment', 'edit'))
	`).Bind(dbx.Params{"workspaceId": workspaceID, "principalId": principalID}).Row(&linked); err != nil || linked == 1 {
		return nil, err
	}
	var err error
	reader.shares, err = pageSharesExist(app, workspaceID, principalID)
	return reader, err
}

// hidden is, of a batch of matches, those certainly hidden from the reader.
func (reader *workspaceSearchHiddenReader) hidden(app core.App, pageIDs []string) (map[string]bool, error) {
	hidden := map[string]bool{}
	if reader == nil || len(pageIDs) == 0 {
		return hidden, nil
	}
	encoded, err := json.Marshal(pageIDs)
	if err != nil {
		return nil, err
	}
	shared := []string{}
	if reader.shares {
		if err := app.DB().NewQuery(`
			SELECT DISTINCT [[page_id]] FROM {{leafen_page_permissions}}
			WHERE [[page_id]] IN (SELECT [[value]] FROM json_each({:ids}))
		`).Bind(dbx.Params{"ids": string(encoded)}).Column(&shared); err != nil {
			return nil, err
		}
	}
	withShares := make(map[string]bool, len(shared))
	for _, id := range shared {
		withShares[id] = true
	}
	pages := []struct {
		ID           string `db:"id"`
		TeamspaceID  string `db:"teamspace_id"`
		PrivateOwner string `db:"private_owner_id"`
		ParentID     string `db:"parent_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[teamspace_id]], [[private_owner_id]], [[parent_id]] FROM {{leafen_pages}}
		WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids})) AND [[data_source_id]] = ''
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&pages); err != nil {
		return nil, err
	}
	nested := []string{}
	for _, page := range pages {
		// People's database, the server's own, is left to the full check,
		// which its own rule decides (people_database_access.go).
		if page.PrivateOwner == leafenSystemAuthorID {
			continue
		}
		if page.TeamspaceID != "" || page.PrivateOwner == reader.principalID || withShares[page.ID] {
			continue
		}
		// A page with none above it is all its own sharing.
		if page.ParentID == "" {
			hidden[page.ID] = true
			continue
		}
		nested = append(nested, page.ID)
	}
	private, err := reader.privateBeyondShares(app, nested)
	if err != nil {
		return nil, err
	}
	for pageID := range private {
		hidden[pageID] = true
	}
	return hidden, nil
}

// privateBeyondShares is, of someone else's private pages, those closed to
// the reader: no share on one or on the pages above it whose shares reach
// it, and no item above it, whose access it would follow. The pages and
// those above them are read at once (2026-10-17: read page by page, a
// member's search asked twice for each of 496 private pages).
func (reader *workspaceSearchHiddenReader) privateBeyondShares(app core.App, pageIDs []string) (map[string]bool, error) {
	private := map[string]bool{}
	if len(pageIDs) == 0 {
		return private, nil
	}
	aboveByPage, err := loadPagesAccessAncestorIDs(app, reader.workspaceID, pageIDs)
	if err != nil {
		return nil, err
	}
	all := []string{}
	seen := map[string]bool{}
	for _, above := range aboveByPage {
		for _, id := range above {
			if !seen[id] {
				seen[id] = true
				all = append(all, id)
			}
		}
	}
	encoded, err := json.Marshal(all)
	if err != nil {
		return nil, err
	}
	// A share above opens only where something is shared with the reader.
	sharesOpen := ""
	if reader.shares {
		sharesOpen = `SELECT [[page_id]] FROM {{leafen_page_permissions}} WHERE [[page_id]] IN (SELECT [[value]] FROM json_each({:ids}))
		UNION `
	}
	opening := []string{}
	if err := app.DB().NewQuery(sharesOpen + `SELECT [[id]] FROM {{leafen_database_rows}} WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
		UNION SELECT [[id]] FROM {{leafen_pages}} WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
			AND ([[teamspace_id]] != '' OR [[private_owner_id]] = {:principalId})
	`).Bind(dbx.Params{"ids": string(encoded), "principalId": reader.principalID}).Column(&opening); err != nil {
		return nil, err
	}
	opens := make(map[string]bool, len(opening))
	for _, id := range opening {
		opens[id] = true
	}
	// A page not found is left to the full check.
	for pageID, above := range aboveByPage {
		if !slices.ContainsFunc(above, func(id string) bool { return opens[id] }) {
			private[pageID] = true
		}
	}
	return private, nil
}
