package app

import (
	"context"
	"encoding/json"
	"sort"
	"unicode/utf16"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A database page's body may name its own databases besides holding them: a
// linked database showing one, with its views, a block or a mention naming
// one, its views or its rows. Its own databases come after the rest of the
// body's copy, each by a copy of its block (database_row_page_database_
// copies.go), so the copy's body is written naming the source's; once a
// database's copy is done, what the copy's body names of it names the copy,
// as a copied page's body names the copies its copy made
// (pageDuplicationBodyRewrite). Before, a row's duplicate, a page made from
// a template and a copied database's rows kept a linked database of the
// body's own database showing the source's — copied into another
// workspace, a database of the workspace they came from — and their
// mentions of it opened the source's.

// nameRowPageDatabaseCopy points the page's blocks, their linked databases'
// views and their text at the copies the block copy moveJobID made, where
// they name what it copied: one of the page's own databases.
func nameRowPageDatabaseCopy(app core.App, actorID, pageID, moveJobID string) error {
	move, err := loadBlockMoveJobByID(app, moveJobID, "")
	if err != nil {
		return err
	}
	made := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[target_block_id]] FROM {{leafen_block_move_items}} WHERE [[job_id]]={:jobId} AND [[target_block_id]]<>''
	`).Bind(dbx.Params{"jobId": moveJobID}).Column(&made); err != nil {
		return err
	}
	copied := make(map[string]bool, len(made))
	for _, id := range made {
		copied[id] = true
	}
	return app.RunInTransaction(func(txApp core.App) error {
		page, err := txApp.FindRecordById(pagesCollection, pageID)
		if err != nil {
			return err
		}
		batches := map[string]*structuralMutationBatch{}
		batch, err := loadStructuralMutationBatch(txApp, pageID, batches)
		if err != nil {
			return err
		}
		metadata, err := structuralMetadata(batch.doc)
		if err != nil {
			return err
		}
		// What the copy made names its own already.
		ids := make([]string, 0, len(metadata))
		for id := range metadata {
			if !copied[id] {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		operations, err := rowPageDatabaseCopyNameOperations(txApp, move, pageID, page.GetString("owner"), ids, metadata)
		if err != nil {
			return err
		}
		if len(operations) > 0 {
			if err := applyNotionGraphOperations(txApp, batch, page.GetString("owner"), page.GetString("workspace"), pageID, actorID, operations); err != nil {
				return err
			}
			if err := saveStructuralMutationBatches(txApp, actorID, batches); err != nil {
				return err
			}
		}
		return nameRowPageCopyText(txApp, actorID, pageID, ids, made, func(pageIDs, blockIDs map[string]struct{}) (map[string]string, map[string]string, error) {
			return pageDuplicationTextMentionMappings(txApp, moveJobID, pageIDs, blockIDs)
		})
	})
}

// rowPageDatabaseCopyNameOperations are the changes that point the page's
// blocks ids at what the copy move made in place of what it copied: each
// block naming it, and each view of a linked database showing it, with
// the copy's data source and what its settings name.
func rowPageDatabaseCopyNameOperations(
	app core.App, move storedBlockMoveJob, pageID, ownerID string, ids []string, metadata map[string]structuralBlockMetadata,
) ([]graphOperation, error) {
	references := pageDuplicationReferenceSet{
		pageIDs: map[string]struct{}{}, blockIDs: map[string]struct{}{},
		dataSourceIDs: map[string]struct{}{}, viewIDs: map[string]struct{}{},
	}
	for _, id := range ids {
		collectPageDuplicationReferences(metadata[id].Content, false, &references)
	}
	pages, err := pageDuplicationMentionTargetMappings(app, move.ID, referenceIDs(references.pageIDs))
	if err != nil {
		return nil, err
	}
	blocks, err := pageDuplicationReferenceMappings(app, move.ID, "block", referenceIDs(references.blockIDs))
	if err != nil {
		return nil, err
	}
	dataSources, err := pageDuplicationReferenceMappings(app, move.ID, "data_source", referenceIDs(references.dataSourceIDs))
	if err != nil {
		return nil, err
	}
	views, err := pageDuplicationReferenceMappings(app, move.ID, "view", referenceIDs(references.viewIDs))
	if err != nil {
		return nil, err
	}
	if len(pages)+len(blocks)+len(dataSources)+len(views) == 0 {
		return nil, nil
	}
	operations := []graphOperation{}
	linked := []string{}
	for _, id := range ids {
		entry := metadata[id]
		before, err := json.Marshal(entry.Content)
		if err != nil {
			return nil, err
		}
		content := map[string]any{}
		if err := json.Unmarshal(before, &content); err != nil {
			return nil, err
		}
		rewritePageDuplicationReferences(content, false, pages, blocks, dataSources, views)
		after, err := json.Marshal(content)
		if err != nil {
			return nil, err
		}
		if string(after) == string(before) {
			continue
		}
		block, err := notionDataSourceMoveBlock(app, entry, id, pageID, ownerID, content)
		if err != nil {
			return nil, err
		}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertBlock, Block: block})
		if isLinked, _ := content["linkedDatabaseSource"].(bool); entry.Type == graphBlockTypeInlineDatabase && isLinked {
			linked = append(linked, id)
		}
	}
	viewOperations, err := rowPageDatabaseCopyLinkedViews(app, move, pageID, linked, dataSources)
	if err != nil {
		return nil, err
	}
	return append(operations, viewOperations...), nil
}

// rowPageDatabaseCopyLinkedViews is each view of the page's linked databases
// that shows a data source the copy move copied, as the copy's: its data
// source and the properties, rows and pages its settings name, those the
// copy into another workspace left out taken out of them, as a copied page's
// linked views (pageDuplicationLinkedViewCopy).
func rowPageDatabaseCopyLinkedViews(
	app core.App, move storedBlockMoveJob, pageID string, linked []string, dataSources map[string]string,
) ([]graphOperation, error) {
	if len(linked) == 0 || len(dataSources) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(linked)
	if err != nil {
		return nil, err
	}
	stored := []struct {
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
		WHERE [[page_id]]={:pageId} AND [[container_id]] IN (SELECT [[value]] FROM json_each({:linked}))
		ORDER BY [[container_id]],[[position]],[[id]]
	`).Bind(dbx.Params{"pageId": pageID, "linked": string(encoded)}).All(&stored); err != nil {
		return nil, err
	}
	operations := []graphOperation{}
	for _, view := range stored {
		target := dataSources[view.DataSourceID]
		if target == "" {
			continue
		}
		config := json.RawMessage(view.Config)
		if move.crossWorkspace() {
			if config, err = pageDuplicationViewConfigWithout(app, move.ID, config); err != nil {
				return nil, err
			}
		}
		configs, err := remapPageDuplicationDatabaseJSON(app, move.ID, []json.RawMessage{config})
		if err != nil {
			return nil, err
		}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
			ID: view.ID, DataSourceID: target, ContainerID: view.ContainerID,
			Type: view.Type, Name: view.Name, Position: view.Position, Config: configs[0],
		}})
	}
	return operations, nil
}

