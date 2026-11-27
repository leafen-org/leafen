package app

import (
	"errors"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// notionPagePositionParentMessage is Notion's answer to a position given for
// a page made in a data source (seen 2026-11-14).
const notionPagePositionParentMessage = "The `position` parameter goes only with a page parent, not with a database or the workspace."

// notionPageTemplateContentMessage is Notion's answer to content given
// beside a template (seen 2026-11-14).
const notionPageTemplateContentMessage = "A template gives the page its content, so 'content', 'children' and 'markdown' don't go with one."

// notionPageCreateTemplateContentGiven reports whether a page create gives
// content beside its template as Notion counts it: children or content,
// even none, or Markdown that isn't empty (seen 2026-11-14: "children": []
// is refused, "markdown": "" not).
func notionPageCreateTemplateContentGiven(input notionPageCreateRequest) bool {
	return input.Children != nil || input.Content != nil || input.Markdown != nil && *input.Markdown != ""
}

func serveNotionDatabaseRowPageCreate(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	workspace notionWorkspaceOperationAuthorization,
	input notionPageCreateRequest,
) error {
	// The data source is found before what the page holds is read, as
	// Notion answers (seen 2026-11-10).
	// One in the trash, or under a page in it, takes the item into the trash
	// with it (notion_trash_reads.go).
	parent, authorized, err := authorizeNotionDataSourceParentBy(
		event, input.Parent.DataSourceID, "pages:write", permissionWorkspaceWrite,
		func(app core.App, identity notionOAuthBearerIdentity, dataSourceID string) (notionDatabaseRowPageAuthorization, error) {
			return authorizeNotionDataSourceItemParentReach(app, identity, dataSourceID, true)
		},
	)
	if err != nil || !authorized {
		return err
	}
	inTrash, err := notionDataSourceInTrash(event.App, parent.Container)
	if err != nil {
		return notionDatabaseRowPageMutationError(event, err)
	}
	if parent.Identity.PrincipalID != workspace.Identity.PrincipalID ||
		parent.Identity.WorkspaceID != workspace.Identity.WorkspaceID {
		return notionPageNotFound(event)
	}
	// A page goes at its data source's end, as Notion's says (seen
	// 2026-11-14, with Markdown and blocks alike).
	if input.Position != nil {
		return notionPageValidationError(event, notionPagePositionParentMessage)
	}
	// A template is checked before the content is read: a default one the
	// data source lacks first, then content given beside any (seen
	// 2026-11-14; notionPageCreateTemplateContentGiven).
	template, _ := parseNotionPageCreateTemplate(input.Template)
	if template.applies() {
		if _, _, err := notionPageCreateContentAsked(input); err != nil {
			return notionPageValidationError(event, err.Error())
		}
		if template.Type == "default" {
			if _, _, err := notionPageCreateTemplate(event.App, parent, template); err != nil {
				return notionDatabaseRowPageMutationError(event, err)
			}
		}
		if notionPageCreateTemplateContentGiven(input) {
			return notionPageValidationError(event, notionPageTemplateContentMessage)
		}
	}
	// Its Markdown's first line "# X" is its title line, as an ordinary
	// page's, which gives its title property when it sends no properties
	// (seen 2026-11-14; notion_page_markdown_leading_title.go).
	titleLine, titles := notionMarkdownAPITitleLine(&input)
	// The faults in its content are said once its values, icon and cover
	// have passed (notionPageCreateContentRefused). Its Markdown's addresses
	// name pages here by this server's origin.
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
	presentation, err := notionPagePresentation(input.Icon, input.Cover)
	if err != nil {
		return notionPageValidationError(event, err.Error())
	}
	rowID := newPageID()
	err = createNotionDatabaseRowPage(event.Request.Context(), ygo, notionDatabaseRowPageCreation{
		Parent: parent, RowID: rowID, MutationID: "notion-public-row-create:" + newDiagnosticReference(),
		Template: template, Content: contentPlan, Presentation: presentation, InTrash: inTrash,
		Properties: func(txApp core.App, current notionDatabaseRowPageAuthorization) (notionRequestProperties, error) {
			if titles {
				return notionRowTitleLineValues(txApp, current, titleLine)
			}
			return notionRowRequestValues(txApp, current, input.Properties)
		},
	})
	if err != nil {
		return notionDatabaseRowPageMutationError(event, err)
	}
	// A body that makes sub-pages or databases is written once the page is
	// there, as its Markdown update writes it.
	if contentPlan.DeferredMarkdown != "" {
		if err := writeNotionCreatedPageMarkdown(
			event, ygo, workspace.Identity, rowID, contentPlan.DeferredMarkdown, input.MarkdownVersion,
		); err != nil {
			return notionPageCreateContentError(event, err)
		}
	}
	if err := recordConnectionStoredMark(event.App, parent.Identity, connectionAuthoredRow, rowID, true, ""); err != nil {
		return notionDatabaseRowPageMutationError(event, err)
	}
	response, err := loadNotionDatabaseRowPageResponse(
		event, parent.Identity, rowID, permissionWorkspaceRead,
	)
	if err != nil {
		return notionDatabaseRowPageReadError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

func serveNotionDatabaseRowPageUpdate(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	publicPageID string,
) error {
	// A template's page takes its values, icon and cover as a page of its
	// data source (notion_database_template_page.go).
	if handled, err := serveNotionDatabaseTemplatePageUpdate(event, ygo, publicPageID); handled {
		return err
	}
	// One in the trash, or under its database in it, is refused next
	// (notion_trash_refusal.go).
	admission, authorized, err := authorizeNotionDatabaseRowPageState(
		event, publicPageID, "pages:write", permissionWorkspaceWrite, notionRowAnyTrash,
	)
	if err != nil || !authorized {
		return err
	}
	input := notionPageUpdateRequest{}
	if err := bindNotionStrictJSON(event, &input); err != nil {
		return notionBindRefused(event, err)
	}
	template, err := parseNotionPageCreateTemplate(input.Template)
	// A page update names the template it applies, as Notion's does.
	if err != nil || input.Template != nil && string(input.Template) != "null" && !template.applies() {
		return notionPageValidationError(event, "The page template is invalid.")
	}
	// A template, or erase_content, changes the page's body as well.
	reshaping := template.applies() || input.Erase != nil && *input.Erase
	inTrash := admission.Authority.TrashedAt != 0
	trash, _, err := input.trashState(inTrash)
	if err != nil {
		return notionPageValidationError(event, err.Error()+".")
	}
	// A row in the trash takes only being taken out (notion_trash_refusal.go).
	if refused, err := refuseNotionTrashedEdit(event, admission.Authority.RowID, true, inTrash && !trash, notionTrashedEdit); refused {
		return err
	}
	rest := input.Properties.count() != 0 || input.Icon != nil || input.Cover != nil || input.IsArchived != nil || input.IsLocked != nil || reshaping
	if trash != inTrash && !rest {
		return changeNotionDatabaseRowPageTrash(event, ygo, admission, trash)
	}
	if input.IsArchived != nil && !*input.IsArchived {
		_, archived, err := loadPageArchiveEffect(event.App, admission.Authority.ContainerPageID)
		if err != nil {
			return notionDatabaseRowPageMutationError(event, err)
		}
		if archived {
			return notionPageValidationError(
				event, "A database row page archived with the page that holds its database is unarchived with that page.",
			)
		}
	}
	presentation, err := notionPagePresentation(input.Icon, input.Cover)
	if err != nil {
		return notionPageValidationError(event, err.Error())
	}
	// With the rest of the update, out of the trash first and into it last
	// (notion_database_row_page_trash.go): values of the wrong form leave it
	// where it is, as Notion's do, and a rest refused once it is out puts it
	// back.
	trashLast, restored := trash && !inTrash, !trash && inTrash
	if restored {
		if _, err := notionRowRequestValues(event.App, admission, input.Properties); err != nil {
			return notionDatabaseRowPageMutationError(event, err)
		}
		if err := setNotionDatabaseRowPageTrash(event, ygo, admission, false); err != nil {
			return notionDatabaseRowPageTrashError(event, err)
		}
		if admission, err = authorizeNotionDatabaseRowPageCurrent(
			event.App, admission.Identity, admission.Authority.RowID, permissionWorkspaceWrite,
		); err != nil {
			return notionDatabaseRowPageMutationError(event, err)
		}
	}
	err = updateNotionDatabaseRowPageRest(event, ygo, admission, input, template, reshaping, presentation)
	if err == nil && trashLast {
		err = trashNotionDatabaseRowPageLast(event, ygo, admission)
	}
	if err != nil && restored {
		_ = trashNotionDatabaseRowPageLast(event, ygo, admission)
	}
	if err != nil {
		return notionDatabaseRowPageTrashError(event, err)
	}
	return answerNotionDatabaseRowPage(event, admission)
}

// updateNotionDatabaseRowPageRest writes a row page update's template,
// values, icon, cover, archive and lock.
func updateNotionDatabaseRowPageRest(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	admission notionDatabaseRowPageAuthorization,
	input notionPageUpdateRequest,
	template notionPageTemplateRequest,
	reshaping bool,
	presentation notionPagePresentationRequest,
) error {
	if reshaping {
		// The request's own values go with the template's.
		if err := applyNotionDatabaseRowPageTemplate(
			event.Request.Context(), event.App, ygo, admission.Identity, admission.Authority.RowID,
			notionDatabaseRowPageTemplateApplication{
				Template: template, Erase: input.Erase != nil && *input.Erase, Properties: input.Properties,
			},
		); err != nil {
			return err
		}
		input.Properties = notionRequestProperties{}
	}
	mark := func() error {
		return recordConnectionStoredMark(event.App, admission.Identity, connectionAuthoredRow, admission.Authority.RowID, false, notionBlockEditMark(admission.Authority.UpdatedAt))
	}
	if input.Properties.count() == 0 && input.Icon == nil && input.Cover == nil && input.IsArchived == nil && input.IsLocked == nil {
		return mark()
	}
	// The icon, cover, archive and lock alone, when no value is written.
	applyRest := func() error {
		return event.App.RunInTransaction(func(txApp core.App) error {
			// The connection's change — a lock too — names it in the page's history and updates.
			defer namePageHistoryConnection(txApp, event.Request.Context())()
			current, err := authorizeNotionDatabaseRowPageCurrent(
				txApp, admission.Identity, admission.Authority.RowID, permissionWorkspaceWrite,
			)
			if err != nil {
				return err
			}
			if err := applyNotionDatabaseRowPagePresentation(
				txApp, current.Authority.RowID, current.Authority.WorkspaceID,
				current.Authority.ContainerPageID,
				admission.Identity.PrincipalID, presentation,
			); err != nil {
				return err
			}
			if err := applyNotionDatabaseRowPageArchive(txApp, current.Authority, admission.Identity.PrincipalID, input.IsArchived); err != nil {
				return err
			}
			return applyNotionDatabaseRowPageLock(txApp, current.Authority, admission.Identity.PrincipalID, input.IsLocked)
		})
	}
	if input.Properties.count() == 0 {
		return applyRest()
	}
	err := writeNotionDatabaseRowPageValues(
		event.Request.Context(), ygo, admission.Identity, admission.Authority,
		"notion-public-row-update:"+newDiagnosticReference(),
		func(txApp core.App, current notionDatabaseRowPageAuthorization) (notionRequestProperties, error) {
			return notionRowRequestValues(txApp, current, input.Properties)
		},
		func(txApp core.App) error {
			if err := applyNotionDatabaseRowPagePresentation(
				txApp, admission.Authority.RowID, admission.Authority.WorkspaceID,
				admission.Authority.ContainerPageID,
				admission.Identity.PrincipalID, presentation,
			); err != nil {
				return err
			}
			if err := applyNotionDatabaseRowPageArchive(
				txApp, admission.Authority, admission.Identity.PrincipalID, input.IsArchived,
			); err != nil {
				return err
			}
			return applyNotionDatabaseRowPageLock(txApp, admission.Authority, admission.Identity.PrincipalID, input.IsLocked)
		},
	)
	// Values that are all passed over (a formula's, a created time's) write
	// none, as Notion's API answers them.
	if errors.Is(err, errNotionDatabaseRowPageValuesKept) {
		err = applyRest()
	}
	if err != nil {
		return err
	}
	return mark()
}

func serveNotionDatabaseRowPageMove(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	publicPageID string,
	input notionPageMoveRequest,
) error {
	// One in the trash, or under its database in it, is refused next
	// (notion_trash_refusal.go).
	admission, authorized, err := authorizeNotionDatabaseRowPageState(
		event, publicPageID, "pages:write", permissionWorkspaceWrite, notionRowAnyTrash,
	)
	if err != nil || !authorized {
		return err
	}
	if refused, err := refuseNotionTrashedEdit(event, admission.Authority.RowID, true, false, notionTrashedMove); refused {
		return err
	}
	switch notionPageParentKind(input.Parent) {
	case "data_source":
	case "page":
		return serveNotionDatabaseRowPageMoveOut(event, ygo, admission, input.Parent.PageID)
	default:
		return notionPageValidationError(event, "A page_id or data_source_id parent is required.")
	}
	target, authorized, err := authorizeNotionDataSourceParent(
		event, input.Parent.DataSourceID, "pages:write", permissionWorkspaceWrite,
	)
	if err != nil || !authorized {
		return err
	}
	if target.Container.WorkspaceID != admission.Authority.WorkspaceID {
		return notionPageNotFound(event)
	}
	if target.Container.DataSource == admission.Authority.DataSourceID {
		return notionPageValidationError(event, "The row page already belongs to that data source.")
	}
	if target.Container.PageID != admission.Authority.ContainerPageID {
		return serveNotionDatabaseRowPageMoveAcross(event, ygo, admission, target)
	}
	plan := databaseRowPageMovePlan{}
	mutationID := "notion-public-row-move:" + newDiagnosticReference()
	_, err = ygo.applyContentGraphMutationContext(
		event.Request.Context(), admission.Identity.PrincipalID,
		admission.Authority.ContainerPageID,
		graphMutationInput{MutationID: mutationID},
		contentGraphMutationHooks{
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				current, err := authorizeNotionDatabaseRowPageCurrent(
					txApp, admission.Identity, admission.Authority.RowID, permissionWorkspaceWrite,
				)
				if err != nil || current.Authority.ContainerPageID != admission.Authority.ContainerPageID {
					return pageAuthorization{}, err
				}
				targetCurrent, err := authorizeNotionDataSourceParentCurrent(
					txApp, admission.Identity, target.Container.DataSource, permissionWorkspaceWrite,
				)
				if err != nil || targetCurrent.Container.PageID != current.Authority.ContainerPageID {
					return pageAuthorization{}, err
				}
				return current.Page, nil
			},
			Prepare: func(txApp core.App, _ pageAuthorization, doc *crdt.Doc) (contentGraphMutationPreparation, error) {
				current, err := authorizeNotionDatabaseRowPageCurrent(
					txApp, admission.Identity, admission.Authority.RowID, permissionWorkspaceWrite,
				)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				targetCurrent, err := authorizeNotionDataSourceParentCurrent(
					txApp, admission.Identity, target.Container.DataSource, permissionWorkspaceWrite,
				)
				if err != nil || targetCurrent.Container.PageID != current.Authority.ContainerPageID {
					return contentGraphMutationPreparation{}, err
				}
				// The API has no confirmation step; like Notion's (seen
				// 2026-10-31), a move brings the properties the target lacks
				// and drops only the values that still can't come.
				plan, err = planDatabaseRowPageMove(
					txApp, current.Authority, targetCurrent.Container.DataSource,
					func(string) bool { return true },
				)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				return contentGraphMutationPreparation{Operations: plan.Operations}, nil
			},
			AfterApply: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
				return plan.finish(txApp, admission.Identity.PrincipalID, mutationID)
			},
		},
	)
	if err != nil {
		if errors.Is(err, errDatabaseRowPageConflict) {
			return notionCompatibilityError(
				event, http.StatusConflict, "conflict_error", "The row page could not be moved to that data source.",
			)
		}
		return notionDatabaseRowPageMutationError(event, err)
	}
	if err := recordConnectionStoredMark(event.App, admission.Identity, connectionAuthoredRow, admission.Authority.RowID, false, notionBlockEditMark(admission.Authority.UpdatedAt)); err != nil {
		return notionDatabaseRowPageMutationError(event, err)
	}
	response, err := loadNotionDatabaseRowPageResponse(
		event, admission.Identity, admission.Authority.RowID, permissionWorkspaceRead,
	)
	if err != nil {
		return notionDatabaseRowPageReadError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

func notionDatabaseRowPageMutationError(event *core.RequestEvent, err error) error {
	if isDLPPolicyDecisionError(err) {
		return notionDLPPolicyError(event, err)
	}
	if errors.Is(err, errDatabaseRowPageNotFound) || isPermissionDenied(err) {
		return notionPageNotFound(event)
	}
	if databaseRowTrashRefused(err) {
		return notionDatabaseRowPageInTrashError(event)
	}
	propertyType := &notionDatabaseRowPagePropertyTypeError{}
	if errors.As(err, &propertyType) {
		return notionPageValidationError(
			event,
			"Database property type is not supported by this Notion-compatible endpoint: "+propertyType.Type+".",
		)
	}
	if answered, refused := notionPropertyAccessError(event, err); refused {
		return answered
	}
	requestError := &graphRequestError{}
	if errors.As(err, &requestError) && (requestError.status == http.StatusBadRequest || requestError.precondition) {
		return notionPageValidationError(event, requestError.message)
	}
	rowRequestError := &notionDatabaseRowPageRequestError{}
	if errors.As(err, &rowRequestError) && rowRequestError.missingPage != "" {
		return notionRelationPageNotFound(event, rowRequestError.missingPage)
	}
	if errors.As(err, &rowRequestError) {
		return notionPageValidationError(event, rowRequestError.message)
	}
	missingUser := &notionDatabaseRowPageUserMissing{}
	if errors.As(err, &missingUser) {
		return notionCompatibilityError(event, http.StatusNotFound, notionErrorObjectNotFound, notionUserNotFoundMessage(missingUser.id))
	}
	if errors.Is(err, errDatabaseRowPageConflict) || errors.Is(err, errDatabaseMutationConflict) {
		return notionCompatibilityError(
			event, http.StatusConflict, "conflict_error", "The database row page changed during this request.",
		)
	}
	if errors.Is(err, errNotionDatabaseRowPageProjectionInvalid) {
		return notionCompatibilityError(
			event, http.StatusConflict, "conflict_error", "The database schema is not available in its current state.",
		)
	}
	return notionRequestNotCompleted(event, err)
}

// serveNotionDatabaseRowPageMoveAcross moves a row page to a data source on
// another page. Like Notion's, it brings the properties the target lacks and
// drops only the values that still can't come.
func serveNotionDatabaseRowPageMoveAcross(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	admission notionDatabaseRowPageAuthorization,
	target notionDatabaseRowPageAuthorization,
) error {
	identity := admission.Identity
	rowID := admission.Authority.RowID
	reference := newDiagnosticReference()
	_, err := ygo.applyDatabaseRowPageMoveAcross(event.Request.Context(), databaseRowPageMoveAcrossRequest{
		PrincipalID: identity.PrincipalID, RowID: rowID,
		MutationID: "notion-public-row-move:" + reference,
		RequestHash: databaseRowPageActionRequestHash("notion-move", rowID, databaseRowPageActionInput{
			RequestID: reference, TargetDataSourceID: target.Container.DataSource,
		}),
		SourcePageID: admission.Authority.ContainerPageID, TargetPageID: target.Container.PageID,
		TargetDataSourceID: target.Container.DataSource,
		Accept:             func(string) bool { return true },
	}, databaseRowPageMoveAccess{
		row: func(txApp core.App) (databaseRowPageAuthority, error) {
			current, err := authorizeNotionDatabaseRowPageCurrent(txApp, identity, rowID, permissionWorkspaceWrite)
			return current.Authority, err
		},
		source: func(txApp core.App, pageID string) error {
			current, err := authorizeNotionDatabaseRowPageCurrent(txApp, identity, rowID, permissionWorkspaceWrite)
			if err == nil && current.Page.Page != nil && current.Page.Page.Id != pageID && current.Authority.ContainerPageID != pageID {
				return errDatabaseRowPageConflict
			}
			return err
		},
		target: func(txApp core.App) (pageAuthorization, error) {
			current, err := authorizeNotionDataSourceParentCurrent(txApp, identity, target.Container.DataSource, permissionWorkspaceWrite)
			return current.Page, err
		},
	})
	if err != nil {
		if errors.Is(err, errDatabaseRowPageConflict) {
			return notionCompatibilityError(
				event, http.StatusConflict, "conflict_error", "The row page could not be moved to that data source.",
			)
		}
		return notionDatabaseRowPageMutationError(event, err)
	}
	if err := recordConnectionStoredMark(event.App, identity, connectionAuthoredRow, rowID, false, notionBlockEditMark(admission.Authority.UpdatedAt)); err != nil {
		return notionDatabaseRowPageMutationError(event, err)
	}
	response, err := loadNotionDatabaseRowPageResponse(event, identity, rowID, permissionWorkspaceRead)
	if err != nil {
		return notionDatabaseRowPageMutationError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}
