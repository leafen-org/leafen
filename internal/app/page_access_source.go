package app

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// How a person's role on a page came, which the access decision names with
// the role (decidePageAccessRole): their own private page, the teamspace the
// page is in, or the page's shares — and, asked of the shares, which one: the
// person's own, a group of theirs, everyone in the workspace, or a web link
// they came in by. Notion's property rule preview says it under an inherited
// level (columnPropertyPermissionsModal.viewAs, read 2026-11-18).
type pageAccessSource struct {
	Kind    string
	GroupID string
}

const (
	pageAccessFromPrivatePage = "private"
	pageAccessFromTeamspace   = "teamspace"
	pageAccessFromShares      = "shares"
	pageAccessFromPerson      = "person"
	pageAccessFromGroup       = "group"
	pageAccessFromWorkspace   = "workspace"
	pageAccessFromLink        = "link"
)

// memberPageAccessSource is the source of a workspace member's own role on a
// page: the teamspace's where the page is in one, else their private page's.
func memberPageAccessSource(page *core.Record) pageAccessSource {
	if page.GetString("teamspace_id") != "" {
		return pageAccessSource{Kind: pageAccessFromTeamspace}
	}
	return pageAccessSource{Kind: pageAccessFromPrivatePage}
}

// pageShareSource is which share gives the role the shares gave the person on
// the page: the widest of the page's and those above it, the person's own
// before a group's before everyone's at the same role; a web link where no
// share gives as much.
func pageShareSource(app core.App, workspaceID, pageID, principalID string, role pageAccessRole) (pageAccessSource, error) {
	pageIDs, err := loadPageAccessAncestorIDs(app, workspaceID, pageID)
	if err != nil || len(pageIDs) == 0 {
		return pageAccessSource{Kind: pageAccessFromLink}, err
	}
	principals, err := loadPageSharePrincipals(app, workspaceID, principalID)
	if err != nil {
		return pageAccessSource{}, err
	}
	params := dbx.Params{"workspaceId": workspaceID}
	placeholders := make([]string, 0, len(pageIDs))
	for index, ancestorID := range pageIDs {
		key := fmt.Sprintf("sourcePage%d", index)
		params[key] = ancestorID
		placeholders = append(placeholders, "{:"+key+"}")
	}
	widest := struct {
		Rank          int    `db:"rank"`
		PrincipalType string `db:"principal_type"`
		PrincipalID   string `db:"principal_id"`
	}{}
	err = app.DB().NewQuery(`
		SELECT CASE [[permission]].[[role]] WHEN 'view' THEN 1 WHEN 'comment' THEN 2
			WHEN 'edit' THEN 3 WHEN 'full_access' THEN 4 ELSE 0 END AS [[rank]],
			[[permission]].[[principal_type]] AS [[principal_type]], [[permission]].[[principal_id]] AS [[principal_id]]
		FROM {{leafen_page_permissions}} [[permission]]
		WHERE ` + principals.condition("[[permission]]", "workspaceId", params) + `
			AND [[permission]].[[page_id]] IN (` + joinSQL(placeholders) + `)
		ORDER BY [[rank]] DESC, CASE [[permission]].[[principal_type]] WHEN 'group' THEN 1 WHEN 'workspace' THEN 2 ELSE 0 END
		LIMIT 1
	`).Bind(params).One(&widest)
	switch {
	case errors.Is(err, sql.ErrNoRows) || widest.Rank < int(role):
		return pageAccessSource{Kind: pageAccessFromLink}, nil
	case err != nil:
		return pageAccessSource{}, err
	case widest.PrincipalType == "group":
		return pageAccessSource{Kind: pageAccessFromGroup, GroupID: widest.PrincipalID}, nil
	case widest.PrincipalType == "workspace":
		return pageAccessSource{Kind: pageAccessFromWorkspace}, nil
	}
	return pageAccessSource{Kind: pageAccessFromPerson}, nil
}
