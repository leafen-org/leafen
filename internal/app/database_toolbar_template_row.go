package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

type databaseToolbarTemplateRowProperty struct {
	ID   string `db:"id"`
	Type string `db:"type"`
}

func (p *pocketBaseYgoPersistence) createDatabaseToolbarTemplateRow(event *core.RequestEvent) error {
	user, ok, err := databaseToolbarConfigActor(event)
	if !ok {
		return err
	}
	pageID, dataSourceID, _ := databaseToolbarConfigIdentity(event)
	input := databaseToolbarTemplateRowCreateInput{}
	if err := event.BindBody(&input); err != nil {
		return writeDatabaseToolbarConfigError(event, bodyRefusal(errDatabaseToolbarConfigInvalid, "page from a template", err))
	}
	if pageID == "" || dataSourceID == "" {
		return writeDatabaseToolbarConfigError(event, databaseToolbarRefused("the page and data source IDs in the path are empty"))
	}
	if err := validateDatabaseToolbarTemplateRowInput(&input); err != nil {
		return writeDatabaseToolbarConfigError(event, err)
	}
	response, admissionErr := p.admitDatabaseToolbarTemplateRow(
		event.Request.Context(), user.Id, pageID, dataSourceID, input,
	)
	if admissionErr != nil {
		var graphErr *graphRequestError
		if errors.As(admissionErr, &graphErr) {
			return writeGraphError(event, admissionErr, "template-row", errorCodeDatabaseToolbarConfigFailed,
				"Couldn't create the database row from the template.")
		}
		return writeDatabaseToolbarConfigError(event, admissionErr)
	}
	status := http.StatusCreated
	if response.Duplicate {
		status = http.StatusOK
	}
	return event.JSON(status, response)
}

func validateDatabaseToolbarTemplateRowInput(input *databaseToolbarTemplateRowCreateInput) error {
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.TemplateID = strings.TrimSpace(input.TemplateID)
	input.TimeZone = strings.TrimSpace(input.TimeZone)
	position, err := normalizeGraphPosition(input.Position)
	switch {
	case input.RequestID == "" || utf8.RuneCountInString(input.RequestID) > 255:
		return databaseToolbarRefused("requestId is a new id of yours, 1–255 characters, so the same page isn't made twice")
	case input.TemplateID == "" || len(input.TemplateID) > maxGraphIdentifierLength:
		return databaseToolbarRefused("templateId names the template, 1–%d characters", maxGraphIdentifierLength)
	case input.ExpectedTemplateRevision < 1:
		return databaseToolbarRefused("expectedTemplateRevision is the template's templateAuthorityRevision as the toolbar-config read lists it, 1 or more")
	case err != nil:
		return databaseToolbarRefused("%s", firstNonempty(refusalReason(err), "position is the new row's place among the rows"))
	case !validDatabaseTemplateTimeZone(input.TimeZone):
		return databaseToolbarRefused("timeZone is left out, or an IANA time zone such as Asia/Seoul, not %q", input.TimeZone)
	}
	input.Position = position
	return nil
}

func databaseToolbarTemplateRowCreateIdentity(
	principalID, pageID, dataSourceID, requestID string,
) string {
	digest := sha256.Sum256([]byte(
		"databaseToolbarTemplateRowCreate\x00" + principalID + "\x00" + pageID + "\x00" +
			dataSourceID + "\x00" + requestID,
	))
	return "toolbar-template-row-" + hex.EncodeToString(digest[:16])
}

func databaseToolbarTemplateRowID(principalID, pageID, dataSourceID, requestID string) string {
	return derivedPageID("database-toolbar-template-row", principalID, pageID, dataSourceID, requestID)
}

