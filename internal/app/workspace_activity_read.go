package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedWorkspaceActivity struct {
	ID         string `db:"id"`
	PageID     string `db:"page_id"`
	ActorID    string `db:"actor_id"`
	Action     string `db:"action"`
	EntityType string `db:"subject_type"`
	EntityID   string `db:"subject_id"`
	Metadata   string `db:"metadata_json"`
	Created    int64  `db:"created"`
	LastAt     int64  `db:"last_at"`
	// Seq is the order the group was first written in (its rowid), which
	// orders groups of one time (workspaceActivityOrder).
	Seq int64 `db:"seq"`
	// The group's first and last version of its own page, if it has them.
	FirstVersion sql.NullInt64 `db:"first_version"`
	LastVersion  sql.NullInt64 `db:"last_version"`
}

func readWorkspaceActivities(app core.App, user *core.Record, workspaceID, rawLimit, token string) (workspaceActivityResponse, error) {
	return readWorkspaceSourceActivities(context.Background(), app, user, workspaceID, rawLimit, token, "", "")
}

// readWorkspaceSourceActivities reads a workspace's updates, or with
// sourceID one database's own: its rows', views', properties' and its own,
// as a database opened alone lists them (schema_baseline.json);
// or with trashOf one page's moves to and from the trash, its own lines and
// its block's deleted from the page that held it, as its updates list them
// (schema_baseline.json), with the reader's permission lines
// of the page (workspace_activity_permission_notices.go).
func readWorkspaceSourceActivities(ctx context.Context, app core.App, user *core.Record, workspaceID, rawLimit, token, sourceID, trashOf string) (workspaceActivityResponse, error) {
	if len(sourceID) > 128 || len(trashOf) > 128 || sourceID != "" && trashOf != "" {
		return workspaceActivityResponse{}, errWorkspaceActivityInvalid
	}
	limit := workspaceActivityDefaultLimit
	if rawLimit != "" {
		var err error
		limit, err = strconv.Atoi(rawLimit)
		if err != nil || limit < 1 || limit > workspaceActivityMaxLimit {
			return workspaceActivityResponse{}, errWorkspaceActivityInvalid
		}
	}
	var cursor workspaceActivityCursor
	if token != "" {
		var err error
		cursor, err = openWorkspaceActivityCursor(user, token)
		if err != nil {
			return workspaceActivityResponse{}, err
		}
	}
	var response workspaceActivityResponse
	err := runReadSnapshot(context.Background(), app, func(tx core.App) error {
		scope, err := authorizeCommentNotificationScope(tx, user.Id, workspaceID, true)
		if err != nil {
			return err
		}
		// The feed keeps its groups for the organization's period, a year
		// unless it chose another; its audit log keeps its own record.
		retention, err := loadOrganizationActivityRetention(tx, scope.Organization.OrganizationID)
		if err != nil {
			return err
		}
		if token == "" {
			cursor = workspaceActivityCursor{Version: workspaceActivityCursorVersion, WorkspaceID: workspaceID, OrganizationID: scope.Organization.OrganizationID,
				PrincipalID: user.Id, StartedAt: time.Now().UTC().UnixMilli(), RetentionDays: retention.FeedDays, SourceID: sourceID, TrashOf: trashOf}
			if err := tx.DB().NewQuery(`SELECT rowid,[[id]] FROM {{leafen_workspace_activity}} ORDER BY rowid DESC LIMIT 1`).Row(&cursor.Frontier, &cursor.FrontierID); err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		} else if cursor.WorkspaceID != workspaceID || cursor.OrganizationID != scope.Organization.OrganizationID ||
			cursor.RetentionDays != retention.FeedDays || cursor.SourceID != sourceID || cursor.TrashOf != trashOf {
			return errWorkspaceActivityInvalid
		}
		if token != "" {
			// SQLite can reuse the largest rowid after retention or scope
			// removal. If this walk's immutable anchor disappeared, restart
			// instead of admitting a later insertion into the old frontier.
			var anchor string
			err := tx.DB().NewQuery(`SELECT [[id]] FROM {{leafen_workspace_activity}} WHERE rowid={:frontier}`).Bind(dbx.Params{"frontier": cursor.Frontier}).Row(&anchor)
			if errors.Is(err, sql.ErrNoRows) || err == nil && anchor != cursor.FrontierID {
				return errWorkspaceActivityInvalid
			}
			if err != nil {
				return err
			}
		}
		response, err = readWorkspaceActivityTurn(ctx, tx, user, scope, cursor, limit)
		return err
	})
	return response, err
}

