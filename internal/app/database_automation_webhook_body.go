package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// An automation's or a button's "웹훅 보내기" posts what Notion's posts
// (seen 2026-11-09): {"source": {"type": "automation", "automation_id",
// "action_id", "event_id", "attempt"}, "data": the page it ran for, as the
// Public API gives it}. An automation's database page carries the
// properties the action chose, by name and in the API's form, empty ones
// too; a button sends its page with all of its properties and says who
// pressed it ("user_id").

// databaseAutomationWebhookBody is the body a webhook action sends, read
// when the automation runs; its attempt is the delivery's own, set as it
// goes (databaseAutomationWebhookBodyAttempt).
func databaseAutomationWebhookBody(
	app core.App,
	candidate databaseAutomationEvaluationCandidate,
	action interactiveAutomationAction,
	outcomeID string,
) (map[string]any, error) {
	// Its ids are UUIDs, as Notion's, each the same for the same
	// automation, action and run.
	source := map[string]any{
		"type":          "automation",
		"automation_id": notionWebhookUUID("automation\x00" + candidate.DefinitionID),
		"action_id":     notionWebhookUUID("automation-action\x00" + candidate.DefinitionID + "\x00" + action.ID),
		"event_id":      notionWebhookUUID("automation-event\x00" + candidate.EventID),
		"attempt":       1,
	}
	if interactiveAutomationButtonDelivery(candidate.DefinitionID) && candidate.ActorID != "" {
		if userID, err := notionCompatibleUserID(candidate.ActorID); err == nil {
			source["user_id"] = userID
		}
	}
	data, err := databaseAutomationWebhookPage(app, candidate, action.PropertyIDs)
	if err != nil {
		return nil, err
	}
	if data != nil {
		// The same run sends the same request, whenever it is admitted.
		data["request_id"] = notionWebhookUUID("automation-request\x00" + outcomeID + "\x00" + action.ID)
	}
	return map[string]any{"source": source, "data": data}, nil
}

// databaseAutomationWebhookPage is the page an automation ran for as the
// Public API reads it to whoever the automation runs as: a database page
// with the properties the automation chose, or a button's page with all of
// its own, as a button chooses none (Notion's help: "database button
// properties can't be selected"; its page button sends the page's title);
// nil for a schedule's run, which is about no page.
func databaseAutomationWebhookPage(
	app core.App,
	candidate databaseAutomationEvaluationCandidate,
	propertyIDs []string,
) (map[string]any, error) {
	principalID := candidate.RunnerID
	if principalID == "" {
		principalID = candidate.ActorID
	}
	identity := notionOAuthBearerIdentity{
		PrincipalID: principalID, ClientID: "leafen:automation", WorkspaceID: candidate.WorkspaceID,
		Scopes: append([]string{}, notionOAuthDefaultScopes...),
	}
	// Its links go where a mail's would: the address this server knows is
	// its own, with no request to go by.
	origin := mailServerOrigin(app, &http.Request{})
	fileIssuer := &notionBlockFileCapabilityIssuer{origin: origin, principalID: principalID, issuedAt: time.Now().UTC()}
	rowID := candidate.RowID
	if rowID == "" && interactiveAutomationButtonDelivery(candidate.DefinitionID) {
		page, err := app.FindRecordById(pagesCollection, candidate.PageID)
		if err != nil {
			return nil, err
		}
		if !isDatabaseRowPageRecord(page) {
			return notionPageObjectAt(app, origin, fileIssuer, identity, page)
		}
		rowID = page.Id
	}
	if rowID == "" {
		return nil, nil
	}
	current, err := authorizeNotionDatabaseRowPageCurrent(app, identity, rowID, permissionWorkspaceRead)
	if err != nil {
		return nil, err
	}
	identities, err := ensureNotionDatabaseRowPagePublicIdentities(app, current)
	if err != nil {
		return nil, err
	}
	projected, err := loadNotionDatabaseRowPageProperties(app, current, fileIssuer)
	if err != nil {
		return nil, err
	}
	if interactiveAutomationButtonDelivery(candidate.DefinitionID) {
		return notionDatabaseRowPageObject(app, origin, current, identities, projected, fileIssuer)
	}
	chosen := make(map[string]struct{}, len(propertyIDs))
	for _, id := range propertyIDs {
		chosen[id] = struct{}{}
	}
	selected := make([]notionDatabaseRowPageProjectedProperty, 0, len(propertyIDs))
	for _, property := range projected {
		if _, ok := chosen[property.ID]; ok {
			selected = append(selected, property)
		}
	}
	if err := peopleWebhookEmail(app, candidate, selected); err != nil {
		return nil, err
	}
	return notionDatabaseRowPageObject(app, origin, current, identities, selected, fileIssuer)
}

// databaseAutomationWebhookBodyAttempt gives a webhook's body the attempt
// it goes out on, as Notion's "source.attempt", keeping the rest as stored.
func databaseAutomationWebhookBodyAttempt(body json.RawMessage, attempt int) json.RawMessage {
	var parts struct {
		Source map[string]json.RawMessage `json:"source"`
		Data   json.RawMessage            `json:"data"`
	}
	if attempt < 1 || json.Unmarshal(body, &parts) != nil || parts.Source == nil || len(parts.Data) == 0 {
		return body
	}
	parts.Source["attempt"], _ = json.Marshal(attempt)
	source, err := json.Marshal(parts.Source)
	if err != nil {
		return body
	}
	var result bytes.Buffer
	result.WriteString(`{"source":`)
	result.Write(source)
	result.WriteString(`,"data":`)
	result.Write(parts.Data)
	result.WriteString(`}`)
	return result.Bytes()
}
