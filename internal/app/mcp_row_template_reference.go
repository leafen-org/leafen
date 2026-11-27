package app

import (
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// notion-create-pages' template_id and notion-update-page's apply_template
// name one of the data source's templates, as fetch lists them: by its ID,
// with or without dashes, or by its address, /p/<template> (mcpPageAddress),
// or its older one, its database's page with the template open
// (…/p/<database's page>?tp=<template>, mcpReferencePart). Any
// other page the connection reaches goes on as given, which the Public API
// makes the page's template, its whole body copied (notion_page_template_
// copy.go), as Notion's API does.
//
// But a page the database is on, or one above it, is refused, and so is the
// database itself (2026-11-24, in daily use: an agent making a page in a ledger
// "from its template" named the page holding the ledger — the template's
// address then, without its ?tp= — and the page took that page's body, a copy of
// the whole ledger with its 106 rows and its template, under itself). Notion's
// API does copy such a page (seen 2026-11-24: a row made in an inline database
// with the database's page as template_id held a copy of the database, the row
// itself among its rows); for an agent it is the template's address read as a
// page, so the refusal names the templates to choose from. So is another
// database's template, which an agent names for one of this one's
// (2026-11-24, an agent's real use: the Public API said only "template not
// found" for one in another workspace, and one in the same workspace was
// copied as a page): the refusal says, as the agent wrote it, whose template
// it is — the database by its title and its data source's address, when the
// connection reads it — and the templates to choose from. A page outside the
// database, as a template, keeps Notion's way; the Public API keeps it all.

// mcpRowTemplateID reads template_id for a page made in a data source whose
// database is on below (its page), or for the page below itself when it is
// changed: one of listed, as the Public API names it; any other page as
// given; none that below is or is under, nor the database, nor another
// database's template (mcpTemplateDatabaseNamed).
func mcpRowTemplateID(
	app core.App, authority mcpAuthority, workspaceID string, listed []notionDataSourceTemplate, raw, below string,
) (string, error) {
	templateID := mcpLocalID(app, raw)
	if templateID == "" {
		return "", nil
	}
	for _, template := range listed {
		if template.ID == templateID {
			return notionDataSourceTemplateID(workspaceID, template.ID), nil
		}
	}
	if whose, template := mcpTemplateDatabaseNamed(app, authority, templateID); template {
		return "", mcpInvalid("template_id %q isn't one of this data source's templates: it is %s. %s",
			strings.TrimSpace(raw), whose, mcpRowTemplatesToChoose(app, authority, listed))
	}
	held := ""
	if pageID, err := resolveNotionParentPageRecordID(app, templateID); err == nil {
		held = pageID
	} else if database, ok, _ := loadNotionMentionedDatabase(app, mcpRowTemplateDatabaseID(app, templateID)); ok {
		held = database.PageID
	}
	if held == "" {
		return templateID, nil
	}
	if above, err := pageAncestryIncludes(app, below, held); err != nil || !above {
		return templateID, err
	}
	return "", mcpInvalid("template_id names this database, the page it is on, a page above that or this page itself, " +
		"not one of its templates: the page would take a copy of the whole database, rows and all. " + mcpRowTemplatesToChoose(app, authority, listed))
}

// mcpRowTemplatesToChoose says which templates a template_id may name, each
// by its name as fetch lists it (mcpUntitledTemplateName).
func mcpRowTemplatesToChoose(app core.App, authority mcpAuthority, listed []notionDataSourceTemplate) string {
	names := make([]string, 0, len(listed))
	for _, template := range listed {
		name := template.Name
		if strings.TrimSpace(name) == "" {
			name = mcpUntitledTemplateName(app, authority.PrincipalID, template.ID)
		}
		names = append(names, fmt.Sprintf("%q (template_id %s)", name, template.ID))
	}
	if len(names) == 0 {
		return "This data source has no templates: leave template_id out."
	}
	return "Name one of the data source's templates, as notion-fetch lists them: " + strings.Join(names, ", ") + "."
}

// mcpTemplateDatabaseNamed tells a database template's ID — one not among
// the data source's live templates — and whose it is in words: deleted or
// not, of the database by its title and its data source's address when the
// connection reads the template, else of another database, naming nothing
// the connection doesn't read.
func mcpTemplateDatabaseNamed(app core.App, authority mcpAuthority, templateID string) (string, bool) {
	found := struct {
		SourceID string `db:"data_source_id"`
		Deleted  string `db:"deleted_at"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[template]].[[data_source_id]], COALESCE([[state]].[[deleted_at]], '') AS [[deleted_at]]
		FROM {{leafen_database_templates}} AS [[template]]
		LEFT JOIN {{leafen_database_toolbar_template_state}} AS [[state]] ON [[state]].[[template_id]] = [[template]].[[id]]
		WHERE [[template]].[[id]] = {:id}
	`).Bind(dbx.Params{"id": templateID}).One(&found); err != nil || found.SourceID == "" {
		return "", false
	}
	kind := "a template"
	if found.Deleted != "" {
		kind = "a deleted template"
	}
	sourceID, unnamed := found.SourceID, kind+" of another database"
	page, err := authorizeMCPPageRead(app, authority.PrincipalID, templateID)
	if err != nil || !mcpAuthorityReachesPage(app, authority, page.Page) {
		return unnamed, true
	}
	if workspace, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{WorkspaceID: page.Workspace.WorkspaceID}, permissionWorkspaceRead); err != nil ||
		workspace.WorkspaceID != page.Workspace.WorkspaceID {
		return unnamed, true
	}
	location, err := loadMCPDataSourceLocationToRead(app, sourceID)
	if err != nil {
		return unnamed, true
	}
	// One without a name as its fetch heads it (mcp_untitled_names.go).
	title := mcpNamedOr(mcpDatabaseTitleAsIndexed(app, location.ID, location.Title),
		mcpDataSourceShownTitle(app, authority.PrincipalID, location))
	return fmt.Sprintf("%s of the database %q (%s)", kind, title, mcpDataSourceURL(app, location.ID)), true
}

// mcpRowTemplateDatabaseID is the database a reference names, by its API ID
// or its own; "" for none.
func mcpRowTemplateDatabaseID(app core.App, reference string) string {
	if identity, err := resolveNotionObjectIdentity(app, notionObjectIdentityDatabase, reference); err == nil {
		return identity.InternalID
	}
	return reference
}
