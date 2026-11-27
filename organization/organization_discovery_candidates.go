package organization

import (
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

const (
	organizationDiscoveryEntityPages    = "pages"
	organizationDiscoveryEntityBlocks   = "blocks"
	organizationDiscoveryEntityComments = "comments"
	organizationDiscoveryEntityFiles    = "files"
)

var organizationDiscoveryEntities = []string{
	organizationDiscoveryEntityPages,
	organizationDiscoveryEntityBlocks,
	organizationDiscoveryEntityComments,
	organizationDiscoveryEntityFiles,
}

type discoveryItem struct {
	Type             string         `json:"type"`
	ID               string         `json:"id"`
	OrganizationID   string         `json:"organizationId"`
	WorkspaceID      string         `json:"workspaceId"`
	WorkspaceName    string         `json:"workspaceName"`
	PageID           string         `json:"pageId,omitempty"`
	CustodianUserIDs []string       `json:"custodianUserIds"`
	CreatedAt        string         `json:"createdAt,omitempty"`
	UpdatedAt        string         `json:"updatedAt,omitempty"`
	Content          map[string]any `json:"content"`
}

type organizationDiscoveryPageCandidate struct {
	ID           string `db:"id"`
	Title        string `db:"title"`
	Content      string `db:"content"`
	InTrash      bool   `db:"in_trash"`
	CreatedBy    string `db:"created_by"`
	LastEditedBy string `db:"last_edited_by"`
	Created      string `db:"created"`
	Updated      string `db:"updated"`
}

type organizationDiscoveryBlockCandidate struct {
	ID        string `db:"id"`
	PageID    string `db:"page_id"`
	Type      string `db:"type"`
	Content   string `db:"content"`
	CreatedBy string `db:"created_by"`
	Created   int64  `db:"created"`
	Updated   int64  `db:"updated"`
}

type organizationDiscoveryCommentCandidate struct {
	ID         string `db:"id"`
	PageID     string `db:"page_id"`
	TargetKind string `db:"target_kind"`
	TargetID   string `db:"target_id"`
	BlockID    string `db:"block_id"`
	AuthorID   string `db:"author_id"`
	BodyJSON   string `db:"body_json"`
	CreatedAt  string `db:"created_at"`
	UpdatedAt  string `db:"updated_at"`
}

type organizationDiscoveryFileCandidate struct {
	ID          string `db:"id"`
	PageID      string `db:"page_id"`
	Name        string `db:"original_name"`
	ContentType string `db:"content_type"`
	Size        int64  `db:"size"`
	CreatedBy   string `db:"created_by"`
	Created     string `db:"created"`
	Updated     string `db:"updated"`
}

func nextOrganizationDiscoveryWindow(
	app core.App, payload organizationDiscoveryExportJobPayload,
) ([]discoveryItem, organizationDiscoveryExportJobPayload, bool, error) {
	workspace, found, err := organizationDiscoveryCurrentWorkspace(app, payload)
	if err != nil || !found {
		return nil, payload, !found, err
	}
	payload.WorkspaceID, payload.WorkspaceName = workspace.ID, workspace.Name
	items, rawCount, after, err := readOrganizationDiscoveryCandidates(app, payload, workspace)
	if err != nil {
		return nil, payload, false, err
	}
	if rawCount == leafen.OrganizationDiscoveryItemsPerTurn {
		payload.EntityCursor = after
		return items, payload, false, nil
	}
	entityIndex := organizationDiscoveryEntityIndex(payload.Entity)
	if entityIndex < 0 {
		return nil, payload, false, errOrganizationDiscoveryUnavailable
	}
	if entityIndex+1 < len(organizationDiscoveryEntities) {
		payload.Entity = organizationDiscoveryEntities[entityIndex+1]
		payload.EntityCursor = ""
		return items, payload, false, nil
	}
	payload.WorkspaceIndex++
	if len(payload.Filter.WorkspaceIDs) == 0 {
		payload.WorkspaceAfter = workspace.ID
	}
	payload.WorkspaceID, payload.WorkspaceName = "", ""
	payload.Entity, payload.EntityCursor = organizationDiscoveryEntityPages, ""
	return items, payload, false, nil
}

func organizationDiscoveryCurrentWorkspace(
	app core.App, payload organizationDiscoveryExportJobPayload,
) (organizationDiscoveryWorkspace, bool, error) {
	row := organizationDiscoveryWorkspace{}
	if payload.WorkspaceID != "" {
		err := app.DB().NewQuery(`
			SELECT [[id]],[[name]] FROM {{leafen_workspaces}}
			WHERE [[id]]={:workspaceId} AND [[organization]]={:organizationId}
		`).Bind(dbx.Params{
			"workspaceId": payload.WorkspaceID, "organizationId": payload.OrganizationID,
		}).One(&row)
		if errors.Is(err, sql.ErrNoRows) {
			return row, false, errOrganizationDiscoverySourceChanged
		}
		return row, err == nil, err
	}
	if len(payload.Filter.WorkspaceIDs) > 0 {
		if payload.WorkspaceIndex >= len(payload.Filter.WorkspaceIDs) {
			return row, false, nil
		}
		err := app.DB().NewQuery(`
			SELECT [[id]],[[name]] FROM {{leafen_workspaces}}
			WHERE [[id]]={:workspaceId} AND [[organization]]={:organizationId}
		`).Bind(dbx.Params{
			"workspaceId":    payload.Filter.WorkspaceIDs[payload.WorkspaceIndex],
			"organizationId": payload.OrganizationID,
		}).One(&row)
		if errors.Is(err, sql.ErrNoRows) {
			return row, false, errOrganizationDiscoverySourceChanged
		}
		return row, err == nil, err
	}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[name]] FROM {{leafen_workspaces}}
		WHERE [[organization]]={:organizationId} AND [[id]]>{:after}
		ORDER BY [[id]] LIMIT 1
	`).Bind(dbx.Params{
		"organizationId": payload.OrganizationID, "after": payload.WorkspaceAfter,
	}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return row, false, nil
	}
	return row, err == nil, err
}

func readOrganizationDiscoveryCandidates(
	app core.App,
	payload organizationDiscoveryExportJobPayload,
	workspace organizationDiscoveryWorkspace,
) ([]discoveryItem, int, string, error) {
	switch payload.Entity {
	case organizationDiscoveryEntityPages:
		return readOrganizationDiscoveryPages(app, payload, workspace)
	case organizationDiscoveryEntityBlocks:
		return readOrganizationDiscoveryBlocks(app, payload, workspace)
	case organizationDiscoveryEntityComments:
		return readOrganizationDiscoveryComments(app, payload, workspace)
	case organizationDiscoveryEntityFiles:
		return readOrganizationDiscoveryFiles(app, payload, workspace)
	default:
		return nil, 0, "", errOrganizationDiscoveryUnavailable
	}
}

func readOrganizationDiscoveryPages(
	app core.App, payload organizationDiscoveryExportJobPayload, workspace organizationDiscoveryWorkspace,
) ([]discoveryItem, int, string, error) {
	rows := []organizationDiscoveryPageCandidate{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[title]],[[content]],[[in_trash]],[[created_by]],[[last_edited_by]],[[created]],[[updated]]
		FROM {{leafen_pages}} WHERE [[workspace]]={:workspaceId} AND [[data_source_id]]='' AND [[id]]>{:after}
		ORDER BY [[id]] LIMIT {:limit}
	`).Bind(discoveryCandidateParams(payload, workspace)).All(&rows)
	if err != nil {
		return nil, 0, "", err
	}
	items := make([]discoveryItem, 0, len(rows))
	for _, row := range rows {
		if !payload.Filter.IncludeTrashed && row.InTrash {
			continue
		}
		item := discoveryItem{
			Type: "page", ID: row.ID, OrganizationID: payload.OrganizationID,
			WorkspaceID: workspace.ID, WorkspaceName: workspace.Name, PageID: row.ID,
			CustodianUserIDs: discoveryCustodians(row.CreatedBy, row.LastEditedBy),
			CreatedAt:        discoveryDateTime(row.Created), UpdatedAt: discoveryDateTime(row.Updated),
			Content: map[string]any{"title": row.Title, "kind": "page", "inTrash": row.InTrash, "body": row.Content},
		}
		if organizationDiscoveryMatches(item, payload.Filter) {
			items = append(items, item)
		}
	}
	return items, len(rows), discoveryLastPageID(rows), nil
}

