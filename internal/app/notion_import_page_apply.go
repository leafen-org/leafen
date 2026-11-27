package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// applyNotionPage makes the page a staged Notion page stands for, in its
// parent's place, and the sub-page block that shows it in its parent's body;
// a sub-page inside a block belongs to the page that holds the block. A page
// made before takes the title Notion changed since, and says true when it did.
func applyNotionPage(
	app core.App,
	job storedJob,
	payload notionImportJobPayload,
	source storedNotionSource,
	collection *core.Collection,
	mappings *notionApplyMappings,
	pageCursors *notionApplyPageCursors,
	organizationID string,
	createdAt int64,
	createdInteractions *[]pageInteraction,
	structuralDocuments map[string]*structuralMutationBatch,
	// The page the import's top goes under, when a migration's plan names one.
	topParentID string,
) (bool, error) {
	title, err := notionImportFittedTitle(app, job.ID, source, "child_page")
	if err != nil {
		return false, err
	}
	if pageID, found := mappings.nativeID(source.SourceID, "page"); found {
		// One an earlier run hung on the title that Notion's body lists now
		// takes its block (notion_migration_restored_pages.go); one hanging
		// on a block is mentioned there as Notion's block now has it.
		if err := unhangNotionMigrationRestoredPage(app, job, payload, source, mappings, structuralDocuments, pageID); err != nil {
			return false, err
		}
		if err := syncNotionImportPageMention(app, source, pageID); err != nil {
			return false, err
		}
		return syncNotionMigrationPageTitle(app, job, payload, pageID, source, title)
	}
	if collection == nil {
		return false, errors.New(errorCodeNotionImportCreateFailed)
	}
	parentPageID, containerID, anchor, unmentioned := "", "", "", false
	if source.ParentKind == "block" || source.ParentKind == "row" {
		// A sub-page inside a block belongs to the page that holds the block,
		// and its block stays inside that block; one made from a line's,
		// a table row's or a caption's mention hangs on that block, one
		// made from a callout's words on its first line
		// (notion_import_line_pages.go).
		if parentPageID, containerID, err = notionImportBlockParent(app, source, mappings); err != nil {
			return false, err
		}
		if containerID != "" {
			if anchor, unmentioned, err = notionImportLinePage(app, job.ID, source, containerID, mappings); err != nil {
				return false, err
			}
		}
	} else if source.ParentSourceID != "" {
		var found bool
		parentPageID, found = mappings.nativeID(source.ParentSourceID, "page")
		if !found {
			return false, errors.New(errorCodeNotionImportCreateFailed)
		}
	} else {
		parentPageID = topParentID
	}
	nextCursor, err := pageCursors.take()
	if err != nil {
		return false, err
	}
	// A page takes its parent's place: a teamspace, or private pages. One
	// made from its parent's title or cell hangs on that field
	// (notion_import_field_pages.go).
	teamspaceID := ""
	if parentPageID != "" {
		parent, err := app.FindRecordById(pagesCollection, parentPageID)
		if err != nil {
			return false, err
		}
		teamspaceID = parent.GetString("teamspace_id")
		if anchor == "" && containerID == "" {
			if anchor, unmentioned, err = notionImportFieldAnchor(app, source, parent); err != nil {
				return false, err
			}
		}
	}
	anchored := anchor != ""
	page := core.NewRecord(collection)
	page.Id = newPageID()
	page.Set("owner", payload.AuthorityOwnerID)
	page.Set("workspace", job.WorkspaceID)
	page.Set("teamspace_id", teamspaceID)
	page.Set("title", title)
	if !strings.HasPrefix(source.SourceID, notionDatabaseRootPageSourceID("")) {
		page.Set("title_spans", notionImportedPageTitleSpans(source.Text, title))
	}
	page.Set("content", "")
	page.Set("revision", 0)
	page.Set("cursor", nextCursor)
	page.Set("parent_id", parentPageID)
	position, err := notionImportFieldPagePosition(app, job.ID, source, mappings, parentPageID, anchor)
	if err != nil {
		return false, err
	}
	page.Set("position", position)
	// A database's own page is a full-page database: one imported on its own,
	// or one Notion shows as a full page.
	page.Set("kind", pageKindPage)
	if strings.HasPrefix(source.SourceID, notionDatabaseRootPageSourceID("")) {
		page.Set("kind", pageKindDatabase)
	}
	applyPageTrashState(page, false, "")
	initializePageStyleMetadata(page)
	initializePageCustomizationMetadata(page)
	applyNotionImportPagePresentation(page, source.Text, job.Owner)
	managedPresentation, err := applyNotionImportPageFiles(app, page, job.ID, job.WorkspaceID, source.SourceID)
	if err != nil {
		return false, err
	}
	page.Set("notion_import_staging", true)
	if anchored {
		page.Set(pageAnchorBlockField, anchor)
		page.Set(pageAnchorUnmentionedField, unmentioned)
	}
	setNewPageCustodianProvenance(page, job.Owner)
	if err := app.Save(page); err != nil {
		return false, err
	}
	if managedPresentation {
		if err := replaceProductFilePageReferences(
			app, job.WorkspaceID, page.Id, page.GetString("icon"), page.GetString("icon_type"), page.GetString("cover"),
		); err != nil {
			return false, err
		}
	}
	*createdInteractions = append(*createdInteractions, pageCreatedInteraction(
		organizationID, job.WorkspaceID, page.Id, job.Owner, createdAt,
	))
	if err := mappings.insert(source.SourceID, page.Id, "page"); err != nil {
		return false, err
	}
	// The plan's page takes the import's top as a sub-page block when the
	// import is published, as any page's body takes a new sub-page.
	if parentPageID == "" || parentPageID == topParentID || anchored {
		return false, nil
	}
	// A sub-page's block stands in its parent's body where Notion's did.
	batch, err := loadStructuralMutationBatch(app, parentPageID, structuralDocuments)
	if err != nil {
		return false, err
	}
	afterID, err := placeNotionImportBlock(app, job, payload, source, batch, containerID, page.Id, mappings)
	if err != nil {
		return false, err
	}
	return false, applyNotionGraphOperations(
		app, batch, payload.AuthorityOwnerID, job.WorkspaceID, parentPageID, job.Owner, []graphOperation{{
			Kind: graphOperationKindUpsertBlock,
			Block: &graphBlock{
				ID: page.Id, ParentID: optionalNotionImportParentID(containerID), AfterID: afterID,
				Position: fmt.Sprintf("notion:%010d", source.Position),
				Type:     graphBlockTypeChildPage, Content: childPageBlockContent(page.Id),
			},
		}},
	)
}