func databaseToolbarTemplateRowRequestHash(
	pageID, dataSourceID string,
	input databaseToolbarTemplateRowCreateInput,
) (string, error) {
	encoded, err := json.Marshal(struct {
		PageID       string                                `json:"pageId"`
		DataSourceID string                                `json:"dataSourceId"`
		Input        databaseToolbarTemplateRowCreateInput `json:"input"`
	}{PageID: pageID, DataSourceID: dataSourceID, Input: input})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func (p *pocketBaseYgoPersistence) admitDatabaseToolbarTemplateRow(
	ctx context.Context,
	principalID, pageID, dataSourceID string,
	input databaseToolbarTemplateRowCreateInput,
) (databaseToolbarTemplateRowCreateResponse, error) {
	response := databaseToolbarTemplateRowCreateResponse{}
	if err := validateDatabaseToolbarTemplateRowInput(&input); err != nil {
		return response, err
	}
	// The template is read from the database the person reads; the page it
	// makes is theirs to add as any new row is: an editor's of the
	// database's page.
	authority, err := authorizeDatabaseToolbarConfig(
		p.app, principalID, pageID, dataSourceID, permissionWorkspaceRead,
	)
	if err != nil {
		return response, err
	}
	requestHash, err := databaseToolbarTemplateRowRequestHash(pageID, dataSourceID, input)
	if err != nil {
		return response, err
	}
	rowID := databaseToolbarTemplateRowID(principalID, pageID, dataSourceID, input.RequestID)
	var admittedTemplate storedDatabaseToolbarTemplate
	// One use of the template gives its values and its body their value.
	var use databaseTemplateUse
	receipt, err := p.applyContentGraphMutationContext(
		ctx, principalID, pageID,
		graphMutationInput{MutationID: databaseToolbarTemplateRowCreateIdentity(
			principalID, pageID, dataSourceID, input.RequestID,
		)},
		contentGraphMutationHooks{
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				current, authorizeErr := authorizeDatabaseToolbarConfig(
					txApp, principalID, pageID, dataSourceID, permissionWorkspaceRead,
				)
				if authorizeErr != nil || current != authority {
					return pageAuthorization{}, refusedUnlessStorage(errDatabaseToolbarConfigNotFound, authorizeErr)
				}
				authorization, authorizeErr := authorizePageBody(txApp, principalID, pageID, permissionWorkspaceWrite)
				if isPermissionDenied(authorizeErr) {
					return pageAuthorization{}, errDatabaseToolbarConfigNotFound
				}
				return authorization, authorizeErr
			},
			ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
				return requestHash, nil
			},
			Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				template, prepareErr := databaseToolbarTemplateForMutation(txApp, authority, input.TemplateID)
				if prepareErr != nil {
					return contentGraphMutationPreparation{}, prepareErr
				}
				if template.DeletedAt != "" {
					return contentGraphMutationPreparation{}, refuse(errDatabaseToolbarConfigRevisionChanged, "templateId names a template in the trash: restore it first")
				}
				if template.AuthorityRevision != input.ExpectedTemplateRevision {
					return contentGraphMutationPreparation{}, errDatabaseToolbarConfigRevisionChanged
				}
				if use, prepareErr = newDatabaseTemplateUse(txApp, principalID, input.TimeZone); prepareErr != nil {
					return contentGraphMutationPreparation{}, refusedUnlessStorage(errDatabaseToolbarConfigInvalid, prepareErr)
				}
				values, prepareErr := databaseToolbarTemplateRowValues(txApp, authority, template, use)
				if prepareErr != nil {
					return contentGraphMutationPreparation{}, prepareErr
				}
				admittedTemplate = template
				return contentGraphMutationPreparation{Operations: []graphOperation{{
					Kind: graphOperationKindUpsertRow,
					Row: &graphDatabaseRow{
						ID: rowID, DataSourceID: dataSourceID, Position: input.Position, Values: values,
					},
				}}}, nil
			},
			// Its steps give the new row's own page its look, which a read of
			// the row takes in, and its body, that page's own: its change says
			// the row (StepsKeepGraph).
			StepsKeepGraph: true,
			AfterApply: func(txApp core.App, authorization pageAuthorization, _ *crdt.Doc) error {
				if err := applyDatabaseToolbarTemplateRowPresentation(txApp, admittedTemplate, rowID); err != nil {
					return err
				}
				// The app's template takes each synced block original as a copy of
				// it, as Notion's app's (seen 2026-11-14).
				return applyDatabaseToolbarTemplateRowBody(
					txApp, admittedTemplate, rowID, authorization.Workspace.WorkspaceID, use, rowPageBodySyncedAsCopies,
				)
			},
		},
	)
	if err != nil {
		return response, err
	}
	response, err = loadDatabaseToolbarTemplateRowResponse(
		p.app, principalID, rowID, input.RequestID, receipt.Duplicate,
	)
	return response, err
}

// databaseToolbarTemplateRowIconType is the kind of a template icon that
// names no workspace emoji (databaseTemplateRowIcon), read by its form.
func databaseToolbarTemplateRowIconType(icon string) string {
	if icon == "" {
		return "none"
	}
	clean := strings.ToLower(strings.TrimSpace(icon))
	for _, prefix := range []string{"http://", "https://", "data:image/", "blob:", "/"} {
		if strings.HasPrefix(clean, prefix) {
			return "image"
		}
	}
	// A built-in icon's "name:color", as Notion's task tracker's ✅ (seen 2026-11-06).
	if validNativeIconValue(strings.TrimSpace(icon)) {
		return nativeIconType
	}
	return "emoji"
}

