package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

const (
	publicMutationEffectRouting      = "routing"
	publicMutationEffectAudit        = "audit"
	publicMutationEffectNotification = "notification"
)

type storedPublicMutationEffect struct {
	ID          string `db:"id"`
	OperationID string `db:"operation_id"`
	WorkspaceID string `db:"workspace_id"`
	PageID      string `db:"page_id"`
	Kind        string `db:"kind"`
	Status      string `db:"status"`
	Cursor      string `db:"cursor"`
	PayloadJSON string `db:"payload_json"`
	Attempts    int    `db:"attempts"`
	ErrorCode   string `db:"error_code"`
	CreatedAt   int64  `db:"created_at"`
	UpdatedAt   int64  `db:"updated_at"`
}

type publicMutationEffectPayload struct {
	InviteShareID    string `json:"inviteShareId,omitempty"`
	OrganizationID   string `json:"organizationId"`
	OwnerID          string `json:"ownerId"`
	ActorID          string `json:"actorId"`
	Action           string `json:"action"`
	RequestID        string `json:"requestId"`
	LinkID           string `json:"linkId"`
	SourceRevision   int    `json:"sourceRevision"`
	MutationRevision int    `json:"mutationRevision"`
	Active           bool   `json:"active"`
	PrincipalType    string `json:"principalType"`
	PrincipalID      string `json:"principalId"`
	PageTitle        string `json:"pageTitle"`
}

type publicMutationRoutingCursor struct {
	PageID      string `json:"pageId" db:"id"`
	LastChildID string `json:"lastChildId"`
}

type publicMutationRoutingPage struct {
	ID string `db:"id"`
}

func admitPublicMutationEffects(
	app core.App,
	authorization pageAuthorization,
	operation storedPublicMutationOperation,
	input publicMutationInput,
	state storedPublicMutationState,
	now time.Time,
) error {
	payload := publicMutationEffectPayload{
		OrganizationID: authorization.Workspace.Organization.OrganizationID,
		OwnerID:        authorization.Workspace.OwnerID, ActorID: operation.ActorID,
		Action: input.Action, RequestID: input.RequestID, MutationRevision: operation.Revision,
		PrincipalType: input.PrincipalType, PrincipalID: input.PrincipalID,
		PageTitle: strings.TrimSpace(authorization.Page.GetString("title")),
	}
	if state.ShareLinkID != "" {
		share, err := loadPublicMutationShareLink(app, state.ShareLinkID, state.WorkspaceID, state.PageID)
		if err != nil {
			return err
		}
		payload.LinkID, payload.SourceRevision, payload.Active = share.ID, share.SourceRevision, share.Enabled
	}
	if input.Action == publicMutationActionUpdatePermission && input.PrincipalID == "" {
		permission, err := loadPublicMutationPermission(app, state, input.PermissionID)
		if err != nil {
			return err
		}
		payload.PrincipalType, payload.PrincipalID = permission.PrincipalType, permission.PrincipalID
	}
	if input.Action == publicMutationActionGrantPermission && input.PrincipalType == publicMutationPrincipalGroup {
		ids := []string{}
		if err := app.DB().NewQuery(`SELECT [[id]] FROM {{leafen_page_permissions}} WHERE [[page_id]]={:page} AND [[principal_type]]='group' AND [[principal_id]]={:group} AND [[created_at]]={:now} AND [[created_by]]={:actor}`).Bind(dbx.Params{"page": state.PageID, "group": input.PrincipalID, "now": now.UnixMilli(), "actor": operation.ActorID}).Column(&ids); err != nil {
			return err
		}
		if len(ids) == 1 {
			payload.InviteShareID = ids[0]
		}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	kinds := []string{publicMutationEffectAudit}
	if input.Action == publicMutationActionSetWebSharing || input.Action == publicMutationActionRotateShareToken ||
		input.Action == publicMutationActionPublishSite || input.Action == publicMutationActionUnpublishSite {
		kinds = append(kinds, publicMutationEffectRouting)
	}
	if input.Action == publicMutationActionGrantPermission || input.Action == publicMutationActionUpdatePermission ||
		input.Action == publicMutationActionRemovePermission {
		kinds = append(kinds, publicMutationEffectNotification)
	}
	for _, kind := range kinds {
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_public_mutation_effects}}
				([[id]],[[operation_id]],[[workspace_id]],[[page_id]],[[kind]],[[status]],[[cursor]],
				 [[payload_json]],[[attempts]],[[error_code]],[[created_at]],[[updated_at]])
			VALUES ({:id},{:operationId},{:workspaceId},{:pageId},{:kind},'pending','',{:payload},0,'',{:now},{:now})
		`).Bind(dbx.Params{
			"id": security.RandomString(15), "operationId": operation.ID,
			"workspaceId": operation.WorkspaceID, "pageId": operation.PageID,
			"kind": kind, "payload": string(encoded), "now": now.UnixMilli(),
		}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

func loadPublicMutationPermission(
	app core.App, state storedPublicMutationState, permissionID string,
) (storedPublicMutationPermission, error) {
	row := storedPublicMutationPermission{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[principal_type]],[[principal_id]],[[role]] FROM {{leafen_page_permissions}}
		WHERE [[id]]={:id} AND [[workspace_id]]={:workspaceId} AND [[page_id]]={:pageId}
	`).Bind(dbx.Params{
		"id": permissionID, "workspaceId": state.WorkspaceID, "pageId": state.PageID,
	}).One(&row)
	return row, err
}

