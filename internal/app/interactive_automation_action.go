package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	errInteractiveAutomationInvalid     = errors.New(errorCodeInteractiveAutomationInvalid)
	errInteractiveAutomationNotFound    = errors.New(errorCodeInteractiveAutomationNotFound)
	errInteractiveAutomationUnsupported = errors.New(errorCodeInteractiveAutomationUnsupported)
	errInteractiveAutomationConflict    = errors.New(errorCodeInteractiveAutomationConflict)
	errInteractiveAutomationUnavailable = errors.New(errorCodeInteractiveAutomationUnavailable)
)

type normalizedInteractiveAutomation struct {
	Document      interactiveAutomationDocument
	CanonicalJSON []byte
	DocumentHash  string
}

// A database automation may be left unnamed, as Notion's, and then reads as
// its maker's.
func normalizeDatabaseAutomationDocument(raw json.RawMessage) (normalizedInteractiveAutomation, error) {
	return normalizeAutomationDocument(raw)
}

// A button's actions, as Notion's, may notify, email, call a webhook or post
// to Slack, as a database automation's may; both go through the same outbox.
// A new page button is unnamed until named, as Notion's (seen 2026-10-25).
func normalizeInteractiveAutomationDocument(raw json.RawMessage) (normalizedInteractiveAutomation, error) {
	return normalizeAutomationDocument(raw)
}

func normalizeAutomationDocument(raw json.RawMessage) (normalizedInteractiveAutomation, error) {
	if len(raw) > interactiveAutomationMaxDocumentBytes {
		return normalizedInteractiveAutomation{}, automationRefused("the document is %d bytes, over %d", len(raw), interactiveAutomationMaxDocumentBytes)
	}
	document := interactiveAutomationDocument{}
	if len(raw) < 2 || !json.Valid(raw) || json.Unmarshal(raw, &document) != nil {
		return normalizedInteractiveAutomation{}, automationRefused("the document is an object with version, label and actions")
	}
	if document.Version != 1 {
		return normalizedInteractiveAutomation{}, automationRefused("version is 1, not %d", document.Version)
	}
	document.Label = strings.TrimSpace(document.Label)
	if document.Label != "" && !boundedAutomationString(document.Label, interactiveAutomationMaxLabelRunes) {
		return normalizedInteractiveAutomation{}, automationRefused("label is at most %d characters", interactiveAutomationMaxLabelRunes)
	}
	if len(document.Actions) < 1 {
		return normalizedInteractiveAutomation{}, automationRefused("actions is empty: name at least one step")
	}
	actionIDs := map[string]struct{}{}
	variables := map[string]struct{}{}
	for index := range document.Actions {
		action := &document.Actions[index]
		action.ID = strings.TrimSpace(action.ID)
		action.Type = strings.TrimSpace(action.Type)
		if !boundedAutomationString(action.ID, interactiveAutomationMaxIdentifierRunes) {
			return normalizedInteractiveAutomation{}, automationRefused("actions[%d].id is 1–%d characters", index, interactiveAutomationMaxIdentifierRunes)
		}
		if _, exists := actionIDs[action.ID]; exists {
			return normalizedInteractiveAutomation{}, automationRefused("actions[%d].id is an earlier step's too: each step has its own", index)
		}
		actionIDs[action.ID] = struct{}{}
		if err := normalizeInteractiveAutomationAction(action, variables); err != nil {
			return normalizedInteractiveAutomation{}, automationActionAt(err, index)
		}
	}
	canonical, err := json.Marshal(document)
	if err != nil || len(canonical) > interactiveAutomationMaxDocumentBytes {
		return normalizedInteractiveAutomation{}, automationRefused("the document is over %d bytes as it is kept", interactiveAutomationMaxDocumentBytes)
	}
	digest := sha256.Sum256(canonical)
	return normalizedInteractiveAutomation{
		Document: document, CanonicalJSON: canonical, DocumentHash: hex.EncodeToString(digest[:]),
	}, nil
}

