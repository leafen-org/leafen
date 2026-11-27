package app

import (
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

const (
	workspacesCollection = "leafen_workspaces"
	pagesCollection      = "leafen_pages"
	userStateCollection  = "leafen_user_state"
	receiptsCollection   = "leafen_mutation_receipts"

	errorCodeUnauthorized           = "LEAFEN_UNAUTHORIZED"
	errorCodeBootstrapFailed        = "LEAFEN_BOOTSTRAP_FAILED"
	errorCodeWorkspaceCreateFailed  = "LEAFEN_WORKSPACE_CREATE_FAILED"
	errorCodeWorkspaceNotFound      = "LEAFEN_WORKSPACE_NOT_FOUND"
	errorCodePageCreateFailed       = "LEAFEN_PAGE_CREATE_FAILED"
	errorCodePageNotFound           = "LEAFEN_PAGE_NOT_FOUND"
	errorCodePageBusy               = "LEAFEN_PAGE_BUSY"
	errorCodePageUpdateFailed       = "LEAFEN_PAGE_UPDATE_FAILED"
	errorCodeCursorConflict         = "LEAFEN_CURSOR_CONFLICT"
	errorCodeMutationIdentityReused = "LEAFEN_MUTATION_ID_REUSED"
	errorCodeAPIRouteNotFound       = "LEAFEN_API_ROUTE_NOT_FOUND"
)

type pageMutationInput struct {
	Content    string `json:"content"`
	MutationID string `json:"mutationId"`
	BaseCursor int    `json:"baseCursor"`
}

type mutationReceiptResponse struct {
	MutationID string `json:"mutationId"`
	Cursor     int    `json:"cursor"`
	Duplicate  bool   `json:"duplicate"`
}

type pageMutationResponse struct {
	Page    pageResponse            `json:"page"`
	Receipt mutationReceiptResponse `json:"receipt"`
}

func registerProductSchema(pb *pocketbase.PocketBase) {
	registerPageStyleMetadataHooks(pb)
	registerPersonRenameHooks(pb)
	registerMigrationAnswerHandOver(pb)
	pb.OnBootstrap().BindFunc(func(event *core.BootstrapEvent) error {
		// PocketBase empties its temporary folder as it starts: not while a
		// backup in another process writes there (data_folder_temp.go).
		release, err := holdTempLease(pb.DataDir(), true)
		if err != nil {
			return err
		}
		err = event.Next()
		release()
		if err != nil {
			return err
		}
		dropAbandonedLeafenTemp(pb.DataDir())
		if err := runProductMigrations(pb); err != nil {
			return err
		}
		// The deletion inventories name every table, and an index serves every
		// foreign key: a development build breaking either stops here, a release
		// build logs it (deletion_inventories_start.go, foreign_key_child_indexes.go).
		if err := checkDeletionInventoriesAtStart(pb); err != nil {
			return err
		}
		return checkForeignKeyChildIndexesAtStart(pb)
	})
}

func registerProductRoutes(
	event *core.ServeEvent,
	jobs *durableJobRunner,
	roomInvalidator workspaceRoomInvalidator,
	ygo *pocketBaseYgoPersistence,
) {
	event.Router.GET(apiRoot+"/bootstrap", bootstrapProduct)
	event.Router.POST(apiRoot+"/workspaces", createWorkspace(ygo))
	event.Router.POST(apiRoot+pageCreatePath, createPage(ygo)).
		Bind(apis.BodyLimit(pageCreationMaxBodyBytes))
	event.Router.GET(apiRoot+"/pages/{id}", readPage)
	event.Router.GET(apiRoot+"/pages/{id}/database-leftovers", readDatabasePageLeftovers)
	registerPageLinkTargetRoutes(event)
	registerPageCopyMentionRoutes(event)
	event.Router.PATCH(apiRoot+"/pages/{id}", updatePage)
	event.Router.POST(apiRoot+pageDuplicationStartPath, startPageDuplication).
		Bind(apis.BodyLimit(pageDuplicationMaxBodyBytes))
	event.Router.GET(apiRoot+pageDuplicationReadPath, readPageDuplication)
	registerPublicPageDuplicateRoutes(event)
	event.Router.POST(apiRoot+pageWorkspaceMoveStartPath, startPageWorkspaceMove).
		Bind(apis.BodyLimit(pageWorkspaceMoveMaxBodyBytes))
	event.Router.GET(apiRoot+pageWorkspaceMoveReadPath, readPageWorkspaceMove)
	event.Router.POST(apiRoot+blockMoveStartPath, startBlockMove).
		Bind(apis.BodyLimit(blockMoveMaxBodyBytes))
	event.Router.GET(apiRoot+blockMoveReadPath, readBlockMove)
	event.Router.POST(apiRoot+wikiMutationStartPath, startWikiMutation).
		Bind(apis.BodyLimit(wikiMutationMaxBodyBytes))
	event.Router.GET(apiRoot+wikiMutationReadPath, readWikiMutation)
	registerPageVerificationRoutes(event)
	// Generated pagePermanentDeletePath is "/pages/{id}/permanent-delete".
	event.Router.POST(
		apiRoot+pagePermanentDeletePath,
		deletePagePermanently(roomInvalidator),
	).Bind(apis.BodyLimit(pagePermanentDeleteMaxBodyBytes))
	event.Router.POST(apiRoot+pageTrashEmptyPath, emptyWorkspaceTrash(roomInvalidator)).
		Bind(apis.BodyLimit(pageTrashEmptyMaxBodyBytes))
	registerPageDeletionHoldRoutes(event, roomInvalidator)
	registerPageTrashLegalHoldRoutes(event)
	registerPageTrashListRoutes(event)
	registerAdministratorWorkspaceContentRoutes(event, roomInvalidator)
	registerProductFileRoutes(event)
	registerOperationAnswerDownloadRoute(event)
	registerProductFilePosterRoutes(event)
	registerCustomEmojiRoutes(event)
	registerPersonalAccessTokenRoutes(event)
	registerInternalConnectionRoutes(event)
	registerWorkspaceConnectionRoutes(event)
	registerWorkspacePublicContentRoutes(event)
	registerWorkspaceVerifiedPageRoutes(event)
	registerAccountDeveloperIDRoutes(event)
	registerWorkspaceConnectionPolicyRoutes(event)
	registerServerTunnelRoutes(event)
	registerWorkspacePeopleDirectoryRoutes(event)
	registerPeopleProfileRoutes(event)
	registerPeopleDatabaseRoutes(event)
	registerWorkspaceAllowedEmailDomainRoutes(event)
	registerProductFileUsageReportRoutes(event)
	registerProductFileManualCleanupRoutes(event)
	event.Router.POST(apiRoot+"/jobs", jobs.create)
	event.Router.GET(apiRoot+"/jobs/{id}", jobs.get)
	event.Router.POST(apiRoot+"/jobs/{id}/retry", jobs.retry)
	event.Router.POST(apiRoot+"/jobs/{id}/cancel", jobs.cancelJob)
	event.Router.POST(apiRoot+"/pages/{id}/content-graph/mutations", ygo.createContentGraphMutation)
	registerSyncedBlockRoutes(event, ygo)
	event.Router.GET(apiRoot+"/pages/{id}/content-graph/changes", ygo.readContentGraphChanges)
	event.Router.GET(apiRoot+"/pages/{id}/content-graph", readContentGraph)
	event.Router.GET(apiRoot+"/pages/{id}/content-graph/file-blocks", readContentGraphFileBlocks)
	event.Router.GET(apiRoot+"/pages/{id}/content-graph/external-revisions", readContentGraphExternalRevisions)
	event.Router.GET(apiRoot+"/workspaces/{workspaceId}/database-containers", readDatabaseContainerPicker)
	event.Router.GET(apiRoot+"/workspaces/{workspaceId}/form-views", readFormViewPicker)
	registerDatabaseRowMentionSearchRoute(event)
	registerPageBacklinksRoutes(event)
	registerPageHistoryRoutes(event, ygo)
	registerPageFavoriteRoutes(event)
	registerDatabaseFavoriteRoutes(event)
	registerFormFavoriteRoutes(event)
	registerMyTaskViewRoutes(event)
	registerSidebarLayoutRoutes(event)
	registerSidebarPresetRoutes(event)
	registerPageVisitRoutes(event)
	registerRecentEditRoutes(event)
	registerWorkspaceDocumentRoutes(event)
	registerPageLockRoutes(event, roomInvalidator, ygo)
	registerDatabaseRowPageRoutes(event, ygo)
	registerDatabaseRowTemplateApplyRoute(event, ygo)
	registerDatabasePageLayoutRoutes(event)
	registerDatabaseRowPageLockRoutes(event, roomInvalidator)
	registerPageLinkRoutes(event)
	registerDatabaseToolbarConfigRoutes(event, ygo)
	registerDatabaseSprintRoutes(event, ygo, roomInvalidator)
	registerDatabaseMyTasksRoute(event)
	registerDatabaseMyTaskSourceRoutes(event)
	registerDatabaseMyTaskActionRoutes(event, ygo)
	registerDatabaseQueryRoutes(event)
	registerDatabaseAutomationRoutes(event)
	registerWorkspaceMembershipRoutes(event, roomInvalidator)
	registerTeamspaceRoutes(event, roomInvalidator)
	registerWorkspaceSettingsRoutes(event)
	registerWorkspaceDeletionRoutes(event, roomInvalidator)
	registerAccountDeletionRoutes(event, roomInvalidator)
	registerWorkspaceChangeCatchupRoutes(event)
	registerOrganizationRoutes(event)
	registerOrganizationSharingSecurityRoutes(event)
	registerOrganizationWorkspaceListRoute(event)
	registerWorkspaceSearchRoutes(event)
	registerNotionImportRoutes(event, jobs)
	registerImportedPeopleMappingRoutes(event, ygo)
	registerWorkspaceImportRoutes(event, jobs)
	registerWorkspaceExportRoutes(event, jobs)
	registerPageAnalyticsRoutes(event)
	registerWorkspaceAnalyticsRoutes(event)
	registerCommentRoutes(event, ygo)
	registerPageSuggestionRoutes(event, ygo)
	registerNotificationRoutes(event)
	registerWorkspaceActivityRoutes(event)
	registerPageChildrenRoutes(event)
	registerPagePathRoutes(event)
	registerBundleSourceRoutes(event)
	registerPublicContentRoutes(event)
	registerPublicContentFileRoutes(event)
	registerPublicCustomEmojiRoutes(event)
	registerPublicMutationRoutes(event)
	registerFormRoutes(event, ygo)
	registerRuntimeConfigRoute(event)
	registerServerIdentityRoute(event)
	registerCoverImageRoutes(event)
	registerPlaceSearchRoutes(event)
	registerAccountLanguageRoutes(event)
	registerAccountDisplayPreferenceRoutes(event)
	registerNotificationPreferenceRoutes(event)
	registerPublicSiteDocumentRoutes(event)
	registerPublicLinkPreviewRoutes(event)
	registerAccountProfileRoutes(event)
	registerAccountAvatarRoutes(event)
	registerAccountPhotoRoutes(event)
	registerSwitcherListRoutes(event)
}

func readPage(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("id"))
	if event.Request.URL.Query().Get("accessOnly") == "1" {
		return readPageActionAccess(event, user.Id, pageID)
	}
	// A read: what it decides about who may see what is kept for it
	// (access_memo.go); it read the workspace three times (2026-10-17).
	app := withAccessMemo(event.App)
	authorization, err := authorizePage(app, user.Id, pageID, permissionWorkspaceRead)
	if isGuestMFARequired(err) {
		return guestMFAAPIError(event)
	}
	if isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodePageNotFound, "Page not found.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodePageCreateFailed, "Couldn't read the page.", err)
	}
	response, err := pageRecordResponseWithWiki(app, authorization.Page)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodePageCreateFailed, "Couldn't read the page.", err)
	}
	// A page's title naming a page closed to its reader (content_graph_reader_mentions.go).
	pageResponseAsRead(app, user.Id, &response)
	return event.JSON(http.StatusOK, response)
}

