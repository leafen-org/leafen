package app

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedVisibleCommentNotificationPage struct {
	ID string `db:"id"`
}

func authorizeCommentNotificationTarget(
	app core.App,
	principalID string,
	pageID string,
) (pageAuthorization, error) {
	if handled, visible, err := systemNoticeSubjectVisible(app, principalID, pageID); handled {
		if err != nil {
			return pageAuthorization{}, err
		}
		if !visible {
			return pageAuthorization{}, permissionDenied()
		}
		return pageAuthorization{}, nil
	}
	// A page an operation holds a while is still theirs to hear of (page_access_hold.go).
	authorization, err := pageAccessDespiteHold(authorizePageAccessRole(
		app, principalID, pageID, pageAccessRoleView,
	))
	if err != nil {
		return pageAuthorization{}, err
	}
	// Notifications require a live page: not one in the trash, nor one
	// under a page there, which went to the trash with it
	// (loadPageTrashEffects).
	if trashed, err := pageInTrashPath(app, pageID); err != nil {
		return pageAuthorization{}, err
	} else if trashed {
		return pageAuthorization{}, permissionDenied()
	}
	return authorization, nil
}

func authorizeCommentNotificationScope(
	app core.App,
	principalID string,
	workspaceID string,
	allowTrashed ...bool,
) (workspaceAuthorization, error) {
	if authorization, err := authorizeWorkspace(
		app, principalID, workspaceID, permissionWorkspaceRead,
	); err == nil {
		return authorization, nil
	} else if !isPermissionDenied(err) {
		return workspaceAuthorization{}, err
	}
	workspace, err := app.FindRecordById(workspacesCollection, workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return workspaceAuthorization{}, permissionDenied()
	}
	if err != nil {
		return workspaceAuthorization{}, err
	}
	authorization, err := authorizePageGrantWorkspace(app, principalID, workspace)
	if err != nil {
		return workspaceAuthorization{}, err
	}
	principals, err := loadPageSharePrincipals(app, workspaceID, principalID)
	if err != nil {
		return workspaceAuthorization{}, err
	}
	// Notifications require a live page. Workspace activity also lists a
	// recoverably trashed page while the page authority still opens it.
	includeTrash := len(allowTrashed) > 0 && allowTrashed[0]
	params := dbx.Params{"workspaceId": workspaceID, "ownerId": authorization.OwnerID, "includeTrash": includeTrash}
	var grantCount int
	err = app.DB().NewQuery(fmt.Sprintf(`
		SELECT COUNT(*) FROM (
			SELECT 1 FROM {{leafen_page_permissions}} [[permission]]
			INDEXED BY [[%s]]
			JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]]=[[permission]].[[page_id]]
			WHERE %s
				AND [[page]].[[workspace]]={:workspaceId} AND [[page]].[[owner]]={:ownerId}
				AND [[page]].[[notion_import_staging]]=0 AND ({:includeTrash}=1 OR [[page]].[[in_trash]]=0)
				AND [[page]].[[deletion_job_id]]=''
			LIMIT 1
		)
	`, pagePermissionActorPageIndex, principals.condition("[[permission]]", "workspaceId", params))).
		Bind(params).Row(&grantCount)
	if err != nil {
		return workspaceAuthorization{}, err
	}
	if grantCount != 1 {
		return workspaceAuthorization{}, permissionDenied()
	}
	return authorization, nil
}

// visibleCommentNotificationPageSet is which of these pages the person's
// inbox still lists notices on: one they can open, in the trash too — Notion's
// inbox keeps a notice whose page went to the trash, and opens that page with
// its trash banner (seen 2026-11-13) — but not one being deleted for good, nor
// one they lost access to.
func visibleCommentNotificationPageSet(
	app core.App,
	principalID string,
	workspaceID string,
	pageIDs []string,
) (map[string]struct{}, error) {
	authorization, err := authorizeCommentNotificationScope(app, principalID, workspaceID, true)
	if err != nil {
		return nil, err
	}
	unique := make([]string, 0, len(pageIDs))
	seen := make(map[string]struct{}, len(pageIDs))
	visible := make(map[string]struct{}, len(pageIDs))
	for _, pageID := range pageIDs {
		if pageID == "" {
			continue
		}
		if _, exists := seen[pageID]; exists {
			continue
		}
		seen[pageID] = struct{}{}
		if handled, shown, err := systemNoticeSubjectVisible(app, principalID, pageID); handled {
			if err != nil {
				return nil, err
			}
			if shown {
				visible[pageID] = struct{}{}
			}
			continue
		}
		unique = append(unique, pageID)
	}
	candidates := make([]string, 0, len(unique))
	for start := 0; start < len(unique); start += commentNotificationInboxPageSetSize {
		end := start + commentNotificationInboxPageSetSize
		if end > len(unique) {
			end = len(unique)
		}
		rows, err := visibleCommentNotificationPageChunk(
			app, authorization, principalID, unique[start:end],
		)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			candidates = append(candidates, row.ID)
		}
	}
	// The pages are judged together, what judging each asks read for all of
	// them at once (primeListedPages): someone shared items one by one has a
	// share's notice on each item's page (2026-11-21: 500 such notices asked
	// a badge's count 24 statements each).
	judge := withAccessMemo(app)
	for start := 0; start < len(candidates); start += listedPagesJudgedTogether {
		batch := candidates[start:min(start+listedPagesJudgedTogether, len(candidates))]
		if _, err := primeListedPages(judge, principalID, batch); err != nil {
			return nil, err
		}
		for _, pageID := range batch {
			// A page an operation holds a while keeps its notices (page_access_hold.go).
			if _, err := pageAccessDespiteHold(authorizePageAccessRole(judge, principalID, pageID, pageAccessRoleView)); err != nil {
				if isPermissionDenied(err) {
					continue
				}
				return nil, err
			}
			visible[pageID] = struct{}{}
		}
	}
	return visible, nil
}

