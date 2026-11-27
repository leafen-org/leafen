package app

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A database page made through the Public API takes its first body as a
// page's body: the request's blocks, or its Markdown's, and their text, with
// the Markdown's files placed, its callouts' custom emoji named, mentions
// prepared, its files checked as every block's are and the organization's
// content rules applied.
func applyNotionDatabaseRowPageCreateContent(
	app core.App,
	authorization notionDatabaseRowPageAuthorization,
	plan notionPageCreateContentPlan,
	actorID string,
) error {
	if plan.ContentRefused != nil {
		return notionPageCreateContentRefused(app, authorization.Identity, authorization.Page, plan)
	}
	if len(plan.Operations) == 0 {
		return nil
	}
	files := notionMarkdownFiles{
		WorkspaceID: authorization.Container.WorkspaceID,
		Upload: func(app core.App, _, uploadID string) (string, error) {
			content, err := resolveNotionCompletedFileUpload(app, authorization.Identity, uploadID)
			return content.FileID, err
		},
	}
	if err := files.place(app, plan.Operations); err != nil {
		return err
	}
	if err := resolveNotionMarkdownCalloutIcons(app, authorization.Container.WorkspaceID, plan.Operations); err != nil {
		return err
	}
	prepared, err := prepareNotionMentionsInput(
		app, authorization.Identity, authorization.Page,
		graphMutationInput{Operations: plan.Operations, TextUpdates: plan.TextUpdates}, plan.Markdown,
	)
	if err != nil {
		return err
	}
	// Its files, placed or uploaded, pass the graph's one check, before its
	// links as Notion's API refuses them (content_graph_given_body_files.go).
	if err := checkGivenPageBodyFiles(app, authorization.Container.WorkspaceID, prepared.Operations); err != nil {
		return err
	}
	// Links, synced blocks and custom emoji are checked as a page's are: a
	// database page's body takes them as Notion's does (seen 2026-11-10).
	if err := notionBlockReferencesBeforeApply(authorization.Identity)(app, authorization.Page, nil, prepared.Operations); err != nil {
		return err
	}
	if err := enforceOrganizationDLPContent(
		app, authorization.Page.Workspace.Organization.OrganizationID,
		prepared.Operations, prepared.TextUpdates,
	); err != nil {
		return err
	}
	textByBlock := make(map[string][]graphTextSpan, len(prepared.TextUpdates))
	for _, update := range prepared.TextUpdates {
		if _, duplicate := textByBlock[update.BlockID]; duplicate {
			return notionDatabaseRowBlockRequestError("duplicate rich_text update")
		}
		textByBlock[update.BlockID] = update.Spans
	}
	seen := make(map[string]struct{}, len(prepared.Operations))
	siblingIndex := map[string]int{}
	body := make([]newRowPageBlock, 0, len(prepared.Operations))
	for _, operation := range prepared.Operations {
		if operation.Kind != graphOperationKindUpsertBlock || operation.Block == nil {
			return notionDatabaseRowBlockRequestError("page content contains an unsupported mutation")
		}
		block := operation.Block
		if _, duplicate := seen[block.ID]; duplicate {
			return notionDatabaseRowBlockRequestError("page content contains duplicate block ids")
		}
		parentID := ""
		if block.ParentID != nil {
			parentID = *block.ParentID
			if _, found := seen[parentID]; !found {
				return notionDatabaseRowBlockRequestError("page content contains an invalid block parent")
			}
		}
		if !notionDatabaseRowBlockCreateType(block.Type) {
			return notionDatabaseRowBlockRequestError("this block type is not supported in database row-page content")
		}
		content := map[string]any{}
		if json.Unmarshal(block.Content, &content) != nil || content == nil {
			return notionDatabaseRowBlockRequestError("page content contains invalid block content")
		}
		spans, found := textByBlock[block.ID]
		delete(textByBlock, block.ID)
		if !found {
			spans = []graphTextSpan{}
		}
		siblingIndex[parentID]++
		body = append(body, newRowPageBlock{
			ID: block.ID, ParentID: parentID, Position: fmt.Sprintf("p%010d", siblingIndex[parentID]),
			Type: block.Type, Content: content, Spans: spans,
		})
		seen[block.ID] = struct{}{}
	}
	if len(textByBlock) != 0 {
		return notionDatabaseRowBlockRequestError("page content contains an orphan rich_text update")
	}
	return writeNewRowPageBody(app, authorization.Authority.RowID, actorID, body, time.Now().UTC().UnixMilli())
}

func notionDatabaseRowBlockCreateType(blockType string) bool {
	switch blockType {
	case graphBlockTypeInlineDatabase, graphBlockTypeChildPage, graphBlockTypeChildDatabase:
		return false
	default:
		return true
	}
}

func notionDatabaseRowBlockRequestError(message string) error {
	return &notionDatabaseRowPageRequestError{message: message}
}