func readOrganizationDiscoveryBlocks(
	app core.App, payload organizationDiscoveryExportJobPayload, workspace organizationDiscoveryWorkspace,
) ([]discoveryItem, int, string, error) {
	rows := []organizationDiscoveryBlockCandidate{}
	err := app.DB().NewQuery(`
		SELECT [[block]].[[id]],[[block]].[[page_id]],[[block]].[[type]],[[block]].[[content]],
			[[block]].[[created_by]],[[block]].[[created]],[[block]].[[updated]]
		FROM {{leafen_blocks}} [[block]] JOIN {{leafen_pages}} [[page]]
			ON [[page]].[[id]]=[[block]].[[page_id]] AND [[page]].[[workspace]]=[[block]].[[workspace_id]]
		WHERE [[block]].[[workspace_id]]={:workspaceId} AND [[block]].[[id]]>{:after}
			AND ({:includeTrashed}=1 OR [[page]].[[in_trash]]=0)
		ORDER BY [[block]].[[id]] LIMIT {:limit}
	`).Bind(discoveryCandidateParams(payload, workspace)).All(&rows)
	if err != nil {
		return nil, 0, "", err
	}
	pageIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.Type == "paragraph" {
			pageIDs = append(pageIDs, row.PageID)
		}
	}
	storedDocuments, err := leafen.LoadMaterializedYgoDocumentsBounded(
		app, pageIDs, leafen.OrganizationDiscoveryItemsPerTurn, leafen.OrganizationDiscoveryMaximumBytes,
	)
	if errors.Is(err, leafen.ErrYgoMaterializedBatchLimit) {
		return nil, 0, "", errOrganizationDiscoveryTooLarge
	}
	if err != nil {
		return nil, 0, "", err
	}
	documents := make(map[string]*crdt.Doc, len(storedDocuments))
	for pageID, stored := range storedDocuments {
		document := crdt.New()
		if err := crdt.ApplyUpdateV1(document, stored.State, nil); err != nil {
			return nil, 0, "", err
		}
		documents[pageID] = document
	}
	items := make([]discoveryItem, 0, len(rows))
	for _, row := range rows {
		content := map[string]any{"plainText": "", "block": discoveryJSONValue(row.Content)}
		if document := documents[row.PageID]; row.Type == "paragraph" && document != nil {
			content["plainText"] = document.GetText(leafen.YgoBlockTextRootPrefix + row.ID).ToString()
		}
		item := discoveryItem{
			Type: "block", ID: row.ID, OrganizationID: payload.OrganizationID,
			WorkspaceID: workspace.ID, WorkspaceName: workspace.Name, PageID: row.PageID,
			CustodianUserIDs: discoveryCustodians(row.CreatedBy),
			CreatedAt:        discoveryMillis(row.Created), UpdatedAt: discoveryMillis(row.Updated),
			Content: content,
		}
		if organizationDiscoveryMatches(item, payload.Filter) {
			items = append(items, item)
		}
	}
	return items, len(rows), discoveryLastBlockID(rows), nil
}

