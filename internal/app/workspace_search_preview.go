package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// readPagePreview gives a page's title, icon and the first lines of its body,
// in the page's order, for the card a page mention shows on hover, as
// Notion's does (seen 2026-09-30); with blocks=1, its first blocks too. It reads the page as search indexes it,
// now rather than from the index, which catches up only as people search. A
// reader sees it for a page search would show them, database pages included,
// and as they read it: without a sub-page closed to them, a database page's
// title with a mention of a page closed to them "Untitled"
// (workspace_search_reader.go). It says whether the page is in the trash,
// itself or under a page there, and who sent it there and when
// (mention_trash_mark.go); with trash=1, as a mention reads it, it answers for
// a page in the trash the reader may open, which search leaves out.
func readPagePreview(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	if pageID == "" {
		return apiError(event, http.StatusNotFound, errorCodePageNotFound, "Page not found.")
	}
	visible, err := filterWorkspaceSearchCandidates(event.App, user.Id, []workspaceSearchCandidate{{PageID: pageID}})
	if err != nil {
		return workspaceSearchInternalError(
			event, err, persistentReadWorkspaceSearch, "preview-access",
			errorCodeWorkspaceSearchFailed, "Couldn't read the page.",
		)
	}
	inTrash, trashMark, err := pageMentionTrashOf(event.App, pageID)
	if err == nil && len(visible) == 0 && inTrash && event.Request.URL.Query().Get("trash") == "1" {
		// A page in the trash opens under the trash's banner, as a link opens it.
		var openable map[string]bool
		openable, err = openablePageIDs(event.App, user.Id, []string{pageID})
		if openable[pageID] {
			visible = []workspaceSearchCandidate{{PageID: pageID}}
		}
	}
	if err != nil {
		return workspaceSearchInternalError(
			event, err, persistentReadWorkspaceSearch, "preview-trash",
			errorCodeWorkspaceSearchFailed, "Couldn't read the page.",
		)
	}
	if len(visible) == 0 {
		return apiError(event, http.StatusNotFound, errorCodePageNotFound, "Page not found.")
	}
	page, err := event.App.FindRecordById(pagesCollection, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return apiError(event, http.StatusNotFound, errorCodePageNotFound, "Page not found.")
	}
	if err != nil {
		return workspaceSearchInternalError(
			event, err, persistentReadWorkspaceSearch, "preview-page",
			errorCodeWorkspaceSearchFailed, "Couldn't read the page.",
		)
	}
	reader := personReader(user.Id)
	closes := reader.closes(event.App)
	var lines map[string]bool
	if !isDatabaseRowPageRecord(page) {
		closed, err := workspaceSearchClosedLines(event.App, reader, page.GetString("workspace"), []string{pageID})
		if err != nil {
			return workspaceSearchInternalError(
				event, err, persistentReadWorkspaceSearch, "preview-lines",
				errorCodeWorkspaceSearchFailed, "Couldn't read the page.",
			)
		}
		lines = closed[pageID]
		if lines == nil {
			lines = map[string]bool{}
		}
		closes = subPageLinesCloser(lines)
	}
	projection, err := buildSearchPageAs(event.App, storedSearchDirty{
		PageID: pageID, WorkspaceID: page.GetString("workspace"), Version: 1,
	}, closes)
	if err != nil {
		return workspaceSearchInternalError(
			event, err, persistentReadWorkspaceSearch, "preview-body",
			errorCodeWorkspaceSearchFailed, "Couldn't read the page.",
		)
	}
	if projection.Deleted {
		return apiError(event, http.StatusNotFound, errorCodePageNotFound, "Page not found.")
	}
	// A database page names its database and the page holding it: the card
	// shows them as where it sits, as Notion's does ("Top / … / Database").
	database := struct {
		ContainerPageID string `db:"container_page_id"`
		DataSourceID    string `db:"data_source_id"`
		Title           string `db:"title"`
		Spans           string `db:"title_spans"`
	}{}
	err = event.App.DB().NewQuery(`
		SELECT [[row_page]].[[container_page_id]], [[row_page]].[[data_source_id]], COALESCE([[source]].[[title]], '') AS [[title]],
			COALESCE([[source]].[[title_spans]], '') AS [[title_spans]]
		FROM {{leafen_database_row_pages}} [[row_page]]
		LEFT JOIN {{leafen_data_sources}} [[source]] ON [[source]].[[id]] = [[row_page]].[[data_source_id]]
		WHERE [[row_page]].[[row_id]] = {:pageId} AND [[row_page]].[[workspace_id]] = {:workspaceId}
		LIMIT 1
	`).Bind(dbx.Params{"pageId": pageID, "workspaceId": page.GetString("workspace")}).One(&database)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return workspaceSearchInternalError(
			event, err, persistentReadWorkspaceSearch, "preview-database",
			errorCodeWorkspaceSearchFailed, "Couldn't read the page.",
		)
	}
	// A database's name as its reader reads it (page_name_mentions.go).
	named := readerCloser(event.App, user.Id)
	response := pagePreviewResponse{
		Title: projection.Title, Icon: page.GetString("icon"), IconType: page.GetString("icon_type"),
		Lines: pagePreviewLines(projection.Body), ContainerPageID: database.ContainerPageID,
		DatabaseTitle: databaseNameAsRead(named, database.Title, decodeStoredTextSpans(database.Spans)),
		InTrash:       inTrash, TrashedAt: trashMark.At, TrashedBy: trashMark.By,
	}
	if isFullPageDatabase(page) {
		response.Title = pageTitleAsReadBy(event.App, named, pageID, page.GetString("title"))
	}
	// Its kind, which names it while untitled where the reader's list doesn't
	// hold it (page_name_kind.go).
	kinds, err := loadPageNameKinds(event.App, []string{pageID})
	if err != nil {
		return workspaceSearchInternalError(
			event, err, persistentReadWorkspaceSearch, "preview-kind",
			errorCodeWorkspaceSearchFailed, "Couldn't read the page.",
		)
	}
	if nameKind := kinds[pageID]; nameKind.Kind == pageKindDatabase {
		response.PageKind, response.ViewBlock, response.SourceTitles = nameKind.Kind, nameKind.ViewBlock, nameKind.SourceTitles
	}
	// A database page without a title — a template too, with no name of its
	// own — is called by its database's word for its untitled pages.
	sourceID := database.DataSourceID
	if sourceID == "" && isDatabaseTemplatePageRecord(page) {
		sourceID = page.GetString("data_source_id")
	}
	if response.UntitledName, err = databaseUntitledRowNameFor(event.Request.Context(), event.App, user.Id, sourceID); err != nil {
		return workspaceSearchInternalError(
			event, err, persistentReadWorkspaceSearch, "preview-database",
			errorCodeWorkspaceSearchFailed, "Couldn't read the page.",
		)
	}
	// Search's preview draws the page's blocks (page_preview_blocks.go).
	if event.Request.URL.Query().Get("blocks") == "1" {
		blocks, err := pagePreviewBlocks(event.App, pageID, page.GetString("workspace"), user.Id, lines)
		if err != nil {
			return workspaceSearchInternalError(
				event, err, persistentReadWorkspaceSearch, "preview-blocks",
				errorCodeWorkspaceSearchFailed, "Couldn't read the page.",
			)
		}
		response.Blocks = blocks
	}
	return event.JSON(http.StatusOK, response)
}

