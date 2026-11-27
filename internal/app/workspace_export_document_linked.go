package app

// Notion's Markdown & CSV export writes a linked database on a page as the
// view it is (seen 2026-11-13): the page links "[제목 없음](<page>/제목 없음
// <its block's ID>.csv)", its own name or none, and that CSV holds the rows
// and columns its view shows, with no "_all" file; its source's files stand
// where the source does, or nowhere when the source is outside the export.
// Its HTML export shows the view's table in the page, as before.

// placeLinkedView gives a linked database on a page its own CSV in a
// Markdown & CSV export, beside the page's other databases, and reports
// whether it did; its sources are then left to their own databases.
func (document *workspaceExportDocument) placeLinkedView(
	container *workspaceExportDocContainer, folder, extension, untitled string,
) bool {
	if extension != ".md" || container.Kind != databaseContainerKindLinked || container.BlockID == "" ||
		container.CSVPath != "" || document.linkedViewSource(container) == "" {
		return container.CSVPath != ""
	}
	title := document.linkedViewTitle(container)
	container.CSVPath = workspaceExportFileFolder(folder, title) +
		workspaceExportFileStem(title, container.BlockID, untitled) + ".csv"
	return true
}

// linkedViewTitle is the name a linked database gives itself, "" for none.
func (document *workspaceExportDocument) linkedViewTitle(container *workspaceExportDocContainer) string {
	block := document.BlockByID[container.BlockID]
	if block == nil {
		return ""
	}
	return mapString(block.Content, "linkedTitle")
}

// linkedViewSource is the source a linked database's view shows: the one its
// block's current view shows, or its first in the export.
func (document *workspaceExportDocument) linkedViewSource(container *workspaceExportDocContainer) string {
	current := ""
	if block := document.BlockByID[container.BlockID]; block != nil {
		current = mapString(block.Content, "viewId")
	}
	first := ""
	for _, dataSourceID := range container.DataSourceIDs {
		if document.DataSources[dataSourceID] == nil {
			continue
		}
		if first == "" {
			first = dataSourceID
		}
		if view := document.Views[current]; view != nil && view.DataSourceID == dataSourceID {
			return dataSourceID
		}
	}
	return first
}

// writeLinkedViews writes each linked database's own CSV: the rows and
// columns its view shows.
func (writer *workspaceExportDocumentWriter) writeLinkedViews() error {
	document := writer.document
	for _, id := range workspaceExportSortedKeys(document.Containers) {
		container := document.Containers[id]
		if container.CSVPath == "" {
			continue
		}
		dataSourceID := document.linkedViewSource(container)
		csv, err := document.databaseViewCSV(dataSourceID, document.containerView(container, dataSourceID), false, writer.files)
		if err != nil {
			return err
		}
		if err := writer.writeEntry(container.CSVPath, csv); err != nil {
			return err
		}
	}
	return nil
}
