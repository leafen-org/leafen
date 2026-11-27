package app

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A form view's response settings live on its form link, as the web form
// editor keeps them: whether it accepts responses, whether people outside
// the workspace may answer it, and what a signed-in respondent may do on the
// page their response creates. The official view API reads them as
// is_form_closed, anonymous_submissions and submission_permissions.

type notionViewFormSettingsInput struct {
	closed    *bool
	anonymous *bool
	access    *string
}

func (settings notionViewFormSettingsInput) present() bool {
	return settings.closed != nil || settings.anonymous != nil || settings.access != nil
}

// splitNotionViewFormSettings takes a form's response settings out of an
// official configuration; the rest is the view's own.
func splitNotionViewFormSettings(viewType string, raw json.RawMessage) (json.RawMessage, notionViewFormSettingsInput, error) {
	settings := notionViewFormSettingsInput{}
	if viewType != graphViewTypeForm || len(raw) == 0 {
		return raw, settings, nil
	}
	fields := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &fields) != nil {
		return raw, settings, nil
	}
	// null clears a setting to how a new form starts: closed, and answered
	// only by workspace members.
	flag := func(name string, cleared bool) (*bool, error) {
		value, found := fields[name]
		if !found {
			return nil, nil
		}
		delete(fields, name)
		var parsed *bool
		if json.Unmarshal(value, &parsed) != nil {
			return nil, notionViewFieldCheck(false, name)
		}
		if parsed == nil {
			parsed = &cleared
		}
		return parsed, nil
	}
	var err error
	if settings.closed, err = flag("is_form_closed", true); err != nil {
		return nil, settings, err
	}
	if settings.anonymous, err = flag("anonymous_submissions", false); err != nil {
		return nil, settings, err
	}
	if value, found := fields["submission_permissions"]; found {
		delete(fields, "submission_permissions")
		var permission *string
		if json.Unmarshal(value, &permission) != nil {
			return nil, settings, notionViewFieldCheck(false, "submission_permissions")
		}
		// null clears it to how a new form starts: no access.
		access := formSubmitterAccessNone
		if permission != nil {
			access = *permission
		}
		if _, valid := formSubmitterAccessRole(access); !valid {
			return nil, settings, notionViewFilterInvalid("the form submission_permissions %q is not supported", access)
		}
		settings.access = &access
	}
	rest, err := json.Marshal(fields)
	return rest, settings, err
}

// withNotionAPIFormStart marks a form the API makes to open on its builder
// with its title's question alone, as Notion's does (seen 2026-11-12), where
// one made on the screen first asks whether its properties become questions.
func withNotionAPIFormStart(viewType string, config json.RawMessage) (json.RawMessage, error) {
	if viewType != graphViewTypeForm {
		return config, nil
	}
	values := map[string]any{}
	if err := json.Unmarshal(config, &values); err != nil {
		return nil, err
	}
	if _, configured := values["leafenForm"]; configured {
		return config, nil
	}
	values["leafenFormStart"] = "title"
	return json.Marshal(values)
}

// nextNotionViewFormAudience reads the audience a form's settings ask for,
// from the link it has.
func nextNotionViewFormAudience(link storedFormLink, found bool, settings notionViewFormSettingsInput) string {
	current := formAudienceNone
	if found {
		current = link.Audience
	}
	open := current != formAudienceNone
	if settings.closed != nil {
		open = !*settings.closed
	}
	anonymous := current == formAudienceWeb
	if settings.anonymous != nil {
		anonymous = *settings.anonymous
	}
	switch {
	case !open:
		return formAudienceNone
	case anonymous:
		return formAudienceWeb
	default:
		return formAudienceWorkspace
	}
}

// withNotionViewFormAudience records a form's audience, and the access a
// request gives its respondents, in its view config, where the web form
// editor reads them.
func withNotionViewFormAudience(config json.RawMessage, audience string, access *string) (json.RawMessage, error) {
	values := map[string]any{}
	if err := json.Unmarshal(config, &values); err != nil {
		return nil, err
	}
	values["leafenFormAudience"] = audience
	if access != nil {
		values["leafenFormSubmitterAccess"] = *access
	}
	return json.Marshal(values)
}