// pagePreviewLines takes a body's first lines with text, each cut to a line's worth.
func pagePreviewLines(body string) []string {
	lines := []string{}
	// A block's own lines are lines of the card too.
	for _, line := range strings.FieldsFunc(body, func(r rune) bool { return r == '\n' || r == searchSoftBreak }) {
		if len(lines) == maxPagePreviewLines {
			break
		}
		text := strings.TrimSpace(line)
		if text == "" {
			continue
		}
		if runes := []rune(text); len(runes) > pagePreviewLineRunes {
			text = strings.TrimSpace(string(runes[:pagePreviewLineRunes])) + "…"
		}
		lines = append(lines, text)
	}
	return lines
}

// workspaceSearchOriginalPassage is a found page's passage
// (workspace_search_passage.go) as the page writes it, from the first part
// of its search text holding the query's first word the text has: the index
// keeps the text folded to lower case, and Notion's result shows the line as
// written (seen 2026-11-01, "probe 1031 Band" showed "Image probe below").
// An English word's part is one holding a word of its stem, which it is
// looked for by the letters they share ("run" for "runs", in "running").
func workspaceSearchOriginalPassage(app core.App, pageID, plainQuery string) (workspaceSearchPassage, error) {
	for _, term := range workspaceSearchPlainWords(plainQuery) {
		texts, err := workspaceSearchTermTexts(app, pageID, searchEnglishStemRoot(term))
		if err != nil {
			return nil, err
		}
		for _, text := range texts {
			if passage := workspaceSearchPassageOf(text, plainQuery); len(passage) > 0 {
				return passage, nil
			}
		}
	}
	return nil, nil
}