func runPublicMutationEffectWindow(app core.App, operation storedPublicMutationOperation) error {
	effects := []storedPublicMutationEffect{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[operation_id]],[[workspace_id]],[[page_id]],[[kind]],[[status]],[[cursor]],
			[[payload_json]],[[attempts]],[[error_code]],[[created_at]],[[updated_at]]
		FROM {{leafen_public_mutation_effects}}
		WHERE [[operation_id]]={:operationId} AND [[status]]='pending'
		ORDER BY [[updated_at]],[[id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"operationId": operation.ID, "limit": publicMutationEffectWindow,
	}).All(&effects); err != nil {
		return err
	}
	for _, effect := range effects {
		if err := settlePublicMutationEffect(app, effect); err != nil {
			return err
		}
	}
	return finalizePublicMutationOperation(app, operation.ID)
}

// A sharing change's effects settle without anyone asking: the share graph
// upkeep takes the oldest unsettled effect after each write request and now
// and then, as the change's continue route would. An effect that keeps
// failing waits behind the others and fails for good after a few tries,
// failing its change.
const publicMutationEffectMaxFailures = 8

// advancePublicMutationEffect settles the oldest unsettled effect of any
// sharing change and reports whether it found one.
func advancePublicMutationEffect(ctx context.Context, app core.App) (bool, error) {
	effect := storedPublicMutationEffect{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[operation_id]],[[workspace_id]],[[page_id]],[[kind]],[[status]],[[cursor]],
			[[payload_json]],[[attempts]],[[error_code]],[[created_at]],[[updated_at]]
		FROM {{leafen_public_mutation_effects}} INDEXED BY [[idx_leafen_public_mutation_effects_pending]]
		WHERE [[status]]='pending' ORDER BY [[updated_at]],[[id]] LIMIT 1
	`).WithContext(ctx).One(&effect)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	release := acquireProductOperationLock(
		publicMutationEffectLockNamespace, effect.WorkspaceID+"\x00"+effect.PageID,
	)
	defer release()
	if err := settlePublicMutationEffect(app, effect); err != nil {
		if recorded := recordPublicMutationEffectFailure(app, effect); recorded != nil {
			return true, recorded
		}
		return true, err
	}
	return true, finalizePublicMutationOperation(app, effect.OperationID)
}

// recordPublicMutationEffectFailure counts a failed try of an effect,
// putting it behind the others, and fails it and its change on the last.
func recordPublicMutationEffectFailure(app core.App, effect storedPublicMutationEffect) error {
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_public_mutation_effects}}
		SET [[failures]]=[[failures]]+1,[[updated_at]]={:now},
			[[status]]=CASE WHEN [[failures]]+1 >= {:maximum} THEN 'failed' ELSE [[status]] END,
			[[error_code]]=CASE WHEN [[failures]]+1 >= {:maximum} THEN {:code} ELSE [[error_code]] END
		WHERE [[id]]={:id} AND [[status]]='pending'
	`).Bind(dbx.Params{
		"now": time.Now().UTC().UnixMilli(), "maximum": publicMutationEffectMaxFailures,
		"code": errorCodePublicMutationFailed, "id": effect.ID,
	}).Execute(); err != nil {
		return err
	}
	return finalizePublicMutationOperation(app, effect.OperationID)
}