func normalizeInteractiveAutomationAction(
	action *interactiveAutomationAction,
	variables map[string]struct{},
) error {
	if action.SourceBlockID != "" && action.Type != interactiveAutomationActionInsertBlocks {
		return automationRefused("sourceBlockId goes only with type insert_blocks")
	}
	if err := refuseInteractiveAutomationStrayIcon(*action); err != nil {
		return err
	}
	if action.RecipientKind != "" && action.Type != interactiveAutomationActionSendNotification {
		return automationRefused("recipientKind goes only with type send_notification")
	}
	// Only a message's words are a formula (interactive_automation_message.go).
	switch action.Type {
	case interactiveAutomationActionSendNotification, interactiveAutomationActionSendEmail, interactiveAutomationActionSendSlack,
		interactiveAutomationActionShowConfirmation:
	default:
		if strings.TrimSpace(action.MessageFormula) != "" {
			return automationRefused("messageFormula goes only with a notification, an email, a Slack message or a confirmation")
		}
	}
	switch action.Type {
	case interactiveAutomationActionDefineVariables:
		if len(action.Variables) < 1 {
			return automationRefused("variables is empty: name at least one")
		}
		for index := range action.Variables {
			variable := &action.Variables[index]
			variable.Name = strings.TrimSpace(variable.Name)
			if !boundedAutomationString(variable.Name, interactiveAutomationMaxLabelRunes) {
				return automationRefused("variables[%d].name is 1–%d characters", index, interactiveAutomationMaxLabelRunes)
			}
			if _, exists := variables[variable.Name]; exists {
				return automationRefused("variables[%d].name %q is an earlier variable's too", index, variable.Name)
			}
			if err := normalizeInteractiveAutomationValue(&variable.Value, variables); err != nil {
				return refusalAt(err, fmt.Sprintf("variables[%d].value.", index))
			}
			variables[variable.Name] = struct{}{}
		}
	case interactiveAutomationActionEditProperty:
		if !automationTargetIsTrigger(action.Target) {
			return errAutomationTargetNotTrigger
		}
		action.PropertyID = strings.TrimSpace(action.PropertyID)
		if !boundedAutomationString(action.PropertyID, interactiveAutomationMaxIdentifierRunes) {
			return automationRefused("propertyId names the property to edit, 1–%d characters", interactiveAutomationMaxIdentifierRunes)
		}
		return refusalAt(normalizeInteractiveAutomationValue(&action.Value, variables), "value.")
	case interactiveAutomationActionInsertBlocks:
		if !automationTargetIsTrigger(action.Target) {
			return errAutomationTargetNotTrigger
		}
		switch {
		case len(action.Blocks) < 1 && action.SourceBlockID == "":
			return automationRefused("blocks is empty and no sourceBlockId names blocks to copy: name one of them")
		case action.SourceBlockID != "" && len(action.Blocks) > 0:
			return automationRefused("blocks and sourceBlockId aren't both named: the blocks are written out or copied")
		case action.SourceBlockID != "" && !boundedAutomationString(action.SourceBlockID, interactiveAutomationMaxIdentifierRunes):
			return automationRefused("sourceBlockId is 1–%d characters", interactiveAutomationMaxIdentifierRunes)
		case action.Placement != "" && !interactiveAutomationBlockInsertionPositions[action.Placement]:
			return automationRefused("placement %q isn't a place blocks go on the page", action.Placement)
		}
		for index := range action.Blocks {
			if err := normalizeInteractiveAutomationBlock(&action.Blocks[index]); err != nil {
				return refusalAt(err, fmt.Sprintf("blocks[%d].", index))
			}
		}
	case interactiveAutomationActionShowConfirmation:
		return normalizeInteractiveAutomationConfirmation(action)
	case interactiveAutomationActionOpenPage:
		action.PageID = strings.TrimSpace(action.PageID)
		// Where it opens, as Notion's "에서" choice (seen 2026-11-02): beside
		// the page, over it, or in its place; none is over it.
		switch action.OpenIn {
		case "", "side_peek", "center_peek", "full_page":
		default:
			return errAutomationOpenIn
		}
		// "이 페이지", as Notion's (seen 2026-11-02): the page the button
		// is on, or the row whose button was pressed, named by no page.
		if len(action.Target) > 0 {
			if !automationTargetIsTrigger(action.Target) {
				return errAutomationTargetNotTrigger
			}
			if action.PageID != "" {
				return automationRefused("pageId and target aren't both named: target trigger_page opens the button's own page")
			}
			break
		}
		if !boundedAutomationString(action.PageID, interactiveAutomationMaxIdentifierRunes) {
			return automationRefused("pageId names the page to open, 1–%d characters, or target is trigger_page", interactiveAutomationMaxIdentifierRunes)
		}
	case interactiveAutomationActionOpenForm:
		action.DatabaseID, action.ViewID = strings.TrimSpace(action.DatabaseID), strings.TrimSpace(action.ViewID)
		if !boundedAutomationString(action.DatabaseID, interactiveAutomationMaxIdentifierRunes) ||
			!boundedAutomationString(action.ViewID, interactiveAutomationMaxIdentifierRunes) {
			return automationRefused("databaseId and viewId name the form's database and its form view, 1–%d characters each", interactiveAutomationMaxIdentifierRunes)
		}
		// Where its answer page opens, as a page's (Notion's, seen 2026-11-05).
		switch action.OpenIn {
		case "", "side_peek", "center_peek", "full_page":
		default:
			return errAutomationOpenIn
		}
	case interactiveAutomationActionOpenUrl:
		parsed, err := url.Parse(strings.TrimSpace(action.URL))
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
			return automationRefused("url is an http or https address with a host and no user name or password")
		}
		action.URL = parsed.String()
	case interactiveAutomationActionAddPage:
		return normalizeInteractiveAutomationAddPage(action, variables)
	case interactiveAutomationActionEditPages:
		return normalizeInteractiveAutomationEditPages(action, variables)
	case interactiveAutomationActionArchivePages:
		// Notion's "페이지 보관하기…" names its pages as "페이지 편집 위치"
		// does and sets nothing (seen 2026-11-15).
		if len(action.Changes) > 0 {
			return automationRefused("changes goes only with edit_pages: archive_pages archives the pages its target names")
		}
		return normalizeInteractiveAutomationEditPages(action, variables)
	case interactiveAutomationActionSetPageIcon:
		return normalizeInteractiveAutomationPageIcon(action)
	case interactiveAutomationActionSendNotification, interactiveAutomationActionSendEmail,
		interactiveAutomationActionSendWebhook, interactiveAutomationActionSendSlack:
		return normalizeInteractiveAutomationDeliveryAction(action)
	case interactiveAutomationActionRunAppAction:
		return normalizeInteractiveAutomationAppAction(action)
	default:
		return automationRefused("type %q isn't a step an automation takes", action.Type)
	}
	return nil
}