func readWorkspaceActivityTurn(ctx context.Context, app core.App, user *core.Record, scope workspaceAuthorization, cursor workspaceActivityCursor, limit int) (workspaceActivityResponse, error) {
	rows := []storedWorkspaceActivity{}
	params := dbx.Params{"workspace": scope.WorkspaceID, "organization": scope.Organization.OrganizationID, "frontier": cursor.Frontier,
		"cutoff": activityRetentionCutoff(cursor.RetentionDays, time.Now()), "time": cursor.Time, "seq": cursor.Seq, "notice": cursor.Notice, "limit": workspaceActivityScanTurn + 1,
		"source": cursor.SourceID, "subject": cursor.TrashOf}
	// One database's own updates are read by its source, one page's trash moves by it, the workspace's by time.
	// A page's way back from the trash is its restore marked trashRestore; a version restored
	// is its own restore too, which a page's updates leave out, as Notion's list no line for
	// one (seen 2026-11-15: three version restores, none listed).
	index, source := "idx_leafen_workspace_activity_feed", ""
	if cursor.SourceID != "" {
		index, source = "idx_leafen_workspace_activity_source", " AND json_extract([[metadata_json]], '$.sourceId')={:source}"
	} else if cursor.TrashOf != "" {
		// A page at the top's own block deleted is its page's deleted line again (workspace_activity_page_block.go).
		index, source = "idx_leafen_workspace_activity_subject", " AND [[subject_id]]={:subject} AND [[subject_type]] IN ('page','block')"+
			" AND ([[action]]='delete' OR [[action]]='restore' AND json_extract([[metadata_json]], '$.kind')='trashRestore')"+
			" AND NOT ([[subject_type]]='block' AND json_extract([[metadata_json]], '$.kind')='page')"
	}
	// Groups of one time list as they were written, the last first
	// (workspaceActivityOrder); a permission line of that time stands over them.
	if err := app.DB().NewQuery(`SELECT [[id]],[[page_id]],[[actor_id]],[[action]],[[subject_type]],[[subject_id]],[[metadata_json]],[[created]],[[last_at]],rowid AS [[seq]],[[first_version]],[[last_version]]
		FROM {{leafen_workspace_activity}} INDEXED BY [[` + index + `]]
		WHERE [[workspace_id]]={:workspace} AND [[organization_id]]={:organization}` + source + `
			AND rowid<={:frontier} AND [[last_at]]>={:cutoff}
			AND ({:time}=0 OR [[last_at]]<{:time} OR ([[last_at]]={:time} AND ({:notice} OR rowid<{:seq})))
		ORDER BY [[last_at]] DESC,rowid DESC LIMIT {:limit}`).Bind(params).All(&rows); err != nil {
		return workspaceActivityResponse{}, err
	}
	more := len(rows) > workspaceActivityScanTurn
	if more {
		rows = rows[:workspaceActivityScanTurn]
	}
	// The workspace's own updates hold the reader's permission lines too
	// (workspace_activity_permission_notices.go), and a page's its own; a database's doesn't.
	notices, noticesMore := []storedWorkspacePermissionNotice{}, false
	if cursor.SourceID == "" {
		var err error
		if notices, err = readWorkspacePermissionNotices(app, user.Id, scope.WorkspaceID, params["cutoff"].(int64), cursor, limit+1); err != nil {
			return workspaceActivityResponse{}, err
		}
		if noticesMore = len(notices) > limit; noticesMore {
			notices = notices[:limit]
		}
	}
	members, err := readWorkspacePermissionGroupMembers(app, notices)
	if err != nil {
		return workspaceActivityResponse{}, err
	}
	candidates := mergeWorkspaceActivityCandidates(rows, more, notices, noticesMore)
	more = more || noticesMore
	response := workspaceActivityResponse{WorkspaceID: scope.WorkspaceID, Items: []workspaceActivityRecord{}, ActorProfiles: []workspaceActivityActorProfile{}, RetentionDays: cursor.RetentionDays}
	response.RefreshToken, err = workspaceActivityRefreshToken(app, user, scope)
	if err != nil {
		return response, err
	}
	targets, err := loadWorkspaceActivityTargets(app, rows)
	if err != nil {
		return response, err
	}
	cache := map[string]*pageAuthorization{}
	places := workspaceActivityPlaces{}
	actors := []string{}
	for index, candidate := range candidates {
		cursor.Time, cursor.ID, cursor.Seq, cursor.Notice = candidate.at, candidate.id, candidate.seq, candidate.notice != nil
		if candidate.notice != nil {
			item := workspacePermissionActivity(*candidate.notice, members[candidate.notice.ID], user.Id)
			if item.Location, err = workspacePermissionPlace(ctx, app, user.Id, item.PageID, places); err != nil {
				return response, err
			}
			response.Items = append(response.Items, item)
			actors = append(actors, item.ActorID)
			for _, change := range item.Permissions {
				if change.EntityID != "" {
					actors = append(actors, change.EntityID)
				}
			}
		} else {
			item, visible, err := visibleWorkspaceActivity(ctx, app, user.Id, scope, *candidate.row, targets, cache, places)
			if err != nil {
				return response, err
			}
			if visible {
				response.Items = append(response.Items, item)
				actors = append(actors, candidate.row.ActorID)
			}
		}
		if len(response.Items) == limit {
			more = more || index+1 < len(candidates)
			break
		}
	}
	if more {
		var err error
		response.NextCursor, err = sealWorkspaceActivityCursor(user, cursor)
		if err != nil {
			return response, err
		}
	}
	profiles, err := readCanonicalActorProfiles(app, actors)
	if err != nil {
		return response, err
	}
	for _, profile := range profiles {
		response.ActorProfiles = append(response.ActorProfiles, workspaceActivityActorProfile(profile))
	}
	return response, nil
}

