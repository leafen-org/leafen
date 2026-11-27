package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// notionUnreachableBlockTarget is Notion's refusal of a link_to_page or
// synced_block whose page, database or source block the integration cannot
// reach or that doesn't exist (seen 2026-11-10, Notion-Version 2026-03-11).
const notionUnreachableBlockTarget = "A synced or linked block can only point to a block in the same workspace that the integration can reach"

// notionLinkNotPage and notionLinkNotDatabasePage are Notion's refusals of a
// link_to_page whose page_id names a database, and whose database_id names
// an inline database or a page — a database page too (seen 2026-11-13,
// Notion-Version 2026-03-11). A database page itself (a row) is a page_id's,
// and a database on a page of its own a database_id's.
const (
	notionLinkNotPage         = "page_id must reference a page"
	notionLinkNotDatabasePage = "database_id must name a database on a page of its own"
)

// notionNotOriginalSyncedBlock is Notion's refusal of a synced_block whose
// source is no original synced block (seen 2026-11-10).
const notionNotOriginalSyncedBlock = "A synced block's source must be an original synced_block, not one that syncs from another block"

func notionBlockReferencesBeforeApply(
	identity notionOAuthBearerIdentity,
) func(core.App, pageAuthorization, *crdt.Doc, []graphOperation) error {
	return func(
		app core.App,
		current pageAuthorization,
		_ *crdt.Doc,
		operations []graphOperation,
	) error {
		if err := authorizeNotionLinkToPageOperations(app, identity, current, operations); err != nil {
			return err
		}
		if err := authorizeNotionSyncedBlockOperations(app, identity, current, operations); err != nil {
			return err
		}
		return authorizeNotionBlockCustomEmojiOperations(app, identity, current, operations)
	}
}

// notionRequestTargetRefused is what Notion's API refuses new blocks for
// before the faults in their content (notion_block_content_faults.go): a
// link's or a synced block's target it can't reach. Leafen reaches those as
// the blocks are written; where the request's content has faults, the
// targets of its blocks planned are reached first, on a copy.
func notionRequestTargetRefused(
	app core.App,
	identity notionOAuthBearerIdentity,
	current pageAuthorization,
	operations []graphOperation,
) error {
	copied, err := copiedGraphOperationsHere(app, current.Workspace.WorkspaceID, operations)
	if err != nil {
		return err
	}
	if err := authorizeNotionLinkToPageOperations(app, identity, current, copied); err != nil {
		return err
	}
	return authorizeNotionSyncedBlockOperations(app, identity, current, copied)
}

func authorizeNotionSyncedBlockOperations(
	app core.App,
	identity notionOAuthBearerIdentity,
	current pageAuthorization,
	operations []graphOperation,
) error {
	// The page of each source the request may name, "" when it may not.
	checked := map[string]string{}
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertBlock || operation.Block == nil ||
			operation.Block.Type != graphBlockTypeSyncedBlock {
			continue
		}
		content := struct {
			SourceID string `json:"syncedBlockId"`
		}{}
		if json.Unmarshal(operation.Block.Content, &content) != nil {
			return graphInputError(errorCodeGraphReferenceInvalid, "The synced block source is invalid.")
		}
		if strings.TrimSpace(content.SourceID) == "" {
			continue
		}
		sourceID, ok := resolveNotionBlockID(app, content.SourceID)
		if !ok || sourceID == operation.Block.ID {
			return graphInputError(errorCodeGraphReferenceInvalid, "The synced block source is invalid.")
		}
		sourcePageID, found := checked[sourceID]
		if !found {
			source, exists, err := loadStoredNotionBlock(app, sourceID)
			if err != nil {
				return err
			}
			reaches := func(pageID string) (bool, error) {
				page, pageErr := authorizePage(app, identity.PrincipalID, pageID, permissionWorkspaceRead)
				if pageErr != nil && !isPermissionDenied(pageErr) {
					return false, pageErr
				}
				return pageErr == nil && !page.Page.GetBool("in_trash") && page.Workspace.WorkspaceID == current.Workspace.WorkspaceID &&
					notionOAuthIdentityAllowsPage(app, identity, page), nil
			}
			if exists && source.Owner == current.Workspace.OwnerID &&
				source.WorkspaceID == current.Workspace.WorkspaceID {
				reached, err := reaches(source.PageID)
				if err != nil {
					return err
				}
				if reached {
					sourcePageID = source.PageID
				}
				// Only an original synced block is synced from, as Notion's
				// API answers (seen 2026-11-10): a block of another kind, or
				// one itself syncing from another, is refused.
				stored := struct {
					SourceID string `json:"syncedBlockId"`
				}{}
				_ = json.Unmarshal([]byte(source.Content), &stored)
				if reached && (source.Type != graphBlockTypeSyncedBlock || stored.SourceID != "") {
					return graphInputError(errorCodeGraphReferenceInvalid, notionNotOriginalSyncedBlock)
				}
			} else if pageID, pageErr := resolvePageOwnPublicID(content.SourceID); !exists && pageErr == nil {
				// So is a page.
				if reached, err := reaches(pageID); err != nil {
					return err
				} else if reached {
					return graphInputError(errorCodeGraphReferenceInvalid, notionNotOriginalSyncedBlock)
				}
			}
			checked[sourceID] = sourcePageID
		}
		if sourcePageID == "" {
			return graphInputError(errorCodeGraphReferenceInvalid, notionUnreachableBlockTarget)
		}
		// The copy names its original by its native ID and the page it is on,
		// as one pasted in the editor does: the page shows the original from
		// there (synced_block_duplication.go).
		stored := map[string]any{}
		if json.Unmarshal(operation.Block.Content, &stored) != nil {
			return graphInputError(errorCodeGraphReferenceInvalid, "The synced block source is invalid.")
		}
		stored["syncedBlockId"], stored["syncedPageId"] = sourceID, sourcePageID
		encoded, err := json.Marshal(stored)
		if err != nil {
			return err
		}
		operation.Block.Content = encoded
	}
	return nil
}

