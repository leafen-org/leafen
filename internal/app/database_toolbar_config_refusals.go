package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// What a refused change of a database's settings says to fix
// (request_refusal.go): the field as the request names it and why, after
// "Check the database settings request.", and for a template or setting in a
// state the change can't start from, that state after "The database settings
// can't be changed this way now." A revision that moved on still says "Load
// the latest database settings again." Status and code stay the route's.

// databaseToolbarRefused refuses a settings change for the reason given.
func databaseToolbarRefused(format string, args ...any) error {
	return refuse(errDatabaseToolbarConfigInvalid, format, args...)
}

// databaseToolbarStateRefused refuses a settings change for the state given.
func databaseToolbarStateRefused(format string, args ...any) error {
	return refuse(errDatabaseToolbarConfigConflict, format, args...)
}

// databaseToolbarConflictMessage is a refused settings change's words.
func databaseToolbarConflictMessage(err error) string {
	if reason := refusalReason(err); reason != "" {
		return refusalMessage("The database settings can't be changed this way now.", reason)
	}
	return "Load the latest database settings again."
}

// errDatabaseToolbarTemplateTrashed is a template a change names that is in
// the trash, where it is only restored or deleted for good.
var errDatabaseToolbarTemplateTrashed = databaseToolbarStateRefused("templateId names a template in the trash: restore it first")

// databaseTemplateBodyRefused refuses a template's body given as blocks for
// the reason given; it is still a settings conflict to its callers.
func databaseTemplateBodyRefused(format string, args ...any) error {
	return databaseToolbarStateRefused(format, args...)
}

// errDatabaseSprintSettingsMissing is a sprint action that sets sprints up
// without their settings.
var errDatabaseSprintSettingsMissing = databaseToolbarRefused("settings is empty: it holds durationWeeks, startWeekday, incompleteTasks, automatic and timeZone")

// databaseTemplateRepeatReason is what a template's repeat lacks, after
// "repeat": the schedule's own reason where it says one.
func databaseTemplateRepeatReason(err error) string {
	if reason := refusalReason(err); reason != "" {
		return "." + reason
	}
	return " is a schedule the automation runner can run: an interval, its start and, if it ends, an end after the next run"
}

var databaseToolbarOperationKinds = []string{
	"configureTaskFeatures", "createTemplate", "updateTemplate", "duplicateTemplate", "deleteTemplate",
	"restoreTemplate", "setDefaultTemplate", "setTemplateRepeat",
}

func validateDatabaseToolbarMutationInput(input databaseToolbarConfigMutationInput) error {
	switch {
	case strings.TrimSpace(input.RequestID) == "" || len(input.RequestID) > 255:
		return databaseToolbarRefused("requestId is a new id of yours, 1–255 characters, so the same change sent again isn't made twice")
	case input.ExpectedWorkspaceContentRevision < 0:
		return databaseToolbarRefused("expectedWorkspaceContentRevision is the revision the settings were read at, 0 or more")
	case len(input.Operations) < 1:
		return databaseToolbarRefused("operations is empty: name at least one change")
	case len(input.Operations) > databaseToolbarConfigMutationOperations:
		return databaseToolbarRefused("operations holds %d changes, over %d", len(input.Operations), databaseToolbarConfigMutationOperations)
	}
	for index, operation := range input.Operations {
		if reason := databaseToolbarOperationReason(operation); reason != "" {
			return databaseToolbarRefused("operations[%d].%s", index, reason)
		}
	}
	if reason := databaseTaskConversionShapeReason(input); reason != "" {
		return databaseToolbarRefused("%s", reason)
	}
	return nil
}

// databaseToolbarOperationReason is why one change can't be made, its field
// first, or "".
func databaseToolbarOperationReason(operation databaseToolbarConfigMutationOperation) string {
	switch operation.Kind {
	case "configureTaskFeatures":
		features := operation.TaskFeatures
		switch {
		case features == nil:
			return "taskFeatures is empty: it holds subitems, dependencies and tasks"
		case operation.ExpectedRevision == nil:
			return "expectedRevision is the task settings' revision as it was read"
		case features.Revision != *operation.ExpectedRevision:
			return fmt.Sprintf("taskFeatures.revision is %d, not expectedRevision %d", features.Revision, *operation.ExpectedRevision)
		}
		if reason := databaseToolbarJSONReason("taskFeatures.subitems", features.Subitems, false, "object", true); reason != "" {
			return reason
		}
		if reason := databaseToolbarJSONReason("taskFeatures.dependencies", features.Dependencies, false, "object", true); reason != "" {
			return reason
		}
		if len(features.Tasks) > 0 {
			return databaseToolbarJSONReason("taskFeatures.tasks", features.Tasks, false, "object", true)
		}
		return ""
	case "createTemplate":
		return databaseToolbarTemplateCreateReason(operation)
	case "updateTemplate":
		if reason := databaseToolbarTemplateIdentityReason(operation); reason != "" {
			return reason
		}
		return databaseToolbarTemplatePatchReason(operation)
	case "duplicateTemplate":
		if reason := databaseToolbarTemplateIdentityReason(operation); reason != "" {
			return reason
		}
		if operation.TargetTemplateID == nil || strings.TrimSpace(*operation.TargetTemplateID) == "" || len(*operation.TargetTemplateID) > 255 {
			return "targetTemplateId is a new id of yours for the copy of the template templateId names, 1–255 characters"
		}
		return ""
	case "deleteTemplate", "restoreTemplate", "setDefaultTemplate", "setTemplateRepeat":
		return databaseToolbarTemplateIdentityReason(operation)
	}
	return fmt.Sprintf("kind is one of %s, not %q", strings.Join(databaseToolbarOperationKinds, ", "), operation.Kind)
}