func updatePage(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("id"))
	var input pageMutationInput
	// The content is what this change writes: a request without it changes
	// nothing rather than emptying it, as a page metadata change keeps what
	// it leaves out (page_metadata_sent_fields.go).
	var sent struct {
		Content *string `json:"content"`
	}
	if err := rereadableRequestBody(event); err != nil || event.BindBody(&input) != nil || event.BindBody(&sent) != nil ||
		sent.Content == nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the page change details.")
	}
	input.MutationID = strings.TrimSpace(input.MutationID)
	if pageID == "" || input.MutationID == "" || len([]rune(input.MutationID)) > 255 || len([]rune(input.Content)) > 2_000_000 || input.BaseCursor < 0 {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the page change details.")
	}

	response, status, err := applyPageMutationWithLock(event.App, user.Id, pageID, input, pageLockEnforced)
	if status == pageMutationPageLocked {
		return answerLockRefusal(event, pageLockedError())
	}
	if status == pageMutationPageMissing {
		return apiError(event, http.StatusNotFound, errorCodePageNotFound, "Page not found.")
	}
	if status == pageMutationIdentityReused {
		return apiError(event, http.StatusConflict, errorCodeMutationIdentityReused, "This ID was used for a different change.")
	}
	if status == pageMutationCursorConflict {
		return apiError(event, http.StatusConflict, errorCodeCursorConflict, pageCursorConflictMessage)
	}
	if status == pageMutationReplacementLocked {
		return apiError(
			event, http.StatusConflict, errorCodeWorkspaceImportTargetNotFresh,
			"Other changes can't be applied while the page content is being replaced.",
		)
	}
	if isDLPPolicyDecisionError(err) {
		return writeDLPPolicyError(event, err, "page-update")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodePageUpdateFailed, "Couldn't save the page.", err)
	}
	// The page's title as its writer reads it (page_name_mentions.go).
	pageResponseAsRead(event.App, user.Id, &response.Page)
	return event.JSON(http.StatusOK, response)
}

