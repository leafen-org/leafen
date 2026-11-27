package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// leafen_organization_audit_exports is owned here and in organization_audit_schema.go.

var (
	errOrganizationAuditInvalid     = errors.New(errorCodeOrganizationAuditInvalid)
	errOrganizationAuditConflict    = errors.New("organization audit mutation conflicts with its receipt")
	errOrganizationAuditNotFound    = errors.New(errorCodeOrganizationAuditNotFound)
	errOrganizationAuditTooLarge    = errors.New(errorCodeOrganizationAuditTooLarge)
	errOrganizationAuditUnavailable = errors.New(errorCodeOrganizationAuditUnavailable)
	// errOrganizationPolicyRevisionChanged is an organization policy changed
	// since the revision a change names: a conflict, not a change ID used
	// again.
	errOrganizationPolicyRevisionChanged = fmt.Errorf("%w: the policy's revision moved on", errOrganizationAuditConflict)
)

type organizationAuditFilter struct {
	Cursor     string
	Action     string
	TargetType string
	Since      int64
	Until      int64
	Limit      int
	// Actor is whose events, Target what they were done to, by ID.
	Actor  string
	Target string
}

// organizationAuditActorNameSQL is who did an audited thing, by name; what an
// identity provider sent through SCIM, which no person did, is "SCIM", as
// the log shows it.
var organizationAuditActorNameSQL = `COALESCE((SELECT ` + accountNameSQL("person") + `
		FROM {{users}} [[person]] WHERE [[person]].[[id]] = {{leafen_organization_audit_events}}.[[actor_id]]),
	CASE WHEN {{leafen_organization_audit_events}}.[[actor_id]] = 'scim' THEN 'SCIM' END, '')`

type storedOrganizationAuditEvent struct {
	ID             string `db:"id"`
	OrganizationID string `db:"organization_id"`
	WorkspaceID    string `db:"workspace_id"`
	ActorID        string `db:"actor_id"`
	Action         string `db:"action"`
	TargetType     string `db:"subject_type"`
	TargetID       string `db:"subject_id"`
	MetadataJSON   string `db:"metadata_json"`
	OccurredAt     int64  `db:"created"`
	// The app's name when the actor is an app's own account.
	ActorApp string `db:"actor_app"`
	// Who did it and to what, by the names the organization's settings show.
	ActorName  string         `db:"actor_name"`
	TargetName sql.NullString `db:"target_name"`
	// Whether the person it is about, or the member it names, has deleted
	// their account since (organizationAuditGonePeopleSQL).
	PersonGone bool `db:"person_gone"`
	MemberGone bool `db:"member_gone"`
}

type storedOrganizationAuditExport struct {
	ID              string `db:"id"`
	OrganizationID  string `db:"organization_id"`
	ActorID         string `db:"actor_id"`
	MutationID      string `db:"mutation_id"`
	RequestHash     string `db:"request_hash"`
	Status          string `db:"status"`
	Format          string `db:"format"`
	Action          string `db:"action"`
	TargetType      string `db:"target_type"`
	Since           int64  `db:"since"`
	Until           int64  `db:"until"`
	RetentionDays   int    `db:"retention_days"`
	Limit           int    `db:"limit_count"`
	EventCount      int    `db:"event_count"`
	ArtifactVersion int    `db:"artifact_version"`
	ChunkCount      int    `db:"chunk_count"`
	ContentBytes    int64  `db:"content_bytes"`
	ContentSHA256   string `db:"content_sha256"`
	Content         string `db:"content"`
	CreatedBy       string `db:"created_by"`
	CompletedAt     int64  `db:"completed_at"`
	CreatedAt       int64  `db:"created_at"`
}

