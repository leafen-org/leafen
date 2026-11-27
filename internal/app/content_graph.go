package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedGraphMutation struct {
	MutationID  string `db:"mutation_id"`
	RequestHash string `db:"request_hash"`
	Cursor      int    `db:"cursor"`
	CountsJSON  string `db:"counts_json"`
	StateVector string `db:"state_vector"`
}

type storedGraphMutationReceipt struct {
	graphMutationCounts
	TextUpdate         string                    `json:"textUpdate,omitempty"`
	Page               *graphPageMutationReceipt `json:"page,omitempty"`
	MergedBlockContent bool                      `json:"mergedBlockContent,omitzero"`
	DateUndo           *databaseDateCommand      `json:"dateUndo,omitempty"`
	DependencyShifts   int                       `json:"dependencyShifts,omitzero"`
	// DependencyShiftTasks tells the moved rows are tasks.
	DependencyShiftTasks bool `json:"dependencyShiftTasks,omitzero"`
	// DependencyShiftsDenied counts the rows left where they were because
	// the writer may not change them.
	DependencyShiftsDenied int `json:"dependencyShiftsDenied,omitzero"`
	// What the change alone changed in its page's graph, when that is its
	// rows and its databases' sources, views and properties
	// (content_graph_changed_rows.go).
	graphChangedParts
}

