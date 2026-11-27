package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"unicode/utf16"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

type importedPeopleRowValue struct {
	RowID           string `db:"row_id"`
	PropertyID      string `db:"property_id"`
	DataSourceID    string `db:"data_source_id"`
	Position        string `db:"position"`
	SubitemParentID string `db:"subitem_parent_id"`
	Type            string `db:"type"`
	Value           string `db:"value"`
	Spans           string `db:"spans"`
}

type importedPeopleMentionFormat struct {
	BlockID string
	Index   int
	Length  int
	UserID  string
}

func applyImportedPeopleMappingTurn(
	app core.App,
	ygo *pocketBaseYgoPersistence,
	principalID, workspaceID string,
	input notionImportedPeopleApplyInput,
) (notionImportedPeopleApplyResult, error) {
	release := acquireProductOperationLock("imported-people-mapping", workspaceID)
	defer release()
	job, mappings, err := admitImportedPeopleMappingJob(app, principalID, workspaceID, input)
	if err != nil {
		return notionImportedPeopleApplyResult{}, err
	}
	if job.Status == "completed" {
		return importedPeopleMappingResult(app, job)
	}
	if job.Phase == "discovering" {
		complete, err := discoverImportedPeopleMappingPages(app, principalID, &job, mappings)
		if err != nil {
			return notionImportedPeopleApplyResult{}, err
		}
		if !complete {
			return importedPeopleMappingResult(app, job)
		}
	}
	items, err := nextImportedPeopleMappingItems(app, job.ID)
	if err != nil {
		return notionImportedPeopleApplyResult{}, err
	}
	for _, item := range items {
		err := applyImportedPeopleMappingPage(ygo, principalID, job, item, mappings)
		if errors.Is(err, errImportedPeopleItemAlreadyApplied) {
			continue
		}
		if isPermissionDenied(err) {
			skipped, skipErr := skipUnreachableImportedPeopleMappingPage(app, principalID, job, item, mappings)
			if skipErr != nil {
				return notionImportedPeopleApplyResult{}, skipErr
			}
			if skipped {
				continue
			}
		}
		if err != nil {
			return notionImportedPeopleApplyResult{}, err
		}
	}
	if err := finishImportedPeopleMappingJob(app, principalID, &job, mappings); err != nil {
		return notionImportedPeopleApplyResult{}, err
	}
	if job.Status != "completed" {
		job, err = loadImportedPeopleMappingJobByID(app, job.ID)
		if err != nil {
			return notionImportedPeopleApplyResult{}, err
		}
	}
	return importedPeopleMappingResult(app, job)
}

func importedPeopleMappingResult(app core.App, job storedImportedPeopleMappingJob) (notionImportedPeopleApplyResult, error) {
	skipped, err := countSkippedImportedPeopleMappingPages(app, job.ID)
	if err != nil {
		return notionImportedPeopleApplyResult{}, err
	}
	result := notionImportedPeopleApplyResult{
		Status: job.Status, Generation: job.Generation, MappedPeople: job.MappedPeople,
		ChangedPages: job.ChangedPages, ChangedBlocks: job.ChangedBlocks, SkippedPages: skipped,
	}
	if job.Status != "completed" {
		result.Status = "pending"
		result.Continuation = job.ID
	}
	return result, nil
}