// visibleCommentNotificationPageChunk reads, of a chunk of the pages, those
// the person may still see notices on by where they are: none being deleted
// for good above them, and reached by the workspace or by a share on them or
// above. Each is then judged in full (visibleCommentNotificationPageSet).
func visibleCommentNotificationPageChunk(
	app core.App,
	authorization workspaceAuthorization,
	principalID string,
	pageIDs []string,
) ([]storedVisibleCommentNotificationPage, error) {
	workspaceAccess := 0
	if workspacePageAccessRole(authorization) >= pageAccessRoleView {
		workspaceAccess = 1
	}
	principals, err := loadPageSharePrincipals(app, authorization.WorkspaceID, principalID)
	if err != nil {
		return nil, err
	}
	params := dbx.Params{
		"workspaceId":     authorization.WorkspaceID,
		"ownerId":         authorization.OwnerID,
		"workspaceAccess": workspaceAccess,
	}
	placeholders := make([]string, len(pageIDs))
	for index, pageID := range pageIDs {
		key := fmt.Sprintf("page%d", index)
		params[key] = pageID
		placeholders[index] = "{:" + key + "}"
	}
	rows := []storedVisibleCommentNotificationPage{}
	err = app.DB().NewQuery(`
		WITH RECURSIVE
		[[candidate_pages]] ([[id]], [[parent_id]], [[deletion_job_id]]) AS (
			SELECT [[page]].[[id]], ` + pageAncestryParentSQL("[[page]]") + `, [[page]].[[deletion_job_id]]
			FROM {{leafen_pages}} [[page]]
			WHERE [[page]].[[workspace]]={:workspaceId} AND [[page]].[[owner]]={:ownerId}
				AND [[page]].[[notion_import_staging]]=0
				AND [[page]].[[id]] IN (` + joinSQL(placeholders) + `)
		),
		[[notification_ancestors]] (
			[[root_id]], [[id]], [[parent_id]], [[deletion_job_id]]
		) AS (
			SELECT [[id]], [[id]], [[parent_id]], [[deletion_job_id]]
			FROM [[candidate_pages]]
			UNION
			SELECT [[ancestor]].[[root_id]], [[page]].[[id]], ` + pageAncestryParentSQL("[[page]]") + `,
				[[page]].[[deletion_job_id]]
			FROM {{leafen_pages}} [[page]]
			JOIN [[notification_ancestors]] [[ancestor]]
				ON [[page]].[[id]]=[[ancestor]].[[parent_id]]
			WHERE [[ancestor]].[[parent_id]]!=''
				AND [[page]].[[workspace]]={:workspaceId} AND [[page]].[[owner]]={:ownerId}
		)
		SELECT [[candidate]].[[id]]
		FROM [[candidate_pages]] [[candidate]]
		WHERE NOT EXISTS (
			SELECT 1 FROM [[notification_ancestors]] [[ancestor]]
			WHERE [[ancestor]].[[root_id]]=[[candidate]].[[id]]
				AND [[ancestor]].[[deletion_job_id]]!=''
		)
		AND ({:workspaceAccess}=1 OR EXISTS (
			SELECT 1 FROM {{leafen_page_permissions}} [[permission]]
			INDEXED BY [[idx_leafen_page_permissions_actor_page]]
			JOIN [[notification_ancestors]] [[ancestor]]
				ON [[ancestor]].[[id]]=[[permission]].[[page_id]]
			WHERE [[ancestor]].[[root_id]]=[[candidate]].[[id]]
				AND ` + principals.condition("[[permission]]", "workspaceId", params) + `
		))
		ORDER BY [[candidate]].[[id]]
	`).Bind(params).All(&rows)
	return rows, err
}
