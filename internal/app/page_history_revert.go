package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// Reverting changes from a page's history: from a version, that version's
// changes, or with connection every change the version's connection made to the
// page from it on, up to through when it names a time. What others changed afterwards
// stays, and the revert names what it kept: a change of the person's own, as the screen's.

const pageHistoryRevertPageBlock = "page"

var (
	errPageHistoryRevertInvalid  = errors.New(errorCodePageHistoryInvalid)
	errPageHistoryRevertNoBlocks = errors.New("page history revert changes no blocks")
)

// pageHistoryRevertHashPrefix marks a receipt as a revert's, not a restore's:
// both keep the version they acted on, and the app's "한 일" says a change
// its person took back stays taken back (2026-11-06: reopened, it offered the
// undo again).
const pageHistoryRevertHashPrefix = "revert:"

func pageHistoryRevertRequestHash(pageID, versionID string, baseRevision int, connection bool, through int64) string {
	value := fmt.Sprintf("page-history-revert\x00%s\x00%s\x00%d\x00%t\x00%d", pageID, versionID, baseRevision, connection, through)
	digest := sha256.Sum256([]byte(value))
	return pageHistoryRevertHashPrefix + hex.EncodeToString(digest[:])
}

// pageHistoryVersionRevertedBy says whether the person took back the changes
// from that version on, by a revert's receipt.
func pageHistoryVersionRevertedBy(app core.App, actorID, pageID, versionID string) bool {
	var count int
	err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_page_history_restore_receipts}}
		WHERE [[page_id]] = {:page} AND [[actor_id]] = {:actor} AND [[restored_version_id]] = {:version}
			AND [[request_hash]] LIKE {:revert}`).
		Bind(dbx.Params{"page": pageID, "actor": actorID, "version": versionID, "revert": pageHistoryRevertHashPrefix + "%"}).Row(&count)
	return err == nil && count > 0
}

func revertPageHistoryVersionRoute(event *core.RequestEvent, ygo *pocketBaseYgoPersistence) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	versionID := strings.TrimSpace(event.Request.PathValue("versionId"))
	input := pageHistoryRevertRequest{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodePageHistoryInvalid, "Check the revert request.")
	}
	input.RequestID = strings.TrimSpace(input.RequestID)
	if pageID == "" || versionID == "" || input.RequestID == "" ||
		len([]rune(input.RequestID)) > 255 || input.BaseRevision < 0 {
		return apiError(event, http.StatusBadRequest, errorCodePageHistoryInvalid, "Check the revert request.")
	}
	storeRoomTypingBeforeRevert(event, pageID)
	response, err := revertPageHistoryVersion(event.Request.Context(), event.App, ygo, user.Id, pageID, versionID, input)
	var requestError *graphRequestError
	switch {
	case errors.As(err, &requestError) || isDLPPolicyDecisionError(err) || errors.Is(err, context.Canceled):
		return writeGraphError(event, err, "page-history-revert", errorCodePageHistoryFailed, "Couldn't revert the changes.")
	case errors.Is(err, errPageHistoryRevertInvalid):
		return apiError(event, http.StatusBadRequest, errorCodePageHistoryInvalid, "This version wasn't made through a connection.")
	case errors.Is(err, errPageHistoryRevertTooFar):
		return apiError(event, http.StatusConflict, errorCodePageHistoryRevertTooFar,
			"These changes are too far back to revert one by one. Restore a version instead.")
	}
	// The page's title as its reverter reads it (page_name_mentions.go).
	pageResponseAsRead(event.App, user.Id, &response.Page)
	return writePageHistoryResult(event, response, err, "revert")
}

func revertPageHistoryVersion(
	ctx context.Context, app core.App, ygo *pocketBaseYgoPersistence,
	actorID, pageID, versionID string, input pageHistoryRevertRequest,
) (pageHistoryRevertResponse, error) {
	requestHash := pageHistoryRevertRequestHash(pageID, versionID, input.BaseRevision, input.Connection, input.Through)
	response := pageHistoryRevertResponse{Kept: []pageHistoryRevertKept{}}
	// A repeated request answers as the first did.
	if existing, err := loadPageHistoryRestoreReceipt(app, actorID, input.RequestID); err == nil {
		if existing.PageID != pageID || existing.RequestHash != requestHash || existing.RestoredVersionID != versionID {
			return response, errPageHistoryRequestReused
		}
		authorization, err := authorizePageHistoryRestore(app, actorID, pageID)
		if err != nil {
			return response, err
		}
		if response.Page, err = pageRecordResponseWithWiki(app, authorization.Page); err != nil {
			return response, err
		}
		response.Receipt = pageHistoryReceiptAt(input.RequestID, versionID, existing.Revision, true)
		return response, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return response, err
	}

	// Only the version that made the page, with nothing after it: the page
	// itself stays, and nothing else of it is there to revert.
	if made, err := revertOnlyPageMaking(app, actorID, pageID, versionID, input, requestHash, &response); made || err != nil {
		return response, err
	}

	var plan pageHistoryRevertPlan
	makePlan := func(txApp core.App) error {
		target, err := loadPageHistoryVersion(txApp, pageID, versionID)
		if pageHistoryVersionMissing(err) {
			return errPageHistoryVersionMissing
		}
		if err != nil {
			return err
		}
		if input.Connection && target.ConnectionID == "" {
			return errPageHistoryRevertInvalid
		}
		frontier, err := pageHistorySnapshotFrontier(txApp, pageID)
		if err != nil {
			return err
		}
		if frontier != input.BaseRevision {
			return errPageHistoryRevisionChanged
		}
		// The version that made the page made the page itself, which stays,
		// as a page a connection made stays in its parent's history; what
		// the connection changed after it is reverted (2026-11-06: an
		// agent's new page offered its changes' revert, and it failed).
		from := target.VersionCursor
		start, err := pageHistoryStartCursor(txApp, pageID)
		if err != nil {
			return err
		}
		made := from <= start
		if made {
			from = start + 1
		}
		if from > frontier {
			plan = pageHistoryRevertPlan{
				Blocks: map[string]pageHistoryRevertBlock{}, Moved: map[string]bool{}, Fields: map[string]json.RawMessage{},
			}
		} else if plan, err = planPageHistoryRevert(txApp, pageID, from, frontier, pageHistoryRevertPicks(txApp, pageID, target, input)); err != nil {
			return err
		}
		if made {
			plan.Kept = append(plan.Kept, pageHistoryRevertKept{BlockID: pageID, Reason: pageHistoryRevertPageBlock})
		}
		return planPageHistoryRevertLinkedBlocks(txApp, pageID, &plan)
	}
	finish := func(txApp core.App, authorization pageAuthorization) error {
		if err := revertPageHistoryFields(txApp, authorization, actorID, &plan); err != nil {
			return err
		}
		revision, err := pageHistorySnapshotFrontier(txApp, pageID)
		if err != nil {
			return err
		}
		if err := storePageHistoryRevertReceipt(txApp, actorID, authorization.Workspace.WorkspaceID, pageID, versionID,
			input.RequestID, requestHash, revision); err != nil {
			return err
		}
		page, err := txApp.FindRecordById(pagesCollection, pageID)
		if err != nil {
			return err
		}
		if response.Page, err = pageRecordResponseWithWiki(txApp, page); err != nil {
			return err
		}
		response.Receipt = pageHistoryReceiptAt(input.RequestID, versionID, revision, false)
		return nil
	}

	explicitStarts := map[string]struct{}{}
	_, err := ygo.applyStoredContentGraphMutationContext(ctx, actorID, pageID, graphMutationInput{
		MutationID: "page-history-revert:" + requestHash[:32],
	}, contentGraphMutationHooks{
		PrepareInput: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc, change graphMutationInput) (graphMutationInput, error) {
			if err := makePlan(txApp); err != nil {
				return change, err
			}
			operations, texts, starts := pageHistoryRevertOperations(&plan)
			if len(operations) == 0 {
				return change, errPageHistoryRevertNoBlocks
			}
			for id := range starts {
				explicitStarts[id] = struct{}{}
			}
			change.Operations, change.TextUpdates = operations, texts
			return change, nil
		},
		ExplicitStartBlockIDs: explicitStarts,
		ReplaceExistingText:   true,
		EnforcePageLock:       true,
		AfterApply:            settlingReturnedBlockTrash(actorID, finish),
	})
	if errors.Is(err, errPageHistoryRevertNoBlocks) {
		err = app.RunInTransaction(func(txApp core.App) error {
			authorization, err := authorizePageHistoryRestore(txApp, actorID, pageID)
			if err != nil {
				return err
			}
			if locked, err := pageLockedFor(txApp, authorization.Page, actorID); err != nil {
				return err
			} else if locked {
				return errPageHistoryPageLocked
			}
			if err := makePlan(txApp); err != nil {
				return err
			}
			// Blocks it would change are kept, as the change carrying them found.
			pageHistoryRevertOperations(&plan)
			return finish(txApp, authorization)
		})
	}
	if err != nil {
		return pageHistoryRevertResponse{}, err
	}
	// The content change records its version as it commits, after the rest.
	if revision, err := settlePageHistoryRevertReceipt(app, actorID, pageID, input.RequestID); err == nil {
		response.Receipt = pageHistoryReceiptAt(input.RequestID, versionID, revision, false)
	}
	rows, err := ygo.revertPageHistoryRows(ctx, actorID, pageID, input.RequestID, &plan)
	if err != nil {
		return pageHistoryRevertResponse{}, err
	}
	response.Reverted = len(plan.Blocks) + len(plan.Fields) + rows
	response.Kept = append(response.Kept, plan.Kept...)
	return response, nil
}

func revertOnlyPageMaking(
	app core.App, actorID, pageID, versionID string, input pageHistoryRevertRequest, requestHash string,
	response *pageHistoryRevertResponse,
) (bool, error) {
	target, err := loadPageHistoryVersion(app, pageID, versionID)
	if err != nil || (input.Connection && target.ConnectionID == "") {
		// The revert itself answers a version that isn't there or wasn't a connection's.
		return false, nil
	}
	start, err := pageHistoryStartCursor(app, pageID)
	if err != nil {
		return false, err
	}
	frontier, err := pageHistorySnapshotFrontier(app, pageID)
	if err != nil {
		return false, err
	}
	if target.VersionCursor > start || frontier > start {
		return false, nil
	}
	if frontier != input.BaseRevision {
		return true, errPageHistoryRevisionChanged
	}
	authorization, err := authorizePageHistoryRestore(app, actorID, pageID)
	if err != nil {
		return true, err
	}
	if response.Page, err = pageRecordResponseWithWiki(app, authorization.Page); err != nil {
		return true, err
	}
	if err := storePageHistoryRevertReceipt(app, actorID, authorization.Workspace.WorkspaceID, pageID, versionID,
		input.RequestID, requestHash, frontier); err != nil {
		return true, err
	}
	response.Kept = append(response.Kept, pageHistoryRevertKept{BlockID: pageID, Reason: pageHistoryRevertPageBlock})
	response.Receipt = pageHistoryReceiptAt(input.RequestID, versionID, frontier, false)
	return true, nil
}

// pageHistoryRevertOperations turns a plan into the content change that
// carries it out: the blocks it removes, then the ones it brings back,
// moves or changes, parents before their children, each after the block
// it followed. It leaves out, as kept, a block that would take away a block
// someone else added, and pages and databases, which come and go with their
// own pages.
func pageHistoryRevertOperations(plan *pageHistoryRevertPlan) ([]graphOperation, []graphTextUpdate, map[string]struct{}) {
	current := plan.Current
	keep := func(id, reason string) {
		plan.Kept = append(plan.Kept, pageHistoryRevertKept{BlockID: id, Reason: reason})
		delete(plan.Blocks, id)
	}
	ids := make([]string, 0, len(plan.Blocks))
	for id := range plan.Blocks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	remove := map[string]bool{}
	for _, id := range ids {
		target, now := plan.Blocks[id], current[id]
		if target.Present == now.Present {
			// Changed and changed back by the reverted versions.
			if target.key == now.key && !plan.Moved[id] {
				delete(plan.Blocks, id)
			}
			continue
		}
		switch blockType := target.Type + now.Type; blockType {
		case graphBlockTypeChildPage:
			keep(id, pageHistoryRevertPageBlock)
			continue
		case graphBlockTypeInlineDatabase, graphBlockTypeChildDatabase:
			// A linked database comes and goes as a block, with its views
			// (page_history_revert_linked_views.go).
			if !plan.Linked[id] {
				keep(id, pageHistoryRevertDatabase)
				continue
			}
		}
		if !target.Present {
			remove[id] = true
		}
	}
	children := map[string][]string{}
	for id, block := range current {
		children[block.ParentID] = append(children[block.ParentID], id)
	}
	var holdsStaying func(string) bool
	holdsStaying = func(id string) bool {
		for _, child := range children[id] {
			if !remove[child] || holdsStaying(child) {
				return true
			}
		}
		return false
	}
	for changed := true; changed; {
		changed = false
		for _, id := range ids {
			if remove[id] && holdsStaying(id) {
				delete(remove, id)
				keep(id, pageHistoryRevertHoldsLaterBlock)
				changed = true
			}
		}
	}

	operations := []graphOperation{}
	for _, id := range ids {
		if remove[id] && !remove[current[id].ParentID] {
			operations = append(operations, graphOperation{
				Kind: graphOperationKindDeleteBlock, Deletion: &graphBlockDelete{BlockID: id},
			})
		}
	}
	placed := map[string]bool{}
	for id, block := range current {
		if block.Present && !remove[id] {
			placed[id] = true
		}
	}
	// A block brought back or moved waits for its parent and the block it
	// followed, when those come back too.
	pending := []string{}
	for _, id := range ids {
		target, found := plan.Blocks[id]
		if !found || !target.Present {
			continue
		}
		now := current[id]
		if !now.Present || now.ParentID != target.ParentID || plan.Moved[id] {
			pending = append(pending, id)
			placed[id] = false
		}
	}
	waits := func(id string) bool {
		target := plan.Blocks[id]
		return slices.Contains(pending, target.ParentID) || slices.Contains(pending, target.AfterID)
	}
	// anchor is the block a block goes after: the one it followed, or the
	// nearest one before that still there.
	anchor := func(id string) string {
		target := plan.Blocks[id]
		after := target.AfterID
		for steps := 0; after != "" && steps < len(current)+len(plan.Blocks); steps++ {
			if placed[after] && finalRevertParent(plan, after) == target.ParentID {
				return after
			}
			if earlier, found := plan.Blocks[after]; found {
				after = earlier.AfterID
			} else {
				after = current[after].AfterID
			}
		}
		return ""
	}
	starts := map[string]struct{}{}
	place := func(id string) {
		target := plan.Blocks[id]
		block := pageHistoryRevertGraphBlock(id, target)
		if after := anchor(id); after != "" {
			block.AfterID = &after
		} else {
			starts[id] = struct{}{}
		}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertBlock, Block: block})
		placed[id] = true
	}
	for len(pending) > 0 {
		next := pending[:0:0]
		for _, id := range pending {
			if waits(id) {
				next = append(next, id)
			}
		}
		if len(next) == len(pending) {
			next = next[1:]
		}
		for _, id := range pending {
			if !slices.Contains(next, id) {
				place(id)
			}
		}
		pending = next
	}
	texts := []graphTextUpdate{}
	written := map[string]bool{}
	for _, operation := range operations {
		if operation.Block != nil {
			written[operation.Block.ID] = true
		}
	}
	for _, id := range ids {
		target, found := plan.Blocks[id]
		if !found || !target.Present {
			continue
		}
		if !written[id] {
			operations = append(operations, graphOperation{
				Kind: graphOperationKindUpsertBlock, Block: pageHistoryRevertGraphBlock(id, target),
			})
		}
		if _, text := graphTextBlockTypes[target.Type]; !text {
			continue
		}
		before, _ := json.Marshal(target.Spans)
		now, _ := json.Marshal(current[id].Spans)
		if string(before) != string(now) || !current[id].Present {
			texts = append(texts, graphTextUpdate{BlockID: id, Spans: target.Spans})
		}
	}
	return append(operations, pageHistoryRevertLinkedViews(plan, operations)...), texts, starts
}

func pageHistoryRevertGraphBlock(id string, target pageHistoryRevertBlock) *graphBlock {
	var parent *string
	if target.ParentID != "" {
		parentID := target.ParentID
		parent = &parentID
	}
	return &graphBlock{
		ID: id, ParentID: parent, Position: "a0", Type: target.Type,
		Content: append(json.RawMessage(nil), target.Content...),
	}
}

// revertPageHistoryFields takes a page's fields back as the plan says. A
// database page's title and property values are its database's, and stay.
func revertPageHistoryFields(
	app core.App, authorization pageAuthorization, actorID string, plan *pageHistoryRevertPlan,
) error {
	if len(plan.Fields) == 0 {
		return nil
	}
	page := authorization.Page
	state, err := pageHistoryPageStateFor(app, page)
	if err != nil {
		return err
	}
	fields, err := pageHistoryRevertFields(state)
	if err != nil {
		return err
	}
	row := isDatabaseRowPageRecord(page)
	names := make([]string, 0, len(plan.Fields))
	for name := range plan.Fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if strings.HasPrefix(name, "values.") || (row && name == "title") {
			plan.Kept = append(plan.Kept, pageHistoryRevertKept{Field: name, Reason: pageHistoryRevertDatabase})
			delete(plan.Fields, name)
			continue
		}
		if value := plan.Fields[name]; len(value) == 0 {
			delete(fields, name)
		} else {
			fields[name] = value
		}
	}
	if len(plan.Fields) == 0 {
		return nil
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	reverted := pageHistoryPageState{}
	if err := json.Unmarshal(encoded, &reverted); err != nil {
		return err
	}
	reverted.Values = state.Values
	if row {
		err = restoreDatabaseRowPageHistoryState(app, page.Id, actorID, reverted)
	} else {
		err = restorePageHistoryPageState(app, authorization, actorID, reverted)
	}
	if err != nil {
		return err
	}
	return recordPageHistoryMetadataMutation(app, page.Id, actorID)
}
