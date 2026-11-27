package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page made or updated through the Public API with a template_id naming a
// page, a database page or a database the connection reaches, but none of
// the data source's own templates, takes that page as its template, as
// Notion's does (seen 2026-11-14, a data source's and a page's pages made
// and updated alike): the page's icon and cover where the request and the
// page give none, its look, and its whole body after the page's own — its
// sub-pages, inline and full-page databases come as copies with their rows,
// its files as the same files (a link to one of its sub-pages still names
// that sub-page, where Notion's names the copy). A database page of the same
// data source gives its values too, the request's own winning and a page
// updated keeping its own; a page of another data source, or an ordinary
// page, gives none. A database named gives a page made in a data source, or
// updated, nothing; a page made under a page becomes a copy of it
// (notion_page_template_database.go). The title is the request's. An update takes no icon or cover beside a template
// (notionPageUpdateSaid). Notion copies after it answers; Leafen copies as a
// block copy does (admitPageBodyCopy) and answers once that is done, or has
// gone on for notionPageTemplateCopyWait, the rest following.

// notionPageTemplateCopyWait is how long a request copies a page template's
// body before it answers and leaves the rest to the job runner.
const notionPageTemplateCopyWait = 5 * time.Second

// notionPageTemplateSource is the page a template_id names: one whose look,
// body and, as a database page, values a page takes, or a database, which
// gives nothing.
type notionPageTemplateSource struct {
	PageID string
	// DataSourceID is the data source the page is a database page of.
	DataSourceID string
	Database     bool
	// ContainerID is the database's, when a database is named.
	ContainerID string
}

// resolveNotionPageTemplateSource finds the page, database page or database
// a template_id names among those the connection reads.
func resolveNotionPageTemplateSource(
	app core.App, identity notionOAuthBearerIdentity, publicID string,
) (notionPageTemplateSource, bool) {
	source := notionPageTemplateSource{}
	pageID, err := resolveNotionParentPageRecordID(app, publicID)
	if err != nil {
		database, resolved := resolveNotionObjectIdentity(app, notionObjectIdentityDatabase, publicID)
		if resolved != nil {
			return source, false
		}
		found, ok, err := loadNotionMentionedDatabase(app, database.InternalID)
		if err != nil || !ok || found.Trashed {
			return source, false
		}
		pageID, source.Database, source.ContainerID = found.PageID, true, database.InternalID
	}
	page, err := authorizePage(app, identity.PrincipalID, pageID, permissionWorkspaceRead)
	if err != nil || page.Workspace.WorkspaceID != identity.WorkspaceID || !notionOAuthIdentityAllowsPage(app, identity, page) {
		return source, false
	}
	source.PageID = pageID
	// A full-page database's page is its database, as in Notion, where the
	// two have one ID: it gives nothing, where its body's copy was refused
	// as the copy of a database's page and the request failed after a wait.
	if !source.Database && isFullPageDatabase(page.Page) {
		if err := app.DB().NewQuery(`SELECT [[id]] FROM {{leafen_database_containers}}
			WHERE [[page_id]] = {:pageId} AND [[kind]] = 'source' AND [[parent_block_id]] = '' ORDER BY [[position]], [[id]] LIMIT 1`).
			Bind(dbx.Params{"pageId": pageID}).Row(&source.ContainerID); err != nil {
			return source, false
		}
		source.Database = true
	}
	if !source.Database && isDatabaseRowPageRecord(page.Page) {
		source.DataSourceID = page.Page.GetString("data_source_id")
	}
	return source, true
}

// copies reports whether the source gives a page anything.
func (source notionPageTemplateSource) copies() bool {
	return source.PageID != "" && !source.Database
}

// notionPageTemplateRowValues are the values a database page of the data
// source gives a page made or updated from it: those its user may set, but
// its title and the values a database computes.
func notionPageTemplateRowValues(
	app core.App, actorID string, source notionPageTemplateSource, dataSourceID string,
) (map[string]graphRowValue, error) {
	values := map[string]graphRowValue{}
	if !source.copies() || source.DataSourceID == "" || source.DataSourceID != dataSourceID {
		return values, nil
	}
	authority, err := authorizeDatabaseRowPage(app, actorID, source.PageID, permissionWorkspaceRead)
	if err != nil {
		return nil, err
	}
	stored, err := loadDatabaseRowPageGraphValues(app, authority)
	if err != nil {
		return nil, err
	}
	for propertyID, value := range stored {
		if value.Type == graphValueTypeTitle || !databaseToolbarTemplateWritableProperty(value.Type) ||
			databaseRowPageMoveValueEmpty(value.Value) {
			continue
		}
		values[propertyID] = value
	}
	// The source gives only what its user may set (database_property_access.go).
	if err := withoutUneditableValues(app, actorID, dataSourceID, map[string]map[string]graphRowValue{"": values}); err != nil {
		return nil, err
	}
	return values, nil
}

