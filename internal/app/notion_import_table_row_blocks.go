package app

// Notion's public API takes blocks appended under a simple table's row and
// then calls that row has_children true, while nothing shows them: not the
// page, not its v2 Markdown, not Notion's own Markdown & CSV or HTML export
// (seen 2026-11-21 on a synthetic page, a header row and a body row each
// holding blocks). Only turning the table into a database makes them the row
// page's body, a header row's dropped; Notion's editor cannot put a block
// under a row. A Leafen table row holds no blocks. So a move brings
// the table as Notion shows and exports it and reads nothing under its rows,
// and names in its report each row that holds blocks, so what an agent wrote
// there is not lost unsaid: the scan lists it before the run, and the run's
// report says where.

// notionImportTableRowBlocks stages, for a table row Notion says holds
// blocks, the report's line that they stayed in Notion.
func notionImportTableRowBlocks(
	parentSourceID, parentKind string, position int, row notionProviderObject,
) []stagedNotionSource {
	if row.Type != graphBlockTypeTableRow || !row.HasChildren {
		return nil
	}
	return []stagedNotionSource{notionImportIssueSource(
		parentSourceID, parentKind, position, row, notionImportIssueTableRowBlocksOmitted,
	)}
}

// notionMigrationBlockNotes notes what the scan sees of a block that the run
// brings other than as it is: an SVG file cleaned, a table row's blocks.
func notionMigrationBlockNotes(change *notionMigrationScanChange, pageID string, block notionProviderObject) {
	notionMigrationSVGIssue(change, pageID, block)
	if block.Type == graphBlockTypeTableRow && block.HasChildren {
		change.issues = append(change.issues, notionMigrationIssue{
			Code: notionImportIssueTableRowBlocksOmitted, SourceID: pageID, Detail: block.Type,
		})
	}
}

func init() {
	notionMigrationReportBlockReasons[notionImportIssueTableRowBlocksOmitted] = true
	ko, en := notionMigrationReportText[accountLanguageKo], notionMigrationReportText[accountLanguageEn]
	ko["reason:"+notionImportIssueTableRowBlocksOmitted] = "행 아래에 Notion이 보여 주지 않는 블록이 있어 그 블록은 Notion에 두었습니다"
	en["reason:"+notionImportIssueTableRowBlocksOmitted] = "held blocks Notion doesn’t show, left in Notion"
	ko["issue:"+notionImportIssueTableRowBlocksOmitted] = "Notion이 보여 주지 않는 블록을 표 행 아래에 품은 페이지 %d개 — 그 블록은 옮기지 않습니다"
	en["issue:"+notionImportIssueTableRowBlocksOmitted] = "%d pages with table rows holding blocks Notion doesn’t show — those blocks don’t move"
}
