package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A copied /p link uses native page access, not a public share token. Only
// assert sole access for an owned private page with no other inherited grant.
// Teamspace pages use the restricted-access notice, even with one member:
// their workspace/teamspace roles remain another source of access.
func populatePublicMutationLinkAccess(
	ctx context.Context, app core.App, authorization pageAuthorization, ancestorIDs []string, state *publicMutationState,
) error {
	state.OnlyReaderOpens = false
	owner, teamspace, err := publicMutationPagePlace(ctx, app, authorization, state.WorkspaceID, ancestorIDs)
	if err != nil {
		return err
	}
	if teamspace != "" || owner != authorization.Workspace.PrincipalID {
		return nil
	}
	params := dbx.Params{"workspace": state.WorkspaceID, "reader": authorization.Workspace.PrincipalID}
	keys := make([]string, 0, len(ancestorIDs))
	for i, id := range ancestorIDs {
		key := fmt.Sprintf("ancestor%d", i)
		params[key] = id
		keys = append(keys, "{:"+key+"}")
	}
	var shared bool
	// The roles are found by page; the workspace is read after (+), or
	// SQLite, which keeps no statistics, reads the workspace's every role.
	err = app.DB().NewQuery(`SELECT EXISTS (
  SELECT 1 FROM {{leafen_page_permissions}}
  WHERE +[[workspace_id]]={:workspace} AND [[page_id]] IN (` + joinSQL(keys) + `)
  AND NOT ([[principal_type]]='user' AND [[principal_id]]={:reader})
 )`).WithContext(ctx).Bind(params).Row(&shared)
	if err != nil {
		return err
	}
	if !shared {
		if shared, err = publicMutationOpenOnWeb(ctx, app, keys, params); err != nil {
			return err
		}
	}
	state.OnlyReaderOpens = !shared
	return nil
}

// A page open to the web, by a live link or a published site on it or above
// it, is no longer only its reader's, whoever else is named on it.
func publicMutationOpenOnWeb(ctx context.Context, app core.App, keys []string, params dbx.Params) (bool, error) {
	links := []struct {
		ExpiresAt string `db:"expires_at"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[expires_at]] FROM {{leafen_public_share_links}}
  WHERE [[workspace_id]]={:workspace} AND [[web_enabled]]=1 AND [[root_page_id]] IN (` + joinSQL(keys) + `)`).
		WithContext(ctx).Bind(params).All(&links); err != nil {
		return false, err
	}
	now := time.Now().UTC()
	for _, link := range links {
		if !publicVisibilityExpired(link.ExpiresAt, now) {
			return true, nil
		}
	}
	var published bool
	err := app.DB().NewQuery(`SELECT EXISTS (SELECT 1 FROM {{leafen_public_sites}}
  WHERE [[workspace_id]]={:workspace} AND [[published]]=1 AND [[root_page_id]] IN (` + joinSQL(keys) + `))`).
		WithContext(ctx).Bind(params).Row(&published)
	return published, err
}

// publicMutationPagePlace is whose private page a page is, if anyone's, and
// its teamspace, if any: a database row's are its enclosing ordinary page's,
// including nested databases and wiki rows, read from the already resolved
// ancestor path (the page first).
func publicMutationPagePlace(
	ctx context.Context, app core.App, authorization pageAuthorization, workspaceID string, ancestorIDs []string,
) (owner, teamspace string, err error) {
	owner, teamspace = authorization.Page.GetString(privatePageOwnerField), authorization.Page.GetString("teamspace_id")
	if !isDatabaseRowPageRecord(authorization.Page) || len(ancestorIDs) == 0 {
		return owner, teamspace, nil
	}
	params := dbx.Params{"workspace": workspaceID}
	keys := make([]string, 0, len(ancestorIDs))
	order := "CASE [[id]]"
	for i, id := range ancestorIDs {
		key := fmt.Sprintf("ancestor%d", i)
		params[key] = id
		keys = append(keys, "{:"+key+"}")
		order += fmt.Sprintf(" WHEN {:%s} THEN %d", key, i)
	}
	base := struct {
		Owner     string `db:"private_owner_id"`
		Teamspace string `db:"teamspace_id"`
	}{}
	err = app.DB().NewQuery(`SELECT [[private_owner_id]],[[teamspace_id]] FROM {{leafen_pages}}
	 WHERE [[workspace]]={:workspace} AND [[id]] IN (` + joinSQL(keys) + `) AND [[data_source_id]]=''
	 ORDER BY ` + order + ` END LIMIT 1`).WithContext(ctx).Bind(params).One(&base)
	// An unlinked item's path ends at its own page, whose record carries
	// its enclosing page's owner and teamspace (database_row_unlinked_access.go).
	if errors.Is(err, sql.ErrNoRows) {
		return owner, teamspace, nil
	}
	if err != nil {
		return "", "", err
	}
	return base.Owner, base.Teamspace, nil
}
