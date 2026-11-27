package app

import (
	"encoding/json"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

type pageDuplicationReferenceSet struct {
	pageIDs       map[string]struct{}
	blockIDs      map[string]struct{}
	dataSourceIDs map[string]struct{}
	viewIDs       map[string]struct{}
}

func remapPageDuplicationBlockContents(
	app core.App,
	jobID string,
	blocks []pageDuplicationSourceBlock,
) error {
	contents := make([]map[string]any, len(blocks))
	references := pageDuplicationReferenceSet{
		pageIDs: map[string]struct{}{}, blockIDs: map[string]struct{}{},
		dataSourceIDs: map[string]struct{}{}, viewIDs: map[string]struct{}{},
	}
	emojiIDs := map[string]struct{}{}
	for index, block := range blocks {
		if len(block.Content) == 0 || len(block.Content) > maxGraphChangeBytes {
			return errPageDuplicationConflict
		}
		contents[index] = map[string]any{}
		if err := json.Unmarshal([]byte(block.Content), &contents[index]); err != nil {
			return err
		}
		collectPageDuplicationReferences(contents[index], false, &references)
		usedCustomEmojiIDs(contents[index], emojiIDs)
	}
	// A copy into another workspace names the emoji it brings
	// (page_duplication_custom_emoji.go): a callout's, a button's, a tab's
	// and those its text mentions.
	emojis, err := pageDuplicationCustomEmojiMappings(app, jobID, emojiIDs)
	if err != nil {
		return err
	}
	for _, content := range contents {
		rewritePageDuplicationCustomEmoji(content, emojis)
	}
	pages, err := pageDuplicationMentionTargetMappings(app, jobID, referenceIDs(references.pageIDs))
	if err != nil {
		return err
	}
	blockMappings, err := pageDuplicationReferenceMappings(
		app, jobID, "block", referenceIDs(references.blockIDs),
	)
	if err != nil {
		return err
	}
	dataSources, err := pageDuplicationReferenceMappings(
		app, jobID, "data_source", referenceIDs(references.dataSourceIDs),
	)
	if err != nil {
		return err
	}
	views, err := pageDuplicationReferenceMappings(
		app, jobID, "view", referenceIDs(references.viewIDs),
	)
	if err != nil {
		return err
	}
	// A synced copy of an original the copy copied shows the original's copy
	// (synced_block_duplication.go).
	originals, err := copiedSyncedOriginals(app, jobID, contents)
	if err != nil {
		return err
	}
	for index, content := range contents {
		pointSyncedCopyAtCopiedOriginal(content, originals)
		encoded, err := remapPageDuplicationBlockContent(
			content, pages, blockMappings, dataSources, views,
		)
		if err != nil {
			return err
		}
		blocks[index].Content = encoded
	}
	return remapPageDuplicationBlockFiles(app, jobID, blocks)
}

func remapPageDuplicationBlockFiles(
	app core.App,
	jobID string,
	blocks []pageDuplicationSourceBlock,
) error {
	contents := make([]*graphFileBlockContent, len(blocks))
	icons := make([]string, len(blocks))
	fileIDs := map[string]struct{}{}
	for index := range blocks {
		candidate := &graphBlock{Type: blocks[index].Type, Content: []byte(blocks[index].Content)}
		// A callout's uploaded icon was copied with the page's other files,
		// and the copy shows the copied picture (block_kept_file.go).
		if icon := graphCalloutIconFileID(candidate.Type, candidate.Content); icon != "" {
			icons[index] = icon
			fileIDs[icon] = struct{}{}
			continue
		}
		if !graphBlockCarriesManagedFileReference(candidate) {
			continue
		}
		content, err := parseGraphFileBlockContent([]byte(blocks[index].Content))
		if err != nil {
			return err
		}
		contents[index] = &content
		fileIDs[content.FileID] = struct{}{}
	}
	mappings, err := pageDuplicationReferenceMappings(
		app, jobID, "file", referenceIDs(fileIDs),
	)
	if err != nil {
		return err
	}
	for index, icon := range icons {
		if icon == "" {
			continue
		}
		targetID := mappings[icon]
		if targetID == "" {
			return errPageDuplicationConflict
		}
		fields := map[string]any{}
		if err := json.Unmarshal([]byte(blocks[index].Content), &fields); err != nil {
			return err
		}
		fields["icon"] = targetID
		encoded, err := json.Marshal(fields)
		if err != nil {
			return err
		}
		blocks[index].Content = string(encoded)
	}
	for index, content := range contents {
		if content == nil {
			continue
		}
		targetID := mappings[content.FileID]
		if targetID == "" {
			return errPageDuplicationConflict
		}
		content.FileID = targetID
		encoded, err := json.Marshal(content)
		if err != nil {
			return err
		}
		blocks[index].Content = string(encoded)
	}
	return nil
}

func remapPageDuplicationBlockContent(
	content map[string]any,
	pages, blocks, dataSources, views map[string]string,
) (string, error) {
	rewritePageDuplicationReferences(content, false, pages, blocks, dataSources, views)
	encoded, err := json.Marshal(content)
	return string(encoded), err
}

func remapPageDuplicationTextDeltas(
	app core.App,
	jobID string,
	deltas [][]crdt.Delta,
) ([][]crdt.Delta, error) {
	pageIDs := map[string]struct{}{}
	databaseIDs := map[string]struct{}{}
	for _, delta := range deltas {
		for _, operation := range delta {
			collectPageDuplicationTextTargets(operation.Attributes, pageIDs, databaseIDs)
		}
	}
	pageMappings, databaseMappings, err := pageDuplicationTextMentionMappings(app, jobID, pageIDs, databaseIDs)
	if err != nil {
		return nil, err
	}
	remapped := make([][]crdt.Delta, len(deltas))
	for index, delta := range deltas {
		remapped[index] = remapPageDuplicationTextDelta(delta, pageMappings, databaseMappings)
	}
	// A copy into another workspace names the emoji it brings (page_duplication_custom_emoji.go).
	if err := remapPageDuplicationDeltaCustomEmoji(app, jobID, remapped); err != nil {
		return nil, err
	}
	return remapped, nil
}

// remapPageDuplicationTextDelta names, in a copied text, the copies of what
// its mentions and links name (page_duplication_links.go); blockMappings
// holds the copied blocks, databases by their block among them.
func remapPageDuplicationTextDelta(
	delta []crdt.Delta,
	pageMappings map[string]string,
	blockMappings map[string]string,
) []crdt.Delta {
	if len(pageMappings) == 0 && len(blockMappings) == 0 {
		return delta
	}
	remapped := append([]crdt.Delta(nil), delta...)
	for index := range remapped {
		changed := pageDuplicationTextCopyAttributes(remapped[index].Attributes, pageMappings, blockMappings)
		if changed == nil {
			continue
		}
		attributes := make(crdt.Attributes, len(remapped[index].Attributes))
		for key, value := range remapped[index].Attributes {
			attributes[key] = value
		}
		for key, value := range changed {
			attributes[key] = value
		}
		remapped[index].Attributes = attributes
	}
	return remapped
}

// remapPageDuplicationSpans names, in text a copy carries outside a block's
// text (a database cell, a database description, a template's cells), the
// copies of the pages, row pages and databases its mentions and links name,
// as a copied block's text does (remapPageDuplicationTextDeltas).
func remapPageDuplicationSpans(app core.App, jobID string, texts [][]graphTextSpan) error {
	pageIDs := map[string]struct{}{}
	blockIDs := map[string]struct{}{}
	for _, spans := range texts {
		collectPageDuplicationSpanTargets(spans, pageIDs, blockIDs)
	}
	pages, blocks, err := pageDuplicationTextMentionMappings(app, jobID, pageIDs, blockIDs)
	if err != nil {
		return err
	}
	for _, spans := range texts {
		pageDuplicationSpanCopy(spans, pages, blocks)
	}
	return remapPageDuplicationSpanCustomEmoji(app, jobID, texts)
}

// pageDuplicationTextMentionMappings maps what a text's mentions may name —
// pages and row pages by pageId, inline databases by their block's
// databaseId, data sources by their address there — to the job's copies.
func pageDuplicationTextMentionMappings(
	app core.App,
	jobID string,
	pageIDs, databaseIDs map[string]struct{},
) (map[string]string, map[string]string, error) {
	pages, err := pageDuplicationMentionTargetMappings(app, jobID, referenceIDs(pageIDs))
	if err != nil {
		return nil, nil, err
	}
	databases, err := pageDuplicationReferenceMappings(app, jobID, "block", referenceIDs(databaseIDs))
	if err != nil {
		return nil, nil, err
	}
	return pages, databases, pageDuplicationDataSourceMentions(app, jobID, databaseIDs, databases)
}

// pageDuplicationMentionTargetMappings maps the pages and database row pages
// a page mention may name to their copies: a sub-page's copy carried by a
// copied body reads the copies that body's copy made too
// (block_copy_page_links.go).
func pageDuplicationMentionTargetMappings(app core.App, jobID string, sourceIDs []string) (map[string]string, error) {
	pages := map[string]string{}
	jobs := []string{jobID}
	if parent, err := pageDuplicationParentBlockCopyJob(app, jobID); err != nil {
		return nil, err
	} else if parent != "" {
		jobs = append(jobs, parent)
	}
	for _, job := range jobs {
		for _, kind := range []string{"page", "row"} {
			found, err := pageDuplicationReferenceMappings(app, job, kind, sourceIDs)
			if err != nil {
				return nil, err
			}
			for sourceID, targetID := range found {
				if _, mapped := pages[sourceID]; !mapped {
					pages[sourceID] = targetID
				}
			}
		}
	}
	return pages, nil
}

func pageDuplicationReferenceMappings(
	app core.App,
	jobID, kind string,
	sourceIDs []string,
) (map[string]string, error) {
	mappings := map[string]string{}
	if len(sourceIDs) == 0 {
		return mappings, nil
	}
	encoded, err := json.Marshal(sourceIDs)
	if err != nil {
		return nil, err
	}
	if kind == "data_source" || kind == "view" || kind == "property" || kind == "row" {
		return pageDuplicationDatabaseReferenceMappings(app, jobID, kind, sourceIDs)
	}
	stored := []pageDuplicationIdentity{}
	err = app.DB().NewQuery(`
		SELECT [[source_id]],[[target_id]]
		FROM {{leafen_page_duplication_identity_map}}
		WHERE [[job_id]]={:jobId} AND [[kind]]={:kind}
			AND [[source_id]] IN (SELECT [[value]] FROM json_each({:sourceIds}))
		ORDER BY [[source_id]]
	`).Bind(dbx.Params{
		"jobId": jobID, "kind": kind, "sourceIds": string(encoded),
	}).All(&stored)
	if err != nil {
		return nil, err
	}
	for _, mapping := range stored {
		mappings[mapping.SourceID] = mapping.TargetID
	}
	return mappings, nil
}

func collectPageDuplicationReferences(
	value any,
	richText bool,
	references *pageDuplicationReferenceSet,
) {
	switch typed := value.(type) {
	case []any:
		for _, child := range typed {
			collectPageDuplicationReferences(child, richText, references)
		}
	case map[string]any:
		if id, ok := pageDuplicationString(typed["dataSourceId"]); ok {
			references.dataSourceIDs[id] = struct{}{}
		}
		if ids, ok := typed["additionalDataSourceIds"].([]any); ok {
			for _, value := range ids {
				if id, ok := pageDuplicationString(value); ok {
					references.dataSourceIDs[id] = struct{}{}
				}
			}
		}
		if id, ok := pageDuplicationString(typed["sourceContainerId"]); ok {
			references.blockIDs[id] = struct{}{}
		}
		if id, ok := pageDuplicationString(typed["sourceBlockId"]); ok {
			references.blockIDs[id] = struct{}{}
		}
		if id, ok := pageDuplicationString(typed["databaseContainerId"]); ok {
			references.blockIDs[id] = struct{}{}
		}
		for _, key := range []string{"viewId", "databaseViewId"} {
			if id, ok := pageDuplicationString(typed[key]); ok {
				references.viewIDs[id] = struct{}{}
			}
		}
		if ids, ok := typed["databaseViewIds"].([]any); ok {
			for _, value := range ids {
				if id, ok := pageDuplicationString(value); ok {
					references.viewIDs[id] = struct{}{}
				}
			}
		}
		if id, ok := pageDuplicationString(typed["childPageId"]); ok {
			references.pageIDs[id] = struct{}{}
		}
		syncedBlockID, hasSyncedBlock := pageDuplicationString(typed["syncedBlockId"])
		if hasSyncedBlock {
			references.blockIDs[syncedBlockID] = struct{}{}
		}
		if id, ok := pageDuplicationString(typed["syncedPageId"]); ok {
			references.pageIDs[id] = struct{}{}
		}
		if richText {
			collectPageDuplicationTextTargets(typed, references.pageIDs, references.blockIDs)
		}
		for key, child := range typed {
			collectPageDuplicationReferences(
				child, richText || graphContentRichTextField(key), references,
			)
		}
	}
}

func rewritePageDuplicationReferences(
	value any,
	richText bool,
	pages, blocks, dataSources, views map[string]string,
) {
	switch typed := value.(type) {
	case []any:
		for _, child := range typed {
			rewritePageDuplicationReferences(child, richText, pages, blocks, dataSources, views)
		}
	case map[string]any:
		pageDuplicationReplaceReference(typed, "dataSourceId", dataSources)
		if ids, ok := typed["additionalDataSourceIds"].([]any); ok {
			for index, value := range ids {
				if sourceID, ok := pageDuplicationString(value); ok {
					if targetID, found := dataSources[sourceID]; found {
						ids[index] = targetID
					}
				}
			}
		}
		pageDuplicationReplaceReference(typed, "sourceContainerId", blocks)
		pageDuplicationReplaceReference(typed, "sourceBlockId", blocks)
		pageDuplicationReplaceReference(typed, "databaseContainerId", blocks)
		pageDuplicationReplaceReference(typed, "viewId", views)
		pageDuplicationReplaceReference(typed, "databaseViewId", views)
		if ids, ok := typed["databaseViewIds"].([]any); ok {
			for index, value := range ids {
				if sourceID, ok := pageDuplicationString(value); ok {
					if targetID, found := views[sourceID]; found {
						ids[index] = targetID
					}
				}
			}
		}
		pageDuplicationReplaceReference(typed, "childPageId", pages)
		// A synced copy's original is named apart (pointSyncedCopyAtCopiedOriginal).
		if richText {
			for key, value := range pageDuplicationTextCopyAttributes(typed, pages, blocks) {
				typed[key] = value
			}
		}
		for key, child := range typed {
			rewritePageDuplicationReferences(
				child, richText || graphContentRichTextField(key),
				pages, blocks, dataSources, views,
			)
		}
	}
}

func pageDuplicationReplaceReference(value map[string]any, key string, mappings map[string]string) {
	sourceID, ok := pageDuplicationString(value[key])
	if !ok {
		return
	}
	if targetID, found := mappings[sourceID]; found {
		value[key] = targetID
	}
}

func pageDuplicationString(value any) (string, bool) {
	result, ok := value.(string)
	return result, ok && result != ""
}

func referenceIDs(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