// notionLinkTargetUncheckedKey marks, until it is written, a link whose
// target isn't checked: a new one without its type, which Notion's API links
// to whatever it names, nothing too, and a changed one, which keeps its
// target (seen 2026-11-10). Every other link written has its target checked.
// A database or a comment named by its API ID is kept by its own ID either
// way, when found.
const notionLinkTargetUncheckedKey = "notionLinkTargetUnchecked"

func authorizeNotionLinkToPageOperations(
	app core.App,
	identity notionOAuthBearerIdentity,
	current pageAuthorization,
	operations []graphOperation,
) error {
	checked := map[string]bool{}
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertBlock || operation.Block == nil ||
			operation.Block.Type != graphBlockTypeLinkToPage {
			continue
		}
		stored := map[string]any{}
		if json.Unmarshal(operation.Block.Content, &stored) != nil {
			return graphInputError(errorCodeGraphReferenceInvalid, "The linked page or database is invalid.")
		}
		unchecked, _ := stored[notionLinkTargetUncheckedKey].(bool)
		delete(stored, notionLinkTargetUncheckedKey)
		check := !unchecked
		target, err := notionLinkTargetOf(app, identity, current, stored, check, checked)
		if err != nil {
			return err
		}
		if target != nil {
			stored = target
		}
		encoded, err := json.Marshal(stored)
		if err != nil {
			return err
		}
		operation.Block.Content = encoded
	}
	return nil
}