func normalizeOrganizationAuditFilter(filter organizationAuditFilter, maximum int) (organizationAuditFilter, error) {
	filter.Cursor = strings.TrimSpace(filter.Cursor)
	filter.Action = strings.TrimSpace(filter.Action)
	filter.TargetType = strings.TrimSpace(filter.TargetType)
	filter.Actor, filter.Target = strings.TrimSpace(filter.Actor), strings.TrimSpace(filter.Target)
	if len([]rune(filter.Actor)) > organizationAuditMaxFilterRunes || len([]rune(filter.Target)) > organizationAuditMaxFilterRunes {
		return organizationAuditFilter{}, refuse(errOrganizationAuditInvalid, "actor and target are at most %d characters each", organizationAuditMaxFilterRunes)
	}
	if filter.Limit == 0 && maximum > 0 {
		filter.Limit = maximum
	}
	switch {
	case filter.Limit < 0 || (maximum > 0 && (filter.Limit < 1 || filter.Limit > maximum)):
		return organizationAuditFilter{}, refuse(errOrganizationAuditInvalid, "limit is a number from 1 to %d", max(maximum, 1))
	case filter.Since < 0 || filter.Until < 0:
		return organizationAuditFilter{}, refuse(errOrganizationAuditInvalid, "since and until are times in milliseconds since 1970, 0 or more")
	case filter.Until > 0 && filter.Since > filter.Until:
		return organizationAuditFilter{}, refuse(errOrganizationAuditInvalid, "since is at or before until")
	case len([]rune(filter.Action)) > organizationAuditMaxFilterRunes || len([]rune(filter.TargetType)) > organizationAuditMaxFilterRunes:
		return organizationAuditFilter{}, refuse(errOrganizationAuditInvalid, "action and targetType are at most %d characters each", organizationAuditMaxFilterRunes)
	case len([]byte(filter.Cursor)) > organizationAuditMaxCursorBytes:
		return organizationAuditFilter{}, errOrganizationAuditCursor
	}
	return filter, nil
}

// errOrganizationAuditCursor is a list cursor the audit log didn't give.
var errOrganizationAuditCursor = refuse(errOrganizationAuditInvalid, "cursor isn't one this list gave: leave it out to read from the start")

func parseOrganizationAuditCursor(value string) (int64, string, error) {
	if value == "" {
		return 0, "", nil
	}
	created, id, ok := strings.Cut(value, ":")
	if !ok || id == "" {
		return 0, "", errOrganizationAuditCursor
	}
	timestamp, err := strconv.ParseInt(created, 10, 64)
	if err != nil || timestamp < 0 {
		return 0, "", errOrganizationAuditCursor
	}
	return timestamp, id, nil
}

