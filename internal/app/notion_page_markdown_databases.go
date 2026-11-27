package app

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/pocketbase/pocketbase/core"
)

// A <database> tag without an address in Markdown written to a page makes a
// full-page database there: a sub-page holding a database with a Name title
// and a table view, titled as the tag. It is made before the body is
// written, and its tag then names it, so the body puts it where the tag
// stands. A full-page database whose tag gives it a new title takes it on
// its page and its first data source.

// notionMarkdownFullPageDatabaseLines lists, in order, the lines of Markdown
// whose tags make full-page databases.
func notionMarkdownFullPageDatabaseLines(lines []string) []int {
	found := []int{}
	fenced := false
	for index, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		tag, ok := parseNotionMarkdownTag(line)
		if ok && tag.Tag == "database" && tag.URL == "" && tag.DataSourceURL == "" && !strings.EqualFold(tag.Inline, "true") {
			found = append(found, index)
		}
	}
	return found
}

// createNotionMarkdownDatabase makes a full-page database under a page, its
// page block at the end of that page's body, and returns its page and first
// data source.
func (p *pocketBaseYgoPersistence) createNotionMarkdownDatabase(
	ctx context.Context,
	principalID string,
	parent pageAuthorization,
	title string,
	allowed func(core.App, pageAuthorization) bool,
) (string, string, error) {
	pageID, sourceID, viewID := newPageID(), newDatabaseEntityID(), newDatabaseEntityID()
	content, err := json.Marshal(map[string]string{"dataSourceId": sourceID, "viewId": viewID})
	if err != nil {
		return "", "", err
	}
	// A database without a title stays without one: each reader's screen
	// names it in their language. One of spaces alone has none; any other
	// keeps the spaces at its ends, as Notion's (seen 2026-11-13).
	pageTitle := title
	if strings.TrimSpace(title) == "" {
		pageTitle = ""
	}
	requestID := "markdown-database:" + newDiagnosticReference()
	_, err = p.applyContentGraphMutationContext(ctx, principalID, pageID, graphMutationInput{
		MutationID: "markdown-database-body:" + newDiagnosticReference(),
		Operations: []graphOperation{
			{Kind: graphOperationKindUpsertDataSource, DataSource: &graphDataSource{ID: sourceID, Title: title}},
			{Kind: graphOperationKindUpsertProperty, Property: &graphProperty{
				ID: core.GenerateDefaultRandomId(), DataSourceID: sourceID, Type: graphValueTypeTitle,
				Name: "Name", Position: "a0", Config: json.RawMessage(`{}`),
			}},
			{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
				ID: viewID, DataSourceID: sourceID, Type: graphViewTypeTable, Name: "",
				Position: "a0", Config: json.RawMessage(`{}`),
			}},
			{Kind: graphOperationKindUpsertBlock, Block: &graphBlock{
				ID: uuid.NewString(), Position: "a0", Type: graphBlockTypeInlineDatabase, Content: content,
			}},
		},
	}, contentGraphMutationHooks{
		Authorize: func(txApp core.App) (pageAuthorization, error) {
			created, status, err := admitPageCreationTransactionWithLock(txApp, principalID, pageCreationAdmissionInput{
				RequestID: requestID, WorkspaceID: parent.Workspace.WorkspaceID, ParentID: parent.Page.Id,
				Title: pageTitle, ContentInBody: true, PreparedPageID: pageID, Kind: pageKindDatabase,
			}, pageLockIgnored)
			if err != nil {
				return pageAuthorization{}, err
			}
			if status != pageCreationApplied && !created.Duplicate {
				return pageAuthorization{}, graphConflictError(
					errorCodeGraphPageCursorConflict, "The page changed before its database was made.",
				)
			}
			current, err := authorizePage(txApp, principalID, pageID, permissionWorkspaceWrite)
			if err != nil {
				return pageAuthorization{}, err
			}
			if !allowed(txApp, current) {
				return pageAuthorization{}, graphNotFoundError()
			}
			return current, nil
		},
	})
	return pageID, sourceID, err
}

// renameNotionMarkdownDatabaseSource gives a full-page database's first data
// source its new title.
func (p *pocketBaseYgoPersistence) renameNotionMarkdownDatabaseSource(
	ctx context.Context, principalID, pageID, sourceID, title string, spans []graphTextSpan,
) error {
	_, err := p.applyContentGraphMutationContext(ctx, principalID, pageID, graphMutationInput{
		MutationID: "markdown-database-title:" + newDiagnosticReference(),
		Operations: []graphOperation{{
			Kind: graphOperationKindUpsertDataSource, DataSource: notionMarkdownTitledSource(sourceID, title, spans),
		}},
	}, contentGraphMutationHooks{
		Authorize: func(txApp core.App) (pageAuthorization, error) {
			return authorizePageBody(txApp, principalID, pageID, permissionWorkspaceWrite)
		},
	})
	return err
}

// makeNotionMarkdownDatabases makes the full-page databases Markdown's tags
// without an address ask for, rewrites those tags to name them, and reads
// the page's current Markdown again into before.
func (p *pocketBaseYgoPersistence) makeNotionMarkdownDatabases(
	ctx context.Context,
	app core.App,
	request pageMarkdownReplacement,
	markdown string,
	titles []string,
	before *notionPageMarkdownProjection,
) (string, error) {
	// A tag with its title on a line of its own stands on one line by now.
	lines := strings.Split(notionMarkdownContainerForms(markdown), "\n")
	tagLines := notionMarkdownFullPageDatabaseLines(lines)
	if len(tagLines) != len(titles) {
		return "", pageMarkdownPlanError{message: "Markdown content could not be planned."}
	}
	allowed := request.Allows
	if allowed == nil {
		allowed = func(core.App, pageAuthorization) bool { return true }
	}
	parent := pageAuthorization{}
	if err := app.RunInTransaction(func(txApp core.App) error {
		var err error
		parent, err = request.Authorize(txApp)
		return err
	}); err != nil {
		return "", err
	}
	made := make([]string, len(titles))
	for index, title := range titles {
		pageID, _, err := p.createNotionMarkdownDatabase(ctx, request.PrincipalID, parent, title, allowed)
		if err != nil {
			return "", err
		}
		made[index] = pageID
	}
	if err := app.RunInTransaction(func(txApp core.App) error {
		current, err := request.Authorize(txApp)
		if err != nil {
			return err
		}
		*before, err = projectNotionPageMarkdown(
			txApp, current.Workspace.OwnerID, current.Page.Id, request.PublicPageID, request.Reader,
		)
		return err
	}); err != nil {
		return "", err
	}
	for index, pageID := range made {
		var reference *notionMarkdownReference
		for candidate := range before.References {
			if before.References[candidate].BlockID == pageID {
				reference = &before.References[candidate]
			}
		}
		if reference == nil {
			return "", pageMarkdownPlanError{message: "Markdown content could not be planned."}
		}
		line := lines[tagLines[index]]
		lines[tagLines[index]] = line[:len(line)-len(strings.TrimLeft(line, " \t"))] + reference.apiLine()
	}
	return strings.Join(lines, "\n"), nil
}
