package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// MCP's page tools write database pages through the Public API's database
// page writers, with the connection acting in the page's workspace and the
// values read from Notion MCP's SQLite form (mcp_database_page_values.go).

// mcpNotionIdentity is an MCP connection acting in one of its workspaces, as
// the Public API's database page writers take a connection.
// notionIdentityWritesMarkdown is an MCP tool's identity, whose values come
// from Markdown and are checked as Markdown's mentions are.
func notionIdentityWritesMarkdown(identity notionOAuthBearerIdentity) bool {
	return strings.HasPrefix(identity.ClientID, mcpNotionIdentityClientPrefix)
}

// mcpNotionIdentityClientPrefix marks the identity an MCP tool writes
// through the Public API's owners as.
const mcpNotionIdentityClientPrefix = "mcp:"

func mcpNotionIdentity(authority mcpAuthority, workspaceID string) notionOAuthBearerIdentity {
	return notionOAuthBearerIdentity{
		PrincipalID: authority.PrincipalID, ClientID: mcpNotionIdentityClientPrefix + authority.ClientID,
		GrantID: authority.ConnectionID, WorkspaceID: workspaceID,
		Scopes: append([]string{}, notionOAuthDefaultScopes...),
	}
}

// mcpPageVerification is update_verification's request: verified, with the
// days until it expires or none, or unverified.
type mcpPageVerification struct {
	Status     string
	ExpiryDays *int
	// ExpiresOn is the last day it stays verified (YYYY-MM-DD), as the
	// screen's "날짜 선택" picks it (mcpVerificationEnd).
	ExpiresOn string
}

var errMCPPageNotVerifiable = fmt.Errorf("%w: the page's database no longer has a Verification property: call again to verify the page itself", errMCPToolInvalid)

func (verification mcpPageVerification) given() bool {
	if verification.ExpiresOn != "" {
		_, err := time.Parse(time.DateOnly, verification.ExpiresOn)
		return verification.Status == "verified" && verification.ExpiryDays == nil && err == nil
	}
	return verification.Status == "verified" && (verification.ExpiryDays == nil || *verification.ExpiryDays >= 1) ||
		verification.Status == "unverified" && verification.ExpiryDays == nil
}

// mcpVerificationEnd is when a verification made now ends: days from now,
// as the screen's "7일 동안" and the rest; to the end of a day in the
// person's time zone (the server's without one), as its "날짜 선택"
// (PropertyComputedCells.tsx), which takes tomorrow or a later day; or never.
func mcpVerificationEnd(app core.App, principalID string, verification mcpPageVerification, now time.Time) (*time.Time, error) {
	switch {
	case verification.ExpiryDays != nil:
		end := now.UTC().AddDate(0, 0, *verification.ExpiryDays)
		return &end, nil
	case verification.ExpiresOn != "":
		location := personLocation(app, principalID)
		day, err := time.ParseInLocation(time.DateOnly, verification.ExpiresOn, location)
		if err != nil {
			return nil, mcpInvalid("verification_expires_on is a day, YYYY-MM-DD")
		}
		today := now.In(location)
		if !day.After(time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, location)) {
			return nil, mcpInvalid("verification_expires_on is tomorrow or a later day, as the screen's 날짜 선택 takes")
		}
		end := time.Date(day.Year(), day.Month(), day.Day(), 23, 59, 59, 0, location).UTC()
		return &end, nil
	}
	return nil, nil
}

// mcpVerificationProperty is the Public API's verification value for a
// request, on its data source's verification property: verified now, by the
// connection's person, until the days given, or unverified.
func mcpVerificationProperty(
	app core.App, current notionDatabaseRowPageAuthorization, verification mcpPageVerification, now time.Time,
) (map[string]json.RawMessage, error) {
	schema, err := loadStoredNotionDatabaseRowPageProperties(app, current.Authority)
	if err != nil {
		return nil, err
	}
	for _, property := range schema {
		if normalizeNotionDatabaseRowPagePropertyType(property.Type) != graphValueTypeVerification {
			continue
		}
		value := map[string]any{"state": verification.Status}
		if verification.Status == "verified" {
			date := map[string]any{"start": now.UTC().Format(time.RFC3339)}
			end, err := mcpVerificationEnd(app, current.Identity.PrincipalID, verification, now)
			if err != nil {
				return nil, err
			}
			if end != nil {
				date["end"] = end.Format(time.RFC3339)
			}
			value["date"] = date
		}
		encoded, err := json.Marshal(map[string]any{"verification": value})
		if err != nil {
			return nil, err
		}
		return map[string]json.RawMessage{property.ID: encoded}, nil
	}
	return nil, errMCPPageNotVerifiable
}