func settlePublicMutationEffect(app core.App, selected storedPublicMutationEffect) error {
	return app.RunInTransaction(func(txApp core.App) error {
		effect := storedPublicMutationEffect{}
		err := txApp.DB().NewQuery(`
			SELECT [[id]],[[operation_id]],[[workspace_id]],[[page_id]],[[kind]],[[status]],[[cursor]],
				[[payload_json]],[[attempts]],[[error_code]],[[created_at]],[[updated_at]]
			FROM {{leafen_public_mutation_effects}} WHERE [[id]]={:id}
		`).Bind(dbx.Params{"id": selected.ID}).One(&effect)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && effect.Status != publicMutationOperationPending) {
			return nil
		}
		if err != nil {
			return err
		}
		payload := publicMutationEffectPayload{}
		if err := json.Unmarshal([]byte(effect.PayloadJSON), &payload); err != nil {
			return err
		}
		switch effect.Kind {
		case publicMutationEffectRouting:
			return settlePublicMutationRoutingEffect(txApp, effect, payload)
		case publicMutationEffectAudit:
			if err := settlePublicMutationAuditEffect(txApp, effect, payload); err != nil {
				return err
			}
		case publicMutationEffectNotification:
			if payload.InviteShareID != "" {
				return settlePageShareGroupInvite(txApp, effect, payload)
			}
			if err := settlePublicMutationNotificationEffect(txApp, effect, payload); err != nil {
				return err
			}
		default:
			return errPublicMutationFailed
		}
		return completePublicMutationEffect(txApp, effect.ID)
	})
}

func settlePublicMutationRoutingEffect(
	app core.App, effect storedPublicMutationEffect, payload publicMutationEffectPayload,
) error {
	if payload.LinkID == "" {
		return completePublicMutationEffect(app, effect.ID)
	}
	share, err := loadPublicMutationShareLink(app, payload.LinkID, effect.WorkspaceID, effect.PageID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && share.SourceRevision != payload.SourceRevision) {
		return completePublicMutationEffect(app, effect.ID)
	}
	if err != nil {
		return err
	}
	deleted, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_public_share_graph_memberships}} WHERE [[rowid]] IN (
			SELECT [[rowid]] FROM {{leafen_public_share_graph_memberships}}
			WHERE [[share_link_id]]={:linkId} AND [[source_revision]]!={:revision}
			ORDER BY [[rowid]] LIMIT {:limit}
		)
	`).Bind(dbx.Params{
		"linkId": payload.LinkID, "revision": payload.SourceRevision,
		"limit": publicMutationRoutingWindow,
	}).Execute()
	if err != nil {
		return err
	}
	if count, _ := deleted.RowsAffected(); count == publicMutationRoutingWindow {
		return touchPublicMutationEffect(app, effect.ID, effect.Cursor)
	}
	if !payload.Active || !share.Enabled {
		return completePublicMutationEffect(app, effect.ID)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_public_share_graph_memberships}}
			([[share_link_id]],[[root_page_id]],[[page_id]],[[source_revision]],[[state]],[[updated_at]],[[position]])
		VALUES ({:linkId},{:rootPageId},{:rootPageId},{:revision},'building',{:now},'')
		ON CONFLICT ([[share_link_id]],[[page_id]]) DO NOTHING
	`).Bind(dbx.Params{
		"linkId": payload.LinkID, "rootPageId": effect.PageID,
		"revision": payload.SourceRevision, "now": now,
	}).Execute(); err != nil {
		return err
	}
	cursor := publicMutationRoutingCursor{}
	if effect.Cursor != "" && json.Unmarshal([]byte(effect.Cursor), &cursor) != nil {
		return errPublicMutationFailed
	}
	if cursor.PageID == "" {
		err := app.DB().NewQuery(`
			SELECT [[page_id]] AS [[id]] FROM {{leafen_public_share_graph_memberships}}
			WHERE [[share_link_id]]={:linkId} AND [[source_revision]]={:revision} AND [[state]]='building'
			ORDER BY [[position]],[[page_id]] LIMIT 1
		`).Bind(dbx.Params{
			"linkId": payload.LinkID, "revision": payload.SourceRevision,
		}).One(&cursor)
		if errors.Is(err, sql.ErrNoRows) {
			return completePublicMutationEffect(app, effect.ID)
		}
		if err != nil {
			return err
		}
	}
	// The graph takes the pages under each page as the upkeep does: its
	// sub-pages, its wikis' pages and its databases' pages.
	children := []publicMutationRoutingPage{}
	if err := app.DB().NewQuery(publicShareGraphLiveChildrenSQL()).Bind(dbx.Params{
		"workspaceId": effect.WorkspaceID, "ownerId": payload.OwnerID,
		"parentId": cursor.PageID, "afterId": cursor.LastChildID,
		"limit": publicMutationRoutingWindow + 1,
	}).All(&children); err != nil {
		return err
	}
	hasMore := len(children) > publicMutationRoutingWindow
	if hasMore {
		children = children[:publicMutationRoutingWindow]
	}
	for _, child := range children {
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_public_share_graph_memberships}}
				([[share_link_id]],[[root_page_id]],[[page_id]],[[source_revision]],[[state]],[[updated_at]],[[position]])
			VALUES ({:linkId},{:rootPageId},{:pageId},{:revision},'building',{:now},{:position})
			ON CONFLICT ([[share_link_id]],[[page_id]]) DO NOTHING
		`).Bind(dbx.Params{
			"linkId": payload.LinkID, "rootPageId": effect.PageID, "pageId": child.ID,
			"revision": payload.SourceRevision, "now": now, "position": child.ID,
		}).Execute(); err != nil {
			return err
		}
	}
	if hasMore {
		cursor.LastChildID = children[len(children)-1].ID
		return storePublicMutationRoutingCursor(app, effect.ID, cursor)
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_public_share_graph_memberships}} SET [[state]]='complete',[[updated_at]]={:now}
		WHERE [[share_link_id]]={:linkId} AND [[page_id]]={:pageId} AND [[source_revision]]={:revision}
	`).Bind(dbx.Params{
		"now": now, "linkId": payload.LinkID, "pageId": cursor.PageID,
		"revision": payload.SourceRevision,
	}).Execute(); err != nil {
		return err
	}
	return touchPublicMutationEffect(app, effect.ID, "")
}