func listOrganizationAuditEvents(
	app core.App,
	organizationID string,
	filter organizationAuditFilter,
	maximum int,
) (organizationAuditEventListResponse, error) {
	filter, err := normalizeOrganizationAuditFilter(filter, maximum)
	if err != nil {
		return organizationAuditEventListResponse{}, err
	}
	cursorCreated, cursorID, err := parseOrganizationAuditCursor(filter.Cursor)
	if err != nil {
		return organizationAuditEventListResponse{}, err
	}
	params := dbx.Params{"organizationId": organizationID, "limit": filter.Limit + 1}
	conditions := []string{"[[organization_id]] = {:organizationId}"}
	if filter.Action != "" {
		params["action"] = filter.Action
		conditions = append(conditions, "[[action]] = {:action}")
	}
	if filter.TargetType != "" {
		params["targetType"] = filter.TargetType
		conditions = append(conditions, "[[subject_type]] = {:targetType}")
	}
	if filter.Actor != "" {
		params["actor"] = filter.Actor
		conditions = append(conditions, "[[actor_id]] = {:actor}")
	}
	if filter.Target != "" {
		params["target"] = filter.Target
		conditions = append(conditions, "[[subject_id]] = {:target}")
	}
	if filter.Since > 0 {
		params["since"] = filter.Since
		conditions = append(conditions, "[[created]] >= {:since}")
	}
	if filter.Until > 0 {
		params["until"] = filter.Until
		conditions = append(conditions, "[[created]] <= {:until}")
	}
	if cursorID != "" {
		params["cursorCreated"], params["cursorId"] = cursorCreated, cursorID
		conditions = append(conditions, "([[created]] < {:cursorCreated} OR ([[created]] = {:cursorCreated} AND [[id]] < {:cursorId}))")
	}
	rows := []storedOrganizationAuditEvent{}
	// Who did it and to what, by the names the organization's settings show
	// (organization_settings_snapshot.go).
	query := `SELECT [[id]], [[organization_id]], [[workspace_id]], [[actor_id]], [[action]],
		[[subject_type]], [[subject_id]], [[metadata_json]], [[created]],
		COALESCE((SELECT [[account]].[[name]] FROM {{users}} [[account]]
			JOIN {{leafen_integration_accounts}} [[integration]] ON [[integration]].[[user_id]] = [[account]].[[id]]
			WHERE [[account]].[[id]] = {{leafen_organization_audit_events}}.[[actor_id]]), '') AS [[actor_app]],
		` + organizationAuditActorNameSQL + ` AS [[actor_name]],
		` + organizationAuditTargetNameSQL + ` AS [[target_name]], ` + organizationAuditGonePeopleSQL + `
		FROM {{leafen_organization_audit_events}} WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY [[created]] DESC, [[id]] DESC LIMIT {:limit}`
	if err := app.DB().NewQuery(query).Bind(params).All(&rows); err != nil {
		return organizationAuditEventListResponse{}, errors.Join(errOrganizationAuditUnavailable, err)
	}
	hasMore := len(rows) > filter.Limit
	if hasMore {
		rows = rows[:filter.Limit]
	}
	events := make([]organizationAuditEventResponse, 0, len(rows))
	for _, row := range rows {
		event, err := organizationAuditEventFromStored(row, organizationID)
		if err != nil {
			return organizationAuditEventListResponse{}, err
		}
		events = append(events, event)
	}
	response := organizationAuditEventListResponse{Events: events}
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		response.NextCursor = fmt.Sprintf("%d:%s", last.OccurredAt, last.ID)
	}
	return response, nil
}

func organizationAuditEventFromStored(
	row storedOrganizationAuditEvent,
	organizationID string,
) (organizationAuditEventResponse, error) {
	metadata := map[string]any{}
	if row.OrganizationID != organizationID || row.ID == "" || row.ActorID == "" ||
		row.Action == "" || row.TargetType == "" || row.TargetID == "" || row.OccurredAt < 0 ||
		len([]byte(row.MetadataJSON)) > organizationAuditMaxMetadataBytes ||
		json.Unmarshal([]byte(row.MetadataJSON), &metadata) != nil {
		return organizationAuditEventResponse{}, errOrganizationAuditUnavailable
	}
	response := organizationAuditEventResponse{
		ID: row.ID, OrganizationID: row.OrganizationID, WorkspaceID: row.WorkspaceID,
		ActorID: row.ActorID, Action: row.Action, TargetType: row.TargetType,
		TargetID: row.TargetID, Metadata: metadata, OccurredAt: row.OccurredAt,
		ActorApp: row.ActorApp, ActorName: row.ActorName,
	}
	if row.TargetName.Valid && !row.PersonGone {
		response.TargetName = &row.TargetName.String
	}
	forgetGoneOrganizationAuditPeople(response.Metadata, row.PersonGone, row.MemberGone)
	return response, nil
}

