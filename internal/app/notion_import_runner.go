package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/pocketbase/pocketbase/core"
)

type stagedNotionSource struct {
	SourceID        string `json:"sourceId"`
	Kind            string `json:"kind"`
	ParentSourceID  string `json:"parentSourceId"`
	ParentKind      string `json:"parentKind"`
	Position        int    `json:"position"`
	Title           string `json:"title"`
	Text            string `json:"text"`
	Blueprint       bool   `json:"blueprint"`
	FileURLCipher   string `json:"fileURLCipher"`
	DiscoveryStatus string `json:"discoveryStatus,omitempty"`
	// DiscoveryPosition is the place its first child takes: 1 under a
	// callout whose first line is staged at 0 (notionImportCalloutFirstLine).
	DiscoveryPosition int `json:"discoveryPosition,omitzero"`
}

func (r *durableJobRunner) applyNotionImportChunk(ctx context.Context, job storedJob) error {
	payload := notionImportJobPayload{}
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return fmt.Errorf("%s: invalid import payload: %w", errorCodeJobRunnerFailed, err)
	}
	if payload.ConnectionID == "" || len(payload.RootIDs) == 0 || payload.AuthorityOwnerID == "" {
		return errors.New(errorCodeNotionImportCreateFailed)
	}
	if _, err := authorizeNotionImportTurn(r.app, job, payload); err != nil {
		return err
	}
	logNotionImportProgress(r.app, job.ID)
	followNotionImportRun(r.app, job, &payload)
	if len(payload.LedgerFrom) > 0 || payload.LedgerPreload {
		return r.startNotionMigrationRunFromLedger(job, payload)
	}
	switch payload.Phase {
	case notionImportPhaseScope:
		return r.discoverNotionImportChunk(ctx, job, payload)
	case notionImportPhaseApply:
		return r.applyDiscoveredNotionChunkIn(ctx, job, payload)
	default:
		return errors.New(errorCodeNotionImportCreateFailed)
	}
}