func databaseToolbarTemplateIdentityReason(operation databaseToolbarConfigMutationOperation) string {
	switch {
	case operation.TemplateID == nil || strings.TrimSpace(*operation.TemplateID) == "" || len(*operation.TemplateID) > 255:
		return "templateId names the template by its id as the toolbar-config read lists it, 1–255 characters"
	case operation.ExpectedRevision == nil || *operation.ExpectedRevision < 1:
		return "expectedRevision is the template's templateAuthorityRevision as the toolbar-config read lists it, 1 or more"
	}
	return ""
}

func validDatabaseToolbarTemplateCreate(operation databaseToolbarConfigMutationOperation) bool {
	return databaseToolbarTemplateCreateReason(operation) == ""
}

// databaseToolbarTemplateCreateReason is why a new template can't be made,
// or "".
func databaseToolbarTemplateCreateReason(operation databaseToolbarConfigMutationOperation) string {
	switch {
	case operation.TemplateID == nil || strings.TrimSpace(*operation.TemplateID) == "" || len(*operation.TemplateID) > 255:
		return "templateId is a new id of yours for the template, 1–255 characters"
	case operation.ExpectedRevision != nil:
		return "expectedRevision is left out to create a template"
	case operation.Name == nil:
		return "name is the template's name, which may be empty text"
	case operation.Position == nil || strings.TrimSpace(*operation.Position) == "" || len(*operation.Position) > 255:
		return "position is the template's place in the list, 1–255 characters"
	}
	if reason := databaseToolbarTemplateTextReason(operation); reason != "" {
		return reason
	}
	if reason := databaseToolbarJSONReason("properties", operation.Properties, true, "object", false); reason != "" {
		return reason
	}
	if reason := databaseToolbarJSONReason("propertySpans", operation.PropertySpans, true, "object", false); reason != "" {
		return reason
	}
	return databaseToolbarJSONReason("blocks", operation.Blocks, true, "array", false)
}

func validDatabaseToolbarTemplatePatch(operation databaseToolbarConfigMutationOperation) bool {
	return databaseToolbarTemplatePatchReason(operation) == ""
}

// databaseToolbarTemplatePatchReason is why a template's metadata change
// can't be made, or "". The template's body is its page's, edited as a page
// is, so a change carries none.
func databaseToolbarTemplatePatchReason(operation databaseToolbarConfigMutationOperation) string {
	if len(operation.Blocks) > 0 {
		return "blocks is left out: a template's body is its page's, edited as a page is"
	}
	if reason := databaseToolbarTemplateTextReason(operation); reason != "" {
		return reason
	}
	if operation.Position != nil && (strings.TrimSpace(*operation.Position) == "" || len(*operation.Position) > 255) {
		return "position is the template's place in the list, 1–255 characters"
	}
	if reason := databaseToolbarJSONReason("properties", operation.Properties, true, "object", false); reason != "" {
		return reason
	}
	return databaseToolbarJSONReason("propertySpans", operation.PropertySpans, true, "object", false)
}

// databaseToolbarTemplateTextReason is why a template's name, title or icon
// can't be kept, or "".
func databaseToolbarTemplateTextReason(operation databaseToolbarConfigMutationOperation) string {
	switch {
	case operation.Name != nil && utf8.RuneCountInString(*operation.Name) > maxGraphTitleRunes:
		return fmt.Sprintf("name is over %d characters", maxGraphTitleRunes)
	case operation.Title != nil && utf8.RuneCountInString(*operation.Title) > maxGraphTitleRunes:
		return fmt.Sprintf("title is over %d characters", maxGraphTitleRunes)
	case operation.Icon != nil && !validDatabaseTemplateIcon(*operation.Icon):
		return fmt.Sprintf("icon is an emoji, an image address or a custom emoji of the workspace, at most %d bytes", maxDatabaseTemplateIconBytes)
	}
	return ""
}

func validDatabaseToolbarJSON(raw json.RawMessage, allowEmpty bool, kind string, allowNull bool) bool {
	return databaseToolbarJSONReason("", raw, allowEmpty, kind, allowNull) == ""
}

// databaseToolbarJSONReason is why a field's JSON isn't the kind it takes,
// or "".
func databaseToolbarJSONReason(field string, raw json.RawMessage, allowEmpty bool, kind string, allowNull bool) string {
	wanted := map[string]string{"object": "an object", "array": "a list"}[kind]
	if allowNull {
		wanted += " or null"
	}
	if len(raw) == 0 {
		if allowEmpty {
			return ""
		}
		return field + " is empty: it takes " + wanted
	}
	if len(raw) > databaseToolbarConfigMaxTemplateBodyBytes {
		return fmt.Sprintf("%s is %d bytes, over %d", field, len(raw), databaseToolbarConfigMaxTemplateBodyBytes)
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return field + " isn't JSON"
	}
	switch value.(type) {
	case nil:
		if allowNull {
			return ""
		}
	case map[string]any:
		if kind == "object" {
			return ""
		}
	case []any:
		if kind == "array" {
			return ""
		}
	}
	return field + " takes " + wanted
}