func applyImportedPeopleMappingPage(
	ygo *pocketBaseYgoPersistence,
	principalID string,
	job storedImportedPeopleMappingJob,
	item storedImportedPeopleMappingItem,
	mappings map[string]string,
) error {
	operations := []graphOperation{}
	formats := []importedPeopleMentionFormat{}
	changedBlocks := 0
	documents := map[string]*notionYgoDocument{}
	var collaborationDocument *notionYgoDocument
	requestDigest := sha256.Sum256([]byte(job.RequestHash + "\x00" + item.PageID))
	requestHash := hex.EncodeToString(requestDigest[:])
	_, err := ygo.applyContentGraphMutation(principalID, item.PageID, graphMutationInput{
		MutationID: "imported-people:" + job.ID + ":" + item.PageID,
	}, contentGraphMutationHooks{
		AllowTrashedPage: true,
		Authorize: func(txApp core.App) (pageAuthorization, error) {
			return authorizePageBody(txApp, principalID, item.PageID, importedPeopleMappingPermission)
		},
		BeforeApply: func(txApp core.App, authorization pageAuthorization, _ *crdt.Doc) error {
			if authorization.Workspace.WorkspaceID != job.WorkspaceID || authorization.Workspace.OwnerID != job.OwnerID {
				return permissionDenied()
			}
			var applied int
			if err := txApp.DB().NewQuery(`SELECT [[applied]] FROM {{leafen_imported_people_mapping_items}}
				WHERE [[job_id]]={:jobId} AND [[page_id]]={:pageId}`).Bind(dbx.Params{"jobId": job.ID, "pageId": item.PageID}).Row(&applied); err != nil {
				return err
			}
			if applied != 0 {
				return errImportedPeopleItemAlreadyApplied
			}
			return validateImportedPeopleTargets(txApp, authorization.Workspace, mappings)
		},
		ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
			return requestHash, nil
		},
		Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
			var err error
			operations, err = importedPeopleRowOperations(txApp, item.PageID, mappings)
			if err != nil {
				return contentGraphMutationPreparation{}, err
			}
			collaborationDocument, err = loadNotionYgoDocument(txApp, item.PageID, documents)
			if err != nil {
				return contentGraphMutationPreparation{}, err
			}
			formats, _, err = importedPeopleMentionFormats(txApp, collaborationDocument.doc, item.PageID, mappings)
			if err != nil {
				return contentGraphMutationPreparation{}, err
			}
			blockOperations, contentBlockIDs, err := importedPeopleBlockOperations(txApp, item.PageID, mappings)
			if err != nil {
				return contentGraphMutationPreparation{}, err
			}
			operations = append(operations, blockOperations...)
			viewOperations, err := importedPeopleViewOperations(txApp, item.PageID, mappings)
			if err != nil {
				return contentGraphMutationPreparation{}, err
			}
			operations = append(operations, viewOperations...)
			changed := map[string]struct{}{}
			for _, format := range formats {
				changed[format.BlockID] = struct{}{}
			}
			for _, blockID := range contentBlockIDs {
				changed[blockID] = struct{}{}
			}
			changedBlocks = len(changed)
			return contentGraphMutationPreparation{Operations: operations, Deferred: len(operations) == 0 && len(formats) == 0}, nil
		},
		AfterApply: func(txApp core.App, authorization pageAuthorization, _ *crdt.Doc) error {
			collaborationDocument.doc.Transact(func(transaction *crdt.Transaction) {
				for _, format := range formats {
					transaction.GetText(ygoBlockTextRootPrefix+format.BlockID).Format(transaction, format.Index, format.Length, crdt.Attributes{"userId": format.UserID})
				}
			})
			if err := saveNotionYgoDocuments(txApp, authorization.Workspace.PrincipalID, documents); err != nil {
				return err
			}
			return markImportedPeopleMappingItem(txApp, job.ID, item.PageID, 1, changedBlocks)
		},
	})
	if err != nil {
		return err
	}
	if len(operations) == 0 && len(formats) == 0 {
		// A page with nothing to change is marked done as a change's
		// follow-up is written (ygo_persistence_follow_ups.go).
		return ygo.runFollowUp(func(txApp core.App) error {
			authorization, err := authorizePageBody(txApp, principalID, item.PageID, importedPeopleMappingPermission)
			if err != nil {
				return err
			}
			if err := validateImportedPeopleTargets(txApp, authorization.Workspace, mappings); err != nil {
				return err
			}
			return markImportedPeopleMappingItem(txApp, job.ID, item.PageID, 0, 0)
		})
	}
	return nil
}

func importedPeopleRowOperations(app core.App, pageID string, mappings map[string]string) ([]graphOperation, error) {
	rows := []importedPeopleRowValue{}
	err := app.DB().NewQuery(`
		SELECT [[value]].[[row_id]],[[value]].[[property_id]],[[row]].[[data_source_id]],
			[[row]].[[position]],[[row]].[[subitem_parent_id]],[[value]].[[type]],[[value]].[[value]],[[value]].[[spans]]
		FROM {{leafen_database_row_values}} [[value]] JOIN {{leafen_database_rows}} [[row]] ON [[row]].[[id]]=[[value]].[[row_id]]
		WHERE [[value]].[[page_id]]={:pageId} AND ([[value]].[[type]]='person' AND ` + importedPeopleMarkedSQL("[[value]].[[value]]") + `
			OR ` + importedPeopleMarkedSQL("[[value]].[[spans]]") + `)
		ORDER BY [[value]].[[row_id]],[[value]].[[property_id]]
	`).Bind(dbx.Params{"pageId": pageID}).All(&rows)
	if err != nil {
		return nil, err
	}
	byRow := map[string]*graphDatabaseRow{}
	order := []string{}
	for _, row := range rows {
		cell := graphRowValue{Type: row.Type, Value: json.RawMessage(row.Value)}
		changed := false
		if row.Type == graphValueTypePerson {
			var value any
			if json.Unmarshal([]byte(row.Value), &value) != nil {
				return nil, errImportedPeopleMappingFailed
			}
			replaced, replacedValue := replaceImportedPeopleSources(value, mappings)
			encoded, err := json.Marshal(replaced)
			if err != nil {
				return nil, err
			}
			cell.Value, changed = encoded, replacedValue
		} else {
			// A formatted text cell's person mentions point at the members.
			if json.Unmarshal([]byte(row.Spans), &cell.Spans) != nil {
				return nil, errImportedPeopleMappingFailed
			}
			for index, span := range cell.Spans {
				if targetID, mapped := mappings[span.UserID]; mapped && span.Mention == "person" && targetID != importedPeopleCleared {
					cell.Spans[index].UserID = targetID
					changed = true
				}
			}
		}
		if !changed {
			continue
		}
		graphRow := byRow[row.RowID]
		if graphRow == nil {
			var parent *string
			if row.SubitemParentID != "" {
				value := row.SubitemParentID
				parent = &value
			}
			graphRow = &graphDatabaseRow{ID: row.RowID, DataSourceID: row.DataSourceID, Position: row.Position, SubitemParentID: parent, Values: map[string]graphRowValue{}}
			byRow[row.RowID] = graphRow
			order = append(order, row.RowID)
		}
		graphRow.Values[row.PropertyID] = cell
	}
	operations := make([]graphOperation, 0, len(order))
	for _, rowID := range order {
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertRow, Row: byRow[rowID]})
	}
	return operations, nil
}