// updateMCPDatabasePage sets a database page's properties or verification,
// icon and cover as the Public API's page update does, gathering in said
// what reading their text said.
func updateMCPDatabasePage(
	ctx context.Context, app core.App, authority mcpAuthority, current pageAuthorization, input mcpUpdatePageInput, said *mcpMarkdownSaid,
) error {
	persistence, ok := app.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence)
	if !ok || persistence == nil {
		return errMCPPageUpdateFailed
	}
	identity := mcpNotionIdentity(authority, current.Workspace.WorkspaceID)
	// A template is a page of its data source too (notion_database_template_page_writes.go).
	if isDatabaseTemplatePageRecord(current.Page) {
		return updateMCPDatabaseTemplatePage(ctx, app, persistence, authority, identity, current.Page.Id, input, said)
	}
	row, err := authorizeNotionDatabaseRowPageCurrent(app, identity, current.Page.Id, permissionWorkspaceWrite)
	if err != nil {
		return mcpDatabasePageError(err)
	}
	presentation, _, err := mcpPagePresentation(app, current.Workspace.WorkspaceID, input.Icon, input.Cover)
	if err != nil {
		return mcpReasoned(err)
	}
	// Its icon and cover as the Public API's page update sets them; its
	// font, small text and width as the screen's "..." menu does, the
	// revision moved on when no icon moves it (database_row_page_presentation.go).
	present := func(txApp core.App) error {
		if err := applyNotionDatabaseRowPagePresentation(
			txApp, row.Authority.RowID, row.Authority.WorkspaceID, row.Authority.ContainerPageID,
			authority.PrincipalID, presentation,
		); err != nil {
			return err
		}
		if input.FullWidth == nil && input.SmallText == nil && input.Font == nil {
			return nil
		}
		if err := setDatabaseRowPageStyle(
			txApp, row.Authority.RowID, row.Authority.WorkspaceID, authority.PrincipalID, input.Font, input.SmallText, input.FullWidth,
		); err != nil || presentation.Icon != nil {
			return err
		}
		return touchDatabaseRowPageAuthority(txApp, row.Authority.RowID, time.Now().UTC().UnixMilli())
	}
	if len(input.Properties) == 0 && !input.Verification.given() {
		return mcpDatabasePageError(app.RunInTransaction(present))
	}
	now := time.Now()
	sent := mcpSentReferences{}
	return sent.pageError(writeNotionDatabaseRowPageValues(
		ctx, persistence, identity, row.Authority, "mcp-row-update:"+newDiagnosticReference(),
		func(txApp core.App, current notionDatabaseRowPageAuthorization) (notionRequestProperties, error) {
			if input.Verification.given() {
				values, err := mcpVerificationProperty(txApp, current, input.Verification, now)
				return sortedNotionRequestProperties(values), err
			}
			values, err := mcpDatabasePageProperties(txApp, authority, current, input.Properties, sent, said)
			return sortedNotionRequestProperties(values), err
		},
		present,
	))
}