// storedGraphMutationReplay finds the change a page already took under a
// mutation ID and answers with its receipt as a duplicate. The same ID sent
// with another request is a conflict.
func storedGraphMutationReplay(
	app core.App,
	ownerID string,
	pageID string,
	mutationID string,
	requestHash string,
) (graphMutationReceipt, bool, error) {
	var stored storedGraphMutation
	err := app.DB().NewQuery(`
		SELECT [[mutation_id]], [[request_hash]], [[cursor]], [[counts_json]], [[state_vector]]
		FROM {{leafen_graph_mutations}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[mutation_id]] = {:mutationId}
	`).Bind(dbx.Params{"owner": ownerID, "pageId": pageID, "mutationId": mutationID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return graphMutationReceipt{}, false, nil
	}
	if err != nil {
		return graphMutationReceipt{}, false, err
	}
	if stored.RequestHash != requestHash {
		return graphMutationReceipt{}, false, graphConflictError(
			errorCodeGraphMutationIDReused, "This ID was used for a different content change.",
		)
	}
	storedReceipt := storedGraphMutationReceipt{}
	if err := json.Unmarshal([]byte(stored.CountsJSON), &storedReceipt); err != nil {
		return graphMutationReceipt{}, false, err
	}
	dateCommand := ""
	if storedReceipt.DateUndo != nil {
		dateCommand = stored.MutationID
	}
	return graphMutationReceipt{
		MutationID: stored.MutationID, Cursor: stored.Cursor,
		Counts: storedReceipt.graphMutationCounts, Duplicate: true,
		StateVector: stored.StateVector, TextUpdate: storedReceipt.TextUpdate,
		Page: storedReceipt.Page, MergedBlockContent: storedReceipt.MergedBlockContent,
		DateCommand: dateCommand, DependencyShifts: storedReceipt.DependencyShifts,
		DependencyShiftTasks: storedReceipt.DependencyShiftTasks, DependencyShiftsDenied: storedReceipt.DependencyShiftsDenied,
	}, true, nil
}

type storedGraphReadItem struct {
	Kind             string `db:"kind"`
	SortKey          string `db:"sort_key"`
	Payload          string `db:"payload"`
	SnapshotRevision int    `db:"snapshot_revision"`
}

type storedGraphRowValue struct {
	RowID      string `db:"row_id"`
	PropertyID string `db:"property_id"`
	Type       string `db:"type"`
	Value      string `db:"value"`
	Spans      string `db:"spans"`
}

type graphEntityReference struct {
	ID           string `db:"id"`
	Owner        string `db:"owner"`
	PageID       string `db:"page_id"`
	ParentID     string `db:"parent_id"`
	DataSourceID string `db:"data_source_id"`
	ContainerID  string `db:"container_id"`
	Type         string `db:"type"`
	Config       string `db:"config"`
}

type graphProjection struct {
	blocks     map[string]graphEntityReference
	sources    map[string]graphEntityReference
	views      map[string]graphEntityReference
	properties map[string]graphEntityReference
	rows       map[string]graphEntityReference
	// containers holds the content of database blocks named by view operations.
	containers map[string]databaseContainerBlockContent
	// returningDatabases names the database blocks a change writes back as
	// their page took them away, unable to show them (database_unavailable_blocks.go).
	returningDatabases map[string]struct{}
}

func (p *pocketBaseYgoPersistence) createContentGraphMutation(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}

	pageID := strings.TrimSpace(event.Request.PathValue("id"))
	var input graphMutationInput
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeGraphInvalid,
			refusalMessage("Check the content change details.", bodyRefusalReason("content change", err)))
	}
	input.MutationID = strings.TrimSpace(input.MutationID)
	if pageID == "" || input.MutationID == "" || len(input.MutationID) > maxGraphIdentifierLength {
		return apiError(event, http.StatusBadRequest, errorCodeGraphInvalid, refusalMessage("Check the content change details.",
			fmt.Sprintf("mutationId is a new id of yours, 1–%d bytes, so the same change sent again isn't made twice", maxGraphIdentifierLength)))
	}
	// The templates the rows it writes start from or were chosen for, filled once it is taken (content_graph_row_templates.go).
	templateFills, templateRows, err := takeRowTemplateFills(&input)
	if err != nil {
		return writeGraphChangeError(event, err, "mutation", errorCodeGraphInvalid, "Check the content change details.")
	}
	// The text the editor wrote with the change, and before it, goes with it (content_graph_mutation_words.go).
	words, typing, err := takeGraphMutationText(&input)
	if err != nil {
		return writeGraphChangeError(event, err, "mutation", errorCodeGraphInvalid, "Check the content change details.")
	}
	lifted, err := liftGraphBlockStoredText(&input)
	if err != nil {
		return writeGraphChangeError(event, err, "mutation", errorCodeGraphInvalid, "Check the content change details.")
	}
	// A row's icon, cover and style change on its page, not with its row
	// (content_graph_row_presentation.go).
	if err := refuseRowPresentationChanges(p.app, pageID, input); err != nil {
		return writeGraphChangeError(event, err, "mutation", errorCodeGraphInvalid, "Check the content change details.")
	}
	if err := validateContentGraphCompoundInput(&input); err != nil {
		return writeGraphChangeError(event, err, "mutation", errorCodeGraphInvalid, "Check the content change details.")
	}
	// What the writer's reading left out is put back (content_graph_writer_mentions.go), an app's names as made.
	restoreGraphWriteAsStored(p.app, &input)
	appGraphWriteInMadeNames(p.app, event.Get(userKeyAuthorityKey), &input)
	keptCopies, err := p.letDeletedSyncedOriginalsGo(event.Request.Context(), user.Id, pageID, input.Operations)
	if err != nil {
		return writeGraphChangeError(event, err, "mutation", errorCodeGraphMutationFailed, "Couldn't save the content.")
	}
	// The rows this change makes are followed by their maker, as Notion's (an
	// integration's or an import's aren't: they don't come this way).
	fresh, err := newRowIDs(p.app, input.Operations)
	if err != nil {
		return writeGraphChangeError(event, err, "mutation", errorCodeGraphMutationFailed, "Couldn't save the content.")
	}
	// The typing the page's room took before the change is stored first.
	storeRoomTypingAhead(event.Request.Context(), p.app, pageID)
	receipt, err := p.applyContentGraphMutationContext(
		event.Request.Context(), user.Id, pageID, input,
		// A block added after the page's last goes in without its siblings'
		// places read and written again, as an import's does
		// (content_graph_append_projection.go; 2026-10-21: a long page's
		// every block read all its siblings while it held the one write
		// connection).
		// A column's options are checked against the ones it holds; an agent's key leaves none out that pages hold (database_option_removal.go).
		contentGraphMutationHooks{EnforcePageLock: true, PersonChange: true, LiftedTextBlockIDs: lifted, SearchFromNamedBlocks: true,
			StructuralAppend: true, Words: words, Typing: typing, TellRoom: true, CheckOperations: databaseOptionChangesChecked(event.Get(userKeyAuthorityKey) != nil)},
	)
	if err != nil {
		return writeGraphChangeError(event, err, "mutation", errorCodeGraphMutationFailed, "Couldn't save the content.")
	}
	if !receipt.Duplicate {
		if err := followNewRowsComments(p.app, user.Id, fresh); err != nil {
			logAPIDiagnostic(event, http.StatusOK, errorCodeGraphMutationFailed, newDiagnosticReference(), "content_graph.mutation",
				"follow-new-rows", errorCodeGraphMutationFailed, err, diagnosticSourcePosition(0))
		}
		p.fillRowsFromTemplates(event, user.Id, input.MutationID, templateFills, templateRows)
	}
	// The copies of a deleted original left on pages the person can't
	// change, which the editor tells them of.
	receipt.KeptSyncedCopies = keptCopies
	// The page's title as its writer reads it (page_name_mentions.go).
	if receipt.Page != nil {
		receipt.Page.Title = rowTitleAsRead(p.app, user.Id, pageID, receipt.Page.Title)
	}
	status := http.StatusCreated
	if receipt.Duplicate {
		status = http.StatusOK
	}
	return event.JSON(status, receipt)
}

