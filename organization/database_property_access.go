package organization

import (
	"errors"
	"net/http"
	"strings"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// Notion's "속성 사용 권한" (its help "Database property access", read
// 2026-10-12): someone with full access to a database sets, for one of its
// properties, the level for everyone who opens it and exceptions for
// people, groups, the people a person property names and apps' accounts,
// and previews what a person gets. The core keeps the rules and applies
// them wherever the property or its values leave the server; a change takes
// effect for everyone with the workspace open at once.

const (
	databasePropertyAccessPath         = "/data-sources/{dataSourceId}/properties/{propertyId}/access"
	databasePropertyAccessPreviewPath  = databasePropertyAccessPath + "/preview"
	databasePagePropertyAccessPath     = "/pages/{pageId}/property-access"
	databasePropertyAccessMaxBodyBytes = 64 << 10
	maxDatabasePropertyAccessIDLength  = 128
)

type databasePropertyAccessInput struct {
	DefaultLevel string                                   `json:"defaultLevel"`
	Exceptions   []leafen.DatabasePropertyAccessException `json:"exceptions"`
}

// A preview says why the person gets the level, as Notion's does under it:
// reason is noDatabaseAccess, fullAccess, personException, groupException
// (with groupName, empty for a group gone), defaultAccess or inherited; via
// how the database gave it (teamspace, group with viaGroupName, workspace or
// database); comment a level the database gives only to comment; conditions
// the person-property exceptions that would give more.
type databasePropertyAccessPreview struct {
	UserID       string                                   `json:"userId"`
	Level        string                                   `json:"level"`
	Inherited    bool                                     `json:"inherited"`
	Reason       string                                   `json:"reason"`
	GroupName    string                                   `json:"groupName,omitempty"`
	Via          string                                   `json:"via,omitempty"`
	ViaGroupName string                                   `json:"viaGroupName,omitempty"`
	Comment      bool                                     `json:"comment,omitempty"`
	Conditions   []leafen.DatabasePropertyAccessCondition `json:"conditions,omitempty"`
}

func registerDatabasePropertyAccessRoutes(event *core.ServeEvent, rooms leafen.WorkspaceRoomInvalidator) {
	event.Router.GET(leafen.APIRoot+databasePropertyAccessPath, readDatabasePropertyAccess)
	event.Router.GET(leafen.APIRoot+databasePagePropertyAccessPath, readDatabasePagePropertyAccess)
	event.Router.PUT(leafen.APIRoot+databasePropertyAccessPath, func(request *core.RequestEvent) error {
		return updateDatabasePropertyAccess(request, rooms)
	}).Bind(apis.BodyLimit(databasePropertyAccessMaxBodyBytes))
	event.Router.POST(leafen.APIRoot+databasePropertyAccessPreviewPath, previewDatabasePropertyAccessRoute).
		Bind(apis.BodyLimit(databasePropertyAccessMaxBodyBytes))
}

func databasePropertyAccessRequestTarget(event *core.RequestEvent) (string, string, bool) {
	dataSourceID := strings.TrimSpace(event.Request.PathValue("dataSourceId"))
	propertyID := strings.TrimSpace(event.Request.PathValue("propertyId"))
	return dataSourceID, propertyID, dataSourceID != "" && propertyID != "" &&
		len(dataSourceID) <= maxDatabasePropertyAccessIDLength && len(propertyID) <= maxDatabasePropertyAccessIDLength
}

func databasePropertyAccessError(event *core.RequestEvent, err error, step string) error {
	switch {
	case errors.Is(err, leafen.ErrDatabasePropertyAccessNotFound) || leafen.IsPermissionDenied(err):
		return leafen.APIError(event, http.StatusNotFound, err.Error(), "Property not found.")
	case errors.Is(err, leafen.ErrDatabasePropertyAccessInvalid):
		return leafen.APIError(event, http.StatusBadRequest, err.Error(), "Check the property's access rule.")
	}
	return leafen.APIErrorWithCause(event, http.StatusInternalServerError, leafen.ErrorCodeInvalidInput,
		"Couldn't change the property's access.", "database_property_access", step, leafen.ErrorCodeInvalidInput, err)
}

func readDatabasePropertyAccess(event *core.RequestEvent) error {
	user, ok := leafen.AuthenticatedUser(event)
	if !ok {
		return leafen.APIError(event, http.StatusUnauthorized, leafen.ErrorCodeUnauthorized, "Login required.")
	}
	dataSourceID, propertyID, valid := databasePropertyAccessRequestTarget(event)
	if !valid {
		return databasePropertyAccessError(event, leafen.ErrDatabasePropertyAccessInvalid, "read")
	}
	rule, err := leafen.LoadDatabasePropertyAccessRule(event.App, user.Id, dataSourceID, propertyID)
	if err != nil {
		return databasePropertyAccessError(event, err, "read")
	}
	return event.JSON(http.StatusOK, rule)
}

// readDatabasePagePropertyAccess lists a database page's rules for its share
// window's "고급".
func readDatabasePagePropertyAccess(event *core.RequestEvent) error {
	user, ok := leafen.AuthenticatedUser(event)
	if !ok {
		return leafen.APIError(event, http.StatusUnauthorized, leafen.ErrorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	if pageID == "" || len(pageID) > maxDatabasePropertyAccessIDLength {
		return databasePropertyAccessError(event, leafen.ErrDatabasePropertyAccessInvalid, "page")
	}
	access, err := leafen.LoadDatabasePagePropertyAccess(event.App, user.Id, pageID)
	if err != nil {
		return databasePropertyAccessError(event, err, "page")
	}
	return event.JSON(http.StatusOK, access)
}

func updateDatabasePropertyAccess(event *core.RequestEvent, rooms leafen.WorkspaceRoomInvalidator) error {
	user, ok := leafen.AuthenticatedUser(event)
	if !ok {
		return leafen.APIError(event, http.StatusUnauthorized, leafen.ErrorCodeUnauthorized, "Login required.")
	}
	dataSourceID, propertyID, valid := databasePropertyAccessRequestTarget(event)
	input := databasePropertyAccessInput{}
	if err := event.BindBody(&input); err != nil || !valid {
		return databasePropertyAccessError(event, leafen.ErrDatabasePropertyAccessInvalid, "bind")
	}
	if input.Exceptions == nil {
		input.Exceptions = []leafen.DatabasePropertyAccessException{}
	}
	rule := leafen.DatabasePropertyAccessRule{DataSourceID: dataSourceID, PropertyID: propertyID,
		DefaultLevel: strings.TrimSpace(input.DefaultLevel), Exceptions: input.Exceptions}
	workspaceID := ""
	changed := false
	change := func(txApp core.App) error {
		now := time.Now().UTC().UnixMilli()
		var err error
		workspaceID, changed, err = leafen.SetDatabasePropertyAccessRule(txApp, user.Id, rule, now)
		if err != nil || !changed {
			return err
		}
		authorization, err := leafen.AuthorizeWorkspace(txApp, user.Id, workspaceID, leafen.PermissionWorkspaceRead)
		if leafen.IsPermissionDenied(err) {
			// A guest with full access to the database keeps no audit trail of their own.
			return nil
		}
		if err != nil {
			return err
		}
		return leafen.RecordOrganizationAuditEvent(txApp, leafen.OrganizationAuditEvent{
			ID: security.RandomString(15), OrganizationID: authorization.Organization.OrganizationID,
			WorkspaceID: workspaceID, ActorID: user.Id, Action: "database.property.access_changed",
			TargetType: "database_property", TargetID: dataSourceID + "/" + propertyID, OccurredAt: now,
		})
	}
	// A key's call asks its person only for a change that would be made
	// (internal/app/user_key_acting_ask.go).
	err := leafen.RehearseBeforeAsking(event, change)
	if err == nil {
		if asked, askErr := leafen.AskBeforeActing(event); asked {
			return askErr
		}
		err = event.App.RunInTransaction(change)
	}
	if err != nil {
		return databasePropertyAccessError(event, err, "update")
	}
	// Whoever has the workspace open reads its databases again under the rule.
	if changed && rooms != nil {
		_ = rooms.InvalidateWorkspace(workspaceID)
	}
	saved, err := leafen.LoadDatabasePropertyAccessRule(event.App, user.Id, dataSourceID, propertyID)
	if err != nil {
		return databasePropertyAccessError(event, err, "read-back")
	}
	return event.JSON(http.StatusOK, saved)
}

func previewDatabasePropertyAccessRoute(event *core.RequestEvent) error {
	user, ok := leafen.AuthenticatedUser(event)
	if !ok {
		return leafen.APIError(event, http.StatusUnauthorized, leafen.ErrorCodeUnauthorized, "Login required.")
	}
	dataSourceID, propertyID, valid := databasePropertyAccessRequestTarget(event)
	input := struct {
		UserID string                       `json:"userId"`
		Rule   *databasePropertyAccessInput `json:"rule"`
	}{}
	if err := event.BindBody(&input); err != nil {
		return databasePropertyAccessError(event, leafen.ErrDatabasePropertyAccessInvalid, "preview")
	}
	personID := strings.TrimSpace(input.UserID)
	if !valid || personID == "" || len(personID) > maxDatabasePropertyAccessIDLength {
		return databasePropertyAccessError(event, leafen.ErrDatabasePropertyAccessInvalid, "preview")
	}
	// The preview reads the rule being drafted, or the saved one.
	var draft *leafen.DatabasePropertyAccessRule
	if input.Rule != nil {
		exceptions := input.Rule.Exceptions
		if exceptions == nil {
			exceptions = []leafen.DatabasePropertyAccessException{}
		}
		draft = &leafen.DatabasePropertyAccessRule{DataSourceID: dataSourceID, PropertyID: propertyID,
			DefaultLevel: strings.TrimSpace(input.Rule.DefaultLevel), Exceptions: exceptions}
	}
	preview, err := leafen.PreviewDatabasePropertyAccess(event.App, user.Id, dataSourceID, propertyID, personID, draft)
	if err != nil {
		return databasePropertyAccessError(event, err, "preview")
	}
	return event.JSON(http.StatusOK, databasePropertyAccessPreview{UserID: personID, Level: preview.Level,
		Inherited: preview.Inherited(), Reason: preview.Reason, GroupName: preview.GroupName, Via: preview.Via,
		ViaGroupName: preview.ViaGroupName, Comment: preview.Comment, Conditions: preview.Conditions})
}