// applyMCPPageTemplate applies one of a database page's templates, as
// Notion's MCP apply_template does: the template's body after the page's
// own and its values in the properties the page leaves empty. An icon or
// cover in the call goes with it.
func applyMCPPageTemplate(
	ctx context.Context, app core.App, authority mcpAuthority, pageID, mutationID string, input mcpUpdatePageInput,
	authorize func(core.App) (pageAuthorization, error),
) (*mcp.CallToolResult, mcpPageMutationOutput, error) {
	current, err := authorize(app)
	if err != nil {
		return nil, mcpPageMutationOutput{}, mcpPageMarkdownError(err)
	}
	if !isDatabaseRowPageRecord(current.Page) || isDatabaseTemplatePageRecord(current.Page) {
		return nil, mcpPageMutationOutput{}, fmt.Errorf("%w: templates apply only to database pages, not to a template", errMCPToolInvalid)
	}
	persistence, ok := app.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence)
	if !ok || persistence == nil {
		return nil, mcpPageMutationOutput{}, errMCPPageUpdateFailed
	}
	identity := mcpNotionIdentity(authority, current.Workspace.WorkspaceID)
	row, err := authorizeNotionDatabaseRowPageCurrent(app, identity, current.Page.Id, permissionWorkspaceWrite)
	if err != nil {
		return nil, mcpPageMutationOutput{}, mcpDatabasePageError(err)
	}
	// One of its data source's templates, as notion-create-pages takes it
	// (mcp_row_template_reference.go); never the page or one above it.
	listed, err := loadNotionDataSourceTemplates(app, row.Container)
	if err != nil {
		return nil, mcpPageMutationOutput{}, failedBecause(errMCPPageUpdateFailed, err)
	}
	templateID, err := mcpRowTemplateID(app, authority, row.Container.WorkspaceID, listed, input.TemplateID, current.Page.Id)
	if err != nil {
		return nil, mcpPageMutationOutput{}, err
	}
	if err := mcpDatabasePageError(applyNotionDatabaseRowPageTemplate(
		ctx, app, persistence, identity, current.Page.Id, notionDatabaseRowPageTemplateApplication{
			Template: notionPageTemplateRequest{Type: "template_id", TemplateID: templateID},
		},
	)); err != nil {
		return nil, mcpPageMutationOutput{}, err
	}
	if input.Icon != nil || input.Cover != nil {
		if err := updateMCPDatabasePage(ctx, app, authority, current, mcpUpdatePageInput{Icon: input.Icon, Cover: input.Cover}, nil); err != nil {
			return nil, mcpPageMutationOutput{}, err
		}
	}
	page, err := mcpPageResponse(app, pageID)
	if err != nil {
		return nil, mcpPageMutationOutput{}, failedBecause(errMCPPageUpdateFailed, err)
	}
	truncated := false
	if page.Content, truncated, err = mcpPageBodyMarkdownRead(app, mcpMarkdownReader(authority), current.Workspace.OwnerID, pageID); err != nil {
		return nil, mcpPageMutationOutput{}, failedBecause(errMCPPageUpdateFailed, err)
	}
	return nil, mcpPageMutationOutput{
		Page:      page,
		Receipt:   mutationReceiptResponse{MutationID: mutationID, Cursor: page.Cursor},
		Truncated: truncated,
	}, nil
}

// updateMCPDatabaseTemplatePage sets a template page's values or
// verification, icon, cover and style, as a database page's are set.
func updateMCPDatabaseTemplatePage(
	ctx context.Context, app core.App, persistence *pocketBaseYgoPersistence, authority mcpAuthority,
	identity notionOAuthBearerIdentity, templateID string, input mcpUpdatePageInput, said *mcpMarkdownSaid,
) error {
	presentation, _, err := mcpPagePresentation(app, identity.WorkspaceID, input.Icon, input.Cover)
	if err != nil {
		return mcpReasoned(err)
	}
	write := databaseTemplatePageWrite{
		Presentation: presentation, Font: input.Font, SmallText: input.SmallText, FullWidth: input.FullWidth,
	}
	sent, now := mcpSentReferences{}, time.Now()
	if len(input.Properties) > 0 || input.Verification.given() {
		write.Properties = func(txApp core.App, current notionDatabaseRowPageAuthorization) (notionRequestProperties, error) {
			if input.Verification.given() {
				values, err := mcpVerificationProperty(txApp, current, input.Verification, now)
				return sortedNotionRequestProperties(values), err
			}
			values, err := mcpDatabasePageProperties(txApp, authority, current, input.Properties, sent, said)
			return sortedNotionRequestProperties(values), err
		}
	}
	err = writeDatabaseTemplatePage(ctx, app, persistence, identity, templateID, write)
	if isDatabaseTemplateWriteRefusal(err) {
		return mcpInvalid("the template changed meanwhile or is gone: fetch it again and retry")
	}
	if isPermissionDenied(err) {
		return errMCPCantChange
	}
	return sent.pageError(err)
}