func readContentGraph(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("id"))
	limit := maxGraphReadItems
	if raw := strings.TrimSpace(event.Request.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > maxGraphReadItems {
			return apiError(event, http.StatusBadRequest, errorCodeGraphCursorInvalid, "Check the content query range.")
		}
		limit = parsed
	}
	cursor, err := decodeGraphCursor(event.Request.URL.Query().Get("cursor"))
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeGraphCursorInvalid, "Check the content query position.")
	}
	// rows=none reads the page without its databases' rows.
	withRows := true
	switch event.Request.URL.Query().Get("rows") {
	case "":
	case "none":
		withRows = false
	default:
		return apiError(event, http.StatusBadRequest, errorCodeGraphCursorInvalid, "Check the content query range.")
	}
	// text=blocks gives the page's text blocks their words from its
	// collaborative document, as a public page's read does
	// (public_content_ygo_projection.go): a preview that draws the page
	// without opening it, the layout builder's "콘텐츠", shows its words.
	withText := false
	switch event.Request.URL.Query().Get("text") {
	case "":
	case "blocks":
		withText = true
	default:
		return apiError(event, http.StatusBadRequest, errorCodeGraphCursorInvalid, "Check the content query range.")
	}
	// since=N reads only the rows the page's changes after position N
	// changed, when they say which, or else its first window, as without it.
	since := -1
	if raw := strings.TrimSpace(event.Request.URL.Query().Get("since")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 || cursor != "" || !withRows {
			return apiError(event, http.StatusBadRequest, errorCodeGraphCursorInvalid, "Check the content query position.")
		}
		since = parsed
	}
	// blocks=page with since says the reader takes the page's blocks among
	// what its changes changed (changedBlocks); a reader that doesn't say so
	// reads the page whole after a change of its blocks.
	withBlocks := false
	switch event.Request.URL.Query().Get("blocks") {
	case "":
	case "page":
		if since < 0 {
			return apiError(event, http.StatusBadRequest, errorCodeGraphCursorInvalid, "Check the content query range.")
		}
		withBlocks = true
	default:
		return apiError(event, http.StatusBadRequest, errorCodeGraphCursorInvalid, "Check the content query range.")
	}
	// external=<page>.<position>,… reads only the rows the changes of other
	// pages the page shows rows of changed (content_graph_external_changed_rows.go).
	external, ok := parseGraphExternalSince(event.Request.URL.Query().Get("external"))
	if !ok || external != nil && (cursor != "" || !withRows || since >= 0) {
		return apiError(event, http.StatusBadRequest, errorCodeGraphCursorInvalid, "Check the content query position.")
	}
	// words=page gives a page's first window its words at the same moment
	// (content_graph_words.go).
	withWords := false
	switch event.Request.URL.Query().Get("words") {
	case "":
	case "page":
		if cursor != "" || since >= 0 || external != nil {
			return apiError(event, http.StatusBadRequest, errorCodeGraphCursorInvalid, "Check the content query range.")
		}
		withWords = true
	default:
		return apiError(event, http.StatusBadRequest, errorCodeGraphCursorInvalid, "Check the content query range.")
	}

	var response graphReadResponse
	words := ""
	workspaceID := ""
	var wordsMoment pageWordsMoment
	// One moment of the page, read beside the writes (read_snapshot.go).
	read := func(txApp core.App) error {
		authorization, err := authorizePage(txApp, user.Id, pageID, permissionWorkspaceRead)
		if isGuestMFARequired(err) {
			return &graphRequestError{
				status: http.StatusForbidden, code: errorCodeGuestMFARequired,
				message: "This organization asks its guests to turn on 2-step verification.",
			}
		}
		if isPermissionDenied(err) {
			if busy := pageBusyForMember(txApp, user.Id, pageID); busy != "" {
				return pageBusyError(busy)
			}
			return graphNotFoundError()
		}
		if err != nil {
			return err
		}
		// Taken as the snapshot has begun: its first reads were the page's access.
		if withWords {
			if words, wordsMoment, err = pageWordsAsRead(txApp, user.Id, pageID); err != nil {
				return err
			}
		}
		workspaceID = authorization.Workspace.WorkspaceID
		linkedScope, err := authorizeLinkedDatabaseReadScope(
			txApp, user.Id, authorization, pageID,
		)
		if err != nil {
			return err
		}
		// An inline database alone in the trash, opened alone, read beside the page (content_graph_trashed_database.go).
		if trashedID := strings.TrimSpace(event.Request.URL.Query().Get("trashedDatabase")); trashedID != "" && since < 0 {
			if linkedScope.trashed, err = loadGraphTrashedDatabase(txApp, authorization.Workspace.OwnerID, pageID, trashedID); err != nil {
				return err
			}
		}
		changed := false
		if since >= 0 {
			response, changed, err = loadContentGraphChangedParts(
				txApp, authorization.Workspace.OwnerID, pageID, linkedScope, since, withBlocks,
			)
			if err != nil {
				return err
			}
		} else if external != nil {
			if response, changed, err = loadContentGraphExternalChangedRows(
				txApp, authorization.Workspace.OwnerID, pageID, linkedScope, external,
			); err != nil {
				return err
			}
		}
		if !changed {
			response, err = loadContentGraphWindowParts(
				txApp, authorization.Workspace.OwnerID, pageID, linkedScope, cursor, limit, withRows,
			)
		}
		if err != nil {
			return err
		}
		if withText {
			if response.Items, err = projectPublicBlockText(txApp, pageID, response.Items); err != nil {
				return err
			}
		}
		if err := markRowLocksUnlockedFor(txApp, user.Id, &response); err != nil {
			return err
		}
		// What the databases' property access rules keep from this reader,
		// and the changed rows gone for them (content_graph_changed_rows.go).
		if err := redactGraphReadForReader(txApp, user.Id, &response); err != nil {
			return err
		}
		// What names a page closed to this reader (content_graph_reader_mentions.go).
		response.Items = graphItemsAsRead(txApp, user.Id, response.Items)
		if err := markPropertyAccessManagers(txApp, user.Id, response.Items); err != nil {
			return err
		}
		if err := markSprintDatabases(txApp, response.Items); err != nil {
			return err
		}
		if err := markNotionPropertyOrder(txApp, response.Items); err != nil {
			return err
		}
		if err := markProjectDatabases(txApp, response.Items); err != nil {
			return err
		}
		if err := markPeopleDatabases(txApp, authorization.Workspace, response.Items); err != nil {
			return err
		}
		if err := markUntitledRowNames(txApp, response.Items); err != nil {
			return err
		}
		if err := markAutomatedDatabases(txApp, authorization.Workspace.OwnerID, response.Items); err != nil {
			return err
		}
		response.CanEdit, err = pageAcceptsContentEdits(txApp, user.Id, pageID)
		if err != nil {
			return err
		}
		response.CanSuggest, err = pageAcceptsSuggestions(txApp, user.Id, pageID)
		if err == nil {
			// Held by an operation under way: it takes edits again after.
			response.Busy = pageHoldForMember(txApp, user.Id, pageID, response.CanEdit)
		}
		return err
	}
	// Words of another moment than the blocks read are read again with them.
	for attempt := 1; ; attempt++ {
		err = runReadSnapshot(event.Request.Context(), event.App, read)
		if err != nil || !withWords || wordsMoment.agrees(response.SnapshotRevision) || attempt == pageWordsReads {
			break
		}
	}
	if err != nil {
		return writeGraphError(event, err, "read", errorCodeGraphReadFailed, "Couldn't load the content.")
	}
	// The items the page's databases list are read for the read log (read_log.go).
	listed := []contentRead{}
	for _, item := range response.Items {
		if id, _ := item.Value["id"].(string); item.Kind == "row" && id != "" {
			listed = append(listed, contentRead{Kind: "row", ID: id, How: contentReadList})
		}
	}
	recordContentReads(event.App, workspaceID, contentReader{UserID: user.Id, Via: contentReadViaApp}, listed)
	response.Words = words
	return event.JSON(http.StatusOK, response)
}