func (r *durableJobRunner) discoverNotionImportChunk(ctx context.Context, job storedJob, payload notionImportJobPayload) error {
	if payload.RootIndex >= len(payload.RootIDs) && len(payload.PendingDatabaseIDs) == 0 && len(payload.PendingFileIDs) == 0 {
		discovery, found, err := loadNotionImportPageDiscovery(r.app, job.ID)
		if err != nil {
			return err
		}
		if !found && payload.UnmentionedSwept {
			payload.Phase = notionImportPhaseApply
			return r.commitNotionDiscovery(job, payload, nil)
		}
		connection, err := loadNotionConnection(r.app, payload.ConnectionID, job.Owner, job.WorkspaceID)
		if err != nil {
			return err
		}
		token, err := decryptNotionConnection(r.app, connection)
		if err != nil {
			return err
		}
		client := r.notionProviderClient(payload.ConnectionID, token)
		if !found {
			// Once all is staged, the pages hanging on its blocks unmentioned.
			return r.sweepNotionImportUnmentionedPages(ctx, job, payload, client)
		}
		return r.discoverNotionPageChunk(ctx, job, payload, client, discovery)
	}
	connection, err := loadNotionConnection(r.app, payload.ConnectionID, job.Owner, job.WorkspaceID)
	if err != nil {
		return err
	}
	token, err := decryptNotionConnection(r.app, connection)
	if err != nil {
		return err
	}
	client := r.notionProviderClient(payload.ConnectionID, token)
	if len(payload.PendingFileIDs) > 0 {
		return r.discoverNotionFileChunk(ctx, job, payload, client)
	}
	if len(payload.PendingDatabaseIDs) > 0 {
		return r.discoverNotionDatabaseChunk(ctx, job, payload, client)
	}
	runPlan, err := loadNotionMigrationRunPlan(r.app, job.Owner, payload)
	if err != nil {
		return err
	}
	rootID := payload.RootIDs[payload.RootIndex]
	rootKind := notionImportRootKind(payload, payload.RootIndex)
	if runPlan.skips(rootID) {
		// A root the plan leaves out is passed by, with all under it.
		payload.RootIndex++
		payload.RootStaged = false
		payload.RootChildPosition = 0
		payload.ProviderCursor = ""
		return r.commitNotionDiscovery(job, payload, nil)
	}
	if rootKind == "database" {
		database, first, others, err := client.databaseForImport(ctx, rootID)
		if err != nil {
			return err
		}
		if database.Object != "database" || database.ID != rootID {
			return errors.New(errorCodeNotionImportProviderFailed)
		}
		pageSourceID := notionDatabaseRootPageSourceID(rootID)
		payload.RootIndex++
		payload.PendingDatabaseIDs = append(payload.PendingDatabaseIDs, rootID)
		payload.ActiveDatabaseID = rootID
		payload.DatabaseDataSourceID = first.ID
		sources := []stagedNotionSource{
			{
				SourceID: pageSourceID, Kind: "page", Position: payload.RootIndex - 1,
				Title: notionObjectTitle(database), Text: notionImportPageSourceText(database),
			},
			{
				SourceID: rootID, Kind: "database", ParentSourceID: pageSourceID,
				ParentKind: "page", Position: 0, Title: notionObjectTitle(database),
				Text: notionImportPresentationText(database, false),
			},
		}
		properties, propertyCursor, propertiesComplete, propertyFingerprint, err := notionDatabasePropertyChunk(
			database,
			"",
			notionImportProviderPageItems-len(sources),
		)
		if err != nil {
			return err
		}
		payload.DatabasePropertyCursor = propertyCursor
		payload.DatabasePropertyFingerprint = propertyFingerprint
		payload.DatabasePropertiesComplete = propertiesComplete
		sources = append(sources, properties...)
		presentationSources, presentationFiles, err := notionImportPresentationFileSources(r.app, database, rootID, true, true)
		if err != nil {
			return err
		}
		sources = append(sources, presentationSources...)
		// Its other data sources follow the first's archived rows.
		payload.DatabaseOtherSources = others
		if len(others) > 0 {
			sources = append(sources, notionImportFirstDataSource(database, first, others))
		}
		payload.PendingFileIDs = append(payload.PendingFileIDs, presentationFiles...)
		return r.commitNotionDiscovery(job, payload, sources)
	}
	if !payload.RootStaged {
		root, err := client.page(ctx, rootID)
		if err != nil {
			return err
		}
		if root.Object != "page" || root.ID != rootID {
			return errors.New(errorCodeNotionImportProviderFailed)
		}
		payload.RootStaged = true
		payload.noteUnmentionedParents([]notionProviderObject{root}, false)
		presentationSources, presentationFiles, err := notionImportPresentationFileSources(r.app, root, root.ID, false, true)
		if err != nil {
			return err
		}
		payload.PendingFileIDs = append(payload.PendingFileIDs, presentationFiles...)
		return r.commitNotionDiscovery(job, payload, append([]stagedNotionSource{{
			SourceID: root.ID, Kind: "page", Position: payload.RootIndex, Title: notionObjectTitle(root),
			Text: notionImportPageSourceText(root),
		}}, presentationSources...))
	}

	children, err := client.children(ctx, rootID, payload.ProviderCursor)
	if err != nil {
		return err
	}
	results, err := notionProviderPageResults(children)
	if err != nil {
		return err
	}
	nextCursor, err := notionProviderContinuationCursor(payload.ProviderCursor, children)
	if err != nil {
		return err
	}
	sources := make([]stagedNotionSource, 0, len(results))
	sources, databaseIDs, fileIDs, err := stageNotionPageChildren(
		r.app, rootID, "page", false, payload.RootChildPosition, runPlan.keep(results), sources,
	)
	if err == nil {
		err = notionImportHeadingReads(ctx, r.app, client, job.ID, rootID, "page", false, runPlan.keep(results), sources)
	}
	if err != nil {
		return err
	}
	payload.noteUnmentionedParents(runPlan.keep(results), false)
	if err := nameNotionImportOutsideLinks(ctx, r.app, client, job.ID, sources); err != nil {
		return err
	}
	// The pages hanging on its blocks, and once its body is staged those on
	// its title, as on any page's (notion_import_page_discovery.go).
	linePages, err := notionImportLinePageSources(ctx, r.app, client, job.ID, runPlan.keep(results), false)
	if err != nil {
		return err
	}
	sources = append(sources, linePages...)
	if !children.HasMore {
		fieldPages, err := notionImportFieldPageSources(
			ctx, r.app, client, job.ID, notionImportPageDiscovery{SourceID: rootID, Kind: "page"}, sources,
		)
		if err != nil {
			return err
		}
		sources = append(sources, fieldPages...)
	}
	payload.PendingDatabaseIDs = append(payload.PendingDatabaseIDs, databaseIDs...)
	payload.PendingFileIDs = append(payload.PendingFileIDs, fileIDs...)
	payload.RootChildPosition += len(results)
	if children.HasMore {
		payload.ProviderCursor = nextCursor
	} else {
		payload.RootIndex++
		payload.RootStaged = false
		payload.RootChildPosition = 0
		payload.ProviderCursor = ""
	}
	return r.commitNotionDiscovery(job, payload, sources)
}

