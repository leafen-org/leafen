package app

import (
	"github.com/pocketbase/pocketbase/core"
)

// A database template is a page of its data source, as in Notion: a page
// record of the template's ID holds the template's body the way a page holds
// its body, while the template keeps its name, order, default and property
// values. The record follows its template: made with it, renamed and moved
// with it, and removed with its body when the template goes. It stays out of
// the rows, so views, queries and counts, which read rows, never see it, and
// out of what lists workspace pages, as a database page does.

const (
	databaseTemplatePagesVersion = 301
	databaseTemplatePagesName    = "native_database_template_pages"
	databaseTemplatePageField    = "database_template"
)

// isDatabaseTemplatePageRecord reports a page record that is a template's.
func isDatabaseTemplatePageRecord(page *core.Record) bool {
	return page != nil && page.GetBool(databaseTemplatePageField)
}
