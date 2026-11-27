package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

const workspaceSettingsReceiptLimit = 100

var (
	errWorkspaceSettingsInvalid          = errors.New(errorCodeWorkspaceSettingsInvalid)
	errWorkspaceSettingsIdentityReused   = errors.New(errorCodeWorkspaceSettingsIdentityReused)
	errWorkspaceSettingsRevisionConflict = errors.New(errorCodeWorkspaceSettingsRevisionConflict)
	errWorkspaceSettingsDomainInUse      = errors.New(errorCodeWorkspaceSettingsDomainInUse)
)

type storedWorkspaceSettingsReceipt struct {
	RequestHash  string `db:"request_hash"`
	ResponseJSON string `db:"response_json"`
}

func registerWorkspaceSettingsRoutes(event *core.ServeEvent) {
	event.Router.Route(
		workspaceSettingsUpdateMethod,
		apiRoot+workspaceSettingsUpdatePath,
		updateWorkspaceSettings,
	).Bind(apis.BodyLimit(workspaceSettingsMaxBodyBytes))
}

func stringPointer(value string) *string {
	return &value
}

func boolPointer(value bool) *bool {
	return &value
}

func (response workspaceResponse) MarshalJSON() ([]byte, error) {
	if response.SettingsRevision == 0 {
		return json.Marshal(struct {
			ID                                    string `json:"id"`
			OrganizationID                        string `json:"organizationId"`
			Name                                  string `json:"name"`
			Role                                  string `json:"role,omitempty"`
			Guest                                 bool   `json:"guest,omitempty"`
			PendingPrivatePages                   bool   `json:"pendingPrivatePages,omitzero"`
			Restricted                            bool   `json:"restricted,omitempty"`
			NoPrivatePages                        bool   `json:"noPrivatePages,omitzero"`
			MemberCount                           int    `json:"memberCount,omitempty"`
			AppInstallID                          string `json:"appInstallId,omitempty"`
			AppSlug                               string `json:"appSlug,omitempty"`
			Staging                               bool   `json:"staging,omitzero"`
			PageAnalyticsEnabled                  *bool  `json:"pageAnalyticsEnabled,omitempty"`
			PageAnalyticsAnonymousTrackingEnabled *bool  `json:"pageAnalyticsAnonymousTrackingEnabled,omitempty"`
			TeamspaceRevision                     *int   `json:"teamspaceRevision,omitempty"`
		}{
			ID: response.ID, OrganizationID: response.OrganizationID,
			Name: response.Name, Role: response.Role, Guest: response.Guest, PendingPrivatePages: response.PendingPrivatePages,
			Restricted: response.Restricted, NoPrivatePages: response.NoPrivatePages, MemberCount: response.MemberCount,
			AppInstallID:                          response.AppInstallID,
			AppSlug:                               response.AppSlug,
			Staging:                               response.Staging,
			PageAnalyticsEnabled:                  response.PageAnalyticsEnabled,
			PageAnalyticsAnonymousTrackingEnabled: response.PageAnalyticsAnonymousTrackingEnabled,
			TeamspaceRevision:                     response.TeamspaceRevision,
		})
	}
	icon, domain := "", ""
	if response.Icon != nil {
		icon = *response.Icon
	}
	if response.Domain != nil {
		domain = *response.Domain
	}
	// The landing page is always said, empty when none, so a reader keeping
	// the workspace merges one let go rather than keeping the old one.
	return json.Marshal(struct {
		ID                                    string `json:"id"`
		OrganizationID                        string `json:"organizationId"`
		Name                                  string `json:"name"`
		Icon                                  string `json:"icon"`
		Domain                                string `json:"domain"`
		SettingsRevision                      int    `json:"settingsRevision"`
		Role                                  string `json:"role,omitempty"`
		Guest                                 bool   `json:"guest,omitempty"`
		PendingPrivatePages                   bool   `json:"pendingPrivatePages,omitzero"`
		Restricted                            bool   `json:"restricted,omitempty"`
		NoPrivatePages                        bool   `json:"noPrivatePages,omitzero"`
		MemberCount                           int    `json:"memberCount,omitempty"`
		LandingPageID                         string `json:"landingPageId"`
		AppInstallID                          string `json:"appInstallId,omitempty"`
		AppSlug                               string `json:"appSlug,omitempty"`
		Staging                               bool   `json:"staging,omitzero"`
		PageAnalyticsEnabled                  *bool  `json:"pageAnalyticsEnabled,omitempty"`
		PageAnalyticsAnonymousTrackingEnabled *bool  `json:"pageAnalyticsAnonymousTrackingEnabled,omitempty"`
		TeamspaceRevision                     *int   `json:"teamspaceRevision,omitempty"`
	}{
		ID: response.ID, OrganizationID: response.OrganizationID, Name: response.Name,
		Icon: icon, Domain: domain, SettingsRevision: response.SettingsRevision, Role: response.Role,
		Guest: response.Guest, PendingPrivatePages: response.PendingPrivatePages, Restricted: response.Restricted, NoPrivatePages: response.NoPrivatePages, MemberCount: response.MemberCount, LandingPageID: response.LandingPageID,
		AppInstallID:                          response.AppInstallID,
		AppSlug:                               response.AppSlug,
		Staging:                               response.Staging,
		PageAnalyticsEnabled:                  response.PageAnalyticsEnabled,
		PageAnalyticsAnonymousTrackingEnabled: response.PageAnalyticsAnonymousTrackingEnabled,
		TeamspaceRevision:                     response.TeamspaceRevision,
	})
}