// Use the existing change snapshot, whose revision triggers cover shares,
// teamspace membership, moves, workspace membership and instance recovery.
// The activity scope has already admitted a guest's recoverably trashed share.
func workspaceActivityRefreshToken(app core.App, user *core.Record, scope workspaceAuthorization) (string, error) {
	// Page-grant authorization deliberately has no workspace role. Let
	// the existing guest snapshot owner admit that scope and assign its
	// token role, including a share in recoverable trash.
	if scope.Role == "" {
		snapshot, err := readGuestWorkspaceChangeSnapshot(app, user.Id, scope.WorkspaceID, true)
		if err != nil {
			return "", err
		}
		return sealWorkspaceRefreshToken(user, snapshot)
	}
	rows, err := loadWorkspaceChangeSnapshotRows(app, dbx.Params{"principalId": user.Id, "workspace0": scope.WorkspaceID}, []string{"{:workspace0}"})
	if err != nil {
		return "", err
	}
	if len(rows) != 1 {
		return "", errWorkspaceChangeBaselineAmbiguous
	}
	snapshot := rows[0]
	snapshot.PrincipalID = user.Id
	if !validWorkspaceChangeSnapshot(snapshot) {
		return "", errWorkspaceChangeBaselineAmbiguous
	}
	return sealWorkspaceRefreshToken(user, snapshot)
}