// stageNotionPageChildren stages one provider page of blocks in their order. A
// block Leafen can't take is reported where it stood, not a reason to stop the
// rest of the import: staging reads nothing from Notion, so a failure that names
// an import code is the block's own and any other is Leafen's.
func stageNotionPageChildren(
	app core.App,
	parentSourceID string,
	parentKind string,
	blueprint bool,
	startPosition int,
	results []notionProviderObject,
	sources []stagedNotionSource,
) ([]stagedNotionSource, []string, []string, error) {
	databaseIDs := make([]string, 0)
	fileIDs := make([]string, 0)
	for index, child := range results {
		if child.Object != "block" {
			continue
		}
		position := startPosition + index
		staged, childDatabases, childFiles, err := stageNotionPageChild(app, parentSourceID, parentKind, blueprint, position, child)
		if err != nil {
			if stableNotionImportFailureCode(err) == "" {
				return nil, nil, nil, err
			}
			sources = append(sources, notionImportIssueSource(
				parentSourceID, parentKind, position, child, notionImportIssueInvalidBlock,
			))
			continue
		}
		sources = append(sources, staged...)
		databaseIDs = append(databaseIDs, childDatabases...)
		fileIDs = append(fileIDs, childFiles...)
		for _, reason := range notionImportBlockTextOmissions(child) {
			issue := notionImportIssueSource(parentSourceID, parentKind, position, child, reason)
			if !slices.ContainsFunc(staged, func(source stagedNotionSource) bool { return source.SourceID == issue.SourceID }) {
				sources = append(sources, issue)
			}
		}
	}
	return sources, databaseIDs, fileIDs, nil
}

// notionImportBlockTextOmissions names what a block's rich text, caption or
// table cells kept their words without.
func notionImportBlockTextOmissions(block notionProviderObject) []string {
	if len(block.Payload) == 0 || string(block.Payload) == "null" {
		return nil
	}
	fields := struct {
		RichText []notionProviderRichText   `json:"rich_text"`
		Caption  []notionProviderRichText   `json:"caption"`
		Cells    [][]notionProviderRichText `json:"cells"`
	}{}
	if json.Unmarshal(block.Payload, &fields) != nil {
		return nil
	}
	reasons := []string{}
	for _, parts := range append([][]notionProviderRichText{fields.RichText, fields.Caption}, fields.Cells...) {
		plan, err := notionProviderRichTextSourcePlan(parts, false)
		if err != nil {
			continue
		}
		for _, reason := range plan.Omitted {
			if !slices.Contains(reasons, reason) {
				reasons = append(reasons, reason)
			}
		}
	}
	return reasons
}