// workspaceSettingsRefused refuses a workspace settings change for the reason
// given (request_refusal.go).
func workspaceSettingsRefused(format string, args ...any) error {
	return refuse(errWorkspaceSettingsInvalid, format, args...)
}

func normalizeWorkspaceSettingsInput(input *workspaceSettingsInput, request *http.Request) error {
	input.RequestID = strings.TrimSpace(input.RequestID)
	switch {
	case input.RequestID == "" || len([]rune(input.RequestID)) > maxWorkspaceSettingsRequestIDLength:
		return workspaceSettingsRefused("requestId is a new id of yours, 1–%d characters, so the same change sent again isn't made twice", maxWorkspaceSettingsRequestIDLength)
	case input.BaseRevision < 0:
		return workspaceSettingsRefused("baseRevision is the settings' revision as GET reads it, 0 or more")
	case input.Patch.Name == nil && input.Patch.Icon == nil && input.Patch.Domain == nil && input.Patch.LandingPageID == nil:
		return workspaceSettingsRefused("patch is empty: name at least one of name, icon, domain and landingPageId")
	}
	if input.Patch.Name != nil {
		name := strings.TrimSpace(*input.Patch.Name)
		if name == "" || len([]rune(name)) > maxWorkspaceCreationNameRunes {
			return workspaceSettingsRefused("patch.name is 1–%d characters", maxWorkspaceCreationNameRunes)
		}
		input.Patch.Name = stringPointer(name)
	}
	if input.Patch.Icon != nil {
		// A picture uploaded as the icon is kept by its address alone
		// (held_icon_files.go); a file's other addresses aren't icons.
		icon := strings.TrimSpace(*input.Patch.Icon)
		_, uploaded := heldIconFileID(icon)
		if len([]rune(icon)) > maxWorkspaceSettingsIconRunes {
			return workspaceSettingsRefused("patch.icon is at most %d characters", maxWorkspaceSettingsIconRunes)
		}
		if !uploaded && isManagedFileMetadataLocator(icon, request) {
			return workspaceSettingsRefused("patch.icon names a file by an address that isn't an icon's: upload the picture as the icon")
		}
		input.Patch.Icon = stringPointer(icon)
	}
	if input.Patch.LandingPageID != nil {
		pageID := strings.TrimSpace(*input.Patch.LandingPageID)
		if len(pageID) > maxWorkspacePermissionIdentifierLength || strings.ContainsAny(pageID, " \t\r\n/") {
			return workspaceSettingsRefused("patch.landingPageId is a page ID, or empty for none")
		}
		input.Patch.LandingPageID = stringPointer(pageID)
	}
	if input.Patch.Domain != nil {
		domain, err := normalizeWorkspaceSettingsDomain(*input.Patch.Domain)
		if err != nil {
			return err
		}
		input.Patch.Domain = stringPointer(domain)
	}
	return nil
}

