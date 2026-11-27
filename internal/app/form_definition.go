package app

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

var errFormAuthenticationRequired = errors.New(errorCodeUnauthorized)

func loadStoredFormLinkByToken(app core.App, token string) (storedFormLink, error) {
	if !validFormToken(token) {
		return storedFormLink{}, errFormNotFound
	}
	rows := []storedFormLink{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[owner]], [[workspace_id]], [[page_id]], [[data_source_id]], [[view_id]],
			[[token_digest]], [[audience]], [[enabled]], [[submitter_access]], [[created_by]], [[created_at]], [[updated_at]]
		FROM {{leafen_form_links}}
		WHERE [[token_digest]] = {:tokenDigest} AND [[enabled]] = 1
		LIMIT 2
	`).Bind(dbx.Params{"tokenDigest": formTokenDigest(token)}).All(&rows)
	if err != nil {
		return storedFormLink{}, err
	}
	if len(rows) != 1 {
		return storedFormLink{}, errFormNotFound
	}
	expected, err := formToken(app, rows[0].ID)
	if err != nil {
		return storedFormLink{}, err
	}
	if !hmac.Equal([]byte(expected), []byte(token)) {
		return storedFormLink{}, errFormNotFound
	}
	return rows[0], nil
}

func authorizeFormDefinition(
	app core.App,
	principalID string,
	link storedFormLink,
) error {
	if link.Audience == formAudienceWorkspace {
		if principalID == "" {
			return errFormAuthenticationRequired
		}
		// A member of its workspace answers it whether or not they can open
		// its database, as Notion's members-only form says it is for the
		// workspace's members (seen 2026-11-05); anyone else as their page access
		// lets them.
		if member, err := formWorkspaceMember(app, principalID, link); err != nil {
			return err
		} else if member {
			return formPageOpen(app, link)
		}
		authorization, err := authorizePage(app, principalID, link.PageID, permissionWorkspaceRead)
		if err != nil || authorization.Workspace.OwnerID != link.Owner ||
			authorization.Workspace.WorkspaceID != link.WorkspaceID {
			if err != nil && !isPermissionDenied(err) {
				return err
			}
			return refusedUnlessStorage(errFormNotFound, err)
		}
		return nil
	}
	if link.Audience != formAudienceWeb {
		return errFormNotFound
	}
	if err := formPageOpen(app, link); err != nil {
		return err
	}
	workspaceAuthorization, err := authorizeWorkspace(
		app, link.Owner, link.WorkspaceID, permissionWorkspaceRead,
	)
	if err != nil || workspaceAuthorization.OwnerID != link.Owner {
		if err != nil && !isPermissionDenied(err) {
			return err
		}
		return refusedUnlessStorage(errFormNotFound, err)
	}
	// A form open to anyone with its link is published to the web, and stays
	// down while the organization has publishing turned off.
	turnedOff, err := publicSharingTurnedOff(app, workspaceAuthorization.Organization.OrganizationID)
	if err != nil {
		return err
	}
	if turnedOff {
		return errFormNotFound
	}
	// So does one whose teamspace has publishing turned off.
	if security, err := teamspacePageSecurity(app, link.PageID); err != nil {
		return err
	} else if security.DisablePublishing {
		return errFormNotFound
	}
	return nil
}

func publicFormProperty(property storedFormProperty) (formPublicProperty, error) {
	result := formPublicProperty{ID: property.ID, Name: property.Name, Type: property.Type}
	choice := property.Type == graphValueTypeSelect || property.Type == graphValueTypeMultiSelect ||
		property.Type == graphValueTypeStatus
	if !choice {
		return result, nil
	}
	config := struct {
		Options []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Color string `json:"color"`
		} `json:"options"`
	}{}
	if jsonUnmarshalObject(property.Config, &config) != nil {
		return formPublicProperty{}, errFormConflict
	}
	// Every option the property holds; how many a respondent picks is the
	// question's own most, if it sets one.
	options := make([]formPublicPropertyOption, 0, len(config.Options))
	for _, option := range config.Options {
		id := strings.TrimSpace(option.ID)
		name := strings.TrimSpace(option.Name)
		if id == "" || name == "" {
			return formPublicProperty{}, errFormConflict
		}
		options = append(options, formPublicPropertyOption{ID: id, Name: name, Color: option.Color})
	}
	result.Config = &formPublicPropertyConfiguration{Options: options}
	return result, nil
}

func jsonUnmarshalObject(raw string, target any) error {
	if strings.TrimSpace(raw) == "" {
		return errFormConflict
	}
	return json.Unmarshal([]byte(raw), target)
}

func readFormDefinition(
	app core.App,
	principalID, token string,
) (formDefinitionResponse, error) {
	response := formDefinitionResponse{}
	err := runReadSnapshot(context.Background(), app, func(txApp core.App) error {
		link, err := loadStoredFormLinkByToken(txApp, strings.TrimSpace(token))
		if err != nil {
			return err
		}
		if err := authorizeFormDefinition(txApp, principalID, link); err != nil {
			return err
		}
		source, err := loadFormSource(
			txApp, link.Owner, link.PageID, link.DataSourceID, link.ViewID, link.Audience, nil,
		)
		if err != nil {
			if errors.Is(err, errFormInvalid) {
				return errFormNotFound
			}
			return err
		}
		if source.ViewConfig["leafenFormAudience"] != link.Audience ||
			source.ViewConfig["type"] != graphViewTypeForm {
			return errFormNotFound
		}
		revision, err := formDefinitionRevision(link, source)
		if err != nil {
			return err
		}
		properties := make([]formPublicProperty, len(source.Properties))
		byID := make(map[string]storedFormProperty, len(source.Properties))
		for index, property := range source.Properties {
			properties[index], err = publicFormProperty(property)
			if err != nil {
				return err
			}
			byID[property.ID] = property
		}
		// Each question says how many it takes, its property's own bound
		// included, which the respondent's page shows and the answer is held to.
		form := source.Form
		form.Questions = append([]formQuestion(nil), source.Form.Questions...)
		for index, question := range form.Questions {
			form.Questions[index].MaxSelections = formQuestionMostSelections(question, byID[question.PropertyID])
		}
		response = formDefinitionResponse{
			Form: form, Properties: properties, Revision: revision,
			ResponseCopy: formResponseCopyAvailable(txApp, principalID),
			Place:        formMemberPlaceFor(txApp, principalID, link),
		}
		// Whom it is open to, told to someone signed in, so that a page reading it
		// as a member answers a web one as the web does (PublicFormView); a
		// visitor's read stays least-data.
		if principalID != "" {
			response.Audience = link.Audience
		}
		return nil
	})
	return response, err
}

// formMemberPlaceFor says where a form lives to a member who can read its
// database, for the bar of its page ("양식 편집", 즐겨찾기); no one else learns it.
// Its database is the block in that page showing the form's view, whose own
// address names the view as a database's view link does (/p/<database>?v=<view>),
// not the page holding it.
func formMemberPlaceFor(app core.App, principalID string, link storedFormLink) *formMemberPlace {
	if principalID == "" {
		return nil
	}
	authorization, err := authorizePage(app, principalID, link.PageID, permissionWorkspaceRead)
	if err != nil {
		return nil
	}
	_, denied := authorizePage(app, principalID, link.PageID, permissionWorkspaceWrite)
	return &formMemberPlace{PageID: link.PageID, DatabaseID: formViewDatabaseBlock(app, link), DataSourceID: link.DataSourceID, ViewID: link.ViewID,
		Title: rowTitleAsRead(app, principalID, link.PageID, authorization.Page.GetString("title")), CanEdit: denied == nil}
}

// formViewDatabaseBlock is the block of the first database in the form's page
// that shows its view, or "" where none does.
func formViewDatabaseBlock(app core.App, link storedFormLink) string {
	var blockID string
	err := app.DB().NewQuery(`SELECT [[container]].[[block_id]] FROM {{leafen_database_container_views}} AS [[membership]]
		JOIN {{leafen_database_containers}} AS [[container]] ON [[container]].[[id]] = [[membership]].[[container_id]]
		WHERE [[membership]].[[owner]] = {:owner} AND [[membership]].[[page_id]] = {:pageId} AND [[membership]].[[view_id]] = {:viewId}
		ORDER BY [[container]].[[position]], [[container]].[[id]] LIMIT 1`).
		Bind(dbx.Params{"owner": link.Owner, "pageId": link.PageID, "viewId": link.ViewID}).Row(&blockID)
	if err != nil {
		return ""
	}
	return blockID
}

// formWorkspaceMember says a person is a member of the workspace a form is
// open to; a guest is not.
func formWorkspaceMember(app core.App, principalID string, link storedFormLink) (bool, error) {
	authorization, err := authorizeWorkspace(app, principalID, link.WorkspaceID, permissionWorkspaceRead)
	if isPermissionDenied(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return authorization.OwnerID == link.Owner, nil
}

// formPageOpen says a form's database page is there to take answers: its
// own, in its workspace, not being imported or deleted, and out of the trash
// with every page above it (pageInTrashPath, the one trash rule).
func formPageOpen(app core.App, link storedFormLink) error {
	page, err := app.FindRecordById(pagesCollection, link.PageID)
	if err != nil || page.GetString("owner") != link.Owner ||
		page.GetString("workspace") != link.WorkspaceID || page.GetBool("notion_import_staging") {
		return refusedUnlessStorage(errFormNotFound, err)
	}
	if trashed, err := pageInTrashPath(app, link.PageID); err != nil || trashed {
		return refusedUnlessStorage(errFormNotFound, err)
	}
	fenced, err := pagePermanentDeleteFenceActive(app, link.PageID)
	if err != nil {
		return err
	}
	if fenced {
		return errFormNotFound
	}
	return nil
}

// answerableFormToken is a form's link for one who may answer it, as a
// button that opens the form hands it to whoever pressed (Notion's opens the
// form's answer page, seen 2026-11-05); "" when it takes no answers or none
// of theirs.
func answerableFormToken(app core.App, principalID, pageID, viewID string) string {
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return ""
	}
	link, found, err := loadStoredFormLinkByView(app, page.GetString("owner"), pageID, viewID)
	if err != nil || !found || !link.Enabled || authorizeFormDefinition(app, principalID, link) != nil {
		return ""
	}
	token, err := formToken(app, link.ID)
	if err != nil || !strings.EqualFold(link.TokenDigest, formTokenDigest(token)) {
		return ""
	}
	return token
}

// authorizeFormRelationPage reads a related database's page as a form's
// respondent may: whether they open it, and whether it is in the trash or
// under a page there (pageInTrashPath), where it offers none of its pages.
func authorizeFormRelationPage(app core.App, principalID, pageID string) (pageAuthorization, bool, error) {
	authorization, err := authorizePage(app, principalID, pageID, permissionWorkspaceRead)
	if err != nil {
		return authorization, false, err
	}
	trashed, err := pageInTrashPath(app, pageID)
	return authorization, trashed, err
}
