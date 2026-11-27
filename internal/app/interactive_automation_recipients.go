package app

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Static IDs remain the version-one representation. Sources are resolved in
// action order in the mutation transaction, then only fixed IDs enter the outbox.
func normalizeInteractiveAutomationRecipientSources(sources []interactiveAutomationRecipientSource) error {
	seen := map[string]bool{}
	for index := range sources {
		source := &sources[index]
		source.Kind = strings.TrimSpace(source.Kind)
		source.PropertyID = strings.TrimSpace(source.PropertyID)
		source.Expression = strings.TrimSpace(source.Expression)
		switch source.Kind {
		case interactiveAutomationRecipientExecutionActor, interactiveAutomationRecipientPageCreator:
			if source.PropertyID != "" || source.Expression != "" {
				return automationRefused("[%d]: propertyId and expression don't go with kind %s", index, source.Kind)
			}
		case interactiveAutomationRecipientPersonProperty:
			if !boundedAutomationString(source.PropertyID, interactiveAutomationMaxIdentifierRunes) || source.Expression != "" {
				return automationRefused("[%d].propertyId names the person property, 1–%d characters, with no expression", index, interactiveAutomationMaxIdentifierRunes)
			}
		case interactiveAutomationRecipientFormula:
			value := interactiveAutomationValueExpression{Kind: "formula", Expression: source.Expression}
			if source.PropertyID != "" {
				return automationRefused("[%d].propertyId doesn't go with kind formula", index)
			}
			if err := normalizeInteractiveAutomationFormula(&value); err != nil {
				return refusalAt(err, fmt.Sprintf("[%d].", index))
			}
		default:
			return automationRefused("[%d].kind is %s, %s, %s or %s, not %q", index, interactiveAutomationRecipientExecutionActor,
				interactiveAutomationRecipientPageCreator, interactiveAutomationRecipientPersonProperty, interactiveAutomationRecipientFormula, source.Kind)
		}
		key := source.Kind + "\x00" + source.PropertyID + "\x00" + source.Expression
		if seen[key] {
			return automationRefused("[%d] names the same recipients as one before it", index)
		}
		seen[key] = true
	}
	return nil
}

// A schedule has no clicking person or triggering row. Its execution principal
// authorizes it; that principal is not impersonated as a notification recipient.
func (formulas *interactiveAutomationFormulas) contextScope() (databaseFormulaScope, error) {
	people := newDatabaseFormulaPeople(formulas.app, nil)
	if formulas.resolver != nil {
		people = formulas.resolver.context.People
	}
	person := func(id string) any {
		if id == "" {
			return nil
		}
		return people.personReference(id, "")
	}
	actor := formulas.authorization.Workspace.PrincipalID
	creator := ""
	if formulas.source.Kind == "database_event" {
		actor = formulas.source.ActorID
	}
	if formulas.source.Kind == "database_schedule" {
		actor = ""
	} else if formulas.source.TriggerRowID != "" {
		creator = formulas.row.CreatedBy
	} else if formulas.authorization.Page != nil {
		creator = formulas.authorization.Page.GetString("created_by")
	}
	scope := databaseFormulaScope{
		interactiveAutomationFormulaExecutionActorName: person(actor), interactiveAutomationFormulaPageCreatorName: person(creator),
		interactiveAutomationFormulaExecutionTimeName: databaseFormulaInstant(formulas.now.UnixMilli()),
		interactiveAutomationFormulaExecutionDateName: formulas.executionDate(),
		interactiveAutomationFormulaTriggerPageName:   "",
	}
	if formulas.resolver != nil {
		scope[interactiveAutomationFormulaTriggerPageName] = formulas.triggerPage()
	} else if page, ok := formulas.buttonPage(); ok {
		scope[interactiveAutomationFormulaTriggerPageName] = page
	}
	return scope, nil
}

