package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
)

// A person property's value an action sets, as Notion's (seen 2026-11-11):
// people picked by name, and among them who runs it ("실행한 모든 사람", a
// button's "클릭한 모든 사람") or made the page ("페이지 작성자") as
// {"source": kind}; put in place of what the property holds ("다음으로
// 교체…"), or added to it ("추가…"), taken from it ("제거…") or toggled in it
// ("토글…") by the value's mode. A multi-select's options are put by the
// same modes ("태그 속성에서 빨강 및 파랑 토글", Notion's, seen 2026-11-11),
// and a relation's pages, among them the page the run starts from ("관계
// 속성을 가 및 페이지 실행 값으로 바꾸기", seen 2026-11-22).
// Nothing picked clears it whatever the mode, as its card says ("담당자
// 지우기").
const (
	// interactiveAutomationValueTriggerPage names, among a relation value's
	// pages, the page the run starts from.
	interactiveAutomationValueTriggerPage = "trigger_page"
	interactiveAutomationPeopleAdd        = "add"
	interactiveAutomationPeopleRemove     = "remove"
	interactiveAutomationPeopleToggle     = "toggle"
)

// normalizeInteractiveAutomationPeopleMode checks a value's mode: none, or
// one of the three on people or options written out.
func normalizeInteractiveAutomationPeopleMode(value *interactiveAutomationValueExpression) error {
	value.Mode = strings.TrimSpace(value.Mode)
	switch value.Mode {
	case "":
		return nil
	case interactiveAutomationPeopleAdd, interactiveAutomationPeopleRemove, interactiveAutomationPeopleToggle:
		if value.Kind == "literal" {
			return nil
		}
		return automationRefused("mode goes only with kind literal: people or options written out")
	}
	return automationRefused("mode is left out, or add, remove or toggle, not %q", value.Mode)
}

// cellFor is the cell an action's value sets on a row's property: the value
// as this run makes it, a person's people found and put by its mode, a
// multi-select's options put by its mode, kept as the property's type keeps
// it.
func (plan *interactiveAutomationPlan) cellFor(
	property interactiveAutomationProperty,
	expression interactiveAutomationValueExpression,
	row *plannedInteractiveAutomationRow,
	variables map[string]json.RawMessage,
	triggerValues map[string]json.RawMessage,
	executionTime time.Time,
) (interactiveAutomationRowValue, error) {
	evaluated, err := evaluateInteractiveAutomationValue(expression, variables, triggerValues, executionTime, plan.formulas)
	if err != nil {
		return interactiveAutomationRowValue{}, errInteractiveAutomationInvalid
	}
	if property.Type == graphValueTypePerson && expression.Kind == "literal" {
		people, err := plan.people(evaluated)
		if err != nil {
			return interactiveAutomationRowValue{}, err
		}
		if people, err = plan.byMode(expression.Mode, row, property.ID, people); err != nil {
			return interactiveAutomationRowValue{}, err
		}
		evaluated, _ = json.Marshal(people)
	} else if property.Type == graphValueTypeRelation && expression.Kind == "literal" {
		pages, err := plan.relationPages(evaluated)
		if err != nil {
			return interactiveAutomationRowValue{}, err
		}
		if pages, err = plan.byMode(expression.Mode, row, property.ID, pages); err != nil {
			return interactiveAutomationRowValue{}, err
		}
		evaluated, _ = json.Marshal(pages)
	} else if property.Type == graphValueTypeMultiSelect && expression.Kind == "literal" && expression.Mode != "" {
		options, err := plan.byMode(expression.Mode, row, property.ID, interactiveAutomationOptionIDs(evaluated))
		if err != nil {
			return interactiveAutomationRowValue{}, err
		}
		evaluated, _ = json.Marshal(options)
	}
	cell, valid := interactiveAutomationCellForType(property, evaluated)
	if !valid {
		return cell, errInteractiveAutomationInvalid
	}
	return cell, nil
}

// people is a person value's people by ID, each once in the order written:
// who a source names on this run in its place — nobody where none is, as a
// schedule's runner.
func (plan *interactiveAutomationPlan) people(value json.RawMessage) ([]string, error) {
	var items []any
	if json.Unmarshal(value, &items) != nil {
		var none any
		if json.Unmarshal(value, &none) == nil && (none == nil || none == "") {
			return []string{}, nil
		}
		return nil, errInteractiveAutomationInvalid
	}
	people, seen := make([]string, 0, len(items)), map[string]bool{}
	for _, item := range items {
		id := ""
		switch typed := item.(type) {
		case string:
			id = strings.TrimSpace(typed)
		case map[string]any:
			if source, named := typed["source"].(string); named {
				person, err := plan.formulas.contextPerson(source)
				if err != nil {
					return nil, err
				}
				id = person
			} else {
				id, _ = typed["id"].(string)
			}
		default:
			return nil, errInteractiveAutomationInvalid
		}
		if id == "" || seen[id] {
			continue
		}
		if !boundedAutomationString(id, interactiveAutomationMaxIdentifierRunes) {
			return nil, errInteractiveAutomationInvalid
		}
		seen[id] = true
		people = append(people, id)
	}
	return people, nil
}