func normalizeWorkspaceSettingsDomain(raw string) (string, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return "", nil
	}
	var normalized strings.Builder
	for _, character := range raw {
		allowed := character >= 'a' && character <= 'z' || character >= '0' && character <= '9'
		if allowed {
			normalized.WriteRune(character)
			continue
		}
		if character != '-' && !unicode.IsSpace(character) {
			continue
		}
		value := normalized.String()
		if value != "" && !strings.HasSuffix(value, "-") {
			normalized.WriteByte('-')
		}
	}
	domain := strings.Trim(normalized.String(), "-")
	if domain == "" || len([]rune(domain)) > maxWorkspaceSettingsDomainRunes {
		return "", workspaceSettingsRefused("patch.domain is 1–%d letters, digits and hyphens", maxWorkspaceSettingsDomainRunes)
	}
	return domain, nil
}

func workspaceSettingsRequestHash(input workspaceSettingsInput) (string, error) {
	payload, err := json.Marshal(struct {
		BaseRevision int                    `json:"baseRevision"`
		Patch        workspaceSettingsPatch `json:"patch"`
	}{BaseRevision: input.BaseRevision, Patch: input.Patch})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func readWorkspaceSettingsReceipt(
	app core.App,
	workspaceID, actorID, requestID string,
) (storedWorkspaceSettingsReceipt, error) {
	var receipt storedWorkspaceSettingsReceipt
	err := app.DB().NewQuery(`
		SELECT [[request_hash]], [[response_json]]
		FROM {{leafen_workspace_settings_receipts}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[actor_id]] = {:actorId}
			AND [[request_id]] = {:requestId}
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "actorId": actorID, "requestId": requestID,
	}).One(&receipt)
	return receipt, err
}

func nextWorkspaceSettingsReceiptCreated(app core.App, workspaceID string, floor int64) (int64, error) {
	var newest int64
	err := app.DB().NewQuery(`
		SELECT COALESCE(MAX([[created]]), -1)
		FROM {{leafen_workspace_settings_receipts}}
		WHERE [[workspace_id]] = {:workspaceId}
	`).Bind(dbx.Params{"workspaceId": workspaceID}).Row(&newest)
	if err != nil {
		return 0, err
	}
	if newest >= floor {
		return newest + 1, nil
	}
	return floor, nil
}

func pruneWorkspaceSettingsReceipts(app core.App, workspaceID string) error {
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_workspace_settings_receipts}}
		WHERE [[rowid]] IN (
			SELECT [[rowid]] FROM {{leafen_workspace_settings_receipts}}
			WHERE [[workspace_id]] = {:workspaceId}
			ORDER BY [[created]] DESC, [[request_id]] DESC, [[actor_id]] DESC
			LIMIT -1 OFFSET {:receiptLimit}
		)
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "receiptLimit": workspaceSettingsReceiptLimit,
	}).Execute()
	return err
}

func readWorkspaceSettingsRevision(app core.App, workspaceID string) (int, error) {
	var revision int
	err := app.DB().NewQuery(`
		SELECT [[revision]] FROM {{leafen_workspace_settings_state}}
		WHERE [[workspace_id]] = {:workspaceId}
	`).Bind(dbx.Params{"workspaceId": workspaceID}).Row(&revision)
	return revision, err
}

func workspaceSettingsResponseFor(
	workspace *core.Record,
	requestID string,
	revision int,
) workspaceSettingsMutationResponse {
	policy := pageAnalyticsPolicy(workspace)
	return workspaceSettingsMutationResponse{
		Workspace: workspaceResponse{
			ID: workspace.Id, OrganizationID: workspace.GetString("organization"),
			Name: workspace.GetString("name"), Icon: stringPointer(workspace.GetString("icon")),
			Domain: stringPointer(workspace.GetString("domain")), SettingsRevision: revision,
			LandingPageID:                         workspace.GetString(workspaceLandingPageField),
			PageAnalyticsEnabled:                  boolPointer(policy.AnalyticsEnabled),
			PageAnalyticsAnonymousTrackingEnabled: boolPointer(policy.AnonymousTrackingEnabled),
		},
		Receipt: workspaceSettingsReceipt{RequestID: requestID, Revision: revision},
	}
}

func updateWorkspaceSettings(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	var input workspaceSettingsInput
	refused := func(err error) error {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceSettingsInvalid, refusalMessage("Check the workspace settings.", refusalReason(err)))
	}
	if err := event.BindBody(&input); err != nil {
		return refused(bodyRefusal(errWorkspaceSettingsInvalid, "workspace settings change", err))
	}
	if workspaceID == "" || len(workspaceID) > maxWorkspacePermissionIdentifierLength {
		return refused(workspaceSettingsRefused("the workspace ID in the path is 1–%d characters", maxWorkspacePermissionIdentifierLength))
	}
	if err := normalizeWorkspaceSettingsInput(&input, event.Request); err != nil {
		return refused(err)
	}
	requestHash, err := workspaceSettingsRequestHash(input)
	if err != nil {
		return workspaceSettingsInternalError(event, err)
	}

	var response workspaceSettingsMutationResponse
	err = event.App.RunInTransaction(func(txApp core.App) error {
		authorization, err := authorizeWorkspace(txApp, user.Id, workspaceID, permissionWorkspaceOwner)
		if err != nil {
			return err
		}
		receipt, err := readWorkspaceSettingsReceipt(txApp, workspaceID, user.Id, input.RequestID)
		if err == nil {
			if receipt.RequestHash != requestHash {
				return errWorkspaceSettingsIdentityReused
			}
			if err := json.Unmarshal([]byte(receipt.ResponseJSON), &response); err != nil {
				return err
			}
			response.Receipt.Duplicate = true
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		currentRevision, err := readWorkspaceSettingsRevision(txApp, workspaceID)
		if err != nil {
			return err
		}
		if currentRevision != input.BaseRevision {
			return errWorkspaceSettingsRevisionConflict
		}

		workspace := authorization.Workspace
		changed := false
		if input.Patch.Name != nil && workspace.GetString("name") != *input.Patch.Name {
			workspace.Set("name", *input.Patch.Name)
			changed = true
		}
		previousIcon, iconChanged := workspace.GetString("icon"), false
		if input.Patch.Icon != nil && previousIcon != *input.Patch.Icon {
			if !checkHeldIconFile(txApp, workspaceID, *input.Patch.Icon) {
				return workspaceSettingsRefused("patch.icon names no image file of the workspace: upload it first, or send an emoji")
			}
			workspace.Set("icon", *input.Patch.Icon)
			changed, iconChanged = true, true
		}
		if input.Patch.LandingPageID != nil && workspace.GetString(workspaceLandingPageField) != *input.Patch.LandingPageID {
			if *input.Patch.LandingPageID != "" {
				usable, err := workspaceLandingPageUsable(txApp, user.Id, workspaceID, *input.Patch.LandingPageID)
				if err != nil {
					return err
				}
				if !usable {
					return workspaceSettingsRefused("patch.landingPageId names no page of the workspace, out of the trash, that you can open")
				}
			}
			workspace.Set(workspaceLandingPageField, *input.Patch.LandingPageID)
			changed = true
		}
		if input.Patch.Domain != nil && workspace.GetString("domain") != *input.Patch.Domain {
			if *input.Patch.Domain != "" {
				var matches int
				if err := txApp.DB().NewQuery(`
					SELECT COUNT(*) FROM {{leafen_workspaces}}
					WHERE [[domain]] = {:domain} AND [[domain]] != '' AND [[id]] != {:workspaceId}
				`).Bind(dbx.Params{
					"domain": *input.Patch.Domain, "workspaceId": workspaceID,
				}).Row(&matches); err != nil {
					return err
				}
				if matches != 0 {
					return errWorkspaceSettingsDomainInUse
				}
			}
			workspace.Set("domain", *input.Patch.Domain)
			changed = true
		}

		now := time.Now().UTC().UnixMilli()
		nextRevision := currentRevision
		if changed {
			nextRevision++
			result, err := txApp.DB().NewQuery(`
				UPDATE {{leafen_workspace_settings_state}}
				SET [[revision]] = {:nextRevision}, [[updated]] = {:updated}
				WHERE [[workspace_id]] = {:workspaceId} AND [[revision]] = {:baseRevision}
			`).Bind(dbx.Params{
				"nextRevision": nextRevision, "updated": now,
				"workspaceId": workspaceID, "baseRevision": currentRevision,
			}).Execute()
			if err != nil {
				return err
			}
			rows, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if rows != 1 {
				return errWorkspaceSettingsRevisionConflict
			}
			if err := txApp.Save(workspace); err != nil {
				if input.Patch.Domain != nil && isSQLiteConstraintError(err) {
					return errWorkspaceSettingsDomainInUse
				}
				return err
			}
			// Open screens of the workspace follow its new name or icon.
			noteWorkspaceLookChangedAfterCommit(txApp, workspaceID)
			// The picture the icon showed goes once nothing shows it.
			if iconChanged {
				if err := settleHeldIconFiles(txApp, workspaceID, previousIcon, workspace.GetString("icon")); err != nil {
					return err
				}
			}
			if err := recordOrganizationAuditEvent(txApp, organizationAuditEvent{
				ID: organizationAuditID(
					authorization.Organization.OrganizationID,
					"workspace.settings.update",
					workspaceID+"\x00"+input.RequestID,
				),
				OrganizationID: authorization.Organization.OrganizationID,
				WorkspaceID:    workspaceID, ActorID: user.Id,
				Action: "workspace.settings.update", TargetType: "workspace", TargetID: workspaceID,
				Metadata:   map[string]any{"requestId": input.RequestID, "revision": nextRevision},
				OccurredAt: now,
			}); err != nil {
				return err
			}
		}

		response = workspaceSettingsResponseFor(workspace, input.RequestID, nextRevision)
		storedResponse, err := json.Marshal(response)
		if err != nil || len(storedResponse) > workspaceSettingsResponseBytes {
			return errors.New(errorCodeWorkspaceSettingsFailed)
		}
		receiptCreated, err := nextWorkspaceSettingsReceiptCreated(txApp, workspaceID, now)
		if err != nil {
			return err
		}
		_, err = txApp.DB().Insert(workspaceSettingsReceiptsTable, dbx.Params{
			"workspace_id": workspaceID, "actor_id": user.Id, "request_id": input.RequestID,
			"request_hash": requestHash, "revision": nextRevision,
			"response_json": string(storedResponse), "created": receiptCreated,
		}).Execute()
		if err != nil {
			return err
		}
		return pruneWorkspaceSettingsReceipts(txApp, workspaceID)
	})
	if isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	}
	if errors.Is(err, errWorkspaceSettingsInvalid) {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceSettingsInvalid, refusalMessage("Check the workspace settings.", refusalReason(err)))
	}
	if errors.Is(err, errWorkspaceSettingsIdentityReused) {
		return apiError(event, http.StatusConflict, errorCodeWorkspaceSettingsIdentityReused, "The same settings request can't be reused with different content.")
	}
	if errors.Is(err, errWorkspaceSettingsRevisionConflict) {
		return apiError(event, http.StatusConflict, errorCodeWorkspaceSettingsRevisionConflict, "The workspace settings already changed.")
	}
	if errors.Is(err, errWorkspaceSettingsDomainInUse) {
		return apiError(event, http.StatusConflict, errorCodeWorkspaceSettingsDomainInUse, "This workspace domain is already in use.")
	}
	if errors.Is(err, errWorkspaceLandingPageHidden) {
		return apiError(event, http.StatusConflict, errorCodeWorkspaceLandingPageHidden,
			"Some people in the workspace can't open this page: share it with the whole workspace from its share menu, or choose a page of a teamspace everyone reads.")
	}
	if err != nil {
		return workspaceSettingsInternalError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

func workspaceSettingsInternalError(event *core.RequestEvent, cause error) error {
	if storageError, ok := persistentWriteStorageAPIError(
		event, cause, persistentWriteWorkspaceSettings, "transaction",
	); ok {
		return storageError
	}
	return apiErrorWithCause(
		event, http.StatusInternalServerError, errorCodeWorkspaceSettingsFailed,
		"Couldn't save the workspace settings.", "workspace-settings.update",
		"transaction", errorCodeWorkspaceSettingsFailed, cause,
	)
}