func pageRecordResponse(record *core.Record) pageResponse {
	return pageResponse{
		ID:                     record.Id,
		WorkspaceID:            record.GetString("workspace"),
		TeamspaceID:            record.GetString("teamspace_id"),
		Title:                  record.GetString("title"),
		Content:                record.GetString("content"),
		Revision:               record.GetInt("revision"),
		Cursor:                 record.GetInt("cursor"),
		ParentID:               record.GetString("parent_id"),
		Position:               record.GetFloat("position"),
		IsLocked:               record.GetBool("is_locked"),
		LockedBy:               strings.TrimSpace(record.GetString("locked_by")),
		InTrash:                record.GetBool("in_trash"),
		TrashedAt:              pageTrashTimestamp(record),
		ArchivedAt:             pageArchiveTimestamp(record),
		ArchivedBy:             strings.TrimSpace(record.GetString("archived_by")),
		Icon:                   record.GetString("icon"),
		IconType:               record.GetString("icon_type"),
		Cover:                  record.GetString("cover"),
		CoverPosition:          record.GetInt("cover_position"),
		Font:                   record.GetString("font"),
		SmallText:              record.GetBool("small_text"),
		FullWidth:              record.GetBool("full_width"),
		BacklinksDisplay:       record.GetString("backlinks_display"),
		PageCommentsDisplay:    record.GetString("page_comments_display"),
		TableOfContentsDisplay: pageTableOfContentsDisplay(record), InlineCommentsDisplay: pageInlineCommentsDisplay(record),
		Kind: pageRecordKind(record),
		// Who made it and edited it last, and when, as the app's start reads
		// them (2026-11-11: a new workspace's first page, answered here, had
		// no maker in its menu's activity until the page list was read again).
		CreatedBy:    record.GetString("created_by"),
		LastEditedBy: record.GetString("last_edited_by"),
		CreatedAt:    record.GetString("created"),
		UpdatedAt:    record.GetString("updated"),
		IsWiki:       record.GetBool("is_wiki"),
		WikiRootID: func() string {
			if record.GetBool("is_wiki") {
				return record.Id
			}
			return ""
		}(),
		VerifiedAt:            record.GetString("verified_at"),
		VerifiedBy:            record.GetString("verified_by"),
		VerificationExpiresAt: record.GetString("verification_expires_at"),
		WikiRevision:          record.GetInt("wiki_revision"),
		// A database's page, read as a page, says so: it opens as its row's
		// page, not as a page of the page list (2026-11-14: one another tab
		// moved into a database stayed drawn as a page; a wiki's page is a
		// page, wiki_read.go).
		DatabaseRow: isDatabaseRowPageRecord(record),
	}
}

func authenticatedUser(event *core.RequestEvent) (*core.Record, bool) {
	if event.Auth == nil || event.Auth.Collection().Name != "users" {
		return nil, false
	}
	return event.Auth, true
}