func normalizeOrganizationAuditExportInput(
	input organizationAuditExportCreateInput,
) (organizationAuditExportCreateInput, string, error) {
	input.MutationID = strings.TrimSpace(input.MutationID)
	input.Format = strings.ToLower(strings.TrimSpace(input.Format))
	input.Action = strings.TrimSpace(input.Action)
	input.TargetType = strings.TrimSpace(input.TargetType)
	input.Actor, input.Target = strings.TrimSpace(input.Actor), strings.TrimSpace(input.Target)
	if input.MutationID == "" || len([]rune(input.MutationID)) > organizationAuditMaxMutationIDRunes {
		return organizationAuditExportCreateInput{}, "", refuse(errOrganizationAuditInvalid,
			"mutationId is a new id of yours, 1–%d characters, so the same export asked again isn't made twice", organizationAuditMaxMutationIDRunes)
	}
	if input.Format != organizationAuditExportFormatJSONL && input.Format != organizationAuditExportFormatCSV && input.Format != organizationAuditExportFormatJSON {
		return organizationAuditExportCreateInput{}, "", refuse(errOrganizationAuditInvalid, "format is %s, %s or %s, not %q",
			organizationAuditExportFormatJSONL, organizationAuditExportFormatCSV, organizationAuditExportFormatJSON, input.Format)
	}
	filter, err := normalizeOrganizationAuditFilter(organizationAuditFilter{
		Action: input.Action, TargetType: input.TargetType,
		Since: input.Since, Until: input.Until, Limit: input.Limit, Actor: input.Actor, Target: input.Target,
	}, 0)
	if err != nil {
		return organizationAuditExportCreateInput{}, "", err
	}
	input.Action, input.TargetType = filter.Action, filter.TargetType
	input.Since, input.Until, input.Limit = filter.Since, filter.Until, filter.Limit
	canonical, err := json.Marshal(input)
	if err != nil {
		return organizationAuditExportCreateInput{}, "", errOrganizationAuditInvalid
	}
	digest := sha256.Sum256(canonical)
	return input, hex.EncodeToString(digest[:]), nil
}

func insertOrganizationAuditExport(app core.App, row storedOrganizationAuditExport) error {
	_, err := app.DB().Insert(organizationAuditExportArtifactsTable, dbx.Params{
		"id": row.ID, "organization_id": row.OrganizationID, "actor_id": row.ActorID,
		"mutation_id": row.MutationID, "request_hash": row.RequestHash, "status": row.Status,
		"format": row.Format, "action": row.Action, "target_type": row.TargetType,
		"since": row.Since, "until": row.Until, "retention_days": row.RetentionDays, "limit_count": row.Limit,
		"event_count": row.EventCount, "content_bytes": row.ContentBytes,
		"artifact_version": row.ArtifactVersion, "chunk_count": row.ChunkCount,
		"content_sha256": row.ContentSHA256,
		"created_by":     row.CreatedBy, "completed_at": row.CompletedAt, "created_at": row.CreatedAt,
	}).Execute()
	if err != nil {
		return errors.Join(errOrganizationAuditUnavailable, err)
	}
	return nil
}

func findOrganizationAuditExportByMutation(
	app core.App, organizationID, actorID, mutationID string,
) (storedOrganizationAuditExport, bool, error) {
	params := dbx.Params{
		"organizationId": organizationID, "actorId": actorID, "mutationId": mutationID,
	}
	for _, query := range []string{organizationAuditArtifactSelect, organizationAuditLegacyExportSelect} {
		row := storedOrganizationAuditExport{}
		err := app.DB().NewQuery(query + `
		WHERE [[organization_id]] = {:organizationId} AND [[actor_id]] = {:actorId}
			AND [[mutation_id]] = {:mutationId}
	`).Bind(params).One(&row)
		if err == nil {
			return row, true, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return storedOrganizationAuditExport{}, false, errors.Join(errOrganizationAuditUnavailable, err)
		}
	}
	return storedOrganizationAuditExport{}, false, nil
}

const organizationAuditLegacyExportSelect = `SELECT [[id]], [[organization_id]], [[actor_id]],
	[[mutation_id]], [[request_hash]], [[status]], [[format]], [[action]], [[target_type]],
	[[since]], [[until]], [[retention_days]], [[limit_count]], [[event_count]],
	1 AS [[artifact_version]], 0 AS [[chunk_count]], [[content_bytes]],
	[[content_sha256]], [[content]],
	[[created_by]], [[completed_at]], [[created_at]] FROM {{leafen_organization_audit_exports}}`

