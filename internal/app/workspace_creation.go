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

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedWorkspaceCreationReceipt struct {
	RequestHash string `db:"request_hash"`
	WorkspaceID string `db:"workspace_id"`
	PageID      string `db:"page_id"`
}

func workspaceCreationRequestHash(input workspaceCreateInput) (string, error) {
	payload, err := json.Marshal(struct {
		Name                   string                     `json:"name"`
		SkipDefaultPages       bool                       `json:"skipDefaultPages"`
		WelcomePage            *workspaceWelcomePageInput `json:"welcomePage,omitempty"`
		EnsureInitialWorkspace bool                       `json:"ensureInitialWorkspace,omitzero"`
	}{
		Name: input.Name, SkipDefaultPages: input.SkipDefaultPages,
		WelcomePage:            input.WelcomePage,
		EnsureInitialWorkspace: input.EnsureInitialWorkspace,
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

// workspaceNameLanguage is the language the server names in that a
// workspace's language tag reads as ("ko-KR" as Korean, any other as
// English), or "" for none.
func workspaceNameLanguage(tag string) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	switch {
	case tag == "":
		return ""
	case tag == accountLanguageKo || strings.HasPrefix(tag, accountLanguageKo+"-"):
		return accountLanguageKo
	default:
		return accountLanguageEn
	}
}

func readWorkspaceCreationReceipt(app core.App, ownerID, requestID string) (storedWorkspaceCreationReceipt, error) {
	var receipt storedWorkspaceCreationReceipt
	err := app.DB().NewQuery(`
		SELECT [[request_hash]], [[workspace_id]], [[page_id]]
		FROM {{leafen_workspace_creation_receipts}}
		WHERE [[owner_id]] = {:ownerId} AND [[request_id]] = {:requestId}
	`).Bind(dbx.Params{"ownerId": ownerID, "requestId": requestID}).One(&receipt)
	return receipt, err
}

func workspaceCreationResponseFromReceipt(
	app core.App,
	receipt storedWorkspaceCreationReceipt,
	duplicate bool,
) (workspaceCreateResponse, error) {
	workspace, err := app.FindRecordById(workspacesCollection, receipt.WorkspaceID)
	if err != nil {
		return workspaceCreateResponse{}, err
	}
	response := workspaceCreateResponse{
		ID:             workspace.Id,
		OrganizationID: workspace.GetString("organization"),
		Name:           workspace.GetString("name"),
		Duplicate:      duplicate,
	}
	if receipt.PageID == "" {
		return response, nil
	}
	page, err := app.FindRecordById(pagesCollection, receipt.PageID)
	if err != nil {
		return workspaceCreateResponse{}, err
	}
	pageResponse := pageRecordResponse(page)
	response.Page = &pageResponse
	return response, nil
}

func createWorkspaceStarterPage(
	app core.App,
	organizationID, ownerID, workspaceID string,
	welcome *workspaceWelcomePageInput,
) (*pageResponse, error) {
	nextCursor, err := nextWorkspaceUserStateCursor(app, ownerID)
	if err != nil {
		return nil, err
	}

	pages, err := app.FindCollectionByNameOrId(pagesCollection)
	if err != nil {
		return nil, err
	}
	page := core.NewRecord(pages)
	page.Id = newPageID()
	page.Set("owner", ownerID)
	page.Set("workspace", workspaceID)
	page.Set("title", "")
	page.Set("content", "")
	if welcome != nil {
		page.Set("title", welcome.Title)
		page.Set("icon", workspaceWelcomeIcon)
		page.Set("icon_type", "emoji")
	}
	page.Set("revision", 0)
	page.Set("cursor", nextCursor)
	page.Set("parent_id", "")
	page.Set("position", 1)
	applyPageTrashState(page, false, "")
	initializePageStyleMetadata(page)
	initializePageCustomizationMetadata(page)
	setNewPageCustodianProvenance(page, ownerID)
	createdAt := time.Now().UTC().UnixMilli()
	if err := app.Save(page); err != nil {
		return nil, err
	}
	if err := followCreatedPageComments(app, workspaceID, page.Id, ownerID, createdAt); err != nil {
		return nil, err
	}
	if err := recordNotionAdminLegalHoldPageInteraction(app, pageCreatedInteraction(
		organizationID, workspaceID, page.Id, ownerID, createdAt,
	)); err != nil {
		return nil, err
	}
	if err := markSearchDirty(app, page); err != nil {
		return nil, err
	}
	response := pageRecordResponse(page)
	return &response, nil
}

func createWorkspace(ygo *pocketBaseYgoPersistence) func(*core.RequestEvent) error {
	return func(event *core.RequestEvent) error {
		return createWorkspaceWithWelcome(event, ygo)
	}
}

func createWorkspaceWithWelcome(event *core.RequestEvent, ygo *pocketBaseYgoPersistence) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	var input workspaceCreateInput
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the workspace details.")
	}
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.Name = strings.TrimSpace(input.Name)
	if input.RequestID == "" || len(input.RequestID) > maxWorkspaceCreationRequestIDLength ||
		input.Name == "" || len([]rune(input.Name)) > maxWorkspaceCreationNameRunes {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the workspace details.")
	}
	welcome, valid := normalizeWorkspaceWelcomePage(input.WelcomePage)
	if !valid {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the workspace details.")
	}
	// A workspace that starts from an import has no welcome page.
	if input.SkipDefaultPages {
		welcome = nil
	}
	input.WelcomePage = welcome
	requestHash, err := workspaceCreationRequestHash(input)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceCreateFailed, "Couldn't create the workspace.", err)
	}

	// However the workspace is made, one that starts empty opens on the welcome page.
	if welcome == nil && !input.SkipDefaultPages {
		language := workspaceNameLanguage(input.Language)
		if language == "" {
			language = generatedNameLanguage(withRequestLanguage(event.Request.Context(), event.Request), event.App, user.Id)
		}
		welcome = workspaceWelcomeTextFor(input.Language, language)
	}

	language := workspaceNameLanguage(input.Language)
	if language == "" {
		language = generatedNameLanguage(withRequestLanguage(event.Request.Context(), event.Request), event.App, user.Id)
	}
	var response workspaceCreateResponse
	identityReused := false
	err = event.App.RunInTransaction(func(txApp core.App) error {
		var createErr error
		response, identityReused, createErr = createWorkspaceRecords(txApp, user, input, requestHash, welcome, language)
		return createErr
	})
	if identityReused {
		return apiError(event, http.StatusConflict, errorCodeWorkspaceCreateIdentityReused, "The same workspace creation request can't be reused with different content.")
	}
	if err != nil {
		receipt, readErr := readWorkspaceCreationReceipt(event.App, user.Id, input.RequestID)
		if readErr == nil && receipt.RequestHash == requestHash {
			response, readErr = workspaceCreationResponseFromReceipt(event.App, receipt, true)
			if readErr == nil {
				writeReplayedWorkspaceWelcomePage(event.Request.Context(), ygo, user.Id, response, welcome)
				return event.JSON(http.StatusOK, response)
			}
		}
		if errors.Is(err, errWorkspaceCreationRestricted) {
			return apiError(event, http.StatusForbidden, errorCodeWorkspaceCreationRestricted,
				"Your organization lets only its owners and admins create workspaces.")
		}
		if storageError, ok := persistentWriteStorageAPIError(
			event, err, persistentWriteWorkspaceCreate, "transaction",
		); ok {
			return storageError
		}
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceCreateFailed, "Couldn't create the workspace.", err)
	}
	if response.Duplicate {
		writeReplayedWorkspaceWelcomePage(event.Request.Context(), ygo, user.Id, response, welcome)
		return event.JSON(http.StatusOK, response)
	}
	if response.Page != nil {
		if err := writeWorkspaceWelcomePage(event.Request.Context(), ygo, user.Id, response.Page.ID, welcome); err != nil {
			return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceCreateFailed,
				"Couldn't create the workspace.", "workspace_create", "welcome_page", errorCodeWorkspaceCreateFailed, err)
		}
	}
	return event.JSON(http.StatusCreated, response)
}