// checkNotionViewFormAudience refuses an audience the form's questions
// cannot take, as the form editor does: people outside the workspace cannot
// answer person or relation questions. It reads the questions in the
// location's config, the one the request makes.
func checkNotionViewFormAudience(app core.App, location notionViewLocation, audience string) error {
	return checkNotionViewFormQuestions(app, location, json.RawMessage(location.Config), audience)
}

// checkNotionViewFormQuestions checks the questions a view's next config
// asks, never the stored ones: a form imported from Notion, or never set
// up, has none stored, and checking those refused every first question
// with the person-and-relation words (2026-11-16). A refusal says the
// editor's own reason.
func checkNotionViewFormQuestions(app core.App, location notionViewLocation, config json.RawMessage, audience string) error {
	next := struct {
		Form *formViewConfiguration `json:"leafenForm"`
	}{}
	if audience == formAudienceNone || json.Unmarshal(config, &next) != nil || next.Form == nil {
		return nil
	}
	_, err := loadFormSource(app, location.OwnerID, location.PageID, location.DataSourceID, location.ID, audience, next.Form)
	switch {
	case errors.Is(err, errFormConflict):
		return errDatabaseRowPageConflict
	case errors.Is(err, errFormNotFound):
		// The view itself is gone or no form: the change says so on its own.
		return nil
	case refusalReason(err) != "":
		return notionViewFilterInvalid("the form's questions cannot be asked with these settings: %s", refusalReason(err))
	case errors.Is(err, errFormInvalid):
		return notionViewFilterInvalid("the form's questions cannot be asked with these settings")
	}
	return err
}

// notionViewFormAudienceSaver writes a form link's audience in the view
// mutation's transaction, or nothing when the request leaves it.
func notionViewFormAudienceSaver(
	principalID string, location notionViewLocation, audience string, access *string,
) func(core.App, pageAuthorization) error {
	if audience == "" {
		return nil
	}
	return func(txApp core.App, authorization pageAuthorization) error {
		return saveNotionViewFormAudience(txApp, authorization.Workspace, principalID, location, audience, access)
	}
}

// saveNotionViewFormAudience writes a form link's audience, making the link
// when the form has none yet.
func saveNotionViewFormAudience(
	app core.App, workspace workspaceAuthorization, principalID string, location notionViewLocation, audience string,
	access *string,
) error {
	link, found, err := loadStoredFormLinkByView(app, workspace.OwnerID, location.PageID, location.ID)
	if err != nil {
		return err
	}
	if found && (link.WorkspaceID != workspace.WorkspaceID || link.DataSourceID != location.DataSourceID || link.CreatedBy == "") {
		return errDatabaseRowPageConflict
	}
	if !found {
		link = storedFormLink{
			ID: core.GenerateDefaultRandomId(), Owner: workspace.OwnerID, WorkspaceID: workspace.WorkspaceID,
			PageID: location.PageID, DataSourceID: location.DataSourceID, ViewID: location.ID,
			CreatedBy: principalID, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		}
	}
	if access != nil {
		link.SubmitterAccess = *access
	}
	token, err := formToken(app, link.ID)
	if err != nil {
		return err
	}
	err = saveFormLink(app, link, formTokenDigest(token), audience)
	if errors.Is(err, errFormConflict) {
		return errDatabaseRowPageConflict
	}
	return err
}

// notionViewFormConfiguration adds a form's response settings to its
// official configuration.
func notionViewFormConfiguration(app core.App, location notionViewLocation, configuration map[string]any) error {
	link, found, err := loadStoredFormLinkByView(app, location.OwnerID, location.PageID, location.ID)
	if err != nil {
		return err
	}
	configuration["is_form_closed"] = !found || !link.Enabled
	config := struct {
		Form struct {
			Anonymous bool `json:"anonymous"`
		} `json:"leafenForm"`
	}{}
	_ = json.Unmarshal([]byte(location.Config), &config)
	configuration["anonymous_submissions"] = found && link.Enabled && (link.Audience == formAudienceWeb || config.Form.Anonymous)
	configuration["submission_permissions"] = formSubmitterAccessNone
	if found {
		configuration["submission_permissions"] = formSubmitterAccessOrNone(link.SubmitterAccess)
	}
	return nil
}
