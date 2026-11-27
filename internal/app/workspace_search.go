package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const workspaceSearchStripes = 64

var searchDrainStripes [workspaceSearchStripes]sync.Mutex

type workspaceSearchCursor struct {
	PrincipalID string `json:"principalId"`
	WorkspaceID string `json:"workspaceId"`
	QueryHash   string `json:"queryHash"`
	Revision    int    `json:"revision"`
	Score       int64  `json:"score"`
	PageID      string `json:"pageId"`
	Row         int64  `json:"row,omitempty"`
}

type workspaceSearchIndexCursor struct {
	PrincipalID string `json:"principalId"`
	WorkspaceID string `json:"workspaceId"`
	QueryHash   string `json:"queryHash"`
	Revision    int    `json:"revision"`
	// The last ranked match a call that goes on has checked.
	CheckedScore  int64  `json:"checkedScore"`
	CheckedPageID string `json:"checkedPageId"`
	CheckedRow    int64  `json:"checkedRow,omitzero"`
	// The matches found so far, in order.
	CandidatePageIDs []string `json:"candidatePageIds"`
	CandidateScores  []int64  `json:"candidateScores"`
	CandidateRows    []int64  `json:"candidateRows"`
	Limit            int      `json:"limit"`
	// The last result shown before, where this page of results starts.
	AfterScore  int64  `json:"afterScore"`
	AfterPageID string `json:"afterPageId"`
	AfterRow    int64  `json:"afterRow,omitzero"`
}

type workspaceSearchRebuildCursor struct {
	PrincipalID string `json:"principalId"`
	WorkspaceID string `json:"workspaceId"`
	PageID      string `json:"pageId"`
}

type workspaceSearchRebuildPage struct {
	PageID string `db:"page_id"`
}

type storedWorkspaceSearchResult struct {
	PageID                 string  `db:"page_id"`
	WorkspaceID            string  `db:"workspace_id"`
	TeamspaceID            string  `db:"teamspace_id"`
	Title                  string  `db:"title"`
	Content                string  `db:"content"`
	Revision               int     `db:"revision"`
	Cursor                 int     `db:"cursor"`
	ParentID               string  `db:"parent_id"`
	Position               float64 `db:"position"`
	IsLocked               bool    `db:"is_locked"`
	LockedBy               string  `db:"locked_by"`
	InTrash                bool    `db:"in_trash"`
	TrashedAt              string  `db:"trashed_at"`
	ArchivedAt             string  `db:"archived_at"`
	ArchivedBy             string  `db:"archived_by"`
	Icon                   string  `db:"icon"`
	IconType               string  `db:"icon_type"`
	Cover                  string  `db:"cover"`
	CoverPosition          int     `db:"cover_position"`
	Font                   string  `db:"font"`
	SmallText              bool    `db:"small_text"`
	FullWidth              bool    `db:"full_width"`
	BacklinksDisplay       string  `db:"backlinks_display"`
	PageCommentsDisplay    string  `db:"page_comments_display"`
	TableOfContentsDisplay string  `db:"table_of_contents_display"`
	InlineCommentsDisplay  string  `db:"inline_comments_display"`
	Kind                   string  `db:"kind"`
	UpdatedAt              string  `db:"updated"`
	CreatedAt              string  `db:"created"`
	CreatedBy              string  `db:"created_by"`
	LastEditedBy           string  `db:"last_edited_by"`
	// A page's verification is its own, in a wiki or not (bootstrap.go).
	VerifiedAt            string `db:"verified_at"`
	VerifiedBy            string `db:"verified_by"`
	VerificationExpiresAt string `db:"verification_expires_at"`
	Preview               string `db:"preview"`
	Score                 int64  `db:"score"`
	// Partial: some of the page's documents' words are not in its entry.
	Partial bool `db:"partial"`
	// PassageLater: a short page indexed in chunks, whose passage the results
	// pick together after (workspaceSearchChunkPassages).
	PassageLater bool `db:"passage_later"`
	// A database item names the page and database that hold it.
	RowContainerPageID string `db:"row_container_page_id"`
	RowDataSourceID    string `db:"row_data_source_id"`
	// TitleFolded is a database item's title as search finds it, without
	// its mentions' words, which its title shown holds (database_row_search.go).
	TitleFolded string `db:"title_folded"`
	// InlineDatabase: a database held in a page's body, named by its block's
	// ID, under the page holding it; Template: a database template, as an
	// item of its database (workspace_search_databases.go).
	InlineDatabase bool `db:"inline_database"`
	Template       bool `db:"template"`
	// readAs: its title and line are as its reader reads it, not as the
	// index keeps it (workspace_search_reader.go), its passage that reading's.
	readAs  bool
	passage workspaceSearchPassage
	// found is the tier it was found in (workspaceSearchCandidate.tier).
	found int64
}

func registerWorkspaceSearchRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/workspaces/{workspaceId}/search", searchWorkspace)
	event.Router.GET(apiRoot+"/workspaces/{workspaceId}/search/highlights", readWorkspaceSearchHighlights)
	event.Router.GET(apiRoot+"/pages/{pageId}/preview", readPagePreview)
	event.Router.GET(apiRoot+"/pages/{pageId}/search-block", readWorkspaceSearchBlock)
	event.Router.GET(apiRoot+"/database-containers/{containerId}/preview", readDatabaseMentionPreview)
	event.Router.GET(apiRoot+blockMentionPreviewPath, readBlockMentionPreview)
	event.Router.POST(apiRoot+"/workspaces/{workspaceId}/search/rebuild", rebuildWorkspaceSearchAsking)
}

func workspaceSearchInternalError(
	event *core.RequestEvent,
	cause error,
	operation persistentWriteStorageOperation,
	stage string,
	fallbackCode string,
	fallbackMessage string,
) error {
	if storageError, ok := persistentWriteStorageAPIError(
		event, cause, operation, stage,
	); ok {
		return storageError
	}
	return apiErrorWithCause(
		event, http.StatusInternalServerError, fallbackCode, fallbackMessage,
		string(operation), stage, fallbackCode, cause,
	)
}