// createWorkspaceRecords makes the workspace, its organization where the
// person has none, and its starter page, once for a request id: what POST
// /workspaces does inside its transaction, and what the first run does for
// a first administrator an agent's conversation started
// (workspace_initial.go). The language names the organization; the welcome
// body is written after the transaction (writeWorkspaceWelcomePage).
func createWorkspaceRecords(txApp core.App, user *core.Record, input workspaceCreateInput, requestHash string,
	welcome *workspaceWelcomePageInput, language string) (response workspaceCreateResponse, identityReused bool, err error) {
	receipt, readErr := readWorkspaceCreationReceipt(txApp, user.Id, input.RequestID)
	if readErr == nil {
		if receipt.RequestHash != requestHash {
			return response, true, nil
		}
		response, readErr = workspaceCreationResponseFromReceipt(txApp, receipt, true)
		return response, false, readErr
	}
	if !errors.Is(readErr, sql.ErrNoRows) {
		return response, false, readErr
	}
	if input.EnsureInitialWorkspace {
		var reused bool
		response, reused, readErr = existingInitialWorkspace(txApp, user.Id)
		if readErr != nil || reused {
			return response, false, readErr
		}
	}

	if createErr := authorizeDomainWorkspaceCreation(txApp, user); createErr != nil {
		return response, false, createErr
	}
	// An organization made with the first workspace is named in the
	// workspace's language, which the first setup sends before it saves
	// the person's choice; otherwise as the server names what it makes.
	organization, createErr := ensureOwnerOrganization(txApp, user.Id, language)
	if createErr != nil {
		return response, false, createErr
	}
	collection, createErr := txApp.FindCollectionByNameOrId(workspacesCollection)
	if createErr != nil {
		return response, false, createErr
	}
	workspace := core.NewRecord(collection)
	workspace.Set("owner", user.Id)
	workspace.Set("workspace_owner", user.Id)
	workspace.Set("organization", organization.Id)
	workspace.Set("name", input.Name)
	if createErr := txApp.Save(workspace); createErr != nil {
		return response, false, createErr
	}
	if createErr := initializeWorkspacePermissionCursor(
		txApp, workspace.Id, time.Now().UTC().UnixMilli(),
	); createErr != nil {
		return response, false, createErr
	}
	if createErr := initializePageInteractionRevision(txApp, workspace.Id); createErr != nil {
		return response, false, createErr
	}
	// Its People database is made with it, once it commits (people_database_every_workspace.go).
	notePeopleChangedAfterCommit(txApp, workspace.Id)
	if createErr := bumpOrganizationWorkspaceSetRevision(txApp, organization.Id); createErr != nil {
		return response, false, createErr
	}
	if createErr := recordOrganizationAudit(
		txApp, organization.Id, user.Id, "workspace.created", "workspace", workspace.Id,
	); createErr != nil {
		return response, false, createErr
	}
	var page *pageResponse
	if !input.SkipDefaultPages {
		page, createErr = createWorkspaceStarterPage(
			txApp, organization.Id, user.Id, workspace.Id, welcome,
		)
		if createErr != nil {
			return response, false, createErr
		}
	}
	pageID := ""
	if page != nil {
		pageID = page.ID
	}
	_, createErr = txApp.DB().Insert(workspaceCreationReceiptsTable, dbx.Params{
		"owner_id": user.Id, "request_id": input.RequestID, "request_hash": requestHash,
		"workspace_id": workspace.Id, "page_id": pageID, "created": time.Now().UnixMilli(),
	}).Execute()
	if createErr != nil {
		return response, false, createErr
	}
	return workspaceCreateResponse{
		ID: workspace.Id, OrganizationID: organization.Id, Name: input.Name, Page: page,
	}, false, nil
}

// writeReplayedWorkspaceWelcomePage finishes a welcome body a failed first
// attempt left unwritten. A replay never fails the creation it repeats: the
// page may since have been changed or trashed.
func writeReplayedWorkspaceWelcomePage(
	ctx context.Context,
	ygo *pocketBaseYgoPersistence,
	ownerID string,
	response workspaceCreateResponse,
	welcome *workspaceWelcomePageInput,
) {
	if response.Page != nil {
		_ = writeWorkspaceWelcomePage(ctx, ygo, ownerID, response.Page.ID, welcome)
	}
}