const organizationAuditArtifactSelect = `SELECT [[id]], [[organization_id]], [[actor_id]],
	[[mutation_id]], [[request_hash]], [[status]], [[format]], [[action]], [[target_type]],
	[[since]], [[until]], [[retention_days]], [[limit_count]], [[event_count]], [[artifact_version]],
	[[chunk_count]], [[content_bytes]], [[content_sha256]], '' AS [[content]],
	[[created_by]], [[completed_at]], [[created_at]] FROM {{leafen_organization_audit_export_artifacts}}`

func listOrganizationAuditExports(
	app core.App,
	organizationID, cursor string,
	limit int,
) (organizationAuditExportListResponse, error) {
	cursor = strings.TrimSpace(cursor)
	if limit == 0 {
		limit = organizationAuditExportsPerPage
	}
	if limit < 1 || limit > organizationAuditExportsPerPage {
		return organizationAuditExportListResponse{}, refuse(errOrganizationAuditInvalid, "limit is a number from 1 to %d", organizationAuditExportsPerPage)
	}
	if len([]byte(cursor)) > organizationAuditMaxCursorBytes {
		return organizationAuditExportListResponse{}, errOrganizationAuditCursor
	}
	cursorCompleted, cursorID, err := parseOrganizationAuditCursor(cursor)
	if err != nil {
		return organizationAuditExportListResponse{}, err
	}
	params := dbx.Params{"organizationId": organizationID, "limit": limit + 1}
	condition := ""
	if cursorID != "" {
		params["cursorCompleted"], params["cursorId"] = cursorCompleted, cursorID
		condition = " AND ([[completed_at]] < {:cursorCompleted} OR ([[completed_at]] = {:cursorCompleted} AND [[id]] < {:cursorId}))"
	}
	rows := make([]storedOrganizationAuditExport, 0, 2*(limit+1))
	for _, query := range []string{organizationAuditArtifactSelect, organizationAuditLegacyExportSelect} {
		part := []storedOrganizationAuditExport{}
		if err := app.DB().NewQuery(query + `
			WHERE [[organization_id]] = {:organizationId}` + condition + `
			ORDER BY [[completed_at]] DESC, [[id]] DESC LIMIT {:limit}
		`).Bind(params).All(&part); err != nil {
			return organizationAuditExportListResponse{}, errors.Join(errOrganizationAuditUnavailable, err)
		}
		rows = append(rows, part...)
	}
	sort.Slice(rows, func(left, right int) bool {
		if rows[left].CompletedAt != rows[right].CompletedAt {
			return rows[left].CompletedAt > rows[right].CompletedAt
		}
		return rows[left].ID > rows[right].ID
	})
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	items := make([]organizationAuditExportResponse, 0, len(rows))
	for _, row := range rows {
		item, err := organizationAuditExportFromStored(row, organizationID, false)
		if err != nil {
			return organizationAuditExportListResponse{}, err
		}
		items = append(items, item)
	}
	response := organizationAuditExportListResponse{AuditExports: items}
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		response.NextCursor = fmt.Sprintf("%d:%s", last.CompletedAt, last.ID)
	}
	return response, nil
}

func loadOrganizationAuditExport(
	app core.App,
	organizationID, exportID string,
) (organizationAuditExportContentResponse, error) {
	row, err := loadStoredOrganizationAuditExport(app, organizationID, exportID)
	if err != nil {
		return organizationAuditExportContentResponse{}, err
	}
	return organizationAuditExportContentFromStored(row, organizationID)
}