// notionLinkTargetOf is a link's content with its database or comment named
// by its own ID (nil to keep it as it is), its target checked when check is
// set; allowed keeps what was checked.
func notionLinkTargetOf(
	app core.App,
	identity notionOAuthBearerIdentity,
	current pageAuthorization,
	stored map[string]any,
	check bool,
	allowed map[string]bool,
) (map[string]any, error) {
	text := func(key string) string {
		value, _ := stored[key].(string)
		return strings.TrimSpace(value)
	}
	pageID, databaseID := text("childPageId"), text("databaseContainerId")
	commentID, publicCommentID := text("commentId"), text("notionCommentId")
	if countNotionLinkTargets(pageID != "", databaseID != "", commentID != "", publicCommentID != "") != 1 {
		if !check {
			// A link kept as it is, as a link changed is.
			return nil, nil
		}
		return nil, graphInputError(errorCodeGraphReferenceInvalid, "The linked page or database is invalid.")
	}
	unreachable := graphInputError(errorCodeGraphReferenceInvalid, notionUnreachableBlockTarget)
	switch {
	case publicCommentID != "":
		identityRow, err := resolveNotionObjectIdentity(app, notionObjectIdentityComment, publicCommentID)
		if err != nil && !errors.Is(err, errInvalidNotionObjectIdentity) {
			return nil, err
		}
		found := err == nil && identityRow.Workspace == current.Workspace.WorkspaceID
		if found && check {
			found, err = authorizeNotionLinkCommentTarget(app, identity, current, identityRow.InternalID)
			if err != nil {
				return nil, err
			}
		}
		if !found && check {
			return nil, graphInputError(errorCodeGraphReferenceInvalid, "comment_id must name a reachable comment")
		}
		if found {
			return map[string]any{"commentId": identityRow.InternalID}, nil
		}
		return nil, nil
	case commentID != "":
		if !check {
			return nil, nil
		}
		ok, seen := allowed["comment:"+commentID]
		if !seen {
			var err error
			if ok, err = authorizeNotionLinkCommentTarget(app, identity, current, commentID); err != nil {
				return nil, err
			}
			allowed["comment:"+commentID] = ok
		}
		if !ok {
			return nil, graphInputError(errorCodeGraphReferenceInvalid, "comment_id must name a reachable comment")
		}
		return nil, nil
	case databaseID != "":
		if normalized, ok := normalizeNotionUUIDRequest(databaseID); !ok || normalized != databaseID {
			return nil, graphInputError(errorCodeGraphReferenceInvalid, "The linked database is invalid.")
		}
		// The database as the Public API names it, or by its own ID, as a
		// database mention takes it; the block keeps its own ID.
		containerID, found, err := (&notionMentionPreparation{app: app, current: current}).databaseContainerID(databaseID)
		if err != nil {
			return nil, err
		}
		if found && check {
			ok, seen := allowed["database:"+containerID]
			if !seen {
				// An inline database the identity reaches is refused, in the
				// trash or not (seen 2026-11-12 and 2026-11-13).
				reached, err := authorizeNotionDatabaseMentionTarget(app, identity, current, containerID)
				if err != nil {
					return nil, err
				}
				if own, err := notionDatabaseOnOwnPage(app, containerID); err != nil {
					return nil, err
				} else if reached && !own {
					return nil, graphInputError(errorCodeGraphReferenceInvalid, notionLinkNotDatabasePage)
				}
				if ok, err = authorizeNotionLinkDatabaseTarget(app, identity, current, containerID); err != nil {
					return nil, err
				}
				allowed["database:"+containerID] = ok
			}
			found = ok
		}
		if !found && check {
			if page, err := notionLinkPageTarget(app, identity, current, notionPageMentionRowPrefix+databaseID, allowed); err != nil {
				return nil, err
			} else if page != "" {
				return nil, graphInputError(errorCodeGraphReferenceInvalid, notionLinkNotDatabasePage)
			}
			return nil, unreachable
		}
		if found && containerID != databaseID {
			return map[string]any{"databaseContainerId": containerID}, nil
		}
		return nil, nil
	}
	// One named by a database page's public ID links its page by the page's
	// own ID; a link to nothing keeps the ID it was given.
	var target map[string]any
	if written, pending := strings.CutPrefix(pageID, notionPageMentionRowPrefix); pending {
		resolved, found := resolveNotionPageMentionID(app, pageID)
		switch {
		case found:
			pageID, target = resolved, map[string]any{"childPageId": resolved}
		case !check:
			return map[string]any{"childPageId": written}, nil
		default:
			if _, database, err := (&notionMentionPreparation{app: app, current: current}).databaseContainerID(written); err != nil {
				return nil, err
			} else if database {
				return nil, graphInputError(errorCodeGraphReferenceInvalid, notionLinkNotPage)
			}
			return nil, unreachable
		}
	}
	if !check {
		return target, nil
	}
	if page, err := notionLinkPageTarget(app, identity, current, pageID, allowed); err != nil || page == "" {
		if err == nil {
			err = unreachable
		}
		return nil, err
	}
	return target, nil
}