func storePublicMutationRoutingCursor(
	app core.App, effectID string, cursor publicMutationRoutingCursor,
) error {
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return err
	}
	return touchPublicMutationEffect(app, effectID, string(encoded))
}

func touchPublicMutationEffect(app core.App, effectID, cursor string) error {
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_public_mutation_effects}}
		SET [[cursor]]={:cursor},[[attempts]]=[[attempts]]+1,[[updated_at]]={:now}
		WHERE [[id]]={:id} AND [[status]]='pending'
	`).Bind(dbx.Params{"cursor": cursor, "now": time.Now().UTC().UnixMilli(), "id": effectID}).Execute()
	return err
}

func settlePublicMutationAuditEffect(
	app core.App, effect storedPublicMutationEffect, payload publicMutationEffectPayload,
) error {
	return insertOrganizationAuditEvent(app, organizationAuditEvent{
		ID: effect.ID, OrganizationID: payload.OrganizationID, WorkspaceID: effect.WorkspaceID,
		ActorID: payload.ActorID, Action: "public_mutation." + payload.Action,
		TargetType: "page", TargetID: effect.PageID,
		Metadata:   map[string]any{"requestId": payload.RequestID, "operationId": effect.OperationID},
		OccurredAt: effect.CreatedAt,
	})
}

func settlePublicMutationNotificationEffect(
	app core.App, effect storedPublicMutationEffect, payload publicMutationEffectPayload,
) error {
	recipients := []string{}
	if payload.PrincipalType == publicMutationPrincipalUser && payload.PrincipalID != payload.ActorID {
		recipients = append(recipients, payload.PrincipalID)
	}
	fixed, err := notificationRecipientJSON(recipients)
	if err != nil {
		return err
	}
	title := payload.PageTitle
	if title == "" {
		title = "Untitled"
	}
	created := time.UnixMilli(effect.CreatedAt).UTC().Format(time.RFC3339Nano)
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_notification_events}}
			([[id]],[[workspace_id]],[[source_kind]],[[source_id]],[[source_revision]],[[actor_id]],
			 [[page_id]],[[block_id]],[[parent_comment_id]],[[title]],[[preview]],[[target]],
			 [[mention_recipient_ids_json]],[[fixed_recipient_ids_json]],[[include_page_audience]],
			 [[audience_at]],[[status]],[[created_at]],[[updated_at]])
		VALUES ({:id},{:workspaceId},'share',{:sourceId},{:revision},{:actorId},{:pageId},'','',
			{:title},{:preview},{:target},'[]',{:recipients},0,{:audienceAt},'pending',{:created},{:created})
		ON CONFLICT ([[workspace_id]],[[source_kind]],[[source_id]],[[source_revision]]) DO NOTHING
	`).Bind(dbx.Params{
		"id":          commentNotificationEventID(effect.WorkspaceID, effect.OperationID, payload.MutationRevision),
		"workspaceId": effect.WorkspaceID, "sourceId": effect.OperationID,
		"revision": payload.MutationRevision, "actorId": payload.ActorID,
		"pageId": effect.PageID, "title": notificationEventTitle(title), "preview": "Sharing settings changed",
		"target": "/p/" + url.PathEscape(effect.PageID), "recipients": fixed,
		"audienceAt": effect.CreatedAt, "created": created,
	}).Execute()
	if err == nil {
		notifyCommentNotificationMaterializationAfterCommit(app)
	}
	return err
}

func completePublicMutationEffect(app core.App, effectID string) error {
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_public_mutation_effects}}
		SET [[status]]='completed',[[cursor]]='',[[error_code]]='',[[updated_at]]={:now}
		WHERE [[id]]={:id} AND [[status]]='pending'
	`).Bind(dbx.Params{"now": time.Now().UTC().UnixMilli(), "id": effectID}).Execute()
	return err
}