func readOrganizationDiscoveryComments(
	app core.App, payload organizationDiscoveryExportJobPayload, workspace organizationDiscoveryWorkspace,
) ([]discoveryItem, int, string, error) {
	rows := []organizationDiscoveryCommentCandidate{}
	err := app.DB().NewQuery(`
		SELECT [[comment]].[[id]],[[comment]].[[page_id]],[[comment]].[[target_kind]],
			[[comment]].[[target_id]],[[comment]].[[block_id]],[[comment]].[[author_id]],
			[[comment]].[[body_json]],[[comment]].[[created_at]],[[comment]].[[updated_at]]
		FROM {{leafen_comments}} [[comment]] JOIN {{leafen_pages}} [[page]]
			ON [[page]].[[id]]=[[comment]].[[page_id]] AND [[page]].[[workspace]]=[[comment]].[[workspace_id]]
		WHERE [[comment]].[[workspace_id]]={:workspaceId} AND [[comment]].[[id]]>{:after}
			AND ({:includeTrashed}=1 OR [[page]].[[in_trash]]=0)
		ORDER BY [[comment]].[[id]] LIMIT {:limit}
	`).Bind(discoveryCandidateParams(payload, workspace)).All(&rows)
	if err != nil {
		return nil, 0, "", err
	}
	items := make([]discoveryItem, 0, len(rows))
	for _, row := range rows {
		logicalPageID := row.PageID
		if row.TargetKind == leafen.CommentTargetDatabaseRow {
			logicalPageID = row.TargetID
		}
		item := discoveryItem{
			Type: "comment", ID: row.ID, OrganizationID: payload.OrganizationID,
			WorkspaceID: workspace.ID, WorkspaceName: workspace.Name, PageID: logicalPageID,
			CustodianUserIDs: discoveryCustodians(row.AuthorID),
			CreatedAt:        discoveryDateTime(row.CreatedAt), UpdatedAt: discoveryDateTime(row.UpdatedAt),
			Content: map[string]any{
				"body": discoveryJSONValue(row.BodyJSON), "targetKind": row.TargetKind,
				"blockId": row.BlockID,
			},
		}
		if organizationDiscoveryMatches(item, payload.Filter) {
			items = append(items, item)
		}
	}
	return items, len(rows), discoveryLastCommentID(rows), nil
}