// notionLinkPageTarget is the page a link's page ID names — its own, or a
// database page's public ID behind notionPageMentionRowPrefix — when the
// identity may reach it in the workspace; "" for none. allowed keeps what
// was checked.
func notionLinkPageTarget(
	app core.App,
	identity notionOAuthBearerIdentity,
	current pageAuthorization,
	pageID string,
	allowed map[string]bool,
) (string, error) {
	if strings.HasPrefix(pageID, notionPageMentionRowPrefix) {
		resolved, found := resolveNotionPageMentionID(app, pageID)
		if !found {
			own, err := resolvePageOwnPublicID(strings.TrimPrefix(pageID, notionPageMentionRowPrefix))
			if err != nil {
				return "", nil
			}
			resolved = own
		}
		pageID = resolved
	}
	if !validPageRecordID(pageID) {
		return "", nil
	}
	ok, seen := allowed["page:"+pageID]
	if !seen {
		target, err := authorizePage(app, identity.PrincipalID, pageID, permissionWorkspaceRead)
		if err != nil && !isPermissionDenied(err) {
			return "", err
		}
		ok = err == nil && target.Workspace.OwnerID == current.Workspace.OwnerID &&
			target.Workspace.WorkspaceID == current.Workspace.WorkspaceID &&
			notionOAuthIdentityAllowsPage(app, identity, target)
		allowed["page:"+pageID] = ok
	}
	if !ok {
		return "", nil
	}
	return pageID, nil
}