// relationPages is a relation value's pages by ID, each once in the order
// picked: the page the run starts from ({"source": "trigger_page"}, Notion's
// "페이지 실행") in its place — none where the run has no page, as a
// schedule's or a page's button.
func (plan *interactiveAutomationPlan) relationPages(value json.RawMessage) ([]string, error) {
	var items []any
	if json.Unmarshal(value, &items) != nil {
		var none any
		if json.Unmarshal(value, &none) == nil && (none == nil || none == "") {
			return []string{}, nil
		}
		return nil, errInteractiveAutomationInvalid
	}
	pages, seen := make([]string, 0, len(items)), map[string]bool{}
	for _, item := range items {
		id := ""
		switch typed := item.(type) {
		case string:
			id = strings.TrimSpace(typed)
		case map[string]any:
			if source, named := typed["source"].(string); named {
				if source != interactiveAutomationValueTriggerPage {
					return nil, errInteractiveAutomationInvalid
				}
				id = plan.formulas.source.TriggerRowID
			} else {
				id, _ = typed["id"].(string)
			}
		default:
			return nil, errInteractiveAutomationInvalid
		}
		if id == "" || seen[id] {
			continue
		}
		if !boundedAutomationString(id, interactiveAutomationMaxIdentifierRunes) {
			return nil, errInteractiveAutomationInvalid
		}
		seen[id] = true
		pages = append(pages, id)
	}
	return pages, nil
}

// byMode is what a property holds once a value's items meet what it holds
// by the value's mode: the items alone with no mode or none picked.
func (plan *interactiveAutomationPlan) byMode(mode string, row *plannedInteractiveAutomationRow, propertyID string, items []string) ([]string, error) {
	if mode == "" || len(items) == 0 {
		return items, nil
	}
	held, err := plan.heldItems(row, propertyID)
	if err != nil {
		return nil, err
	}
	return interactiveAutomationItemsByMode(mode, held, items), nil
}

// interactiveAutomationOptionIDs is a multi-select value's options, each once
// in the order written; anything but a list of them is none.
func interactiveAutomationOptionIDs(value json.RawMessage) []string {
	var items []any
	_ = json.Unmarshal(value, &items)
	options, seen := make([]string, 0, len(items)), map[string]bool{}
	for _, item := range items {
		id, _ := item.(string)
		if id = strings.TrimSpace(id); id != "" && !seen[id] {
			seen[id] = true
			options = append(options, id)
		}
	}
	return options
}

// contextPerson is who a person source names on this run, as a formula's
// executionActor and pageCreator name them (contextScope).
func (formulas *interactiveAutomationFormulas) contextPerson(source string) (string, error) {
	name := ""
	switch source {
	case interactiveAutomationRecipientExecutionActor:
		name = interactiveAutomationFormulaExecutionActorName
	case interactiveAutomationRecipientPageCreator:
		name = interactiveAutomationFormulaPageCreatorName
	default:
		return "", errInteractiveAutomationInvalid
	}
	if err := formulas.load(); err != nil {
		return "", err
	}
	scope, err := formulas.contextScope()
	if err != nil {
		return "", err
	}
	if reference, ok := scope[name].(databaseFormulaReference); ok && reference.Kind == "person" {
		return reference.ID, nil
	}
	return "", nil
}

// heldItems is what a row's person, relation or multi-select property holds now: what
// this run set there already, else what is kept; a page this run adds holds
// nothing.
func (plan *interactiveAutomationPlan) heldItems(row *plannedInteractiveAutomationRow, propertyID string) ([]string, error) {
	raw := ""
	if cell, set := row.Values[propertyID]; set {
		raw = cell.Value
	} else if !row.Insert {
		err := plan.formulas.app.DB().NewQuery(`SELECT [[value]] FROM {{leafen_database_row_values}}
			WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[row_id]] = {:rowId} AND [[property_id]] = {:propertyId}`).Bind(dbx.Params{
			"owner": plan.formulas.authorization.Workspace.OwnerID, "pageId": row.PageID, "rowId": row.ID, "propertyId": propertyID,
		}).Row(&raw)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	held := []string{}
	for _, item := range databaseFormulaItems(databaseQueryRawValue(json.RawMessage(raw))) {
		id := databaseQueryValueText(item)
		if object, ok := item.(map[string]any); ok {
			id, _ = object["id"].(string)
		}
		if id != "" {
			held = append(held, id)
		}
	}
	return held, nil
}

// interactiveAutomationItemsByMode is what a person or multi-select property
// holds once a value's items are added to what it holds, taken from it, or
// toggled in it — each taken out where it is, put at the end where it is not.
func interactiveAutomationItemsByMode(mode string, held []string, people []string) []string {
	picked := map[string]bool{}
	for _, id := range people {
		picked[id] = true
	}
	result, kept := []string{}, map[string]bool{}
	for _, id := range held {
		if kept[id] || picked[id] && mode != interactiveAutomationPeopleAdd {
			continue
		}
		kept[id] = true
		result = append(result, id)
	}
	if mode == interactiveAutomationPeopleRemove {
		return result
	}
	wasHeld := map[string]bool{}
	for _, id := range held {
		wasHeld[id] = true
	}
	for _, id := range people {
		if kept[id] || mode == interactiveAutomationPeopleToggle && wasHeld[id] {
			continue
		}
		kept[id] = true
		result = append(result, id)
	}
	return result
}