// stageNotionPageChild stages one block: what it holds, the databases and the
// files that follow from it.
func stageNotionPageChild(
	app core.App,
	parentSourceID string,
	parentKind string,
	blueprint bool,
	position int,
	child notionProviderObject,
) ([]stagedNotionSource, []string, []string, error) {
	if child.Unreadable {
		return nil, nil, nil, errors.New(errorCodeNotionImportProviderFailed)
	}
	sources := []stagedNotionSource{}
	databaseIDs, fileIDs := []string(nil), []string(nil)
	switch child.Type {
	case "template":
		text, err := notionProviderRichTextSourceText(child.Template.RichText, true)
		if err != nil {
			return nil, nil, nil, err
		}
		sources = append(sources, stagedNotionSource{
			SourceID: child.ID, Kind: "template", ParentSourceID: parentSourceID,
			ParentKind: parentKind, Position: position, Text: text, Blueprint: true,
			DiscoveryStatus: notionImportBlockDiscoveryStatus(child),
		})
	case graphBlockTypeTable:
		// One without rows, and then without width (table_width null), is
		// taken as it is (content_graph_table.go).
		if child.Table.TableWidth < 0 || child.Table.TableWidth > maxGraphTableWidth {
			return nil, nil, nil, errors.New(errorCodeNotionImportProviderFailed)
		}
		content, err := json.Marshal(graphTableBlockContent{
			TableWidth: child.Table.TableWidth, HasColumnHeader: child.Table.HasColumnHeader,
			HasRowHeader: child.Table.HasRowHeader,
		})
		if err != nil {
			return nil, nil, nil, err
		}
		sources = append(sources, stagedNotionSource{
			SourceID: child.ID, Kind: graphBlockTypeTable, ParentSourceID: parentSourceID,
			ParentKind: parentKind, Position: position, Text: string(content), Blueprint: blueprint,
			DiscoveryStatus: notionImportPageDiscoveryPending,
		})
	case graphBlockTypeTableRow:
		content, err := notionProviderTableRowContent(child.TableRow.Cells)
		if err != nil {
			return nil, nil, nil, err
		}
		// Blocks under it stay in Notion, named in the report
		// (notion_import_table_row_blocks.go).
		sources = append(append(sources, stagedNotionSource{
			SourceID: child.ID, Kind: graphBlockTypeTableRow, ParentSourceID: parentSourceID,
			ParentKind: parentKind, Position: position, Text: string(content), Blueprint: blueprint,
		}), notionImportTableRowBlocks(parentSourceID, parentKind, position, child)...)
	case graphBlockTypeBookmark, graphBlockTypeLinkPreview:
		source, staged, err := stageNotionLinkBlock(parentSourceID, parentKind, blueprint, position, child)
		if err != nil {
			return nil, nil, nil, err
		}
		if staged {
			sources = append(sources, source)
		} else {
			sources = append(sources, notionImportIssueSource(
				parentSourceID, parentKind, position, child, notionImportIssueInvalidBlock,
			))
		}
	case "child_page":
		// A sub-page inside a toggle or column is a sub-page of the page
		// that holds the block, with its block inside that block.
		if (parentKind != "page" && parentKind != "row" && (blueprint || parentKind != "block")) || child.ID == "" {
			return nil, nil, nil, errors.New(errorCodeNotionImportProviderFailed)
		}
		sources = append(sources, stagedNotionSource{
			SourceID: child.ID, Kind: "page", ParentSourceID: parentSourceID,
			ParentKind: parentKind, Position: position, Title: child.ChildPage.Title,
			DiscoveryStatus: notionImportPageDiscoveryPending,
		})
	case "child_database":
		if parentKind != "page" && parentKind != "row" && (blueprint || parentKind != "block") {
			return nil, nil, nil, errors.New(errorCodeNotionImportProviderFailed)
		}
		sources = append(sources, stagedNotionSource{
			SourceID: child.ID, Kind: "database", ParentSourceID: parentSourceID,
			ParentKind: parentKind, Position: position, Title: child.ChildDatabase.Title,
		})
		databaseIDs = append(databaseIDs, child.ID)
	case graphBlockTypeFile, graphBlockTypePDF, graphBlockTypeImage, graphBlockTypeVideo, graphBlockTypeAudio:
		return stageNotionFileChild(app, parentSourceID, parentKind, blueprint, position, child)
	default:
		// An embed of a file Notion keeps comes over as that file, copied
		// before Notion's address expires, in an embed (2026-11-06).
		if notionImportFileKind(child) != "" {
			return stageNotionFileChild(app, parentSourceID, parentKind, blueprint, position, child)
		}
		converted, issues, err := stageNotionConvertedBlock(parentSourceID, parentKind, blueprint, position, child)
		if err != nil {
			return nil, nil, nil, err
		}
		if converted != nil {
			sources = append(sources, *converted)
			iconSources, iconFiles, err := notionImportBlockIconFileSources(app, child)
			if err != nil {
				return nil, nil, nil, err
			}
			sources, fileIDs = append(sources, iconSources...), append(fileIDs, iconFiles...)
		}
		sources = append(sources, issues...)
	}
	return sources, databaseIDs, fileIDs, nil
}