func loadStoredOrganizationAuditExport(
	app core.App,
	organizationID, exportID string,
) (storedOrganizationAuditExport, error) {
	exportID = strings.TrimSpace(exportID)
	if exportID == "" {
		return storedOrganizationAuditExport{}, errOrganizationAuditNotFound
	}
	params := dbx.Params{"organizationId": organizationID, "exportId": exportID}
	for _, query := range []string{organizationAuditArtifactSelect, organizationAuditLegacyExportSelect} {
		row := storedOrganizationAuditExport{}
		err := app.DB().NewQuery(query + `
			WHERE [[organization_id]] = {:organizationId} AND [[id]] = {:exportId}
		`).Bind(params).One(&row)
		if err == nil {
			return row, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return storedOrganizationAuditExport{}, errors.Join(errOrganizationAuditUnavailable, err)
		}
	}
	return storedOrganizationAuditExport{}, errOrganizationAuditNotFound
}

func organizationAuditExportContentFromStored(
	row storedOrganizationAuditExport,
	organizationID string,
) (organizationAuditExportContentResponse, error) {
	response, err := organizationAuditExportFromStored(row, organizationID, true)
	if err != nil {
		return organizationAuditExportContentResponse{}, err
	}
	return organizationAuditExportContentResponse{AuditExport: response}, nil
}

func organizationAuditExportFromStored(
	row storedOrganizationAuditExport,
	organizationID string,
	withContent bool,
) (organizationAuditExportResponse, error) {
	validFormat := row.Format == organizationAuditExportFormatJSONL ||
		row.Format == organizationAuditExportFormatCSV || row.Format == organizationAuditExportFormatJSON
	if row.OrganizationID != organizationID || len(row.ID) != 32 || row.ActorID == "" ||
		row.MutationID == "" || row.MutationID != strings.TrimSpace(row.MutationID) ||
		len([]rune(row.MutationID)) > organizationAuditMaxMutationIDRunes || len(row.RequestHash) != 64 ||
		row.Status != organizationAuditExportStatusCompleted || !validFormat ||
		row.Action != strings.TrimSpace(row.Action) || row.TargetType != strings.TrimSpace(row.TargetType) ||
		len([]rune(row.Action)) > organizationAuditMaxFilterRunes ||
		len([]rune(row.TargetType)) > organizationAuditMaxFilterRunes ||
		row.Since < 0 || row.Until < 0 || (row.Until > 0 && row.Since > row.Until) ||
		row.RetentionDays < 0 ||
		row.Limit < 0 || row.EventCount < 0 || (row.Limit > 0 && row.EventCount > row.Limit) ||
		row.ContentBytes < 0 || row.ChunkCount < 0 ||
		len(row.ContentSHA256) != 64 ||
		row.CreatedBy == "" || row.CompletedAt < 0 || row.CreatedAt < 0 ||
		!validOrganizationAuditArtifactStorage(row, withContent) {
		return organizationAuditExportResponse{}, errOrganizationAuditUnavailable
	}
	return organizationAuditExportResponse{
		ID: row.ID, OrganizationID: row.OrganizationID, Status: row.Status, Format: row.Format,
		Action: row.Action, TargetType: row.TargetType, Since: row.Since, Until: row.Until,
		RetentionDays: row.RetentionDays,
		Limit:         row.Limit, EventCount: row.EventCount, ProcessedEvents: row.EventCount,
		ContentBytes: row.ContentBytes, ContentSHA256: row.ContentSHA256,
		DownloadPath: apiRoot + "/organizations/" + row.OrganizationID + "/audit-exports/" + row.ID + "/download",
		CreatedBy:    row.CreatedBy, CompletedAt: row.CompletedAt, CreatedAt: row.CreatedAt,
	}, nil
}

func validOrganizationAuditArtifactStorage(row storedOrganizationAuditExport, withContent bool) bool {
	switch row.ArtifactVersion {
	case 1:
		return row.Limit >= 1 && row.Limit <= 5000 && row.EventCount <= row.Limit &&
			row.ChunkCount == 0 && row.ContentBytes <= 8*1024*1024 &&
			(!withContent || int64(len([]byte(row.Content))) == row.ContentBytes)
	case organizationAuditExportArtifactFormat:
		return row.Content == ""
	default:
		return false
	}
}
