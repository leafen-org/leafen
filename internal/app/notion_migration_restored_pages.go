package app

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A page made from its parent's title's "@" that Notion restored from its
// trash comes back with a sub-page block at its parent's body end, though
// the title still mentions it (seen 2026-11-18 in two tabs of
// a Notion test page, through its trash window, the page's banner and its API),
// as Leafen's restore makes it (restoreMentionAnchoredPage). The first run
// brings such a page with its block (notionImportFieldAnchor); a run again
// that finds a page an earlier run hung on the title so listed in the body
// takes it off the title and gives it that block where Notion's stands. One
// made from a line's "@" comes back the last inside its line, which Notion's
// API lists among the line's children (notion_import_line_pages.go); a run
// again takes one an earlier run hung on that line off it so too, and one
// made from a callout's words, hung on its first line, the last inside the
// callout.

// unhangNotionMigrationRestoredPage gives a page an earlier run hung on its
// parent's title, or on a line, the block its parent's body or that line now
// lists, while it still hangs there under that parent in Leafen; a page
// somebody moved or trashed in Leafen since stays as it is.
func unhangNotionMigrationRestoredPage(
	app core.App, job storedJob, payload notionImportJobPayload, source storedNotionSource,
	mappings *notionApplyMappings, structuralDocuments map[string]*structuralMutationBatch, pageID string,
) error {
	if payload.MigrationID == "" || source.ParentSourceID == "" {
		return nil
	}
	stored := notionImportPresentation{}
	if strings.HasPrefix(source.Text, notionPresentationSourcePrefix) &&
		json.Unmarshal([]byte(strings.TrimPrefix(source.Text, notionPresentationSourcePrefix)), &stored) == nil &&
		stored.Anchor != "" {
		return nil
	}
	parentID, containerID, anchor, found := "", "", pageAnchorTitle, false
	switch source.ParentKind {
	case "page":
		parentID, found = mappings.nativeID(source.ParentSourceID, "page")
	case "block":
		containerID, found = mappings.nativeID(source.ParentSourceID, "block")
		anchor = containerID
		// One made from a callout's words hangs on its first line
		// (notionImportLinePage).
		if line, err := notionImportWordsLineID(app, job.ID, source.ParentSourceID+notionImportCalloutFirstLineSuffix, mappings); err != nil {
			return err
		} else if line != "" {
			anchor = line
		}
	}
	if !found {
		return nil
	}
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return err
	}
	if page.GetString(pageAnchorBlockField) != anchor || page.GetBool("in_trash") {
		return nil
	}
	if containerID != "" {
		block, found, err := loadStoredNotionBlock(app, containerID)
		if err != nil || !found {
			return err
		}
		parentID = block.PageID
	}
	if page.GetString("parent_id") != parentID {
		return nil
	}
	if err := setPageAnchor(app, pageID, "", false); err != nil {
		return err
	}
	batch, err := loadStructuralMutationBatch(app, parentID, structuralDocuments)
	if err != nil {
		return err
	}
	afterID, err := placeNotionImportBlock(app, job, payload, source, batch, containerID, pageID, mappings)
	if err != nil {
		return err
	}
	if err := applyNotionGraphOperations(
		app, batch, payload.AuthorityOwnerID, job.WorkspaceID, parentID, job.Owner, []graphOperation{{
			Kind: graphOperationKindUpsertBlock,
			Block: &graphBlock{
				ID: pageID, ParentID: optionalNotionImportParentID(containerID), AfterID: afterID,
				Position: fmt.Sprintf("notion:%010d", source.Position),
				Type:     graphBlockTypeChildPage, Content: childPageBlockContent(pageID),
			},
		}},
	); err != nil {
		return err
	}
	// Published already, it takes its sidebar place where its block now
	// stands, as a restored page does (alignMentionAnchoredPages).
	metadata, err := structuralMetadata(batch.doc)
	if err != nil {
		return err
	}
	return alignChildPageSidebar(app, batch.doc, metadata, payload.AuthorityOwnerID, job.WorkspaceID, parentID, []string{pageID})
}
