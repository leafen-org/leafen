package app

import (
	"github.com/pocketbase/pocketbase/core"
)

// A readable export — Markdown & CSV, HTML, printable HTML and a page's own
// Markdown — names what its text mentions as the person exporting reads it
// now, as the app draws a mention: a page by its title now, not the one it
// was written with, and a person by "@" and their name now
// (person_mention_names.go). A page mention closed to them, or one nothing
// answers to, keeps its "사용 권한 없음"/"찾을 수 없음" words
// (workspaceExportRunsFromYText) and goes without its page's address.

type workspaceExportMentionNames struct {
	app         core.App
	reader      string
	workspaceID string
	language    string
	titles      map[string]string
	closed      map[string]bool
	people      map[string]string
	// failed is a lookup's error other than a refusal; the writer returns it.
	failed error
}

func newWorkspaceExportMentionNames(app core.App, job storedJob, language string) *workspaceExportMentionNames {
	return &workspaceExportMentionNames{app: app, reader: job.Owner, workspaceID: job.WorkspaceID, language: language,
		titles: map[string]string{}, closed: map[string]bool{}, people: map[string]string{}}
}

// The names a readable export gives its pages and databases are as
// its exporter reads them, as its text is (page_name_mentions.go): a page
// or database mention in a title or a database's name closed to them in
// words, "@찾을 수 없음", as a database page's title is
// (closedMentionsInTitle) — its file's and folder's names too.

// namesAsRead puts the document's page titles and database names as its
// exporter reads them, in words: an ordinary page's and a database's were
// read from their spans as they came (add); a full-page database's, and a
// listed sub-page's, are read here.
func (document *workspaceExportDocument) namesAsRead(app core.App) {
	titles := map[string]*string{}
	for id, page := range document.Pages {
		if page.Kind == pageKindDatabase || page.Listed {
			titles[id] = &page.Title
		}
	}
	pageTitlesAsReadBy(app, document.closes, titles, true)
	for _, page := range document.Pages {
		page.Title = closedMentionsInTitle(page.Title, document.Language)
	}
	for _, source := range document.DataSources {
		source.Title = closedMentionsInTitle(source.Title, document.Language)
	}
}

// namedRuns are runs with their page and person mentions named now, a page
// without a title by untitled (linkUntitled).
func (document *workspaceExportDocument) namedRuns(runs []workspaceExportTextRun, untitled string) []workspaceExportTextRun {
	if document.names == nil {
		return runs
	}
	var named []workspaceExportTextRun
	for index, run := range runs {
		switch {
		case run.Mention == "page" && run.PageID != "":
			if searchMentionClosed(document.closes(), run.Mention, run.PageID, "") {
				run.PageID = ""
			} else if title, named := document.mentionTitle(run.PageID); named {
				run.Text = workspaceExportTitle(title, untitled)
			}
		case run.Mention == "person" && run.UserID != "":
			run.Text = personMentionText(map[string]string{run.UserID: document.names.person(run.UserID)}, run.UserID, run.Text)
		default:
			continue
		}
		if named == nil {
			named = append([]workspaceExportTextRun(nil), runs...)
		}
		named[index] = run
	}
	if named == nil {
		return runs
	}
	return named
}

// mentionTitle is a mentioned page's title now, as written: the export's own
// page or database page, or one it leaves out that the exporter may open;
// named is false for one they may not.
func (document *workspaceExportDocument) mentionTitle(pageID string) (title string, named bool) {
	if page := document.Pages[pageID]; page != nil && !page.Trashed {
		if row := document.RowByID[pageID]; row != nil {
			return row.Title, true
		}
		return page.Title, true
	}
	if row := document.RowByID[pageID]; row != nil {
		return row.Title, true
	}
	return document.names.page(pageID)
}

func (names *workspaceExportMentionNames) page(pageID string) (string, bool) {
	if title, known := names.titles[pageID]; known || names.failed != nil {
		return title, known && !names.closed[pageID]
	}
	// A page an operation holds a while keeps its name (page_access_hold.go).
	authorization, err := pageAccessDespiteHold(authorizePage(names.app, names.reader, pageID, permissionWorkspaceRead))
	switch {
	case err == nil:
		title := pageTitleAsReadBy(names.app, readerCloser(names.app, names.reader), pageID, authorization.Page.GetString("title"))
		names.titles[pageID] = closedMentionsInTitle(title, names.language)
	case isPermissionDenied(err):
		names.titles[pageID], names.closed[pageID] = "", true
	default:
		names.failed = err
		return "", false
	}
	return names.titles[pageID], !names.closed[pageID]
}

// person is a mentioned person's name now, "" for one who isn't the
// workspace's or has none.
func (names *workspaceExportMentionNames) person(userID string) string {
	if name, known := names.people[userID]; known || names.failed != nil {
		return name
	}
	found, err := personMentionNames(names.app, names.workspaceID, []string{userID})
	if err != nil {
		names.failed = err
		return ""
	}
	names.people[userID] = found[userID]
	// One a copy or a move brought from another workspace reads by the name
	// it kept, for a reader of what keeps it (workspace_carried_people.go).
	if found[userID] == "" {
		carried, err := lookupWorkspaceCarriedPeople(names.app, names.reader, names.workspaceID, []string{userID})
		if err != nil {
			names.failed = err
			return ""
		}
		if len(carried) > 0 {
			names.people[userID] = carried[0].DisplayName
		}
	}
	return names.people[userID]
}

// mentionFailure is the error a mention's lookup met while the export was
// written, if any.
func (document *workspaceExportDocument) mentionFailure() error {
	if document.names == nil {
		return nil
	}
	return document.names.failed
}
