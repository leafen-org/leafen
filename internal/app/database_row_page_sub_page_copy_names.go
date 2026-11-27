package app

import (
	"encoding/json"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page made from a database template takes copies of the template's
// sub-pages (database_row_page_sub_page_copies.go), and what its body names
// of them names the copies, as a page's copy does (page_duplication_links.go)
// and as Notion's page made from a template does (seen 2026-11-23, a template
// of a database under the test page with a sub-page, a mention of it and
// links to it and to a line in it: the new page's mention and links named
// the new page's sub-page, /p/ and its ID, the anchor its copied line). Once
// the sub-pages' copies are done, the body's mentions and links — in its
// text and in its blocks' own rich text, a table's cells — that name a
// copied sub-page, a page under one or a block in them name its copy, by
// the rule a copy's text follows (pageDuplicationTextCopyAttributes); what
// the copies didn't take stays named as it was. So does a template's copy,
// which takes copies of its sub-pages the same way.

// nameRowPageSubPageCopies points what the page's body names of the
// sub-pages the page duplications jobIDs copied into it at their copies.
func nameRowPageSubPageCopies(app core.App, actorID, pageID string, jobIDs []string) error {
	if len(jobIDs) == 0 {
		return nil
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
		ids := make([]string, 0, len(metadata))
		for id := range metadata {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		mappings := func(pageIDs, blockIDs map[string]struct{}) (map[string]string, map[string]string, error) {
			return rowPageSubPageCopyMappings(txApp, jobIDs, pageIDs, blockIDs)
		}
		operations, err := rowPageSubPageCopyNameOperations(txApp, pageID, page.GetString("owner"), ids, metadata, mappings)
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
		return nameRowPageCopyText(txApp, actorID, pageID, ids, nil, mappings)
	})
}

// rowPageSubPageCopyMappings names the copies the sub-pages' copies made of
// the pages and row pages (pages) and of the blocks (blocks) among these;
// a copy that failed names none, its link staying as it was.
func rowPageSubPageCopyMappings(
	app core.App, jobIDs []string, pageIDs, blockIDs map[string]struct{},
) (map[string]string, map[string]string, error) {
	pages, blocks := map[string]string{}, map[string]string{}
	encoded, err := json.Marshal(jobIDs)
	if err != nil {
		return nil, nil, err
	}
	completed := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[job_id]] FROM {{leafen_page_duplication_jobs}}
		WHERE [[job_id]] IN (SELECT [[value]] FROM json_each({:ids})) AND [[phase]] = 'completed'
		ORDER BY [[job_id]]
	`).Bind(dbx.Params{"ids": string(encoded)}).Column(&completed); err != nil {
		return nil, nil, err
	}
	for _, jobID := range completed {
		found, err := pageDuplicationMentionTargetMappings(app, jobID, referenceIDs(pageIDs))
		if err != nil {
			return nil, nil, err
		}
		for source, target := range found {
			pages[source] = target
		}
		if found, err = pageDuplicationReferenceMappings(app, jobID, "block", referenceIDs(blockIDs)); err != nil {
			return nil, nil, err
		}
		for source, target := range found {
			blocks[source] = target
		}
	}
	return pages, blocks, nil
}

// rowPageSubPageCopyNameOperations are the changes that point the rich text
// the page's blocks ids hold in their own content at the copies.
func rowPageSubPageCopyNameOperations(
	app core.App, pageID, ownerID string, ids []string, metadata map[string]structuralBlockMetadata,
	mappings func(pageIDs, blockIDs map[string]struct{}) (map[string]string, map[string]string, error),
) ([]graphOperation, error) {
	references := pageDuplicationReferenceSet{
		pageIDs: map[string]struct{}{}, blockIDs: map[string]struct{}{},
		dataSourceIDs: map[string]struct{}{}, viewIDs: map[string]struct{}{},
	}
	for _, id := range ids {
		collectPageDuplicationReferences(metadata[id].Content, false, &references)
	}
	pages, blocks, err := mappings(references.pageIDs, references.blockIDs)
	if err != nil || len(pages)+len(blocks) == 0 {
		return nil, err
	}
	operations := []graphOperation{}
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
		rewritePageDuplicationReferences(content, false, pages, blocks, map[string]string{}, map[string]string{})
		after, err := json.Marshal(content)
		if err != nil {
			return nil, err
		}
		if string(after) == string(before) {
			continue
		}
		// The block as it stands, its content renamed.
		block := &graphBlock{ID: id, Type: entry.Type, Content: after}
		if err := app.DB().NewQuery(`
			SELECT [[position]] FROM {{leafen_blocks}} WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[id]]={:id}
		`).Bind(dbx.Params{"owner": ownerID, "pageId": pageID, "id": id}).Row(&block.Position); err != nil {
			return nil, err
		}
		if entry.ParentID != "" {
			parentID := entry.ParentID
			block.ParentID = &parentID
		}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertBlock, Block: block})
	}
	return operations, nil
}