func normalizeInteractiveAutomationValue(
	value *interactiveAutomationValueExpression,
	variables map[string]struct{},
) error {
	value.Kind = strings.TrimSpace(value.Kind)
	// Only words written out hold mentions (interactive_automation_message.go).
	if value.Kind != "literal" && len(value.Spans) > 0 {
		return automationRefused("spans go only with kind literal")
	}
	if err := normalizeInteractiveAutomationPeopleMode(value); err != nil {
		return err
	}
	switch value.Kind {
	case "literal":
		var canonical any
		if len(value.Value) == 0 || len(value.Value) > interactiveAutomationMaxDocumentBytes || json.Unmarshal(value.Value, &canonical) != nil {
			return automationRefused("value is the JSON value a literal holds")
		}
		value.Value, _ = json.Marshal(canonical)
		if len(value.Spans) > 0 {
			return normalizeAutomationTextValue(value)
		}
	case "variable":
		value.Name = strings.TrimSpace(value.Name)
		if _, exists := variables[value.Name]; !exists {
			return automationRefused("name %q is no variable an earlier define_variables step names", value.Name)
		}
	case "execution_time", "execution_date":
	case "trigger_property":
		value.PropertyID = strings.TrimSpace(value.PropertyID)
		if !boundedAutomationString(value.PropertyID, interactiveAutomationMaxIdentifierRunes) {
			return automationRefused("propertyId names the triggering page's property, 1–%d characters", interactiveAutomationMaxIdentifierRunes)
		}
	case "formula":
		return normalizeInteractiveAutomationFormula(value)
	default:
		return automationRefused("kind is literal, variable, execution_time, execution_date, trigger_property or formula, not %q", value.Kind)
	}
	return nil
}

