package app

// workspaceExportFilePlacer gives each included file one archive path, next
// to the first page that shows it. A picture the export draws itself (a
// gallery cover, workspace_export_document_html_covers.go) is placed so too,
// its contents its own. A file of no page's folder stands in "files/" in the
// export's top folder, root (workspaceExportSectionFolder).
type workspaceExportFilePlacer struct {
	root     string
	byRecord map[string]string
	used     map[string]bool
	order    []string
	drawn    map[string]func() ([]byte, error)
}

func newWorkspaceExportFilePlacer() *workspaceExportFilePlacer {
	return &workspaceExportFilePlacer{byRecord: map[string]string{}, used: map[string]bool{}, drawn: map[string]func() ([]byte, error){}}
}

// placeDrawn places a picture the export draws, named key among its files.
func (placer *workspaceExportFilePlacer) placeDrawn(key, name string, contents func() ([]byte, error), folder string) string {
	if _, placed := placer.byRecord[key]; !placed {
		placer.drawn[key] = contents
	}
	return placer.place(workspaceExportFileSnapshot{RecordID: key, Name: name}, folder)
}

func (placer *workspaceExportFilePlacer) place(file workspaceExportFileSnapshot, folder string) string {
	if path, exists := placer.byRecord[file.RecordID]; exists {
		return path
	}
	if folder == "" || folder == placer.root {
		folder = placer.root + "files/"
	}
	path := folder + workspaceExportFileName(file.Name)
	if placer.used[path] {
		path = folder + file.RecordID + "-" + workspaceExportFileName(file.Name)
	}
	placer.used[path] = true
	placer.byRecord[file.RecordID] = path
	placer.order = append(placer.order, file.RecordID)
	return path
}
