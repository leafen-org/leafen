package app

import "github.com/pocketbase/pocketbase/core"

// notion-update-data-source changes a data source's title and schema, then
// its page layout, then turns its database, each saved on its own. What one
// of them would refuse is refused before any is saved: a page_layout that
// doesn't read, or an is_inline for a database at the workspace's top,
// refused the call after its title, statements or layout were saved
// (2026-11-26), so an agent told no had in fact changed them.

// errMCPStatementsWithLayout refuses a call that gives statements and a
// page_layout, as Notion's reference refuses it with a validation_error
// (tools-databases-reference, read 2026-11-26): properties and the layout are
// changed in separate calls. A title or is_inline may come with either.
var errMCPStatementsWithLayout = mcpInvalid("statements and page_layout go in separate calls, and this one changed nothing: " +
	"send the statements first, then page_layout in a call of its own, naming the properties as they are then")

var errMCPInlineWithoutDatabase = mcpInvalid("is_inline turns the database that holds this data source, and none holds it")

// mcpDataSourceUpdateFits refuses what the call's parts would refuse once
// another was saved; it changes nothing. A part refused for what changes
// meanwhile is still refused as it is saved.
func mcpDataSourceUpdateFits(
	app core.App, authority mcpAuthority, location mcpDataSourceLocation, dataSourceID string,
	input notionMCPUpdateDataSourceInput, statements []mcpDataSourceUpdateStatement,
) error {
	if input.PageLayout != nil && len(statements) > 0 {
		return errMCPStatementsWithLayout
	}
	if input.PageLayout != nil {
		if _, err := planMCPPageLayout(app, location, *input.PageLayout); err != nil {
			return mcpPageLayoutError(err)
		}
	}
	if input.IsInline != nil {
		return mcpDataSourceTurnRefusal(app, authority, location, dataSourceID, *input.IsInline)
	}
	return nil
}