// nameRowPageCopyText points the mentions and links in the text of the
// page's blocks ids at the copies a copy into the page made — of a database
// and the row pages they name, or of a template's sub-pages
// (database_row_page_sub_page_copy_names.go) — as a copied block's text does
// (remapBlockCopyTextDelta); mappings names the copies of what the text
// names, made are the blocks the copy made. It is actorID's change
// (ygo_server_text_documents.go): sent to the page's open editors and, once
// its history has begun, in its history — stored apart from both, the copy's
// renamed mentions showed in no version and an open editor kept the source's.
func nameRowPageCopyText(
	app core.App, actorID, pageID string, ids, made []string,
	mappings func(pageIDs, blockIDs map[string]struct{}) (map[string]string, map[string]string, error),
) error {
	document, err := loadNotionYgoDocument(app, pageID, map[string]*notionYgoDocument{})
	if err != nil || !document.found {
		return err
	}
	doc := document.doc
	deltas := make(map[string][]crdt.Delta, len(ids))
	pageIDs, databaseIDs := map[string]struct{}{}, map[string]struct{}{}
	for _, id := range ids {
		delta := doc.GetText(ygoBlockTextRootPrefix + id).ToDelta()
		for _, operation := range delta {
			collectPageDuplicationTextTargets(operation.Attributes, pageIDs, databaseIDs)
		}
		deltas[id] = delta
	}
	if len(pageIDs) == 0 && len(databaseIDs) == 0 {
		return nil
	}
	pages, databases, err := mappings(pageIDs, databaseIDs)
	if err != nil || len(pages) == 0 && len(databases) == 0 {
		return err
	}
	type textFormat struct {
		blockID       string
		index, length int
		attributes    crdt.Attributes
	}
	formats := []textFormat{}
	pagesChanged := false
	for _, id := range ids {
		index := 0
		for _, operation := range deltas[id] {
			insert, _ := operation.Insert.(string)
			length := len(utf16.Encode([]rune(insert)))
			if insert == "" {
				length = 1
			}
			if attributes := pageDuplicationTextCopyAttributes(operation.Attributes, pages, databases); attributes != nil {
				_, mention := attributes["pageId"]
				_, link := attributes["link"]
				pagesChanged = pagesChanged || mention || link
				formats = append(formats, textFormat{id, index, length, attributes})
			}
			index += length
		}
	}
	if len(formats) == 0 {
		return nil
	}
	doc.Transact(func(transaction *crdt.Transaction) {
		for _, format := range formats {
			transaction.GetText(ygoBlockTextRootPrefix+format.blockID).Format(transaction, format.index, format.length, format.attributes)
		}
	})
	if err := saveNotionYgoDocuments(app, actorID, map[string]*notionYgoDocument{pageID: document}); err != nil {
		return err
	}
	if !pagesChanged {
		return nil
	}
	// The page mentions it now holds are the page's references.
	targets := map[string][]string{}
	for _, id := range append(append([]string(nil), ids...), made...) {
		if list := blockTextPageTargets(doc.GetText(ygoBlockTextRootPrefix + id)); len(list) > 0 {
			targets[id] = list
		}
	}
	return projectPageTextReferences(context.Background(), app, pageID, targets)
}
