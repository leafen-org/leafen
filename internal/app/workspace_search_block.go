package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A page found by its body's words opens at the block whose line its result
// shows, that block chosen, as Notion's search opens a body match ("#<block>"
// in its address, the block selected; seen 2026-11-11), a sub-page's line at
// that sub-page's block. A page found by its title opens at its top, whatever
// its line, as Notion's does. A result's line is one block's passage
// (workspace_search_passage.go): the block is the first, in the page's order,
// whose passage is that line — a text block, or a sub-page or database the
// page holds by its name. It is found when the result is opened, for that
// result alone: finding it reads the page's document, which a search's every
// result reading would make a search cost as much more as it has results.

// maxWorkspaceSearchLineRunes bounds a result's line asked about: a long
// block's passage keeps a highlight's worth around its first keyword.
const maxWorkspaceSearchLineRunes = 4 * workspaceSearchHighlightRunes

// readWorkspaceSearchBlock answers the block of a page whose passage for a
// search's keywords is the line its result showed, or none (the page opens at
// its top). A reader asks it of a page search would show them.
func readWorkspaceSearchBlock(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	query := strings.TrimSpace(event.Request.URL.Query().Get("q"))
	line := event.Request.URL.Query().Get("line")
	if reason := workspaceSearchTextsRefusal(pageID != "", query, false, "line", line, maxWorkspaceSearchLineRunes); reason != "" {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceSearchInvalid, refusalMessage("Check the search query.", reason))
	}
	visible, err := filterWorkspaceSearchCandidates(event.App, user.Id, []workspaceSearchCandidate{{PageID: pageID}})
	if err != nil {
		return workspaceSearchInternalError(
			event, err, persistentReadWorkspaceSearch, "block-access",
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
			event, err, persistentReadWorkspaceSearch, "block-page",
			errorCodeWorkspaceSearchFailed, "Couldn't read the page.",
		)
	}
	plainQuery := workspaceSearchPlainText(foldSearchText(query))
	blockID, err := workspaceSearchLineBlock(event.App, page.GetString("workspace"), pageID, plainQuery, line)
	if err != nil {
		return workspaceSearchInternalError(
			event, err, persistentReadWorkspaceSearch, "block",
			errorCodeWorkspaceSearchFailed, "Couldn't read the page.",
		)
	}
	return event.JSON(http.StatusOK, workspaceSearchBlockResponse{BlockID: blockID})
}

// workspaceSearchLineBlock names the block of a page whose passage is a
// result's line, or nothing when none is (a line of the page's own words
// before blocks, or of its files').
func workspaceSearchLineBlock(app core.App, workspaceID, pageID, plainQuery, line string) (string, error) {
	terms := workspaceSearchPlainWords(plainQuery)
	if line == "" || len(terms) == 0 {
		return "", nil
	}
	doc, found, done, err := borrowYgoStreamDocument(app, ygoCollaborationStream, pageID)
	if err != nil {
		return "", err
	}
	defer done()
	structural, structuralFound, structuralDone, err := borrowYgoStreamDocument(app, ygoStructuralStream, pageID)
	if err != nil {
		return "", err
	}
	defer structuralDone()
	if !found && !structuralFound {
		return "", nil
	}
	indexed, lines, err := searchPageIndexed(app, pageID)
	if err != nil {
		return "", err
	}
	orderDoc := doc
	if structuralFound {
		orderDoc = structural
	}
	read := searchIndexTextReader(app, workspaceID)
	for _, id := range searchBlocksInLineOrder(orderDoc, indexed) {
		text := ""
		if named, isNamed := lines[id]; isNamed {
			text = named.Title
		} else if found {
			text = searchBlockText(read(doc.GetText(ygoBlockTextRootPrefix + id)))
		}
		if block, held := workspaceSearchPassageBlockOf(text, terms); held && block.text == line {
			return id, nil
		}
	}
	return "", nil
}