// importedPeopleViewOperations points the imported people the database views
// on a page filter on at the mapped members, so a view keeps finding the
// rows whose values were mapped.
func importedPeopleViewOperations(app core.App, pageID string, mappings map[string]string) ([]graphOperation, error) {
	views := []struct {
		ID           string `db:"id"`
		DataSourceID string `db:"data_source_id"`
		ContainerID  string `db:"container_id"`
		Type         string `db:"type"`
		Name         string `db:"name"`
		Position     string `db:"position"`
		Config       string `db:"config"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[data_source_id]],[[container_id]],[[type]],[[name]],[[position]],[[config]]
		FROM {{leafen_database_views}}
		WHERE [[page_id]]={:pageId} AND ` + importedPeopleMarkedSQL("[[config]]") + ` ORDER BY [[id]]
	`).Bind(dbx.Params{"pageId": pageID}).All(&views); err != nil {
		return nil, err
	}
	operations := []graphOperation{}
	for _, view := range views {
		var config any
		if json.Unmarshal([]byte(view.Config), &config) != nil {
			return nil, errImportedPeopleMappingFailed
		}
		replaced, changed := replaceImportedPeopleSources(config, mappings)
		if !changed {
			continue
		}
		encoded, err := json.Marshal(replaced)
		if err != nil {
			return nil, err
		}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
			ID: view.ID, DataSourceID: view.DataSourceID, ContainerID: view.ContainerID, Type: view.Type,
			Name: view.Name, Position: view.Position, Config: encoded,
		}})
	}
	return operations, nil
}