func rebuildWorkspaceSearch(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	input := workspaceSearchRebuildInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceSearchCursorInvalid, "Check the search rebuild position.")
	}
	cursor, err := decodeWorkspaceSearchRebuildCursor(input.Cursor, user.Id, workspaceID)
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceSearchCursorInvalid, "Check the search rebuild position.")
	}
	authorization, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceOwner)
	if isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	}
	if err != nil {
		return workspaceSearchInternalError(
			event, err, persistentWriteWorkspaceSearchRebuild, "authorize",
			errorCodeWorkspaceSearchRebuildFailed, "Couldn't start the search rebuild.",
		)
	}
	pages := []workspaceSearchRebuildPage{}
	// Database items, databases held in pages' bodies and templates are
	// indexed beside pages and rebuilt with them.
	err = event.App.DB().NewQuery(`
		SELECT [[page_id]] FROM (
			SELECT [[id]] AS [[page_id]] FROM {{leafen_pages}}
			WHERE [[workspace]] = {:workspaceId} AND [[owner]] = {:ownerId} AND [[data_source_id]] = ''
			UNION ALL
			SELECT [[row_id]] AS [[page_id]] FROM {{leafen_database_row_pages}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[owner]] = {:ownerId}
			UNION ALL
			SELECT [[id]] AS [[page_id]] FROM {{leafen_database_containers}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[owner]] = {:ownerId} AND [[kind]] = 'source'
			UNION ALL
			SELECT [[id]] AS [[page_id]] FROM {{leafen_database_templates}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[owner]] = {:ownerId}
		)
		WHERE [[page_id]] > {:afterPage}
		ORDER BY [[page_id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"workspaceId": authorization.WorkspaceID, "ownerId": authorization.OwnerID,
		"afterPage": cursor.PageID, "limit": workspaceSearchProjectionWindow + 1,
	}).All(&pages)
	if err != nil {
		return workspaceSearchInternalError(
			event, err, persistentWriteWorkspaceSearchRebuild, "list-pages",
			errorCodeWorkspaceSearchRebuildFailed, "Couldn't start the search rebuild.",
		)
	}
	hasMore := len(pages) > workspaceSearchProjectionWindow
	if hasMore {
		pages = pages[:workspaceSearchProjectionWindow]
	}
	if len(pages) > 0 {
		err = event.App.RunInTransaction(func(txApp core.App) error {
			ids := make([]string, 0, len(pages))
			for _, page := range pages {
				ids = append(ids, page.PageID)
			}
			encoded, err := json.Marshal(ids)
			if err != nil {
				return err
			}
			_, err = txApp.DB().NewQuery(`
				INSERT INTO {{leafen_search_dirty}} ([[page_id]], [[workspace_id]], [[version]], [[enqueued_at]])
				SELECT [[value]], {:workspaceId}, 1, {:now} FROM json_each({:ids}) WHERE TRUE
				ON CONFLICT ([[page_id]]) DO UPDATE SET
					[[workspace_id]] = excluded.[[workspace_id]],
					[[version]] = [[leafen_search_dirty]].[[version]] + 1,
					[[enqueued_at]] = excluded.[[enqueued_at]]
			`).Bind(dbx.Params{
				"now": time.Now().UnixMilli(), "workspaceId": authorization.WorkspaceID, "ids": string(encoded),
			}).Execute()
			return err
		})
		if err != nil {
			return workspaceSearchInternalError(
				event, err, persistentWriteWorkspaceSearchRebuild, "enqueue",
				errorCodeWorkspaceSearchRebuildFailed, "Couldn't start the search rebuild.",
			)
		}
	}
	response := workspaceSearchRebuildResponse{Queued: len(pages), HasMore: hasMore}
	if hasMore && len(pages) > 0 {
		next, err := encodeSearchCursor(workspaceSearchRebuildCursor{
			PrincipalID: user.Id, WorkspaceID: workspaceID, PageID: pages[len(pages)-1].PageID,
		})
		if err != nil {
			return workspaceSearchInternalError(
				event, err, persistentWriteWorkspaceSearchRebuild, "encode-cursor",
				errorCodeWorkspaceSearchRebuildFailed, "Couldn't start the search rebuild.",
			)
		}
		response.NextCursor = &next
	}
	return event.JSON(http.StatusOK, response)
}

func searchWorkspace(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	query := strings.TrimSpace(event.Request.URL.Query().Get("q"))
	if workspaceID == "" {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceSearchInvalid, refusalMessage("Check the search query.", "the workspace is empty"))
	}
	if count := utf8.RuneCountInString(query); count > maxWorkspaceSearchQueryRunes {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceSearchInvalid,
			refusalMessage("Check the search query.", fmt.Sprintf("q is at most %d characters, not %d", maxWorkspaceSearchQueryRunes, count)))
	}
	limit, err := workspaceSearchLimit(event.Request.URL.Query().Get("limit"))
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceSearchInvalid,
			refusalMessage("Check the search scope.", fmt.Sprintf("limit is from 1 to %d", maxWorkspaceSearchResults)))
	}
	filter, err := parseWorkspaceSearchFilter(event.Request.URL.Query())
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceSearchInvalid, refusalMessage("Check the search filters.", refusalReason(err)))
	}
	// Without keywords, search browses by its filters or sort.
	if query == "" {
		browse := false
		if filter, browse = filter.forBrowse(); !browse {
			return apiError(event, http.StatusBadRequest, errorCodeWorkspaceSearchInvalid,
				refusalMessage("Check the search query.", "q is empty: give words to look for, or a filter or sort to browse by"))
		}
	}
	queryFolded := foldSearchText(query)
	queryHash := workspaceSearchQueryHash(queryFolded + filter.hashKey())
	cursor, err := decodeWorkspaceSearchCursor(
		event.Request.URL.Query().Get("cursor"), user.Id, workspaceID, queryHash, filter.bestMatch(),
	)
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceSearchCursorInvalid, "Check the search position.")
	}
	indexCursor, hasIndexCursor, err := decodeWorkspaceSearchIndexCursor(
		event.Request.URL.Query().Get("indexCursor"), user.Id, workspaceID, queryHash, filter.bestMatch(),
	)
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceSearchCursorInvalid, "Check the search index position.")
	}

	authorization, err := authorizeWorkspaceReader(event.App, user.Id, workspaceID)
	if isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	}
	if err != nil {
		return workspaceSearchInternalError(
			event, err, persistentReadWorkspaceSearch, "authorize",
			errorCodeWorkspaceSearchFailed, "Couldn't search.",
		)
	}

	keywords := workspaceSearchMatchFor(queryFolded)
	if keywords.Exact == "" && query != "" {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceSearchInvalid,
			refusalMessage("Check the search query.", "q holds no words to look for"))
	}
	plainQuery := workspaceSearchPlainText(queryFolded)
	step, err := advanceWorkspaceSearch(
		event.App, authorization, user.Id, plainQuery, keywords,
		cursor, indexCursor, hasIndexCursor, limit, filter,
	)
	if err != nil {
		if errors.Is(err, errWorkspaceSearchRevisionChanged) {
			return apiError(event, http.StatusConflict, errorCodeWorkspaceSearchCursorInvalid, "The search results changed. Search again from the start.")
		}
		return workspaceSearchInternalError(
			event, err, persistentReadWorkspaceSearch, "advance",
			errorCodeWorkspaceSearchFailed, "Couldn't search.",
		)
	}
	response := workspaceSearchResponse{
		Results: []workspaceSearchResult{}, HasMore: step.HasMore && !step.Indexing,
		Indexing: step.Indexing, Revision: step.Revision, Actors: []workspaceSearchActor{},
		Backlog: !step.Indexing && workspaceSearchBacklog(event.App, workspaceID),
	}
	// Results carry what the page list carries: who made a page, when, and
	// its wiki verification.
	pageIDs := make([]string, 0, len(step.Rows))
	for _, row := range step.Rows {
		if row.RowContainerPageID == "" && !row.InlineDatabase {
			pageIDs = append(pageIDs, row.PageID)
		}
	}
	wikiMetadata, err := loadWikiPageMetadataSet(event.App, pageIDs)
	if err != nil {
		return workspaceSearchInternalError(
			event, err, persistentReadWorkspaceSearch, "wiki-metadata",
			errorCodeWorkspaceSearchFailed, "Couldn't search.",
		)
	}
	editors := make([]string, 0, len(step.Rows))
	found := []contentRead{}
	// A database page's path names its inline database, as Notion's does.
	locations := newInlineDatabaseLocations(event.App, user.Id, workspaceID).reading(event.Request.Context())
	sourceCloser := readerCloser(event.App, user.Id)
	for _, row := range step.Rows {
		match := workspaceSearchMatchBody
		preview := row.Preview
		// A page is found by its title when its title answers the keywords
		// as the page was found — all of them, or, among the pages matching
		// some, its phrases and some words (workspace_search_keywords.go);
		// it still shows the line of its body holding the most of them, one
		// alone too, as Notion's results do (seen 2026-11-11: "vorquen
		// plistar" shows "vorquen only line one" under the page "W383 Vorquen
		// Plistar one", and under "W383 Vorquen two"). The line shown is
		// always the body's own passage (workspace_search_passage.go), never
		// the folded index.
		if workspaceSearchFoundByTitle(keywords.Keywords, row.Title, row.found) {
			match = workspaceSearchMatchTitle
			if !keywords.Keywords.holdsAny(preview) {
				preview = ""
			}
		}
		if preview != "" {
			passage := row.passage
			if !row.readAs {
				if passage, err = workspaceSearchOriginalPassage(event.App, row.PageID, plainQuery); err != nil {
					return workspaceSearchInternalError(
						event, err, persistentReadWorkspaceSearch, "preview-line",
						errorCodeWorkspaceSearchFailed, "Couldn't search.",
					)
				}
			}
			if match == workspaceSearchMatchTitle {
				if passage, err = searchPassageWithoutSubPages(event.App, row.PageID, row.Title, keywords.Keywords, passage); err != nil {
					return workspaceSearchInternalError(
						event, err, persistentReadWorkspaceSearch, "preview-sub-pages",
						errorCodeWorkspaceSearchFailed, "Couldn't search.",
					)
				}
			}
			preview = passage.line()
		}
		if row.LastEditedBy != "" {
			editors = append(editors, row.LastEditedBy)
		} else {
			editors = append(editors, row.CreatedBy)
		}
		page := pageResponse{
			ID: row.PageID, WorkspaceID: row.WorkspaceID, Title: row.Title,
			TeamspaceID: row.TeamspaceID,
			Content:     row.Content, Revision: row.Revision, Cursor: row.Cursor,
			ParentID: row.ParentID, Position: row.Position, IsLocked: row.IsLocked, LockedBy: row.LockedBy,
			InTrash: row.InTrash, TrashedAt: optionalPageTrashTimestamp(row.TrashedAt),
			ArchivedAt: optionalPageTrashTimestamp(row.ArchivedAt), ArchivedBy: row.ArchivedBy,
			Icon:     row.Icon,
			IconType: row.IconType, Cover: row.Cover,
			CoverPosition: row.CoverPosition,
			Font:          row.Font, SmallText: row.SmallText, FullWidth: row.FullWidth,
			BacklinksDisplay: row.BacklinksDisplay, PageCommentsDisplay: row.PageCommentsDisplay,
			TableOfContentsDisplay: normalizedPageTableOfContentsDisplay(&row.TableOfContentsDisplay), InlineCommentsDisplay: normalizedPageInlineCommentsDisplay(&row.InlineCommentsDisplay),
			Kind:      readPageKind(row.Kind),
			CreatedBy: row.CreatedBy, LastEditedBy: row.LastEditedBy,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			VerifiedAt: row.VerifiedAt, VerifiedBy: row.VerifiedBy, VerificationExpiresAt: row.VerificationExpiresAt,
		}
		if metadata, isWikiPage := wikiMetadata[row.PageID]; isWikiPage {
			page = applyWikiMetadataToPageResponse(page, metadata)
		}
		// A wiki or a full-page database whose name holds every keyword shows
		// no body line, as Notion's search row for it (seen 2026-11-11: "W374"
		// finds the wiki "W374 wiki probe" with none, though its home reads
		// "w374 home line"); found by its home's words alone, it shows their line.
		if all, _ := keywords.Keywords.match(row.Title); all && match == workspaceSearchMatchTitle &&
			(page.Kind == pageKindDatabase || page.IsWiki && page.WikiDataSourceID != "") {
			preview = ""
		}
		if page.Kind == pageKindDatabase && !row.InlineDatabase {
			if page.ViewBlock, err = isViewBlockDatabasePage(event.App, row.PageID); err != nil {
				return workspaceSearchInternalError(
					event, err, persistentReadWorkspaceSearch, "view-block",
					errorCodeWorkspaceSearchFailed, "Couldn't search.",
				)
			}
		}
		rowResult := workspaceSearchRow(row)
		if rowResult != nil {
			location, err := locations.ofRow(rowResult)
			if err != nil {
				return workspaceSearchInternalError(
					event, err, persistentReadWorkspaceSearch, "row-location",
					errorCodeWorkspaceSearchFailed, "Couldn't search.",
				)
			}
			rowResult.InlineDatabase, rowResult.DatabaseTitle = location.Inline, location.Title
			// A row is called by its database's word for its untitled pages,
			// and so is a template with neither a title nor a name of its own.
			rowResult.Template = row.Template
			page.UntitledName = location.UntitledRow
		}
		// A database held in a page's body opens alone, as its own page.
		var database *workspaceSearchDatabaseResult
		if row.InlineDatabase {
			database = &workspaceSearchDatabaseResult{ContainerID: row.PageID, PageID: row.ParentID}
			// One of several sources is listed once for each (workspace_search_database_sources.go).
			if err := fillWorkspaceSearchDatabaseSources(event.App, sourceCloser, database); err != nil {
				return workspaceSearchInternalError(
					event, err, persistentReadWorkspaceSearch, "database-sources",
					errorCodeWorkspaceSearchFailed, "Couldn't search.",
				)
			}
		}
		dataSourceID := ""
		if rowResult != nil {
			dataSourceID = rowResult.DataSourceID
		}
		file, err := workspaceSearchFileOf(event.App, row.PageID, dataSourceID, preview)
		if err != nil {
			return workspaceSearchInternalError(event, err, persistentReadWorkspaceSearch, "file", errorCodeWorkspaceSearchFailed, "Couldn't search.")
		}
		// A page found by its body opens at its line's block, found when the
		// result is opened (workspace_search_block.go).
		response.Results = append(response.Results, workspaceSearchResult{
			Match: match, Page: page, Preview: preview, Row: rowResult, Database: database, Partial: row.Partial, File: file,
		})
		// A result's passage is a read of its page for the read log (read_log.go).
		if preview != "" {
			found = append(found, contentRead{Kind: "page", ID: row.PageID, How: contentReadList})
		}
	}
	// Each result's title comes with its spans as its reader reads them, for
	// its custom emoji to be drawn, as the sidebar's (page_title_spans.go),
	// says whether its body holds anything (page_has_content.go) and, an untitled
	// database of several sources, their titles (full_page_database_sources_name.go).
	resultPages := make([]*pageResponse, len(response.Results))
	for index := range response.Results {
		resultPages[index] = &response.Results[index].Page
	}
	pagesTitleSpansAsRead(event.App, user.Id, resultPages)
	if err := errors.Join(withPageContents(event.App, resultPages), nameUntitledDatabasesBySources(event.App, resultPages)); err != nil {
		return workspaceSearchInternalError(event, err, persistentReadWorkspaceSearch, "contents", errorCodeWorkspaceSearchFailed, "Couldn't search.")
	}
	recordContentReads(event.App, workspaceID, contentReader{UserID: user.Id, Via: contentReadViaApp}, found)
	// Each result names the person who last edited its page.
	profiles, err := readCanonicalActorProfiles(event.App, editors)
	if err != nil {
		return workspaceSearchInternalError(
			event, err, persistentReadWorkspaceSearch, "editors",
			errorCodeWorkspaceSearchFailed, "Couldn't search.",
		)
	}
	for _, profile := range profiles {
		response.Actors = append(response.Actors, workspaceSearchActor{
			UserID: profile.UserID, DisplayName: profile.DisplayName, Avatar: profile.Avatar,
		})
	}
	if step.Indexing {
		encoded, err := encodeWorkspaceSearchIndexCursor(step.IndexCursor)
		if err != nil {
			return workspaceSearchInternalError(
				event, err, persistentReadWorkspaceSearch, "encode-index-cursor",
				errorCodeWorkspaceSearchFailed, "Couldn't search.",
			)
		}
		response.IndexCursor = &encoded
	} else if step.HasMore && len(step.Rows) > 0 {
		encoded, err := encodeWorkspaceSearchCursor(workspaceSearchCursor{
			PrincipalID: user.Id, WorkspaceID: workspaceID, QueryHash: queryHash,
			Revision: step.Revision, Score: step.Last.Score, PageID: step.Last.PageID, Row: step.Last.Row,
		})
		if err != nil {
			return workspaceSearchInternalError(
				event, err, persistentReadWorkspaceSearch, "encode-result-cursor",
				errorCodeWorkspaceSearchFailed, "Couldn't search.",
			)
		}
		response.NextCursor = &encoded
	}
	return event.JSON(http.StatusOK, response)
}

// workspaceSearchMatchExpression is keywords as one whole text answers them
// (workspace_search_order.go).
func workspaceSearchMatchExpression(query string) string {
	return workspaceSearchMatchFor(query).Exact
}

func workspaceSearchLimit(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return defaultWorkspaceSearchResults, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > maxWorkspaceSearchResults {
		return 0, permissionDenied()
	}
	return value, nil
}

func workspaceSearchQueryHash(query string) string {
	sum := sha256.Sum256([]byte(query))
	return hex.EncodeToString(sum[:])
}

func workspaceSearchStripe(workspaceID string) *sync.Mutex {
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(workspaceID))
	return &searchDrainStripes[hasher.Sum32()%workspaceSearchStripes]
}

func workspaceSearchRow(row storedWorkspaceSearchResult) *workspaceSearchRowResult {
	if row.RowContainerPageID == "" {
		return nil
	}
	return &workspaceSearchRowResult{
		RowID: row.PageID, ContainerPageID: row.RowContainerPageID, DataSourceID: row.RowDataSourceID,
	}
}