// applyDatabaseToolbarTemplateRowPresentation gives a page made from a
// template, or one it is applied to, the template page's look, as Notion's
// (seen 2026-11-13: a page made from a template, and an empty page it is
// applied to, take its cover, font, small text, full width and table of
// contents): the template's icon, its cover where the page has none, and its
// style. The icon's and cover's files are the page's too.
func applyDatabaseToolbarTemplateRowPresentation(
	app core.App,
	template storedDatabaseToolbarTemplate,
	rowID string,
) error {
	if utf8.RuneCountInString(template.Icon) > maxPageIconRunes {
		return errDatabaseToolbarConfigConflict
	}
	// A new row has no icon but what its database gives it, which one with none leaves.
	if template.Icon != "" {
		icon, iconType := databaseTemplateRowIcon(template.Icon)
		if len(iconType) > maxPageIconType {
			return errDatabaseToolbarConfigConflict
		}
		found, err := setRowPageRecordIcon(app, rowID, "", "", "", icon, iconType)
		if err != nil {
			return err
		}
		if !found {
			return errDatabaseToolbarConfigConflict
		}
	}
	look := struct {
		Cover                  string `db:"cover"`
		CoverPosition          int    `db:"cover_position"`
		Font                   string `db:"font"`
		SmallText              bool   `db:"small_text"`
		FullWidth              bool   `db:"full_width"`
		TableOfContentsDisplay string `db:"table_of_contents_display"`
	}{}
	err := app.DB().NewQuery(`SELECT [[cover]], [[cover_position]], [[font]], [[small_text]], [[full_width]],
		[[table_of_contents_display]] FROM {{leafen_pages}} WHERE [[id]] = {:id} AND [[database_template]]`).
		Bind(dbx.Params{"id": template.ID}).One(&look)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	found, err := updateRowPageRecord(app, `
		[[cover_position]] = CASE WHEN [[cover]] = '' THEN {:coverPosition} ELSE [[cover_position]] END,
		[[cover]] = CASE WHEN [[cover]] = '' THEN {:cover} ELSE [[cover]] END,
		[[font]] = {:font}, [[small_text]] = {:smallText}, [[full_width]] = {:fullWidth},
		[[table_of_contents_display]] = {:tableOfContents}`, "", dbx.Params{
		"rowId": rowID, "workspaceId": "", "pageId": "", "cover": look.Cover, "coverPosition": look.CoverPosition,
		"font": look.Font, "smallText": look.SmallText, "fullWidth": look.FullWidth,
		"tableOfContents": look.TableOfContentsDisplay,
	})
	if err != nil {
		return err
	}
	if !found {
		return errDatabaseToolbarConfigConflict
	}
	// The icon's and cover's files are the row's too: its icon's under its
	// own reference, its cover's as a page's.
	return replaceProductFileRowPageLookReferences(app, rowID)
}

func databaseToolbarTemplateRowValues(
	app core.App,
	authority databaseToolbarConfigAuthority,
	template storedDatabaseToolbarTemplate,
	use databaseTemplateUse,
) (map[string]graphRowValue, error) {
	properties := []databaseToolbarTemplateRowProperty{}
	if err := app.DB().NewQuery(`SELECT [[id]], [[type]] FROM {{leafen_database_properties}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[data_source_id]]={:dataSourceId}
		ORDER BY [[position]], [[id]]`).Bind(dbx.Params{
		"owner": authority.OwnerID, "pageId": authority.PageID, "dataSourceId": authority.DataSourceID,
	}).All(&properties); err != nil {
		return nil, err
	}
	seeded := map[string]json.RawMessage{}
	if json.Unmarshal([]byte(template.Properties), &seeded) != nil {
		return nil, errDatabaseToolbarConfigConflict
	}
	seededSpans := map[string][]graphTextSpan{}
	if template.PropertySpans != "" && json.Unmarshal([]byte(template.PropertySpans), &seededSpans) != nil {
		return nil, errDatabaseToolbarConfigConflict
	}
	values := make(map[string]graphRowValue)
	foundTitle := false
	for _, property := range properties {
		if property.Type == graphValueTypeTitle {
			title, _ := json.Marshal(template.Title)
			values[property.ID] = graphRowValue{Type: property.Type, Value: title}
			foundTitle = true
			continue
		}
		raw, exists := seeded[property.ID]
		if !exists || !databaseToolbarTemplateWritableProperty(property.Type) {
			continue
		}
		if spans := seededSpans[property.ID]; property.Type == graphValueTypeRichText && len(spans) > 0 {
			value, resolved := databaseTemplateUsedText(use, spans)
			values[property.ID] = graphRowValue{Type: property.Type, Value: value, Spans: resolved}
			continue
		}
		values[property.ID] = graphRowValue{Type: property.Type, Value: use.resolveValue(property.Type, raw)}
	}
	if !foundTitle {
		return nil, errDatabaseToolbarConfigConflict
	}
	// The template sets only what its user may set (database_property_access.go).
	if use.ActorID != "" {
		if err := withoutUneditableValues(app, use.ActorID, authority.DataSourceID, map[string]map[string]graphRowValue{"": values}); err != nil {
			return nil, err
		}
	}
	return values, nil
}

