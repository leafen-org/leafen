package app

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

var (
	errPageHistoryCursorInvalid   = errors.New(errorCodePageHistoryCursorInvalid)
	errPageHistoryVersionMissing  = errors.New(errorCodePageHistoryVersionMissing)
	errPageHistoryRevisionChanged = errors.New(errorCodePageHistoryRevisionChanged)
	errPageHistoryRestoreConflict = errors.New(errorCodePageHistoryRestoreConflict)
)

type pageHistoryCursor struct {
	Kind             string `json:"kind"`
	PrincipalID      string `json:"principalId"`
	PageID           string `json:"pageId"`
	VersionID        string `json:"versionId,omitempty"`
	SnapshotFrontier int    `json:"snapshotFrontier"`
	VersionCursor    int    `json:"versionCursor"`
	ParentID         string `json:"parentId,omitempty"`
	Position         string `json:"position,omitempty"`
	BlockID          string `json:"blockId,omitempty"`
	SinceID          string `json:"sinceId,omitempty"`
	Limit            int    `json:"limit"`
}

func registerPageHistoryRoutes(event *core.ServeEvent, ygo *pocketBaseYgoPersistence) {
	event.Router.GET(apiRoot+pageHistoryListPath, listPageHistoryVersions)
	event.Router.GET(apiRoot+pageHistorySnapshotPath, readPageHistorySnapshot)
	event.Router.GET(apiRoot+pageHistoryChangesPath, listPageHistoryChanges)
	event.Router.POST(apiRoot+pageHistoryRestorePath, func(event *core.RequestEvent) error {
		return restorePageHistoryVersionRoute(event, ygo)
	}).Bind(apis.BodyLimit(pageHistoryRestoreMaxBodyBytes))
	event.Router.POST(apiRoot+pageHistoryRevertPath, func(event *core.RequestEvent) error {
		return revertPageHistoryVersionRoute(event, ygo)
	}).Bind(apis.BodyLimit(pageHistoryRestoreMaxBodyBytes))
	registerPageHistoryBookmarkRoutes(event)
	event.Router.GET(apiRoot+pageHistoryBlockLastEditPath, readPageBlockLastEdit)
	event.Router.GET(apiRoot+pageLockRecordListPath, listPageLockRecords)
	registerPageHistoryDatabaseAtRoute(event)
}

// authorizePageHistoryRestore authorizes restoring a page's body, a database
// page's under its row page's permission.
func authorizePageHistoryRestore(
	app core.App, actorID, pageID string,
) (pageAuthorization, error) {
	return authorizePageBody(app, actorID, pageID, permissionWorkspaceWrite)
}