// workspaceSearchTermTextsLimit is how many chunks holding a word's letters
// a passage looks through for the word.
const workspaceSearchTermTextsLimit = 8

// workspaceSearchTermTexts are the first chunks of a page's search text
// holding some folded letters (workspace_search_chunks.go), or, for a page
// indexed before chunks, the part of its body around them.
func workspaceSearchTermTexts(app core.App, pageID, term string) ([]string, error) {
	texts := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[body]] FROM {{leafen_search_chunks}} WHERE [[page_id]] = {:pageId} AND instr([[body_folded]], {:term}) > 0
		ORDER BY [[chunk_no]] LIMIT {:limit}
	`).Bind(dbx.Params{"term": term, "pageId": pageID, "limit": workspaceSearchTermTextsLimit}).Column(&texts); err != nil || len(texts) > 0 {
		return texts, err
	}
	window := struct {
		Text string `db:"text"`
		At   int    `db:"at"`
	}{}
	err := app.DB().NewQuery(`
		SELECT substr([[body]], max(1, instr([[body_folded]], {:term}) - 400), 800) AS [[text]],
			instr([[body_folded]], {:term}) AS [[at]]
		FROM {{leafen_search_pages}} WHERE [[page_id]] = {:pageId}
	`).Bind(dbx.Params{"term": term, "pageId": pageID}).One(&window)
	if errors.Is(err, sql.ErrNoRows) || err == nil && window.At == 0 {
		return texts, nil
	}
	if err != nil {
		return nil, err
	}
	return append(texts, window.Text), nil
}

// readDatabaseMentionPreview gives a mentioned database's name and the page
// holding it, for a database mention to open that page and show its card,
// as Notion's database mentions do (seen 2026-09-30), and whether it is in
// the trash — its page sent there or under a page there
// (pageOutOfTrashPathSQL), or its block gone from the page holding it — which
// its mentions say after its name, as a page's do, and which opens under the
// trash's banner (Notion's, seen 2026-11-24 in Chrome at 1440 under
// a Notion test page: a full-page database sent to the trash, an inline one
// sent there alone and one under a page sent there) — but one sent there alone
// from a page out of it (trashedAlone), whose page opens without it. A reader
// sees it while the page holding it opens to them, as the mention's words do
// (pageReader).
func readDatabaseMentionPreview(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	containerID := strings.TrimSpace(event.Request.PathValue("containerId"))
	if containerID == "" {
		return apiError(event, http.StatusNotFound, errorCodeDatabaseNotFound, "Database not found.")
	}
	database := struct {
		PageID      string `db:"page_id"`
		PageInTrash bool   `db:"page_in_trash"`
		Gone        bool   `db:"gone"`
	}{}
	// The database's page is found by its id; its workspace and flags are
	// read after (+), or SQLite reads the workspace's every page.
	err := event.App.DB().NewQuery(`
		SELECT [[container]].[[page_id]],
			NOT ` + pageOutOfTrashPathSQL("[[page]].[[id]]") + ` AS [[page_in_trash]],
			(EXISTS (SELECT 1 FROM {{leafen_database_container_trash}} [[trash]]
					WHERE [[trash]].[[container_id]] = [[container]].[[id]])
				OR EXISTS (SELECT 1 FROM {{leafen_deleted_databases}} [[deleted]]
					WHERE [[deleted]].[[block_id]] = [[container]].[[id]])) AS [[gone]]
		FROM {{leafen_database_containers}} [[container]]
		JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[container]].[[page_id]]
		WHERE [[container]].[[id]] = {:containerId}
			AND +[[page]].[[workspace]] = [[container]].[[workspace_id]]
			AND +[[page]].[[notion_import_staging]] = 0
		LIMIT 1
	`).Bind(dbx.Params{"containerId": containerID}).One(&database)
	if errors.Is(err, sql.ErrNoRows) {
		return apiError(event, http.StatusNotFound, errorCodeDatabaseNotFound, "Database not found.")
	}
	if err != nil {
		return workspaceSearchInternalError(
			event, err, persistentReadWorkspaceSearch, "database-mention",
			errorCodeWorkspaceSearchFailed, "Couldn't read the database.",
		)
	}
	open, err := pageReader(user.Id).opens(event.App, []string{database.PageID})
	if err != nil {
		return workspaceSearchInternalError(
			event, err, persistentReadWorkspaceSearch, "database-mention-access",
			errorCodeWorkspaceSearchFailed, "Couldn't read the database.",
		)
	}
	// One deleted for good is gone for every reader (pageRecordExists).
	if !open[database.PageID] || !pageRecordExists(event.App, database.PageID) {
		return apiError(event, http.StatusNotFound, errorCodeDatabaseNotFound, "Database not found.")
	}
	// Its name as its reader reads it (database_container_name.go), one
	// without a name of several sources by their titles.
	titles, sources, err := databaseContainerNamesAsRead(event.App, readerCloser(event.App, user.Id), []string{containerID})
	if err != nil {
		return workspaceSearchInternalError(
			event, err, persistentReadWorkspaceSearch, "database-mention-name",
			errorCodeWorkspaceSearchFailed, "Couldn't read the database.",
		)
	}
	// The icon its mentions draw (database_mention_icon.go).
	icons, err := readDatabaseMentionIcons(event.App, []string{containerID})
	if err != nil {
		return workspaceSearchInternalError(
			event, err, persistentReadWorkspaceSearch, "database-mention-icon",
			errorCodeWorkspaceSearchFailed, "Couldn't read the database.",
		)
	}
	preview := databaseMentionPreviewResponse{
		PageID: database.PageID, Title: titles[containerID], SourceTitles: sources[containerID],
		Icon: icons[containerID].Icon, IconType: icons[containerID].IconType,
		InTrash: database.PageInTrash || database.Gone, TrashedAlone: database.Gone && !database.PageInTrash,
	}
	// Who sent it there and when, which its mention says under the pointer (mention_trash_mark.go).
	if preview.InTrash {
		mark, err := databaseMentionTrashMarkOf(event.App, containerID, database.PageID)
		if err != nil {
			return workspaceSearchInternalError(
				event, err, persistentReadWorkspaceSearch, "database-mention-trash",
				errorCodeWorkspaceSearchFailed, "Couldn't read the database.",
			)
		}
		preview.TrashedAt, preview.TrashedBy = mark.At, mark.By
	}
	return event.JSON(http.StatusOK, preview)
}
