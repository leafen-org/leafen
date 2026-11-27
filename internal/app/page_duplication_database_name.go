package app

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
)

// A full-page database copied as a page is named as its page's copy is, its
// name's formatting kept on the words the copy keeps, as Notion's copy does
// (seen 2026-11-09: "Name (1)", " (1)" plain, the database's and its
// source's name alike): the one source its page names takes the copy's name
// (full_page_database_name.go), and a database its page names keeps its
// formatting on its block (database_page_name_spans.go).

// pageDuplicationRoot is a full-page database page being copied: its name,
// and the source its top database names (none for one its page names).
type pageDuplicationRoot struct {
	title, namingSource string
	ok                  bool
}

func loadPageDuplicationRoot(app core.App, job storedPageDuplicationJob, sourcePageID string) pageDuplicationRoot {
	if sourcePageID != job.SourceRootPageID {
		return pageDuplicationRoot{}
	}
	page, err := app.FindRecordById(pagesCollection, sourcePageID)
	if err != nil || !isFullPageDatabase(page) {
		return pageDuplicationRoot{}
	}
	top, found, err := loadDatabasePageBlock(app, job.sourceOwner(), sourcePageID)
	if err != nil || !found {
		return pageDuplicationRoot{}
	}
	return pageDuplicationRoot{title: page.GetString("title"), namingSource: databaseNamingSource([]byte(top.Content)), ok: true}
}

// pageDuplicationSourceName is the name and formatting a copied source
// takes: the page copy's name for the one source a full-page database's page
// names, its own otherwise.
func pageDuplicationSourceName(
	job storedPageDuplicationJob, root pageDuplicationRoot, sourceID, title string, spans []graphTextSpan,
) (string, []graphTextSpan) {
	if !root.ok || sourceID != root.namingSource || title != root.title || job.Title == title {
		return title, spans
	}
	return job.Title, copiedNameSpans(spans, title, job.Title)
}

// nameDuplicatedDatabaseBlock gives the copy of the top database of a page
// being copied, when its page names it, its copy's name's formatting.
func nameDuplicatedDatabaseBlock(app core.App, job storedPageDuplicationJob, sourcePageID string, blocks []pageDuplicationSourceBlock) {
	root := loadPageDuplicationRoot(app, job, sourcePageID)
	if !root.ok || root.namingSource != "" || job.Title == root.title {
		return
	}
	top, found, err := loadDatabasePageBlock(app, job.sourceOwner(), sourcePageID)
	if err != nil || !found {
		return
	}
	for index := range blocks {
		if blocks[index].ID != top.ID {
			continue
		}
		content := map[string]any{}
		if json.Unmarshal([]byte(blocks[index].Content), &content) != nil || content["pageTitleSpans"] == nil {
			return
		}
		spans := databasePageNameSpans([]byte(blocks[index].Content), root.title)
		delete(content, "pageTitleSpans")
		if kept := copiedNameSpans(spans, root.title, job.Title); len(kept) > 0 {
			content["pageTitleSpans"] = kept
		}
		if encoded, err := json.Marshal(content); err == nil {
			blocks[index].Content = string(encoded)
		}
		return
	}
}