func workspaceActivityPage(app core.App, actor string, scope workspaceAuthorization, id string, cache map[string]*pageAuthorization) (*pageAuthorization, error) {
	if id == "" {
		return nil, nil
	}
	if page, found := cache[id]; found {
		return page, nil
	}
	page, err := authorizePageAccessRole(app, actor, id, pageAccessRoleView)
	if isPermissionDenied(err) || errors.Is(err, sql.ErrNoRows) {
		cache[id] = nil
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if page.Workspace.WorkspaceID != scope.WorkspaceID || page.Workspace.OwnerID != scope.OwnerID {
		cache[id] = nil
		return nil, nil
	}
	cache[id] = &page
	return &page, nil
}

func visibleWorkspaceActivity(ctx context.Context, app core.App, actor string, scope workspaceAuthorization, row storedWorkspaceActivity, targets workspaceActivityTargets, cache map[string]*pageAuthorization, places workspaceActivityPlaces) (workspaceActivityRecord, bool, error) {
	var metadata workspaceActivityMetadata
	var item workspaceActivityRecord
	if json.Unmarshal([]byte(row.Metadata), &metadata) != nil || metadata.PageID == "" {
		return item, false, nil
	}
	if metadata.OperationJobID != "" && !targets.shownJobs[metadata.OperationJobID] {
		return item, false, nil
	}
	// A page copy's sub-page is a change of its edit, no line of its own, as
	// copies made before that was so wrote it (recordWorkspaceCopiedBlock).
	if row.EntityType == "block" && metadata.SubjectPageID != "" && targets.copyJobs[metadata.OperationJobID] == pageDuplicationJobKind {
		return item, false, nil
	}
	page, err := workspaceActivityPage(app, actor, scope, metadata.PageID, cache)
	if err != nil || page == nil {
		return item, false, err
	}
	// The page a block is on, or a view's database is held by, before a
	// sub-page block's own page stands for it.
	holder := page
	if metadata.SubjectPageID != "" {
		subject, err := workspaceActivityPage(app, actor, scope, metadata.SubjectPageID, cache)
		if err != nil || subject == nil {
			return item, false, err
		}
		page = subject
	}
	if visible, err := workspaceActivityTargetVisible(app, actor, scope, row, targets, cache); err != nil || !visible {
		return item, false, err
	}
	// Linked database activity also needs the live source page's authority.
	if sourcePage := targets.sources[metadata.SourceID]; sourcePage != "" {
		source, err := workspaceActivityPage(app, actor, scope, sourcePage, cache)
		if err != nil || source == nil {
			return item, false, err
		}
	}
	// A group shows when it last changed, as the feed orders it.
	item = workspaceActivityRecord{ID: row.ID, Action: row.Action, EntityType: row.EntityType, EntityID: row.EntityID,
		PageID: page.Page.Id, BlockID: metadata.BlockID, ActorID: row.ActorID, Name: metadata.Name, PreviousName: metadata.PreviousName, Kind: metadata.Kind,
		OccurredAt: notificationInboxTime(row.LastAt),
		HasChanges: workspaceActivityHasChanges(row, metadata, page.Page) || workspaceActivityCopyKind(row, metadata, targets.copyJobs) != "", Through: metadata.Through}
	// A page back from the trash is its block restored, a row made again, and
	// a page at the top put in the trash its block deleted too
	// (workspace_activity_page_block.go).
	ownBlock := readWorkspaceActivityTrashRestore(&item, page.Page) || workspaceActivityOwnBlock(row.EntityType, metadata)
	// A page's own made and deleted lines say where it is now
	// (workspace_activity_page_scope.go).
	if workspaceActivityScoped(item) {
		if item.Scope, err = workspaceActivityPageScope(app, page.Page); err != nil {
			return item, false, err
		}
	}
	// The lock, while only the reader opens the page (workspace_activity_only_reader.go).
	if item.OnlyReader, err = targets.onlyReader.of(app, actor, page); err != nil {
		return item, false, err
	}
	// A database's, its views' and its properties' lines name the database
	// as it is named now, as Notion's do (seen 2026-11-13: lines written
	// while it was "새 데이터베이스" read its new name once it had one).
	if row.EntityType == "dataSource" || row.EntityType == "view" || row.EntityType == "property" {
		name, known := targets.sourceNames[metadata.SourceID]
		switch {
		case known:
			item.DatabaseName = strings.TrimSpace(name)
		case row.EntityType == "dataSource":
			item.DatabaseName = metadata.Name
		default:
			item.DatabaseName = metadata.DatabaseName
		}
		// An inline database's lines link to it opened alone.
		if item.BlockID, err = workspaceActivityDatabaseBlock(app, row.EntityType, row.EntityID, metadata.SourceID, page.Page); err != nil {
			return item, false, err
		}
	}
	// A sub-page's block line names the sub-page as it is named now, as
	// Notion's do: one made with no name and named after reads its name; a
	// page's own block's line too.
	subPageBlock := row.EntityType == "block" && metadata.SubjectPageID != ""
	if subPageBlock || ownBlock || item.Name == "" && row.EntityType == "page" && row.EntityID == page.Page.Id {
		if item.Name, err = workspaceActivityPageName(app, page.Page); err != nil {
			return item, false, err
		}
	}
	// A page's line names it while untitled by its kind where the reader's
	// list doesn't hold it (page_name_kind.go): a full-page database "새
	// 데이터베이스".
	if row.EntityType == "page" && item.Name == "" {
		kind, err := pageNameKindOf(app, page.Page)
		if err != nil {
			return item, false, err
		}
		if kind.Kind == pageKindDatabase {
			item.PageKind, item.ViewBlock, item.SourceTitles = kind.Kind, kind.ViewBlock, kind.SourceTitles
		}
	}
	// A database's page, its own block's line too, by what its title's place
	// and path call it (pagePathUntitledName): a task database's "새 작업".
	if (row.EntityType == "page" || ownBlock) && item.Name == "" && isDatabaseRowPageRecord(page.Page) {
		if item.UntitledName, err = pagePathUntitledName(ctx, app, actor, page.Page); err != nil {
			return item, false, err
		}
	}
	if item.Integrations, item.IntegrationsOnly, err = workspaceActivityIntegrations(app, row); err != nil {
		return item, false, err
	}
	// Where it is, read for the feed's lines (a line's changes need none);
	// a line that says the page is private or the workspace's says no place.
	// A page's own block is placed as its page's line is.
	if places != nil && item.Scope == "" {
		placed := row.EntityType
		if ownBlock {
			placed = "page"
		}
		if item.Location, err = places.read(ctx, app, actor, placed, holder.Page, page.Page); err != nil {
			return item, false, err
		}
	}
	return item, true, nil
}

// workspaceActivityIntegrations are the integrations a group's versions came
// through, and whether all of them did: Notion's workspace updates name an
// integration's edit by it alone, and "나와 〈integration〉님" when its
// person edited too (seen 2026-11-06).
func workspaceActivityIntegrations(app core.App, row storedWorkspaceActivity) ([]string, bool, error) {
	first, last := row.FirstVersion, row.LastVersion
	if !first.Valid || !last.Valid {
		// A page's making keeps no versions of its own; its first version
		// says what made it.
		if row.PageID == "" || row.Action != "create" || row.EntityType != "page" || row.EntityID != row.PageID {
			return nil, false, nil
		}
		first, last = sql.NullInt64{Valid: true}, sql.NullInt64{Valid: true}
	}
	versions := []struct {
		ConnectionID   string `db:"connection_id"`
		ConnectionName string `db:"connection_name"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[connection_id]],[[connection_name]] FROM {{leafen_page_history_versions}}
			INDEXED BY [[idx_leafen_page_history_versions_page_frontier]]
		WHERE [[page_id]]={:pageId} AND [[version_cursor]]>={:first} AND [[version_cursor]]<={:last}
	`).Bind(dbx.Params{"pageId": row.PageID, "first": first.Int64, "last": last.Int64}).All(&versions); err != nil {
		return nil, false, err
	}
	names, only := []string{}, len(versions) > 0
	for _, version := range versions {
		if pageHistoryConnectionKind(version.ConnectionID) != "integration" || version.ConnectionName == "" {
			only = false
			continue
		}
		if !slices.Contains(names, version.ConnectionName) {
			names = append(names, version.ConnectionName)
		}
	}
	if len(names) == 0 {
		return nil, false, nil
	}
	return names, only, nil
}