func databaseToolbarTemplateWritableProperty(valueType string) bool {
	switch valueType {
	case graphValueTypeCreatedTime, graphValueTypeLastEditedTime, graphValueTypeCreatedBy,
		graphValueTypeLastEditedBy, graphValueTypeRollup, graphValueTypeFormula,
		graphValueTypeUniqueID, graphValueTypeButton, graphValueTypeLocation,
		graphValueTypeLastVisitedTime:
		return false
	default:
		return true
	}
}

// applyDatabaseToolbarTemplateRowBody gives a new database page its
// template's body: the template page's blocks under new IDs, with their text
// and its template mentions given their value for this use, its synced block
// originals as synced says. A body larger than a database page takes is not
// applied.
func applyDatabaseToolbarTemplateRowBody(
	app core.App,
	template storedDatabaseToolbarTemplate,
	rowID, workspaceID string,
	use databaseTemplateUse,
	synced rowPageBodySynced,
) error {
	var size int
	if err := app.DB().NewQuery(`
		SELECT COALESCE((SELECT SUM(length([[content]])) FROM {{leafen_blocks}} WHERE [[page_id]] = {:templateId}), 0)
			+ COALESCE((SELECT length([[state]]) FROM {{leafen_ygo_document_states}} WHERE [[room]] = {:templateId}), 0)
	`).Bind(dbx.Params{"templateId": template.ID}).Row(&size); err != nil {
		return err
	}
	if size > databaseRowPageMaxBodyBytes {
		return databaseToolbarStateRefused("the template's body is %d bytes, over the %d a row's page keeps", size, databaseRowPageMaxBodyBytes)
	}
	if err := duplicateDatabaseTemplateBodyText(app, template.ID, rowID, workspaceID, use.ActorID, use.resolve, synced); err != nil {
		return err
	}
	if made, err := rowPageHasBlocks(app, rowID); err != nil || made {
		return err
	}
	return writeNewRowPageBody(app, rowID, use.ActorID, databaseTemplateEmptyLine(), time.Now().UTC().UnixMilli())
}

// databaseTemplateEmptyLine is the body an empty template gives a page: one
// empty line, as Notion's does (seen 2026-11-02: a page made from a template
// with nothing in it has one empty paragraph), so the page isn't empty and
// asks for no template again.
func databaseTemplateEmptyLine() []newRowPageBlock {
	return []newRowPageBlock{{
		ID: uuid.NewString(), Position: "p0000000000", Type: graphBlockTypeParagraph, Content: map[string]any{},
	}}
}

func rowPageHasBlocks(app core.App, rowID string) (bool, error) {
	var count int
	err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_blocks}} WHERE [[page_id]]={:rowId}`).
		Bind(dbx.Params{"rowId": rowID}).Row(&count)
	return count > 0, err
}

func loadDatabaseToolbarTemplateRowResponse(
	app core.App,
	principalID, rowID, requestID string,
	duplicate bool,
) (databaseToolbarTemplateRowCreateResponse, error) {
	response := databaseToolbarTemplateRowCreateResponse{}
	authority, err := authorizeDatabaseRowPage(app, principalID, rowID, permissionWorkspaceRead)
	if err != nil {
		return response, err
	}
	values, err := loadDatabaseRowPageGraphValues(app, authority)
	if err != nil {
		return response, err
	}
	// Its body's own databases may still be copied in (database_row_page_database_copies.go).
	copying, err := rowPageDatabasesCopying(app, rowID)
	if err != nil {
		return response, err
	}
	return databaseToolbarTemplateRowCreateResponse{
		RequestID: requestID, RowID: rowID, DataSourceID: authority.DataSourceID,
		Position: authority.Position, Values: values,
		Icon: authority.Icon, IconType: authority.IconType, Duplicate: duplicate, CopyingDatabases: copying,
	}, nil
}