// importedPeopleBlockOperations points the imported person mentions in each
// block's caption or table cells on a page at the mapped members.
func importedPeopleBlockOperations(app core.App, pageID string, mappings map[string]string) ([]graphOperation, []string, error) {
	blocks := []storedNotionBlock{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[parent_id]],[[position]],[[type]],[[content]] FROM {{leafen_blocks}}
		WHERE [[page_id]]={:pageId} AND ` + importedPeopleMarkedSQL("[[content]]") + ` ORDER BY [[id]]
	`).Bind(dbx.Params{"pageId": pageID}).All(&blocks); err != nil {
		return nil, nil, err
	}
	operations := []graphOperation{}
	blockIDs := []string{}
	for _, block := range blocks {
		var content any
		if json.Unmarshal([]byte(block.Content), &content) != nil {
			return nil, nil, errImportedPeopleMappingFailed
		}
		changed := false
		visitImportedPeopleContentMentions(content, false, func(span map[string]any, sourceID string) {
			if targetID, mapped := mappings[sourceID]; mapped && targetID != importedPeopleCleared {
				span["userId"] = targetID
				changed = true
			}
		})
		if !changed {
			continue
		}
		encoded, err := json.Marshal(content)
		if err != nil {
			return nil, nil, err
		}
		var parentID *string
		if block.ParentID != "" {
			parent := block.ParentID
			parentID = &parent
		}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertBlock, Block: &graphBlock{
			ID: block.ID, ParentID: parentID, Position: block.Position, Type: block.Type, Content: encoded,
		}})
		blockIDs = append(blockIDs, block.ID)
	}
	return operations, blockIDs, nil
}

func importedPeopleMentionFormats(app core.App, doc *crdt.Doc, pageID string, mappings map[string]string) ([]importedPeopleMentionFormat, int, error) {
	blocks := []importedPeopleBlockRow{}
	if err := app.DB().NewQuery(`SELECT [[id]],[[page_id]] FROM {{leafen_blocks}} WHERE [[page_id]]={:pageId} ORDER BY [[id]]`).Bind(dbx.Params{"pageId": pageID}).All(&blocks); err != nil {
		return nil, 0, err
	}
	formats := []importedPeopleMentionFormat{}
	changedBlocks := 0
	for _, block := range blocks {
		index := 0
		blockChanged := false
		for _, delta := range doc.GetText(ygoBlockTextRootPrefix + block.ID).ToDelta() {
			text, isText := delta.Insert.(string)
			length := utf16TextLength(text)
			if delta.Op == crdt.DeltaOpInsert && isText && delta.Attributes != nil {
				sourceID, _ := delta.Attributes["userId"].(string)
				if targetID, ok := mappings[sourceID]; ok && length > 0 && targetID != importedPeopleCleared {
					formats = append(formats, importedPeopleMentionFormat{BlockID: block.ID, Index: index, Length: length, UserID: targetID})
					blockChanged = true
				}
			}
			// An embed takes a position too (graph_text_empty_pieces.go).
			if delta.Op == crdt.DeltaOpInsert {
				index += graphTextDeltaLength(delta)
			}
		}
		if blockChanged {
			changedBlocks++
		}
	}
	return formats, changedBlocks, nil
}

func utf16TextLength(value string) int {
	return len(utf16.Encode([]rune(value)))
}

// skipUnreachableImportedPeopleMappingPage passes over a page the mapping
// can't write while its asker still owns the workspace: one deleted or moved
// out since, or one they can't open — another member's private page, a page of
// a teamspace closed to them. One the mapping would have changed is counted
// as skipped; its imported people stay as they were.
func skipUnreachableImportedPeopleMappingPage(
	app core.App,
	principalID string,
	job storedImportedPeopleMappingJob,
	item storedImportedPeopleMappingItem,
	mappings map[string]string,
) (bool, error) {
	skipped := false
	err := app.RunInTransaction(func(txApp core.App) error {
		authorization, err := authorizeWorkspace(txApp, principalID, job.WorkspaceID, importedPeopleMappingPermission)
		if err != nil {
			return err
		}
		if authorization.OwnerID != job.OwnerID {
			return permissionDenied()
		}
		if err := validateImportedPeopleTargets(txApp, authorization, mappings); err != nil {
			return err
		}
		gone := false
		if page, err := txApp.FindRecordById(pagesCollection, item.PageID); errors.Is(err, sql.ErrNoRows) {
			gone = true
		} else if err != nil {
			return err
		} else if page.GetString("workspace") != job.WorkspaceID {
			gone = true
		} else if _, err := authorizePageBody(txApp, principalID, item.PageID, importedPeopleMappingPermission); err == nil {
			// The page opens now; what was refused wasn't the page.
			return nil
		} else if !isPermissionDenied(err) {
			return err
		}
		skipped = true
		left := false
		if !gone {
			if left, err = importedPeoplePageNeedsMapping(txApp, item.PageID, mappings); err != nil {
				return err
			}
		}
		if left {
			err = keepImportedPeopleMappingItemSkipped(txApp, job.ID, item.PageID)
		} else {
			err = markImportedPeopleMappingItem(txApp, job.ID, item.PageID, 0, 0)
		}
		if errors.Is(err, errImportedPeopleItemAlreadyApplied) {
			return nil
		}
		return err
	})
	return skipped, err
}

// importedPeoplePageNeedsMapping reports whether the mapping would change a
// page: a cell, block, view or mention on it names a mapped imported person.
func importedPeoplePageNeedsMapping(app core.App, pageID string, mappings map[string]string) (bool, error) {
	if operations, err := importedPeopleRowOperations(app, pageID, mappings); err != nil || len(operations) > 0 {
		return len(operations) > 0, err
	}
	if operations, _, err := importedPeopleBlockOperations(app, pageID, mappings); err != nil || len(operations) > 0 {
		return len(operations) > 0, err
	}
	if operations, err := importedPeopleViewOperations(app, pageID, mappings); err != nil || len(operations) > 0 {
		return len(operations) > 0, err
	}
	document, err := loadNotionYgoDocument(app, pageID, map[string]*notionYgoDocument{})
	if err != nil {
		return false, err
	}
	formats, _, err := importedPeopleMentionFormats(app, document.doc, pageID, mappings)
	return len(formats) > 0, err
}