func readOrganizationDiscoveryFiles(
	app core.App, payload organizationDiscoveryExportJobPayload, workspace organizationDiscoveryWorkspace,
) ([]discoveryItem, int, string, error) {
	rows := []organizationDiscoveryFileCandidate{}
	err := app.DB().NewQuery(`
		SELECT [[file]].[[id]],COALESCE(MIN([[reference]].[[page_id]]), '') AS [[page_id]],
			[[file]].[[original_name]],[[file]].[[content_type]],[[file]].[[size]],
			[[file]].[[created_by]],[[file]].[[created]],[[file]].[[updated]]
		FROM {{leafen_files}} [[file]]
		LEFT JOIN {{leafen_file_references}} [[reference]] ON [[reference]].[[file_id]]=[[file]].[[id]]
		WHERE [[file]].[[workspace]]={:workspaceId}
			AND [[file]].[[authority_state]]='workspace' AND [[file]].[[id]]>{:after}
		GROUP BY [[file]].[[id]]
		ORDER BY [[file]].[[id]] LIMIT {:limit}
	`).Bind(discoveryCandidateParams(payload, workspace)).All(&rows)
	if err != nil {
		return nil, 0, "", err
	}
	items := make([]discoveryItem, 0, len(rows))
	for _, row := range rows {
		item := discoveryItem{
			Type: "file", ID: row.ID, OrganizationID: payload.OrganizationID,
			WorkspaceID: workspace.ID, WorkspaceName: workspace.Name, PageID: row.PageID,
			CustodianUserIDs: discoveryCustodians(row.CreatedBy),
			CreatedAt:        discoveryDateTime(row.Created), UpdatedAt: discoveryDateTime(row.Updated),
			Content: map[string]any{
				"name": row.Name, "contentType": row.ContentType, "size": row.Size, "status": "ready",
			},
		}
		if organizationDiscoveryMatches(item, payload.Filter) {
			items = append(items, item)
		}
	}
	return items, len(rows), discoveryLastFileID(rows), nil
}

func discoveryCandidateParams(
	payload organizationDiscoveryExportJobPayload, workspace organizationDiscoveryWorkspace,
) dbx.Params {
	return dbx.Params{
		"workspaceId": workspace.ID, "after": payload.EntityCursor,
		"limit": leafen.OrganizationDiscoveryItemsPerTurn, "includeTrashed": payload.Filter.IncludeTrashed,
	}
}

func organizationDiscoveryEntityIndex(entity string) int {
	for index, candidate := range organizationDiscoveryEntities {
		if candidate == entity {
			return index
		}
	}
	return -1
}

func discoveryCustodians(values ...string) []string {
	unique := map[string]struct{}{}
	for _, value := range values {
		if value != "" {
			unique[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(unique))
	for value := range unique {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func discoveryDateTime(value string) string {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.000Z", "2006-01-02 15:04:05Z"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC().Format(time.RFC3339Nano)
		}
	}
	return ""
}

func discoveryMillis(value int64) string {
	if value < 0 {
		return ""
	}
	return time.UnixMilli(value).UTC().Format(time.RFC3339Nano)
}

func discoveryJSONValue(value string) any {
	var decoded any
	if json.Unmarshal([]byte(value), &decoded) == nil {
		return decoded
	}
	return value
}

func discoveryLastPageID(rows []organizationDiscoveryPageCandidate) string {
	if len(rows) == 0 {
		return ""
	}
	return rows[len(rows)-1].ID
}

func discoveryLastBlockID(rows []organizationDiscoveryBlockCandidate) string {
	if len(rows) == 0 {
		return ""
	}
	return rows[len(rows)-1].ID
}

func discoveryLastCommentID(rows []organizationDiscoveryCommentCandidate) string {
	if len(rows) == 0 {
		return ""
	}
	return rows[len(rows)-1].ID
}

func discoveryLastFileID(rows []organizationDiscoveryFileCandidate) string {
	if len(rows) == 0 {
		return ""
	}
	return rows[len(rows)-1].ID
}