func normalizeInteractiveAutomationBlock(
	block *interactiveAutomationBlockTemplate,
) error {
	// As many blocks, as deep, as the settings document holds (64 KB).
	block.Type = strings.TrimSpace(block.Type)
	if _, supported := graphBlockTypes[block.Type]; !supported {
		return errInteractiveAutomationUnsupported
	}
	if block.Type == graphBlockTypeChildPage || block.Type == graphBlockTypeChildDatabase || block.Type == graphBlockTypeInlineDatabase {
		return errInteractiveAutomationUnsupported
	}
	if block.ID != "" && !boundedAutomationString(block.ID, interactiveAutomationMaxIdentifierRunes) {
		return automationRefused("id is left out, or 1–%d characters", interactiveAutomationMaxIdentifierRunes)
	}
	if len(block.Content) == 0 {
		block.Content = json.RawMessage(`{}`)
	}
	object := map[string]any{}
	if json.Unmarshal(block.Content, &object) != nil {
		return automationRefused("content is an object")
	}
	if text, exists := object["text"]; exists {
		if _, ok := text.(string); !ok {
			return automationRefused("content.text is text")
		}
	}
	if rich, exists := object["rich"]; exists {
		encoded, _ := json.Marshal(rich)
		spans := []graphTextSpan{}
		if json.Unmarshal(encoded, &spans) != nil {
			return automationRefused("content.rich is a list of text spans")
		}
		if _, supportsText := graphTextBlockTypes[block.Type]; !supportsText {
			return automationRefused("content.rich goes only with a block that holds text, not %s", block.Type)
		}
		// Body drafts share native body-text rules (date reminders, comments,
		// template mentions and equations), rather than caption/cell rules.
		input := graphMutationInput{Operations: []graphOperation{{Kind: graphOperationKindUpsertBlock,
			Block: &graphBlock{ID: "draft", Type: block.Type}}}, TextUpdates: []graphTextUpdate{{BlockID: "draft", Spans: spans}}}
		if err := validateContentGraphCompoundInput(&input); err != nil {
			return automationRefused("content.rich %s", firstNonempty(strings.TrimPrefix(refusalReason(err), "textUpdates[0]."), "holds a span a page's text doesn't"))
		}
	}
	block.Content, _ = json.Marshal(object)
	for index := range block.Children {
		if err := normalizeInteractiveAutomationBlock(&block.Children[index]); err != nil {
			return refusalAt(err, fmt.Sprintf("children[%d].", index))
		}
	}
	return nil
}

func normalizeInteractiveAutomationConfirmation(action *interactiveAutomationAction) error {
	title := ""
	if len(action.Title) > 0 && json.Unmarshal(action.Title, &title) != nil {
		return automationRefused("title is text")
	}
	title = strings.TrimSpace(title)
	action.ConfirmLabel = strings.TrimSpace(action.ConfirmLabel)
	action.CancelLabel = strings.TrimSpace(action.CancelLabel)
	if action.CancelLabel == "" {
		action.CancelLabel = "Cancel"
	}
	// Its words may hold mentions or be one formula, as a notification's
	// (interactive_automation_message.go), as Notion's (seen 2026-11-01).
	switch {
	case len(action.Title) > 0 && !boundedAutomationString(title, 200):
		return automationRefused("title is 1–200 characters")
	case !normalizeAutomationMessage(action):
		return errAutomationMessage
	case !boundedAutomationString(action.ConfirmLabel, interactiveAutomationMaxLabelRunes):
		return automationRefused("confirmLabel is 1–%d characters", interactiveAutomationMaxLabelRunes)
	case !boundedAutomationString(action.CancelLabel, interactiveAutomationMaxLabelRunes):
		return automationRefused("cancelLabel is at most %d characters", interactiveAutomationMaxLabelRunes)
	}
	if len(action.Title) > 0 {
		action.Title, _ = json.Marshal(title)
	}
	return nil
}

func normalizeInteractiveAutomationAddPage(action *interactiveAutomationAction, variables map[string]struct{}) error {
	if !automationTargetIsDatabase(action.Target) {
		return automationRefused("target is database: the page is added to databaseId")
	}
	action.DatabaseID = strings.TrimSpace(action.DatabaseID)
	if !boundedAutomationString(action.DatabaseID, interactiveAutomationMaxIdentifierRunes) {
		return automationRefused("databaseId names the database to add the page to, 1–%d characters", interactiveAutomationMaxIdentifierRunes)
	}
	if len(action.Title) == 0 {
		return automationRefused("title is the new page's title: text, or a value as a property's is")
	}
	var expression interactiveAutomationValueExpression
	if action.Title[0] == '"' {
		expression = interactiveAutomationValueExpression{Kind: "literal", Value: action.Title}
	} else if json.Unmarshal(action.Title, &expression) != nil {
		return automationRefused("title is text, or a value as a property's is")
	}
	if err := normalizeInteractiveAutomationValue(&expression, variables); err != nil {
		return refusalAt(err, "title.")
	}
	action.Title, _ = json.Marshal(expression)
	if action.TemplateID = strings.TrimSpace(action.TemplateID); action.TemplateID != "" &&
		!boundedAutomationString(action.TemplateID, interactiveAutomationMaxIdentifierRunes) {
		return automationRefused("templateId is left out, or 1–%d characters", interactiveAutomationMaxIdentifierRunes)
	}
	return refusalAt(normalizeInteractiveAutomationChanges(action.Properties, variables), "properties")
}