// pageHistoryRestoreRequestHash names a restore; one that takes the page's
// databases back too is another, and one that takes back a database alone.
func pageHistoryRestoreRequestHash(pageID, versionID string, baseRevision int, input pageHistoryRestoreRequest) string {
	value := fmt.Sprintf("page-history-restore\x00%s\x00%s\x00%d", pageID, versionID, baseRevision)
	if input.RestoreDatabases {
		value += "\x00databases"
	}
	if input.KeepDatabaseProperties {
		value += "\x00keep-properties"
	}
	if input.KeepRowValues {
		value += "\x00keep-row-values"
	}
	if input.KeepRowNames {
		value += "\x00keep-row-names"
	}
	if input.DatabaseID != "" {
		value += "\x00database\x00" + input.DatabaseID
	}
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func listPageHistoryVersions(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	limit, err := parsePageHistoryLimit(event.Request.URL.Query().Get("limit"), pageHistoryPageSize)
	if err != nil || pageID == "" {
		return apiError(event, http.StatusBadRequest, errorCodePageHistoryInvalid, "Check the page history query range.")
	}
	response := pageHistoryListResponse{}
	err = runReadSnapshot(event.Request.Context(), event.App, func(txApp core.App) error {
		if _, err := authorizePage(txApp, user.Id, pageID, permissionWorkspaceRead); err != nil {
			return err
		}
		frontier, err := pageHistorySnapshotFrontier(txApp, pageID)
		if err != nil {
			return err
		}
		start, err := pageHistoryStartCursor(txApp, pageID)
		if err != nil {
			return err
		}
		cursor, err := decodePageHistoryCursor(
			event.Request.URL.Query().Get("cursor"), "versions", user.Id, pageID, "", limit,
		)
		if err != nil {
			return err
		}
		if cursor.SnapshotFrontier == 0 {
			cursor.SnapshotFrontier = frontier
			cursor.VersionCursor = frontier + 1
		}
		rows := []storedPageHistoryVersion{}
		if err := txApp.DB().NewQuery(`
			SELECT [[id]],[[page_id]],[[version_cursor]],[[actor_id]],[[source]],[[created_at]],[[connection_name]],[[connection_id]]
			FROM {{leafen_page_history_versions}}
				INDEXED BY [[idx_leafen_page_history_versions_page_frontier]]
			WHERE [[page_id]]={:pageId} AND [[version_cursor]]<={:frontier}
				AND [[version_cursor]]<{:beforeCursor} AND [[version_cursor]]>={:start}
				AND ` + pageHistoryEditVersion("") + `
			ORDER BY [[version_cursor]] DESC,[[id]] DESC LIMIT {:limit}
		`).Bind(dbx.Params{
			"pageId": pageID, "frontier": cursor.SnapshotFrontier,
			"beforeCursor": cursor.VersionCursor, "start": start, "limit": limit + 1,
		}).All(&rows); err != nil {
			return err
		}
		hasMore := len(rows) > limit
		if hasMore {
			rows = rows[:limit]
		}
		versions := make([]pageHistoryVersionWire, 0, len(rows))
		actorIDs := make([]string, 0, len(rows))
		for _, row := range rows {
			versions = append(versions, pageHistoryVersionWire{
				ID: row.ID, PageID: row.PageID, VersionCursor: row.VersionCursor,
				Revision: row.VersionCursor, ActorID: row.ActorID,
				Source: row.Source, CreatedAt: row.CreatedAt, ConnectionName: row.ConnectionName,
				ConnectionKind: pageHistoryConnectionKind(row.ConnectionID),
			})
			actorIDs = append(actorIDs, row.ActorID)
		}
		actorProfiles, err := pageHistoryActorProfiles(txApp, actorIDs)
		if err != nil {
			return err
		}
		response = pageHistoryListResponse{
			PageID: pageID, SnapshotFrontier: cursor.SnapshotFrontier,
			Versions: versions, ActorProfiles: actorProfiles, HasMore: hasMore,
		}
		if event.Request.URL.Query().Get("cursor") == "" {
			if response.Creation, err = pageHistoryCreation(txApp, pageID, cursor.SnapshotFrontier); err != nil {
				return err
			}
		}
		if hasMore && len(rows) > 0 {
			next, err := encodePageHistoryCursor(pageHistoryCursor{
				Kind: "versions", PrincipalID: user.Id, PageID: pageID,
				SnapshotFrontier: cursor.SnapshotFrontier,
				VersionCursor:    rows[len(rows)-1].VersionCursor, Limit: limit,
			})
			if err != nil {
				return err
			}
			response.NextCursor = &next
		}
		return nil
	})
	return writePageHistoryResult(event, response, err, "list")
}

// pageHistoryCreation is a page's first version when a connection made it,
// whatever baseline its history now starts from, so the activity a page's
// menu foot opens can name an integration as its maker the way Notion's
// does (seen 2026-11-05); nil for a page made in the app.
func pageHistoryCreation(app core.App, pageID string, frontier int) (*pageHistoryVersionWire, error) {
	rows := []storedPageHistoryVersion{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[page_id]],[[version_cursor]],[[actor_id]],[[source]],[[created_at]],[[connection_name]],[[connection_id]]
		FROM {{leafen_page_history_versions}}
			INDEXED BY [[idx_leafen_page_history_versions_page_frontier]]
		WHERE [[page_id]]={:pageId} AND [[version_cursor]]<={:frontier}
		ORDER BY [[version_cursor]] ASC,[[id]] ASC LIMIT 1
	`).Bind(dbx.Params{"pageId": pageID, "frontier": frontier}).All(&rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 || rows[0].ConnectionID == "" {
		return nil, nil
	}
	row := rows[0]
	return &pageHistoryVersionWire{
		ID: row.ID, PageID: row.PageID, VersionCursor: row.VersionCursor,
		Revision: row.VersionCursor, ActorID: row.ActorID,
		Source: row.Source, CreatedAt: row.CreatedAt, ConnectionName: row.ConnectionName,
		ConnectionKind: pageHistoryConnectionKind(row.ConnectionID),
	}, nil
}

// pageHistoryActorProfiles are the profiles of the people who made versions.
func pageHistoryActorProfiles(app core.App, actorIDs []string) ([]pageHistoryActorProfile, error) {
	canonicalProfiles, err := readCanonicalActorProfiles(app, actorIDs)
	if err != nil {
		return nil, err
	}
	profiles := make([]pageHistoryActorProfile, 0, len(canonicalProfiles))
	known := map[string]bool{}
	for _, profile := range canonicalProfiles {
		known[profile.UserID] = true
		profiles = append(profiles, pageHistoryActorProfile(profile))
	}
	// A moved workspace's people who have no account here yet go by the
	// names it brought (workspace_move_history_people.go).
	standIns, err := workspaceMovedPeopleProfiles(app, actorIDs, known)
	if err != nil {
		return nil, err
	}
	return append(profiles, standIns...), nil
}

func readPageHistorySnapshot(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	versionID := strings.TrimSpace(event.Request.PathValue("versionId"))
	limit, err := parsePageHistoryLimit(event.Request.URL.Query().Get("limit"), pageHistoryBlockPageSize)
	if err != nil || pageID == "" || versionID == "" {
		return apiError(event, http.StatusBadRequest, errorCodePageHistoryInvalid, "Check the page history query range.")
	}
	response := pageHistorySnapshotResponse{}
	version := storedPageHistoryVersion{}
	cursor := pageHistoryCursor{}
	afterSequence := 0
	err = runReadSnapshot(event.Request.Context(), event.App, func(txApp core.App) error {
		if _, err := authorizePage(txApp, user.Id, pageID, permissionWorkspaceRead); err != nil {
			return err
		}
		var err error
		version, err = loadPageHistoryVersion(txApp, pageID, versionID)
		if pageHistoryVersionMissing(err) {
			return errPageHistoryVersionMissing
		}
		if err != nil {
			return err
		}
		if start, err := pageHistoryStartCursor(txApp, pageID); err != nil {
			return err
		} else if version.VersionCursor < start {
			return errPageHistoryVersionMissing
		}
		frontier, err := pageHistorySnapshotFrontier(txApp, pageID)
		if err != nil {
			return err
		}
		cursor, err = decodePageHistoryCursor(
			event.Request.URL.Query().Get("cursor"), "blocks", user.Id, pageID, versionID, limit,
		)
		if err != nil {
			return err
		}
		if cursor.SnapshotFrontier == 0 {
			cursor.SnapshotFrontier = frontier
			cursor.VersionCursor = version.VersionCursor
		}
		if cursor.VersionCursor != version.VersionCursor || version.VersionCursor > cursor.SnapshotFrontier {
			return errPageHistoryCursorInvalid
		}
		afterSequence, err = pageHistorySnapshotCursorSequence(
			txApp, pageHistorySnapshotMaterializationID(pageID, versionID), cursor,
		)
		return err
	})
	if err != nil {
		return writePageHistoryResult(event, response, err, "snapshot")
	}
	job, err := advancePageHistorySnapshotMaterialization(
		event.App, pageID, versionID, version.VersionCursor,
		cursor.SnapshotFrontier, afterSequence+limit+1,
	)
	if err != nil {
		return writePageHistoryResult(event, response, err, "snapshot")
	}
	err = runReadSnapshot(event.Request.Context(), event.App, func(txApp core.App) error {
		if _, err := authorizePage(txApp, user.Id, pageID, permissionWorkspaceRead); err != nil {
			return err
		}
		state, err := readPageHistorySnapshotPageState(txApp, pageID, version.VersionCursor)
		if err != nil {
			return err
		}
		// A database page's automatic properties as the version shows them
		// (page_history_automatic_values.go).
		if err := fillPageHistorySnapshotAutomaticValues(txApp, pageID, version.VersionCursor, &state); err != nil {
			return err
		}
		blocks, hasMore, err := readPageHistorySnapshotWindow(txApp, job, afterSequence, limit)
		if err != nil {
			return err
		}
		response = pageHistorySnapshotResponse{
			PageID: pageID, VersionID: versionID, SnapshotFrontier: cursor.SnapshotFrontier,
			Revision: version.VersionCursor, Title: state.Title,
			Icon: state.Icon, IconType: state.IconType, Cover: state.Cover,
			CoverPosition: float64(state.CoverPosition), Values: state.Values, Blocks: blocks, HasMore: hasMore,
			// The version's font and small text, which its preview reads in, as Notion's.
			Font: state.Font, SmallText: state.SmallText,
		}
		// The version's databases come with its first blocks.
		if afterSequence == 0 {
			if response.Databases, err = pageHistoryVersionDatabases(txApp, pageID, version.VersionCursor, version.CreatedAt); err != nil {
				return err
			}
		}
		if err := redactPageHistorySnapshot(txApp, user.Id, pageID, &response); err != nil {
			return err
		}
		// What names a page closed to this reader (content_graph_reader_mentions.go).
		pageHistoryAsRead(txApp, user.Id, &response)
		if hasMore && len(blocks) > 0 {
			last := blocks[len(blocks)-1]
			next, err := encodePageHistoryCursor(pageHistoryCursor{
				Kind: "blocks", PrincipalID: user.Id, PageID: pageID, VersionID: versionID,
				SnapshotFrontier: cursor.SnapshotFrontier, VersionCursor: version.VersionCursor,
				ParentID: last.ParentID, Position: last.Position, BlockID: last.ID, Limit: limit,
			})
			if err != nil {
				return err
			}
			response.NextCursor = &next
		}
		return nil
	})
	return writePageHistoryResult(event, response, err, "snapshot")
}

func restorePageHistoryVersionRoute(event *core.RequestEvent, ygo *pocketBaseYgoPersistence) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	versionID := strings.TrimSpace(event.Request.PathValue("versionId"))
	input := pageHistoryRestoreRequest{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodePageHistoryInvalid, "Check the restore request.")
	}
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.DatabaseID = strings.TrimSpace(input.DatabaseID)
	if pageID == "" || versionID == "" || input.RequestID == "" ||
		len([]rune(input.RequestID)) > 255 || len(input.DatabaseID) > 255 || input.BaseRevision < 0 {
		return apiError(event, http.StatusBadRequest, errorCodePageHistoryInvalid, "Check the restore request.")
	}
	if input.DatabaseID != "" {
		// One database alone (page_history_database_alone_restore.go).
		response, err := admitPageHistoryDatabaseRestore(event.App, user.Id, pageID, versionID, input)
		if err == nil {
			err = ygo.restorePageHistoryDatabaseContents(event.Request.Context(), user.Id, pageID, versionID, input)
			ygo.notifyJobsAfterCommit()
		}
		pageResponseAsRead(event.App, user.Id, &response.Page)
		return writePageHistoryResult(event, response, err, "restore")
	}
	// The typing the page's room took and hasn't stored yet (it gathers typing
	// 2–10 s) is stored first, so the restore covers all that reached the
	// server before its press, as Notion's restore covers typing from another
	// tab a second before it (seen 2026-11-15); merged in after it, that
	// typing stayed on the restored page. Stored typing is a newer version,
	// which the history's restore goes ahead over (PageHistoryDialog).
	storeRoomTypingAhead(event.Request.Context(), event.App, pageID)
	response, err := restorePageHistoryVersion(event.App, user.Id, pageID, versionID, input)
	if err == nil {
		// A version from before 2026-11-21 may hold blocks under a synced
		// copy, which go where they are seen (applyStoredContentGraphMutation).
		ygo.bringSyncedCopyLeftoversIntoSight(pageID)
		// A database page's property values follow its body, as its values
		// route writes them; a repeated request writes them again.
		err = ygo.restoreDatabaseRowPageHistoryValues(event.Request.Context(), user.Id, pageID, versionID, input.RequestID)
	}
	if err == nil && input.RestoreDatabases {
		// The databases the page holds take the version's views, properties
		// and rows; a repeated request finishes what one before left.
		err = ygo.restorePageHistoryDatabaseContents(event.Request.Context(), user.Id, pageID, versionID, input)
		ygo.notifyJobsAfterCommit()
	}
	// The page's title as its restorer reads it (page_name_mentions.go).
	pageResponseAsRead(event.App, user.Id, &response.Page)
	return writePageHistoryResult(event, response, err, "restore")
}

func parsePageHistoryLimit(raw string, maximum int) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return maximum, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > maximum {
		return 0, errPageHistoryCursorInvalid
	}
	return limit, nil
}

func pageHistoryBlocksAfter(
	blocks []pageHistoryBlockWire, cursor pageHistoryCursor,
) []pageHistoryBlockWire {
	if cursor.BlockID == "" {
		return blocks
	}
	for index, block := range blocks {
		if block.ParentID > cursor.ParentID ||
			(block.ParentID == cursor.ParentID && block.Position > cursor.Position) ||
			(block.ParentID == cursor.ParentID && block.Position == cursor.Position && block.ID > cursor.BlockID) {
			return blocks[index:]
		}
	}
	return blocks[:0]
}

func decodePageHistoryCursor(
	raw, kind, principalID, pageID, versionID string, limit int,
) (pageHistoryCursor, error) {
	if strings.TrimSpace(raw) == "" {
		return pageHistoryCursor{
			Kind: kind, PrincipalID: principalID, PageID: pageID, VersionID: versionID, Limit: limit,
		}, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(decoded) == 0 || len(decoded) > pageHistoryCursorBytes {
		return pageHistoryCursor{}, errPageHistoryCursorInvalid
	}
	cursor := pageHistoryCursor{}
	if json.Unmarshal(decoded, &cursor) != nil || cursor.Kind != kind ||
		cursor.PrincipalID != principalID || cursor.PageID != pageID ||
		cursor.VersionID != versionID || cursor.Limit != limit ||
		cursor.SnapshotFrontier < 1 || cursor.VersionCursor < 1 {
		return pageHistoryCursor{}, errPageHistoryCursorInvalid
	}
	return cursor, nil
}

func encodePageHistoryCursor(cursor pageHistoryCursor) (string, error) {
	encoded, err := json.Marshal(cursor)
	if err != nil || len(encoded) > pageHistoryCursorBytes {
		return "", errPageHistoryCursorInvalid
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func writePageHistoryResult(event *core.RequestEvent, response any, err error, stage string) error {
	switch {
	case err == nil:
		return event.JSON(http.StatusOK, response)
	case isPermissionDenied(err):
		return apiError(event, http.StatusNotFound, errorCodePageNotFound, "Page not found.")
	case errors.Is(err, errPageHistoryCursorInvalid):
		return apiError(event, http.StatusBadRequest, errorCodePageHistoryCursorInvalid, "Check the page history query position.")
	case errors.Is(err, errPageHistoryVersionMissing):
		return apiError(event, http.StatusNotFound, errorCodePageHistoryVersionMissing, "Page history version not found.")
	case errors.Is(err, errPageHistoryRevisionChanged):
		return apiError(event, http.StatusConflict, errorCodePageHistoryRevisionChanged, "The page changed. Load the latest state again.")
	case errors.Is(err, errPageHistoryRestoreConflict):
		return apiError(event, http.StatusConflict, errorCodePageHistoryRestoreConflict, refusalMessage("The current page can't be restored.", refusalReason(err)))
	default:
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodePageHistoryFailed,
			"Couldn't complete the page history operation.", "page_history", stage, errorCodePageHistoryFailed, err)
	}
}