// notionImportFittedTitle is a Notion title as Leafen can hold it: a longer one
// is cut and reported, so it costs the end of the name and not the import.
func notionImportFittedTitle(app core.App, jobID string, source storedNotionSource, blockType string) (string, error) {
	title := []rune(source.Title)
	if len(title) <= maxGraphTitleRunes {
		return source.Title, nil
	}
	return string(title[:maxGraphTitleRunes]),
		recordNotionImportApplyIssue(app, jobID, source.SourceID, blockType, notionImportIssueTitleShortened)
}

func optionalNotionImportParentID(parentID string) *string {
	if parentID == "" {
		return nil
	}
	value := parentID
	return &value
}

func notionImportBlockParent(
	app core.App,
	source storedNotionSource,
	mappings *notionApplyMappings,
) (string, string, error) {
	switch source.ParentKind {
	case "page", "row":
		pageID, found := mappings.nativeID(source.ParentSourceID, source.ParentKind)
		if !found {
			return "", "", errors.New(errorCodeNotionImportCreateFailed)
		}
		return pageID, "", nil
	case "block":
		parentID, found := mappings.nativeID(source.ParentSourceID, "block")
		if !found {
			return "", "", errors.New(errorCodeNotionImportCreateFailed)
		}
		parent, found, err := loadStoredNotionBlock(app, parentID)
		if err != nil || !found {
			return "", "", errors.New(errorCodeNotionImportCreateFailed)
		}
		return parent.PageID, parentID, nil
	default:
		return "", "", errors.New(errorCodeNotionImportCreateFailed)
	}
}