// pageAcceptsContentEdits tells a reader whether the page's content would
// take their edits now: write access to a page outside the trash. Its lock,
// which they may lift, shows with the page: the app's writes and its room
// then refuse their changes (web lib/refusedTextLetGo.ts). A reader showing
// this page inside another one, such as a synced block copy, edits it only then.
func pageAcceptsContentEdits(app core.App, principalID string, pageID string) (bool, error) {
	if _, err := authorizePageBody(app, principalID, pageID, permissionWorkspaceWrite); isPermissionDenied(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	trashed, err := pageInTrashPath(app, pageID)
	if err != nil {
		return false, err
	}
	return !trashed, nil
}

// pageAcceptsSuggestions tells a reader whether they may suggest edits to
// the page's text, by the rule the suggestion routes apply: comment access
// to a page, or a database page, outside the trash. The page's lock is shown
// with the page.
func pageAcceptsSuggestions(app core.App, principalID string, pageID string) (bool, error) {
	rowPage, err := isDatabaseRowPageRoom(app, pageID)
	if err != nil {
		return false, err
	}
	authorization, err := authorizeSuggestionBody(app, principalID, suggestionSurface{Room: pageID, Row: rowPage})
	if isPermissionDenied(err) || errors.Is(err, errDatabaseRowPageNotFound) || errors.Is(err, errCommentDeleteBusy) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	trashed, err := suggestionBodyInTrash(app, authorization)
	return err == nil && !trashed, err
}