// notionDatabaseOnOwnPage is whether a database is on a page of its own,
// Notion's database page, rather than inline on another page.
func notionDatabaseOnOwnPage(app core.App, containerID string) (bool, error) {
	var kind string
	err := app.DB().NewQuery(`
		SELECT [[page]].[[kind]] FROM {{leafen_database_containers}} AS [[container]]
		JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[id]] = [[container]].[[page_id]]
		WHERE [[container]].[[id]] = {:id} LIMIT 1
	`).Bind(dbx.Params{"id": containerID}).Row(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return kind == pageKindDatabase, err
}

func projectNotionLinkToPageContent(
	app core.App,
	authorization notionBlockAuthorization,
	content map[string]any,
) map[string]any {
	commentID, _ := content["commentId"].(string)
	if strings.TrimSpace(commentID) != "" {
		allowed, err := authorizeNotionLinkCommentTarget(
			app, authorization.Identity, authorization.Page, commentID,
		)
		if err != nil || !allowed {
			return content
		}
		publicID, err := notionObjectIdentityPublicID(
			app, notionObjectIdentityComment, authorization.Page.Workspace.WorkspaceID, commentID,
		)
		if err != nil {
			return content
		}
		return map[string]any{"type": "comment_id", "comment_id": publicID}
	}
	databaseID, _ := content["databaseContainerId"].(string)
	if strings.TrimSpace(databaseID) != "" {
		if normalized, ok := normalizeNotionUUIDRequest(databaseID); !ok || normalized != databaseID {
			return content
		}
		allowed, err := authorizeNotionLinkDatabaseTarget(
			app, authorization.Identity, authorization.Page, databaseID,
		)
		if err != nil || !allowed {
			return content
		}
		publicID, err := ensureNotionObjectIdentity(app, notionObjectIdentityDatabase, authorization.Page.Workspace.WorkspaceID, databaseID)
		if err != nil {
			return content
		}
		return map[string]any{"type": "database_id", "database_id": publicID}
	}
	pageID, _ := content["childPageId"].(string)
	if !validPageRecordID(strings.TrimSpace(pageID)) {
		return content
	}
	target, err := authorizePage(
		app, authorization.Identity.PrincipalID, pageID, permissionWorkspaceRead,
	)
	if err != nil || target.Workspace.OwnerID != authorization.Page.Workspace.OwnerID ||
		target.Workspace.WorkspaceID != authorization.Page.Workspace.WorkspaceID ||
		!notionOAuthIdentityAllowsPage(app, authorization.Identity, target) {
		return content
	}
	publicPageID, err := notionPagePublicID(app, pageID)
	if err != nil {
		return content
	}
	return map[string]any{"type": "page_id", "page_id": publicPageID}
}

func authorizeNotionLinkCommentTarget(
	app core.App,
	identity notionOAuthBearerIdentity,
	current pageAuthorization,
	commentID string,
) (bool, error) {
	if !validCommentIdentifier(commentID) {
		return false, nil
	}
	target := struct {
		WorkspaceID string `db:"workspace_id"`
		PageID      string `db:"page_id"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[workspace_id]],[[page_id]] FROM {{leafen_comments}}
		WHERE [[id]]={:commentId} LIMIT 2
	`).Bind(dbx.Params{"commentId": commentID}).One(&target)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if target.WorkspaceID != current.Workspace.WorkspaceID {
		return false, nil
	}
	page, err := authorizePage(app, identity.PrincipalID, target.PageID, permissionWorkspaceRead)
	if isPermissionDenied(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !page.Page.GetBool("in_trash") &&
		page.Workspace.OwnerID == current.Workspace.OwnerID &&
		page.Workspace.WorkspaceID == current.Workspace.WorkspaceID &&
		notionOAuthIdentityAllowsPage(app, identity, page), nil
}

func countNotionLinkTargets(values ...bool) int {
	result := 0
	for _, value := range values {
		if value {
			result++
		}
	}
	return result
}

type notionLinkDatabaseTarget struct {
	OwnerID     string `db:"owner"`
	WorkspaceID string `db:"workspace_id"`
	PageID      string `db:"page_id"`
}

// authorizeNotionLinkDatabaseTarget is whether a link_to_page may name a
// database: one of the workspace, out of the trash, on a page the identity
// may read.
func authorizeNotionLinkDatabaseTarget(
	app core.App,
	identity notionOAuthBearerIdentity,
	current pageAuthorization,
	containerID string,
) (bool, error) {
	return reachNotionDatabaseTarget(app, identity, current, containerID, false)
}

// authorizeNotionDatabaseMentionTarget is whether a database mention reaches
// its database: one of the workspace on a page the identity may read, in
// the trash or on a page in it too, as Notion's API and its "v1" Markdown
// name a mention of a database in the trash by its kind and name, written
// before it went there or after (seen 2026-11-13; "v2" by its address
// alone, notionMarkdownMentions.InTrash).
func authorizeNotionDatabaseMentionTarget(
	app core.App,
	identity notionOAuthBearerIdentity,
	current pageAuthorization,
	containerID string,
) (bool, error) {
	return reachNotionDatabaseTarget(app, identity, current, containerID, true)
}

func reachNotionDatabaseTarget(
	app core.App,
	identity notionOAuthBearerIdentity,
	current pageAuthorization,
	containerID string,
	trashToo bool,
) (bool, error) {
	target := notionLinkDatabaseTarget{}
	if trashToo {
		database, found, err := loadNotionMentionedDatabase(app, containerID)
		if err != nil || !found {
			return false, err
		}
		target = notionLinkDatabaseTarget{OwnerID: database.Owner, WorkspaceID: database.WorkspaceID, PageID: database.PageID}
	} else if err := app.DB().NewQuery(`
		SELECT [[container]].[[owner]], [[container]].[[workspace_id]], [[container]].[[page_id]]
		FROM {{leafen_database_containers}} AS [[container]]
		WHERE [[container]].[[id]]={:containerId} AND [[container]].[[kind]]='source'
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_database_container_trash}} AS [[trash]]
				WHERE [[trash]].[[container_id]]=[[container]].[[id]]
			)
			AND EXISTS (
				SELECT 1 FROM {{leafen_database_container_sources}} AS [[membership]]
				WHERE [[membership]].[[container_id]]=[[container]].[[id]]
			)
		LIMIT 2
	`).Bind(dbx.Params{"containerId": containerID}).One(&target); errors.Is(err, sql.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if target.OwnerID != current.Workspace.OwnerID ||
		target.WorkspaceID != current.Workspace.WorkspaceID {
		return false, nil
	}
	page, err := authorizePage(app, identity.PrincipalID, target.PageID, permissionWorkspaceRead)
	if isPermissionDenied(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return (trashToo || !page.Page.GetBool("in_trash")) &&
		page.Workspace.OwnerID == target.OwnerID &&
		page.Workspace.WorkspaceID == target.WorkspaceID &&
		notionOAuthIdentityAllowsPage(app, identity, page), nil
}

// notionDatabaseMentionTitle is the name a database mention written by the
// identity reads with, false for a database it doesn't reach
// (authorizeNotionDatabaseMentionTarget).
func notionDatabaseMentionTitle(
	app core.App,
	identity notionOAuthBearerIdentity,
	current pageAuthorization,
	containerID string,
) (string, bool, error) {
	allowed, err := authorizeNotionDatabaseMentionTarget(app, identity, current, containerID)
	if err != nil || !allowed {
		return "", false, err
	}
	return databaseContainerTitle(app, containerID)
}
