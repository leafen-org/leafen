package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

func serveNotionPageCreate(event *core.RequestEvent, ygo *pocketBaseYgoPersistence) error {
	workspace, authorized, err := authorizeNotionWorkspaceOperation(
		event, "pages:write", permissionWorkspaceWrite,
	)
	if err != nil || !authorized {
		return err
	}
	input := notionPageCreateRequest{}
	if err := bindNotionStrictJSON(event, &input); err != nil {
		return notionBindRefused(event, err)
	}
	if input.AllowAsync {
		if input.Markdown == nil {
			return notionPageValidationError(event, "allow_async requires markdown")
		}
		input.AllowAsync = false
		return serveNotionAsyncWrite(event, workspace.Identity, notionAsyncOperationPageCreate, func() error {
			return createNotionPage(event, ygo, workspace, input)
		})
	}
	return createNotionPage(event, ygo, workspace, input)
}

func createNotionPage(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	workspace notionWorkspaceOperationAuthorization,
	input notionPageCreateRequest,
) error {
	// A database parent is its one data source (notionDatabaseParentSource).
	if notionPageParentKind(input.Parent) == "database" {
		source, ok, err := notionDatabaseParentSource(event, workspace.Identity, input.Parent.DatabaseID, nil)
		if err != nil || !ok {
			return err
		}
		input.Parent = notionPageParentRequest{Type: "data_source_id", DataSourceID: source}
	}
	if notionPageParentKind(input.Parent) == "data_source" {
		return serveNotionDatabaseRowPageCreate(event, ygo, workspace, input)
	}
	// The parent is found before what the page holds is read, as Notion
	// answers (seen 2026-11-10).
	parentID := ""
	// Where the new blocks' link targets are reached from.
	targetScope := pageAuthorization{Workspace: workspace.Workspace}
	internal := notionInternalConnection(workspace.Identity)
	switch notionPageParentKind(input.Parent) {
	case "workspace":
		if internal {
			return notionPageValidationError(event, notionInternalPageParentMessage)
		}
		if len(workspace.Identity.ResourceIDs) > 0 {
			return notionPageNotFound(event)
		}
	case "page":
		// A database page takes sub-pages as a page does (notion_page_parent.go).
		parent, ok, err := authorizeNotionParentPage(event, input.Parent.PageID, notionPageCreateParentAnswers)
		if err != nil || !ok {
			return err
		}
		parentID, targetScope = parent.Page.Page.Id, parent.Page
	default:
		// A missing parent is refused where Notion refuses it (seen
		// 2026-10-29), in Leafen's words, and to an internal connection as
		// Notion refuses one (2026-11-11).
		if internal {
			return notionPageValidationError(event, notionInternalPageParentMessage)
		}
		return notionPageValidationError(event, "A new page needs a parent: give `parent.page_id`, `parent.data_source_id` or `parent.workspace`.")
	}
	// How the content is asked for is checked before the title is read, and
	// what it holds after, as Notion's (seen 2026-11-10).
	placement, _, err := notionPageCreateContentAsked(input)
	if err != nil {
		return notionPageValidationError(event, err.Error())
	}
	// A block to go after that isn't there is said before the template and
	// the title are read, as Notion's (seen 2026-11-10: 404, naming it); a
	// top block or sub-page of the parent in the trash puts the page at the
	// end (seen 2026-11-14). One elsewhere is refused where the page is
	// placed (page_creation.go).
	trashedAnchor := false
	if placement != nil && placement.Type == "after" {
		_, err := notionBodyAnchor(event.App, workspace.Identity, parentID, "", placement.AfterID)
		switch {
		case errors.Is(err, errNotionBodyAnchorTrashed):
			trashedAnchor = true
		case errors.Is(err, errNotionBodyAnchorMissing):
			return notionNamedObjectNotFound(event, "block", placement.AfterID)
		case err != nil && !errors.Is(err, errNotionBodyAnchorOutside):
			return err
		}
	}
	// A default template is a data source's, as Notion's API says; a named
	// one is any page the connection reaches (notion_page_template_copy.go).
	source, templated, refused := notionPageTemplateFor(event, workspace.Identity, input)
	if refused != nil || templated && source.PageID == "" {
		return refused
	}
	// A page without a title stays without one: each reader's screen names it
	// in their language. Its title's mentions are named for the workspace.
	title, titleSpans, _, err := notionOrdinaryPageTitle(input.Properties.object(),
		notionNameMentions(event.App, workspace.Identity, pageAuthorization{Workspace: workspace.Workspace}))
	if err != nil {
		if answered, missing := notionUserMissingAnswer(event, err); missing {
			return answered
		}
		return notionPageValidationError(event, err.Error())
	}
	// Its Markdown's first line "# X" is its title line, which titles it when
	// it sends no properties (notion_page_markdown_leading_title.go).
	if line, titles := notionMarkdownAPITitleLine(&input); titles {
		if title, titleSpans, err = notionPlainPageTitle(line); err != nil {
			return notionPageValidationError(event, err.Error())
		}
	}
	// Its Markdown's addresses name pages here by this server's origin.
	origin, _ := outwardServerOrigin(event.Request)
	contentPlan, err := planNotionPageCreateContent(input, notionCustomEmojiContentCheck(
		event.App, workspace.Identity.PrincipalID, workspace.Identity.WorkspaceID,
	), origin)
	if unknown := (notionMarkdownCreateUnknownError{}); errors.As(err, &unknown) {
		return notionPageValidationError(event, unknown.answer(origin))
	}
	if err != nil && contentPlan.ContentRefused == nil {
		return notionPageValidationError(event, err.Error())
	}
	if err != nil {
		// Its icon and cover are read, and their uploads found, before the
		// faults in its content are said, as Notion's API does (seen
		// 2026-11-10).
		presentation, err := notionPagePresentation(input.Icon, input.Cover)
		if err != nil {
			return notionPageValidationError(event, err.Error())
		}
		if err := validateNotionPagePresentationReferences(
			event.App, workspace.Identity.PrincipalID, workspace.Identity.WorkspaceID, &presentation,
		); err != nil {
			return notionPageCreateContentError(event, err)
		}
		return notionPageCreateContentError(
			event, notionPageCreateContentRefused(event.App, workspace.Identity, targetScope, contentPlan),
		)
	}
	if placement := contentPlan.BodyPlacement; placement != nil && placement.AfterKind == pageBodyItemBlock {
		placement.AfterID, _ = resolveNotionBlockID(event.App, placement.AfterID)
	}
	if trashedAnchor {
		contentPlan.BodyPlacement = &pageBodyPlacement{Type: "end"}
	}
	presentation, err := notionPagePresentation(input.Icon, input.Cover)
	if err != nil {
		return notionPageValidationError(event, err.Error())
	}
	createdPageID := ""
	if len(contentPlan.Operations) > 0 {
		createdPageID, err = createNotionPageWithContent(
			event, ygo, workspace.Identity, parentID, title, titleSpans, presentation, input.IsLocked, contentPlan,
		)
		if err != nil {
			return notionPageCreateContentError(event, err)
		}
	} else {
		err = event.App.RunInTransaction(func(txApp core.App) error {
			defer namePageHistoryConnection(txApp, event.Request.Context())()
			created, status, err := admitPageCreationTransactionWithLock(txApp, workspace.Identity.PrincipalID, pageCreationAdmissionInput{
				RequestID:   "notion-public-create:" + newDiagnosticReference(),
				WorkspaceID: workspace.Identity.WorkspaceID,
				ParentID:    parentID, Title: title, TitleSpans: titleSpans, Content: contentPlan.AdmissionContent, ContentInBody: true,
				BodyPlacement: contentPlan.BodyPlacement,
			}, pageLockIgnored)
			if err == nil {
				err = notionPageCreationRefused(status)
			}
			if err != nil {
				return err
			}
			createdPageID = created.Page.ID
			if presentation.Icon == nil && presentation.Cover == nil && input.IsLocked == nil {
				return nil
			}
			if err := validateNotionPagePresentationReferences(
				txApp, workspace.Identity.PrincipalID, workspace.Identity.WorkspaceID, &presentation,
			); err != nil {
				return err
			}
			metadata := pageMetadataInput{
				Title: title, TitleSpans: &titleSpans, ParentID: parentID, InTrash: false,
				Icon: presentation.Icon, IconType: presentation.IconType, Cover: presentation.Cover,
				IsLocked:   input.IsLocked,
				MutationID: "notion-public-presentation:" + newDiagnosticReference(),
				BaseCursor: created.Page.Cursor,
			}
			_, metadataStatus, err := applyPageMetadataTransaction(
				txApp, workspace.Identity.PrincipalID, createdPageID, metadata,
				pageMetadataRequestHash(createdPageID, metadata),
			)
			if err == nil && metadataStatus != pageMetadataApplied {
				err = graphConflictError(errorCodeGraphPageCursorConflict, "The page presentation could not be applied.")
			}
			return err
		})
	}
	// A body that makes sub-pages or databases is written once the page is
	// there, as its Markdown update writes it.
	if err == nil && createdPageID != "" && contentPlan.DeferredMarkdown != "" {
		err = writeNotionCreatedPageMarkdown(
			event, ygo, workspace.Identity, createdPageID, contentPlan.DeferredMarkdown, input.MarkdownVersion,
		)
	}
	if err == nil && createdPageID != "" && templated && !source.Database {
		err = applyNotionPageTemplate(event, workspace.Identity, source, createdPageID)
	}
	if err == nil {
		// Made whole, a page made under a page of a wiki is the wiki's
		// (notion_page_written.go).
		err = settleNotionPageInWiki(event, ygo, workspace.Identity, createdPageID)
	}
	if errors.Is(err, errDatabaseRowPageConflict) {
		return notionPageTemplateConflict(event)
	}
	if err != nil {
		requestError := &graphRequestError{}
		if errors.As(err, &requestError) && (requestError.status == http.StatusBadRequest || requestError.precondition) {
			return notionPageValidationError(event, notionPageCreateRefusal(requestError.message))
		}
		return notionPageCreateContentError(event, err)
	}
	if createdPageID == "" {
		return notionRequestNotCompleted(event, errInvalidNotionPageIdentity)
	}
	page, err := event.App.FindRecordById(pagesCollection, createdPageID)
	if err == nil {
		err = recordConnectionWrittenPage(event.App, workspace.Identity, page, true, "")
	}
	if err == nil {
		err = grantConnectionTopLevelPage(event.App, workspace.Identity, parentID, createdPageID)
	}
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	response, err := notionWrittenPageObject(event, workspace.Identity, page)
	if err != nil {
		return notionDatabaseRowPageReadError(event, err)
	}
	// A database named as the template makes the page a copy of it, which
	// the answer, read before, names (notion_page_template_database.go).
	if templated && source.Database {
		databaseID, err := copyNotionPageTemplateDatabase(event, ygo, workspace.Identity, source, createdPageID, input.Icon)
		if errors.Is(err, errDatabaseRowPageConflict) {
			return notionPageTemplateConflict(event)
		}
		if err != nil {
			return notionRequestNotCompleted(event, err)
		}
		response["id"], response["url"] = databaseID, notionDatabasePublicURL(origin, databaseID)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

func serveNotionPageUpdate(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	publicPageID string,
) error {
	if !notionPublicIDNamesPage(event.App, strings.TrimSpace(publicPageID)) {
		return serveNotionDatabaseRowPageUpdate(event, ygo, publicPageID)
	}
	authorization, authorized, err := authorizeNotionPage(
		event, publicPageID, "pages:write", permissionWorkspaceWrite,
	)
	if err != nil || !authorized {
		return err
	}
	input := notionPageUpdateRequest{}
	if err := bindNotionStrictJSON(event, &input); err != nil {
		return notionBindRefused(event, err)
	}
	// A page in the trash takes only being taken out (notion_trash_refusal.go).
	restoring := input.InTrash != nil && !*input.InTrash
	if refused, err := refuseNotionTrashedEdit(event, authorization.Page.Page.Id, true, restoring, notionTrashedEdit); refused {
		return err
	}
	source, templated, refused := notionPageTemplateFor(event, authorization.Identity, notionPageCreateRequest{Template: input.Template})
	if refused != nil || templated && source.PageID == "" {
		return refused
	}
	// The rest of the update is checked before anything is written, and goes
	// on the page as the erase and the template leave it: erase_content takes
	// a title, icon, cover, lock or trash beside it (seen 2026-11-14).
	title, titleSpans, titleChanged, err := notionOrdinaryPageTitle(input.Properties.object(),
		notionNameMentions(event.App, authorization.Identity, authorization.Page))
	if err != nil {
		if answered, missing := notionUserMissingAnswer(event, err); missing {
			return answered
		}
		return notionPageValidationError(event, err.Error())
	}
	page := authorization.Page.Page
	presentation, err := notionPagePresentation(input.Icon, input.Cover)
	if err != nil {
		return notionPageValidationError(event, err.Error())
	}
	inTrash, trashChanged, err := input.trashState(page.GetBool("in_trash"))
	if err != nil {
		return notionPageValidationError(event, err.Error()+".")
	}
	archiveChanged := false
	if input.IsArchived != nil {
		effect, archived, err := loadPageArchiveEffect(event.App, page.Id)
		if err != nil {
			return notionRequestNotCompleted(event, err)
		}
		if !*input.IsArchived && archived && effect.RootID != page.Id {
			return notionPageValidationError(event, "This page is archived through an archived parent page.")
		}
		archiveChanged = *input.IsArchived != pageArchived(page)
	}
	changed := titleChanged || input.Icon != nil || input.Cover != nil || input.IsLocked != nil ||
		trashChanged || archiveChanged
	update := func() (*core.Record, func() error) {
		current, err := event.App.FindRecordById(pagesCollection, page.Id)
		if err != nil {
			return nil, func() error { return notionPageNotFound(event) }
		}
		if !changed {
			return current, nil
		}
		// A title written takes its spans; none written keeps the page's.
		name, written := title, &titleSpans
		if !titleChanged {
			name, written = current.GetString("title"), nil
		}
		return updateNotionPageMetadata(event, authorization, current, pageMetadataInput{
			Title: name, TitleSpans: written, ParentID: current.GetString("parent_id"), InTrash: inTrash,
			IsLocked: input.IsLocked, IsArchived: input.IsArchived,
			Icon: presentation.Icon, IconType: presentation.IconType, Cover: presentation.Cover,
			MutationID: "notion-public-update:" + newDiagnosticReference(), BaseCursor: current.GetInt("cursor"),
		}, presentation)
	}
	answer := func(updated *core.Record) error {
		// One taken out of the trash under a wiki's page is the wiki's (notion_page_written.go).
		if restoring && trashChanged {
			if err := settleNotionPageInWiki(event, ygo, authorization.Identity, updated.Id); err != nil {
				return notionPageCreateContentError(event, err)
			}
			if current, err := event.App.FindRecordById(pagesCollection, updated.Id); err == nil {
				updated = current
			}
		}
		if changed {
			if err := recordConnectionWrittenPage(event.App, authorization.Identity, updated, false, page.GetString("updated")); err != nil {
				return notionRequestNotCompleted(event, err)
			}
		}
		return writeCurrentNotionPage(event, authorization.Identity, updated)
	}
	// A template applies to a page as to a database page: any page the
	// connection reaches (notion_page_template_copy.go).
	applyTemplate := func() func() error {
		if !templated {
			return nil
		}
		if err := applyNotionPageTemplate(event, authorization.Identity, source, page.Id); errors.Is(err, errDatabaseRowPageConflict) {
			return func() error { return notionPageTemplateConflict(event) }
		} else if err != nil {
			return func() error { return notionPageCreateContentError(event, err) }
		}
		return nil
	}
	rest := func() error {
		if failed := applyTemplate(); failed != nil {
			return failed()
		}
		updated, failed := update()
		if failed != nil {
			return failed()
		}
		return answer(updated)
	}
	if input.Erase == nil || !*input.Erase {
		return rest()
	}
	if !restoring || !trashChanged {
		return eraseNotionPageContentThen(event, ygo, authorization, publicPageID, rest)
	}
	// Out of the trash first: a page's body in it isn't erased.
	updated, failed := update()
	if failed != nil {
		return failed()
	}
	return eraseNotionPageContentThen(event, ygo, authorization, publicPageID, func() error {
		if failed := applyTemplate(); failed != nil {
			return failed()
		}
		current, err := event.App.FindRecordById(pagesCollection, updated.Id)
		if err != nil {
			return notionPageNotFound(event)
		}
		return answer(current)
	})
}

func serveNotionPageMove(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	publicPageID string,
) error {
	input := notionPageMoveRequest{}
	if err := bindNotionStrictJSON(event, &input); err != nil {
		return notionBindRefused(event, err)
	}
	// The new parent is checked before the page (notionMoveParent).
	newParent, ok, err := notionMoveParent(event, input.Parent)
	if err != nil || !ok {
		return err
	}
	input.Parent = newParent
	if !notionPublicIDNamesPage(event.App, strings.TrimSpace(publicPageID)) {
		return serveNotionDatabaseRowPageMove(event, ygo, publicPageID, input)
	}
	authorization, authorized, err := authorizeNotionPage(
		event, publicPageID, "pages:write", permissionWorkspaceWrite,
	)
	if err != nil || !authorized {
		return err
	}
	if refused, err := refuseNotionTrashedEdit(event, authorization.Page.Page.Id, true, false, notionTrashedMove); refused {
		return err
	}
	switch notionPageParentKind(input.Parent) {
	case "page":
	case "data_source":
		return serveNotionPageMoveIntoDataSource(event, ygo, authorization, input.Parent.DataSourceID)
	default:
		return notionPageValidationError(event, "A page_id or data_source_id parent is required.")
	}
	parent, ok, err := authorizeNotionParentPage(event, input.Parent.PageID, notionPageMoveParentAnswers)
	if err != nil || !ok {
		return err
	}
	if parent.Page.Workspace.WorkspaceID != authorization.Page.Workspace.WorkspaceID {
		return notionPageNotFound(event)
	}
	if refused, err := refuseNotionMoveIntoItself(event, authorization.Page.Page.Id, parent.Page.Page.Id); refused {
		return err
	}
	page := authorization.Page.Page
	metadata := pageMetadataInput{
		Title: page.GetString("title"), ParentID: parent.Page.Page.Id,
		InTrash:    page.GetBool("in_trash"),
		MutationID: "notion-public-move:" + newDiagnosticReference(), BaseCursor: page.GetInt("cursor"),
	}
	result, status, err := applyPageMetadata(
		event.App, authorization.Identity.PrincipalID, page.Id, metadata,
		pageMetadataRequestHash(page.Id, metadata),
	)
	if status == pageMetadataPageMissing {
		return notionPageNotFound(event)
	}
	if status == pageMetadataParentConflict {
		return notionPageParentRefused(event, "The page could not be moved to that parent.",
			pageParentConflictReason(event.App, authorization.Identity.PrincipalID, page.Id, metadata, pageLockIgnored))
	}
	if status != pageMetadataApplied || err != nil {
		return notionCompatibilityErrorWithData(event, http.StatusConflict, "conflict_error", "The page could not be moved to that parent.", nil, err)
	}
	// A page moved under a wiki's page, or a page of a wiki, is the wiki's
	// (notion_page_written.go).
	if err := settleNotionPageInWiki(event, ygo, authorization.Identity, result.Page.ID); err != nil {
		return notionPageCreateContentError(event, err)
	}
	updated, err := event.App.FindRecordById(pagesCollection, result.Page.ID)
	if err == nil {
		err = recordConnectionWrittenPage(event.App, authorization.Identity, updated, false, page.GetString("updated"))
	}
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	response, err := notionWrittenPageObject(event, authorization.Identity, updated)
	if err != nil {
		return notionDatabaseRowPageReadError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}