// executionDate is the day the run began, as the person whose authority it
// runs on keeps their days (their time zone, else the server's): Notion's "자동 실행된
// 날짜" (seen 2026-10-23), a date without a time.
func (formulas *interactiveAutomationFormulas) executionDate() any {
	return databaseFormulaDate{Text: formulas.executionDay()}
}

// executionDay is that day as a date property keeps one: "2006-01-02", on
// the run's clock.
func (formulas *interactiveAutomationFormulas) executionDay() string {
	return formulas.now.In(formulas.clock()).Format("2006-01-02")
}

// triggerPage is the row that triggered the run, as a page: Notion's "페이지
// 실행", whose properties follow it — triggerPage.prop("Name") — as they
// were when it was triggered.
func (formulas *interactiveAutomationFormulas) triggerPage() any {
	title := ""
	for _, value := range formulas.row.Values {
		if value.Type == graphValueTypeTitle {
			title, _ = databaseQueryRawValue(value.Value).(string)
			break
		}
	}
	return databaseFormulaReference{Kind: "page", ID: formulas.row.ID, Name: databaseFormulaTrim(title), DataSourceID: formulas.source.TriggerDatabaseID}
}

func (formulas *interactiveAutomationFormulas) resolveNotificationRecipients(
	action interactiveAutomationAction, variables map[string]json.RawMessage,
) (interactiveAutomationAction, error) {
	if len(action.RecipientSources) == 0 {
		return action, nil
	}
	if err := formulas.load(); err != nil {
		return action, err
	}
	context, err := formulas.contextScope()
	if err != nil {
		return action, err
	}
	ids := append([]string(nil), action.RecipientIDs...)
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	for _, source := range action.RecipientSources {
		var value any
		switch source.Kind {
		case interactiveAutomationRecipientExecutionActor:
			value = context[interactiveAutomationFormulaExecutionActorName]
		case interactiveAutomationRecipientPageCreator:
			value = context[interactiveAutomationFormulaPageCreatorName]
		case interactiveAutomationRecipientPersonProperty:
			if formulas.resolver == nil {
				return action, errInteractiveAutomationInvalid
			}
			property, found := formulas.resolver.context.PropertiesByID[source.PropertyID]
			if !found || property.Type != graphValueTypePerson || property.DataSourceID != formulas.source.TriggerDatabaseID {
				return action, errInteractiveAutomationInvalid
			}
			value, err = formulas.resolver.formulaProperty(formulas.row, property)
		case interactiveAutomationRecipientFormula:
			value, err = formulas.evaluateValue(source.Expression, variables)
		default:
			return action, errInteractiveAutomationInvalid
		}
		if err != nil {
			return action, err
		}
		people, err := interactiveAutomationRecipientPeople(value)
		if err != nil {
			return action, err
		}
		for _, id := range people {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	if len(ids) > 0 {
		valid, err := databaseAutomationNotificationRecipients(formulas.app, formulas.authorization.Workspace.WorkspaceID, ids)
		if err != nil {
			return action, err
		}
		for _, id := range ids {
			if _, found := valid[id]; !found {
				return action, errInteractiveAutomationInvalid
			}
		}
	}
	action.RecipientIDs, action.RecipientSources = ids, nil
	return action, nil
}

// Notion permits one user, a flat person list, or an undefined value. Text,
// page references and mixed or nested lists are not notification addresses.
func interactiveAutomationRecipientPeople(value any) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	items, list := value.([]any)
	if !list {
		items = []any{value}
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		reference, person := item.(databaseFormulaReference)
		if !person || reference.Kind != "person" || !boundedAutomationString(reference.ID, interactiveAutomationMaxIdentifierRunes) {
			return nil, errInteractiveAutomationInvalid
		}
		// A connection's bot a person value names (notion_people_value_bots.go)
		// has no inbox, nor someone invited (people_value_invitees.go): the
		// others are sent to.
		if notionBotUserIDShape(reference.ID) || peopleValueInviteeIDShape(reference.ID) {
			continue
		}
		ids = append(ids, reference.ID)
	}
	return ids, nil
}