// notionMCPDataSourceTemplate is one of a data source's templates, in its
// order, as fetch lists them.
type notionMCPDataSourceTemplate struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"is_default"`
}

// loadMCPDataSourceTemplates reads a data source's live templates, as the
// Public API lists them, by their Leafen IDs.
func loadMCPDataSourceTemplates(app core.App, location mcpDataSourceLocation) ([]notionMCPDataSourceTemplate, error) {
	stored, err := loadNotionDataSourceTemplates(app, notionDataSourceParentAuthority{
		OwnerID: location.OwnerID, WorkspaceID: location.WorkspaceID,
		PageID: location.PageID, DataSource: location.ID,
	})
	if err != nil {
		return nil, err
	}
	templates := make([]notionMCPDataSourceTemplate, 0, len(stored))
	for _, template := range stored {
		templates = append(templates, notionMCPDataSourceTemplate{
			ID: template.ID, Name: template.Name, IsDefault: template.IsDefault,
		})
	}
	return templates, nil
}

// mcpCreatePagesDataSource is the data source a notion-create-pages parent
// names: its data source, or a database with one source of its own.
func mcpCreatePagesDataSource(app core.App, parent notionMCPCreatePagesParent) (string, error) {
	if raw := strings.TrimSpace(parent.DataSourceID); raw != "" {
		if id, ok := parseMCPDataSourceURL(raw); ok {
			return id, nil
		}
		if strings.Contains(raw, "://") || strings.ContainsAny(raw, "/?#") {
			return "", mcpInvalid("that is an address, not an ID: give the ID notion-fetch shows")
		}
		return raw, nil
	}
	databaseID, ok := resolveMCPDatabaseID(app, parent.DatabaseID)
	if !ok || !mcpDatabaseContainerExists(app, databaseID) {
		return "", errMCPPageMissing
	}
	sources, err := loadMCPDatabaseContainerShownOwnMembers(app, databaseID)
	if err != nil {
		return "", failedBecause(errMCPToolFailed, err)
	}
	if len(sources) != 1 {
		return "", fmt.Errorf(
			"%w: this database has %d data sources; name one with data_source_id", errMCPToolInvalid, len(sources),
		)
	}
	return sources[0], nil
}

// createMCPDatabasePages makes database pages in one data source, each with
// its values, icon, cover and body. Every page's values are checked before
// the first is made; the pages are then made one by one.
func createMCPDatabasePages(
	ctx context.Context, app core.App, authority mcpAuthority, dataSourceID string,
	definitions []notionMCPCreatePageDefinition,
) (notionMCPCreatePagesOutput, error) {
	persistence, ok := app.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence)
	if !ok || persistence == nil {
		return notionMCPCreatePagesOutput{}, errMCPPageUpdateFailed
	}
	location, err := loadMCPDataSourceLocation(app, dataSourceID)
	if err != nil {
		return notionMCPCreatePagesOutput{}, mcpRefusedUnlessStorage(errMCPPageMissing, err)
	}
	workspace, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{
		WorkspaceID: location.WorkspaceID,
	}, permissionWorkspaceWrite)
	if err != nil || workspace.WorkspaceID != location.WorkspaceID {
		return notionMCPCreatePagesOutput{}, mcpRefusedUnlessStorage(errMCPOutsideConnection, err)
	}
	identity := mcpNotionIdentity(authority, location.WorkspaceID)
	parent, err := authorizeNotionDataSourceItemParent(app, identity, location.ID)
	if err != nil {
		return notionMCPCreatePagesOutput{}, mcpDatabasePageError(err)
	}
	presentations := make([]notionPagePresentationRequest, len(definitions))
	templates := make([]notionPageTemplateRequest, len(definitions))
	listed, err := loadNotionDataSourceTemplates(app, notionDataSourceParentAuthority{
		OwnerID: location.OwnerID, WorkspaceID: location.WorkspaceID, PageID: location.PageID, DataSource: location.ID,
	})
	if err != nil {
		return notionMCPCreatePagesOutput{}, failedBecause(errMCPToolFailed, err)
	}
	origin, _ := mcpServerURL(authority.Resource, "")
	moved := make([]mcpCreatePageMoves, len(definitions))
	for index, definition := range definitions {
		// A body it refuses stops the call before any page is made, as
		// Notion's does, the pages it moves in found first (mcpCreateRowContent).
		if moved[index], err = mcpCreateRowContent(app, authority, location, definition, origin, len(definitions), index); err != nil {
			return notionMCPCreatePagesOutput{}, err
		}
		if presentations[index], _, err = mcpPagePresentation(
			app, location.WorkspaceID, definition.Icon, definition.Cover,
		); err != nil {
			return notionMCPCreatePagesOutput{}, mcpCreatePagesItemRefusal(mcpReasoned(err), len(definitions), index)
		}
		// One of the data source's templates (mcp_row_template_reference.go).
		if templateID, err := mcpRowTemplateID(app, authority, location.WorkspaceID, listed, definition.TemplateID, location.PageID); err != nil {
			return notionMCPCreatePagesOutput{}, mcpCreatePagesItemRefusal(err, len(definitions), index)
		} else if templateID != "" {
			if definition.Content != "" {
				return notionMCPCreatePagesOutput{}, fmt.Errorf(
					"%w: a page made from a template takes its body from the template; leave content out", errMCPToolInvalid,
				)
			}
			templates[index] = notionPageTemplateRequest{Type: "template_id", TemplateID: templateID}
		}
	}
	sent, said := mcpSentReferences{}, make([]mcpMarkdownSaid, len(definitions))
	if err := app.RunInTransaction(func(txApp core.App) error {
		for index, definition := range definitions {
			properties, err := mcpDatabasePageProperties(txApp, authority, parent, definition.Properties, sent, &said[index])
			if err != nil {
				return mcpCreatePagesItemRefusal(sent.pageError(err), len(definitions), index)
			}
			if _, _, err := planNotionDatabaseRowPageRequestValues(txApp, parent, sortedNotionRequestProperties(properties), true); err != nil {
				return mcpCreatePagesItemRefusal(sent.pageError(err), len(definitions), index)
			}
		}
		return nil
	}); err != nil {
		return notionMCPCreatePagesOutput{}, sent.pageError(err)
	}
	output := notionMCPCreatePagesOutput{Pages: make([]notionMCPCreatedPage, 0, len(definitions))}
	for index, definition := range definitions {
		rowID := newPageID()
		if err := createNotionDatabaseRowPage(ctx, persistence, notionDatabaseRowPageCreation{
			Parent: parent, RowID: rowID, MutationID: "mcp-row-create:" + newDiagnosticReference(),
			Template: templates[index], Presentation: presentations[index],
			Properties: func(txApp core.App, current notionDatabaseRowPageAuthorization) (notionRequestProperties, error) {
				values, err := mcpDatabasePageProperties(txApp, authority, current, definition.Properties, sent, nil)
				return sortedNotionRequestProperties(values), err
			},
		}); err != nil {
			return output, sent.pageError(err)
		}
		output.said(index, said[index].answer())
		if definition.Content != "" {
			warnings, err := writeMCPCreatedBody(ctx, app, rowID, definition, moved[index])
			if err != nil {
				return output, err
			}
			output.said(index, warnings)
		}
		output.Pages = append(output.Pages, notionMCPCreatedPage{ID: rowID, URL: mcpPageURL(authority, rowID)})
	}
	// They share the call's time, as Notion's (mcp_database_pages_created.go).
	return output, shareMCPCreatedRowsTime(app, output.Pages)
}

// mcpSentReferences keeps how an agent wrote each page or person a write
// names, by the Public API ID the planner reads it by, so what stops the
// write names it as the agent wrote it, as Notion's MCP does (seen
// 2026-11-10: "not-a-page", mcpInvalidReference).
type mcpSentReferences map[string]string

// note keeps how the agent wrote what the planner reads as id.
func (sent mcpSentReferences) note(id, written string) {
	if sent != nil {
		sent[id] = strings.TrimSpace(written)
	}
}

// as is id as the agent wrote it.
func (sent mcpSentReferences) as(id string) string {
	if written, ok := sent[id]; ok && written != "" {
		return written
	}
	return id
}

// mcpToolErrorKind is whether err is one of the kinds an MCP tool answers
// with as they are.
func mcpToolErrorKind(err error) bool {
	return err == errMCPToolInvalid || err == errMCPScopeDenied || err == errMCPPageMissing
}

// mcpDatabasePageError names what stopped a database page's write, with the
// planner's reason for a value it refused.
func mcpDatabasePageError(err error) error {
	return mcpSentReferences(nil).pageError(err)
}

// pageError is mcpDatabasePageError, naming a page or person as the agent
// wrote it.
func (sent mcpSentReferences) pageError(err error) error {
	requestError := &notionDatabaseRowPageRequestError{}
	typeError := &notionDatabaseRowPagePropertyTypeError{}
	missingUser := &notionDatabaseRowPageUserMissing{}
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errMCPToolInvalid), errors.Is(err, errMCPScopeDenied), errors.Is(err, errMCPPageMissing):
		// Said as it was made, not as the write passed it on.
		for own := err; own != nil; own = errors.Unwrap(own) {
			inner := errors.Unwrap(own)
			if mcpToolErrorKind(own) || mcpToolErrorKind(inner) && strings.HasPrefix(own.Error(), inner.Error()) {
				return own
			}
		}
		return err
	case errors.As(err, &requestError):
		message := requestError.message
		if requestError.missingPage != "" {
			message = strings.Replace(message, requestError.missingPage, sent.as(requestError.missingPage), 1)
		}
		return fmt.Errorf("%w: %s", errMCPToolInvalid, message)
	case errors.As(err, &typeError):
		return fmt.Errorf("%w: Leafen doesn't write %s properties", errMCPToolInvalid, typeError.Type)
	case errors.As(err, &missingUser):
		return mcpInvalid("The person value user://%s names no one: give a user or group of this workspace.", sent.as(missingUser.id))
	case errors.Is(err, errDatabaseRowPageNotFound):
		return errMCPPageMissing
	case isPermissionDenied(err):
		return errMCPCantChange
	}
	return mcpPageMarkdownError(err)
}