// notionPageTemplateLook is the look a page template gives.
type notionPageTemplateLook struct {
	Icon                   string `db:"icon"`
	IconType               string `db:"icon_type"`
	Cover                  string `db:"cover"`
	CoverPosition          int    `db:"cover_position"`
	Font                   string `db:"font"`
	SmallText              bool   `db:"small_text"`
	FullWidth              bool   `db:"full_width"`
	TableOfContentsDisplay string `db:"table_of_contents_display"`
}

func loadNotionPageTemplateLook(app core.App, pageID string) (notionPageTemplateLook, error) {
	look := notionPageTemplateLook{}
	err := app.DB().NewQuery(`SELECT [[icon]], [[icon_type]], [[cover]], [[cover_position]], [[font]],
		[[small_text]], [[full_width]], [[table_of_contents_display]] FROM {{leafen_pages}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": pageID}).One(&look)
	return look, err
}

// applyNotionPageTemplateRowLook gives a database page its template page's
// look: its icon and cover where it has none, its style.
func applyNotionPageTemplateRowLook(app core.App, source notionPageTemplateSource, rowID string) error {
	if !source.copies() {
		return nil
	}
	look, err := loadNotionPageTemplateLook(app, source.PageID)
	if err != nil {
		return err
	}
	found, err := updateRowPageRecord(app, `
		[[icon_type]] = CASE WHEN [[icon]] = '' THEN {:iconType} ELSE [[icon_type]] END,
		[[icon]] = CASE WHEN [[icon]] = '' THEN {:icon} ELSE [[icon]] END,
		[[cover_position]] = CASE WHEN [[cover]] = '' THEN {:coverPosition} ELSE [[cover_position]] END,
		[[cover]] = CASE WHEN [[cover]] = '' THEN {:cover} ELSE [[cover]] END,
		[[font]] = {:font}, [[small_text]] = {:smallText}, [[full_width]] = {:fullWidth},
		[[table_of_contents_display]] = {:tableOfContents}`, "", dbx.Params{
		"rowId": rowID, "workspaceId": "", "pageId": "", "icon": look.Icon, "iconType": look.IconType,
		"cover": look.Cover, "coverPosition": look.CoverPosition, "font": look.Font, "smallText": look.SmallText,
		"fullWidth": look.FullWidth, "tableOfContents": look.TableOfContentsDisplay,
	})
	if err != nil {
		return err
	}
	if !found {
		return errDatabaseRowPageConflict
	}
	// The icon's and cover's files are the page's too: its icon's under the
	// row's own reference, its cover's as a page's.
	return replaceProductFileRowPageLookReferences(app, rowID)
}

// applyNotionPageTemplatePageLook gives an ordinary page its template
// page's look: its icon and cover where it has none, its style.
func applyNotionPageTemplatePageLook(
	ctx context.Context, app core.App, actorID string, source notionPageTemplateSource, pageID string,
) error {
	if !source.copies() {
		return nil
	}
	look, err := loadNotionPageTemplateLook(app, source.PageID)
	if err != nil {
		return err
	}
	return app.RunInTransaction(func(txApp core.App) error {
		defer namePageHistoryConnection(txApp, ctx)()
		page, err := txApp.FindRecordById(pagesCollection, pageID)
		if err != nil {
			return err
		}
		metadata := pageMetadataInput{
			Title: page.GetString("title"), ParentID: page.GetString("parent_id"), InTrash: page.GetBool("in_trash"),
			Font: &look.Font, SmallText: &look.SmallText, FullWidth: &look.FullWidth,
			TableOfContentsDisplay: &look.TableOfContentsDisplay,
			MutationID:             "notion-public-template-look:" + newDiagnosticReference(), BaseCursor: page.GetInt("cursor"),
		}
		if page.GetString("icon") == "" && look.Icon != "" {
			metadata.Icon, metadata.IconType = &look.Icon, &look.IconType
		}
		if page.GetString("cover") == "" && look.Cover != "" {
			metadata.Cover, metadata.CoverPosition = &look.Cover, &look.CoverPosition
		}
		_, status, err := applyPageMetadataTransaction(
			txApp, actorID, pageID, metadata, pageMetadataRequestHash(pageID, metadata),
		)
		if err == nil && status != pageMetadataApplied {
			return graphConflictError(errorCodeGraphPageCursorConflict, "The page changed before its template's look was applied.")
		}
		return err
	})
}

// copyNotionPageTemplateBody copies a template page's body after a page's
// own, as one block copy, and goes on with it, and with the copies of its
// sub-pages, until they are done or the wait ends. A source being copied
// elsewhere is waited for as long.
func copyNotionPageTemplateBody(
	ctx context.Context, app core.App, actorID string, source notionPageTemplateSource, targetPageID string,
) error {
	if !source.copies() {
		return nil
	}
	var roots int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_blocks}} WHERE [[page_id]] = {:pageId}`).
		Bind(dbx.Params{"pageId": source.PageID}).Row(&roots); err != nil || roots == 0 {
		return err
	}
	deadline := time.Now().Add(notionPageTemplateCopyWait)
	requestID := "notion-template-copy:" + newDiagnosticReference()
	var admission blockMoveAdmission
	for {
		var err error
		admission, err = admitPageBodyCopy(app, actorID, source.PageID, blockMoveInput{
			RequestID: requestID, DestinationPageID: targetPageID,
		})
		if err == nil {
			break
		}
		if !errors.Is(err, errBlockMoveConflict) && !isPermissionDenied(err) {
			return err
		}
		if !time.Now().Before(deadline) {
			return errDatabaseRowPageConflict
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	if runner, ok := app.Store().Get(durableJobRunnerStoreKey).(*durableJobRunner); ok {
		defer runner.notify()
	}
	for time.Now().Before(deadline) {
		job, err := loadBlockMoveJobByID(app, admission.Job.ID, "")
		if err != nil {
			return err
		}
		if job.Phase == blockMovePhaseCompleted || job.Phase == blockMovePhaseFailed {
			break
		}
		if err := advanceBlockMove(ctx, app, job.ID); err != nil {
			return nil
		}
	}
	// The sub-pages' copies, which the block copy started once it was done.
	pages := []string{}
	if err := app.DB().NewQuery(`SELECT [[job_id]] FROM {{leafen_page_duplication_jobs}}
		WHERE [[parent_block_copy_job_id]] = {:jobId} ORDER BY [[created_at]], [[job_id]]`).
		Bind(dbx.Params{"jobId": admission.Job.ID}).Column(&pages); err != nil {
		return err
	}
	for _, jobID := range pages {
		for time.Now().Before(deadline) {
			phase := ""
			err := app.DB().NewQuery(`SELECT [[phase]] FROM {{leafen_page_duplication_jobs}} WHERE [[job_id]] = {:jobId}`).
				Bind(dbx.Params{"jobId": jobID}).Row(&phase)
			if errors.Is(err, sql.ErrNoRows) || phase == pageDuplicationPhaseCompleted || phase == pageDuplicationPhaseFailed {
				break
			}
			if err != nil {
				return err
			}
			if err := advancePageDuplication(ctx, app, jobID); err != nil {
				break
			}
		}
	}
	return nil
}

// notionPageTemplateFor reads the template of a page made or updated outside
// a data source: a default one is refused as Notion's API refuses it, content
// beside a named one too, and one naming nothing the connection reaches is a
// page Notion can't find (seen 2026-11-14). templated reports a template;
// with no source page, the answer is written and refused is what to return.
func notionPageTemplateFor(
	event *core.RequestEvent, identity notionOAuthBearerIdentity, input notionPageCreateRequest,
) (source notionPageTemplateSource, templated bool, refused error) {
	template, _ := parseNotionPageCreateTemplate(input.Template)
	switch template.Type {
	case "default":
		return source, true, notionPageValidationError(event, "A 'default' template needs a data source parent.")
	case "template_id":
	default:
		return source, false, nil
	}
	// Content beside it is said before it is looked up (seen 2026-11-14).
	if notionPageCreateTemplateContentGiven(input) {
		return source, true, notionPageValidationError(event, notionPageTemplateContentMessage)
	}
	id := strings.TrimSpace(template.TemplateID)
	source, reached := resolveNotionPageTemplateSource(event.App, identity, id)
	if !reached {
		if dashed, ok := normalizeNotionUUIDRequest(id); ok {
			id = dashed
		}
		return notionPageTemplateSource{}, true, notionNamedObjectNotFound(event, "page", id)
	}
	return source, true, nil
}

// applyNotionPageTemplate gives an ordinary page its template page's look
// and body.
func applyNotionPageTemplate(
	event *core.RequestEvent, identity notionOAuthBearerIdentity, source notionPageTemplateSource, pageID string,
) error {
	ctx := event.Request.Context()
	if err := applyNotionPageTemplatePageLook(ctx, event.App, identity.PrincipalID, source, pageID); err != nil {
		return err
	}
	return copyNotionPageTemplateBody(ctx, event.App, identity.PrincipalID, source, pageID)
}

// notionPageTemplateConflict answers a template whose page is being copied
// elsewhere for longer than a request waits.
func notionPageTemplateConflict(event *core.RequestEvent) error {
	return notionCompatibilityError(event, http.StatusConflict, "conflict_error", notionConflictWhileSaving)
}