// stageNotionFileChild stages a block whose file the import copies from
// Notion or keeps as an external link.
func stageNotionFileChild(
	app core.App, parentSourceID, parentKind string, blueprint bool, position int, child notionProviderObject,
) ([]stagedNotionSource, []string, []string, error) {
	source, err := prepareNotionFileSource(app, parentSourceID, parentKind, blueprint, position, child)
	if err != nil {
		return nil, nil, nil, err
	}
	if source.FileURLCipher != "" {
		return []stagedNotionSource{source}, nil, []string{child.ID}, nil
	}
	return []stagedNotionSource{source}, nil, nil, nil
}

// notionImportBlockTypeTaken reports whether the import takes a Notion block
// type: the types the switch above stages itself, and those it converts.
func notionImportBlockTypeTaken(blockType string) bool {
	switch blockType {
	case "template", graphBlockTypeTable, graphBlockTypeTableRow, graphBlockTypeBookmark, graphBlockTypeLinkPreview,
		"child_page", "child_database", graphBlockTypeFile, graphBlockTypePDF, graphBlockTypeImage, graphBlockTypeVideo,
		graphBlockTypeAudio, graphBlockTypeSyncedBlock, graphBlockTypeLinkToPage:
		return true
	}
	_, convertible := notionImportRequestKeys[blockType]
	return convertible
}

func notionProviderTableRowContent(cells [][]notionProviderRichText) ([]byte, error) {
	if len(cells) > maxGraphTableWidth {
		return nil, errors.New(errorCodeNotionImportProviderFailed)
	}
	content := graphTableRowBlockContent{Cells: make([][]graphTextSpan, len(cells))}
	for cellIndex, parts := range cells {
		encoded, err := notionProviderRichTextSourceText(parts, false)
		if err != nil {
			return nil, err
		}
		plan, err := decodeNotionParagraphSourceText(encoded)
		if err != nil {
			return nil, err
		}
		spans := make([]graphTextSpan, 0, len(plan.Spans))
		for _, source := range plan.Spans {
			span, err := notionSourceGraphSpan(source)
			if err != nil {
				return nil, err
			}
			spans = append(spans, span)
		}
		content.Cells[cellIndex] = spans
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}
	if _, err := parseGraphTableRowBlockContent(encoded); err != nil {
		return nil, errors.New(errorCodeNotionImportProviderFailed)
	}
	return encoded, nil
}

func notionImportBlockDiscoveryStatus(block notionProviderObject) string {
	if notionProviderBlockMayHold(block) {
		return notionImportPageDiscoveryPending
	}
	return notionImportPageDiscoveryComplete
}

func notionImportRootKind(payload notionImportJobPayload, index int) string {
	if index >= 0 && index < len(payload.RootKinds) {
		return payload.RootKinds[index]
	}
	return "page"
}

func notionDatabaseRootPageSourceID(databaseID string) string {
	return "notion-database-root-page:" + databaseID
}