func normalizeInteractiveAutomationEditPages(action *interactiveAutomationAction, variables map[string]struct{}) error {
	var target interactiveAutomationPageTarget
	if json.Unmarshal(action.Target, &target) != nil || (target.Type != "database" && target.Type != interactiveAutomationTargetThisPage) {
		return automationRefused("target is an object whose type is database or %s", interactiveAutomationTargetThisPage)
	}
	if len(target.Filter) > 32768 {
		return automationRefused("target.filter is %d bytes, over 32768", len(target.Filter))
	}
	target.DatabaseID = strings.TrimSpace(target.DatabaseID)
	if !boundedAutomationString(target.DatabaseID, interactiveAutomationMaxIdentifierRunes) {
		return automationRefused("target.databaseId names the database whose pages are edited or archived, 1–%d characters", interactiveAutomationMaxIdentifierRunes)
	}
	action.DatabaseID = target.DatabaseID
	if target.Type == interactiveAutomationTargetThisPage {
		return refusalAt(normalizeInteractiveAutomationChanges(action.Changes, variables), "changes")
	}
	if !json.Valid(target.Filter) {
		return automationRefused("target.filter is a database filter in JSON")
	}
	if interactiveAutomationUsesFilter(*action) {
		request, err := interactiveAutomationFilterRequest(*action, time.Unix(0, 0))
		if err != nil {
			return err
		}
		if _, _, err := canonicalDatabaseQueryRequest("automation", "automation", request); err != nil {
			return automationRefused("target.filter isn't a filter the database's view takes")
		}
	} else if target.Limit < 1 || target.Limit > interactiveAutomationMaxTargetRows {
		return automationRefused("target.limit is 1 to %d pages", interactiveAutomationMaxTargetRows)
	}
	return refusalAt(normalizeInteractiveAutomationChanges(action.Changes, variables), "changes")
}

func normalizeInteractiveAutomationChanges(changes []interactiveAutomationPropertyChange, variables map[string]struct{}) error {
	for index := range changes {
		changes[index].PropertyID = strings.TrimSpace(changes[index].PropertyID)
		if !boundedAutomationString(changes[index].PropertyID, interactiveAutomationMaxIdentifierRunes) {
			return automationRefused("[%d].propertyId names the property to set, 1–%d characters", index, interactiveAutomationMaxIdentifierRunes)
		}
		if err := normalizeInteractiveAutomationValue(&changes[index].Value, variables); err != nil {
			return refusalAt(err, fmt.Sprintf("[%d].value.", index))
		}
	}
	return nil
}

func automationTargetIsTrigger(raw json.RawMessage) bool {
	var target string
	return json.Unmarshal(raw, &target) == nil && target == "trigger_page"
}

func automationTargetIsDatabase(raw json.RawMessage) bool {
	var target string
	return json.Unmarshal(raw, &target) == nil && target == "database"
}

func boundedAutomationString(value string, maximum int) bool {
	return value != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= maximum
}

func automationDigest(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}

func automationStableID(prefix string, parts ...string) string {
	return fmt.Sprintf("%s_%s", prefix, automationDigest(parts...)[:24])
}

func automationPositionAfter(sourcePosition, executionID, actionID string, index int) string {
	suffix := fmt.Sprintf("~automation~%s~%03d", automationDigest(executionID, actionID)[:12], index)
	prefix := sourcePosition
	for len(prefix)+len(suffix) > maxGraphPositionLength {
		_, size := utf8.DecodeLastRuneInString(prefix)
		if size == 0 {
			break
		}
		prefix = prefix[:len(prefix)-size]
	}
	return prefix + suffix
}
