package app

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type interactiveAutomationSource struct {
	Kind                string
	ID                  string
	Document            json.RawMessage
	Position            string
	ParentID            string
	TriggerDatabaseID   string
	TriggerRowID        string
	TriggerSnapshotJSON string
	// ActorID is who set off a database automation; it runs with another's
	// rights (databaseAutomationEvaluationCandidate.RunnerID).
	ActorID string
}

type interactiveAutomationProperty struct {
	ID         string `db:"id"`
	DatabaseID string `db:"data_source_id"`
	Type       string `db:"type"`
	// PageID is the page holding the property's database, which may be
	// another page than the button's (an add or edit target).
	PageID string `db:"page_id"`
}

type interactiveAutomationRowValue struct {
	PropertyID string          `db:"property_id"`
	Type       string          `db:"type"`
	Value      string          `db:"value"`
	Spans      []graphTextSpan `db:"-"`
}

type plannedInteractiveAutomationBlock struct {
	ID           string
	ParentID     string
	AfterID      string
	Position     string
	Type         string
	Content      string
	Text         string
	TextSpans    []graphTextSpan
	Start        bool
	SourceTextID string
}

type plannedInteractiveAutomationRow struct {
	ID         string
	DatabaseID string
	// PageID is the page holding the row's database.
	PageID   string
	Position string
	Insert   bool
	Values   map[string]interactiveAutomationRowValue
}

type interactiveAutomationPlan struct {
	// SystemAuthored is a database automation's run, whose changes read as Leafen's.
	SystemAuthored bool
	// formulas computes the run's formula values against its trigger.
	progress     interactiveAutomationProgress
	formulas     *interactiveAutomationFormulas
	Confirmation *interactiveAutomationConfirmation
	Blocks       []plannedInteractiveAutomationBlock
	Rows         map[string]*plannedInteractiveAutomationRow
	Effects      interactiveAutomationEffects
	Outcomes     []interactiveAutomationOutcome
	Deliveries   []interactiveAutomationAction
	// TemplateRows are the rows it adds from a template, filled once they exist.
	TemplateRows []interactiveAutomationTemplateRow
	// The rows it archives and whose page icon it sets (interactive_automation_page_icon.go).
	interactiveAutomationRowPageWrites
	// pageID is the page the run belongs to, and databasePages the page
	// holding each database its actions read or write.
	pageID        string
	databasePages map[string]string
}

func loadInteractiveAutomationSource(
	app core.App,
	authorization pageAuthorization,
	locator interactiveAutomationSourceLocator,
) (interactiveAutomationSource, error) {
	ownerID := authorization.Workspace.OwnerID
	if locator.Kind == interactiveAutomationSourcePageButton {
		var row struct {
			ID       string `db:"id"`
			Position string `db:"position"`
			ParentID string `db:"parent_id"`
			Content  string `db:"content"`
		}
		err := app.DB().NewQuery(`
			SELECT [[id]], [[position]], [[parent_id]], [[content]] FROM {{leafen_blocks}}
			WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
				AND [[id]] = {:blockId} AND [[type]] = 'button'
		`).Bind(dbx.Params{
			"owner": ownerID, "pageId": locator.PageID, "blockId": locator.BlockID,
		}).One(&row)
		if errors.Is(err, sql.ErrNoRows) {
			return interactiveAutomationSource{}, errInteractiveAutomationNotFound
		}
		if err != nil {
			return interactiveAutomationSource{}, err
		}
		var content struct {
			Document json.RawMessage `json:"buttonActionDocument"`
		}
		if json.Unmarshal([]byte(row.Content), &content) != nil || len(content.Document) == 0 {
			return interactiveAutomationSource{}, automationRefused("the button has no steps yet: give it buttonActionDocument first")
		}
		return interactiveAutomationSource{
			Kind: locator.Kind, ID: locator.sourceID(), Document: content.Document, Position: row.Position,
			ParentID: row.ParentID,
		}, nil
	}
	if locator.Kind == interactiveAutomationSourceLegacyTemplate {
		var row struct {
			ID       string `db:"id"`
			Position string `db:"position"`
			ParentID string `db:"parent_id"`
		}
		err := app.DB().NewQuery(`
			SELECT [[id]], [[position]], [[parent_id]] FROM {{leafen_blocks}}
			WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
				AND [[id]] = {:blockId} AND [[type]] = 'template'
		`).Bind(dbx.Params{
			"owner": ownerID, "pageId": locator.PageID, "blockId": locator.BlockID,
		}).One(&row)
		if errors.Is(err, sql.ErrNoRows) {
			return interactiveAutomationSource{}, errInteractiveAutomationNotFound
		}
		if err != nil {
			return interactiveAutomationSource{}, err
		}
		return interactiveAutomationSource{
			Kind: locator.Kind, ID: locator.sourceID(), Position: row.Position,
			ParentID: row.ParentID,
		}, nil
	}

	var row struct {
		ID     string `db:"id"`
		Config string `db:"config"`
	}
	err := app.DB().NewQuery(`
		SELECT [[property]].[[id]], [[property]].[[config]]
		FROM {{leafen_database_properties}} [[property]]
		JOIN {{leafen_database_rows}} [[row]]
			ON [[row]].[[id]] = {:rowId}
			AND [[row]].[[data_source_id]] = [[property]].[[data_source_id]]
		WHERE [[property]].[[owner]] = {:owner} AND [[property]].[[page_id]] = {:pageId}
			AND [[property]].[[id]] = {:propertyId} AND [[property]].[[type]] = 'button'
			AND [[property]].[[data_source_id]] = {:databaseId}
			AND [[row]].[[owner]] = {:owner} AND [[row]].[[page_id]] = {:pageId}
			AND ` + databaseSourceNotTrashedSQL("[[property]].[[data_source_id]]") + `
			AND ` + databaseRowNotTrashedSQL("[[row]].[[id]]") + `
	`).Bind(dbx.Params{
		"owner": ownerID, "pageId": locator.PageID, "databaseId": locator.DatabaseID,
		"rowId": locator.RowID, "propertyId": locator.PropertyID,
	}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return interactiveAutomationSource{}, errInteractiveAutomationNotFound
	}
	if err != nil {
		return interactiveAutomationSource{}, err
	}
	var config struct {
		Document json.RawMessage `json:"button"`
	}
	if json.Unmarshal([]byte(row.Config), &config) != nil || len(config.Document) == 0 {
		return interactiveAutomationSource{}, automationRefused("the button property has no steps yet: give it a button document first")
	}
	return interactiveAutomationSource{
		Kind: locator.Kind, ID: locator.sourceID(), Document: config.Document,
		TriggerDatabaseID: locator.DatabaseID, TriggerRowID: locator.RowID,
	}, nil
}

func planInteractiveAutomationActions(
	app core.App,
	authorization pageAuthorization,
	source interactiveAutomationSource,
	document interactiveAutomationDocument,
	executionID string,
	executionTime time.Time,
	seeds ...interactiveAutomationProgress,
) (interactiveAutomationPlan, error) {
	properties, err := loadInteractiveAutomationProperties(app, authorization, source, document)
	if err != nil {
		return interactiveAutomationPlan{}, err
	}
	triggerValues, err := loadInteractiveAutomationTriggerValues(app, authorization, source)
	if err != nil {
		return interactiveAutomationPlan{}, err
	}
	plan := interactiveAutomationPlan{
		Rows: map[string]*plannedInteractiveAutomationRow{},
		Effects: interactiveAutomationEffects{
			Blocks: []interactiveAutomationEffectBlock{}, Pages: []interactiveAutomationEffectPage{},
		},
		Outcomes: []interactiveAutomationOutcome{},
		formulas: newInteractiveAutomationFormulas(app, authorization, source, executionTime),
		pageID:   authorization.Page.Id, databasePages: map[string]string{},
	}
	for _, property := range properties {
		plan.databasePages[property.DatabaseID] = property.PageID
	}
	progress := interactiveAutomationProgress{Variables: map[string]json.RawMessage{}}
	if len(seeds) == 1 {
		progress = seeds[0]
	}
	if progress.Variables == nil {
		progress.Variables = map[string]json.RawMessage{}
	}
	variables := progress.Variables
	blockIndex, pageIndex := progress.BlockIndex, progress.PageIndex
	for index, action := range document.Actions {
		switch action.Type {
		case interactiveAutomationActionDefineVariables:
			for _, variable := range action.Variables {
				value, err := evaluateInteractiveAutomationValue(variable.Value, variables, triggerValues, executionTime, plan.formulas)
				if err != nil {
					return interactiveAutomationPlan{}, err
				}
				variables[variable.Name] = value
			}
		case interactiveAutomationActionEditProperty:
			if source.TriggerRowID == "" {
				return interactiveAutomationPlan{}, automationActionAt(errAutomationNoTriggerRow, index)
			}
			property, ok := properties[action.PropertyID]
			if !ok || property.DatabaseID != source.TriggerDatabaseID || property.Type == graphValueTypeButton {
				return interactiveAutomationPlan{}, errInteractiveAutomationNotFound
			}
			row := plan.row(source.TriggerRowID, source.TriggerDatabaseID, "", false)
			cell, err := plan.cellFor(property, action.Value, row, variables, triggerValues, executionTime)
			if err != nil {
				return interactiveAutomationPlan{}, err
			}
			row.Values[property.ID] = cell
		case interactiveAutomationActionInsertBlocks:
			if action.SourceBlockID != "" {
				return interactiveAutomationPlan{}, errInteractiveAutomationUnsupported
			}
			if source.Kind != interactiveAutomationSourcePageButton {
				return interactiveAutomationPlan{}, automationActionAt(automationRefused("type insert_blocks runs only from a button on a page, which has a place for the blocks"), index)
			}
			place, err := interactiveAutomationBlockPlacement(app, authorization, source, action.Placement)
			if err != nil {
				return interactiveAutomationPlan{}, err
			}
			for _, block := range action.Blocks {
				blockIndex++
				if place.AfterID, err = plan.addBlock(block, place.ParentID, place.AfterID, source.Position, executionID, action.ID, &blockIndex); err != nil {
					return interactiveAutomationPlan{}, err
				}
			}
		case interactiveAutomationActionAddPage:
			pageIndex++
			if err := plan.addPage(action, properties, variables, triggerValues, executionTime, executionID, pageIndex); err != nil {
				return interactiveAutomationPlan{}, err
			}
		case interactiveAutomationActionEditPages, interactiveAutomationActionArchivePages, interactiveAutomationActionSetPageIcon:
			if err := plan.aimAtPages(app, authorization, source, action, properties, variables, triggerValues, executionTime); err != nil {
				return interactiveAutomationPlan{}, err
			}
		case interactiveAutomationActionShowConfirmation:
			if plan.Confirmation != nil {
				return interactiveAutomationPlan{}, automationActionAt(automationRefused("type show_confirmation comes once in a run"), index)
			}
			resolved, err := plan.formulas.resolveMessage(action, variables)
			if err != nil {
				return interactiveAutomationPlan{}, err
			}
			title := ""
			_ = json.Unmarshal(resolved.Title, &title)
			plan.Confirmation = &interactiveAutomationConfirmation{
				Title: title, Message: resolved.Message, MessageSpans: resolved.MessageSpans,
				ConfirmLabel: resolved.ConfirmLabel, CancelLabel: resolved.CancelLabel,
			}
		case interactiveAutomationActionOpenPage:
			outcome, found := interactiveAutomationOpenPageOutcome(app, authorization, source, action)
			if !found {
				return interactiveAutomationPlan{}, errInteractiveAutomationNotFound
			}
			plan.Outcomes = append(plan.Outcomes, outcome)
		case interactiveAutomationActionOpenForm:
			pageID, containerID, found := interactiveAutomationFormTarget(app, authorization, action.DatabaseID, action.ViewID)
			if !found {
				return interactiveAutomationPlan{}, errInteractiveAutomationNotFound
			}
			plan.Outcomes = append(plan.Outcomes, interactiveAutomationOutcome{Type: "openForm", DatabaseID: action.DatabaseID, ViewID: action.ViewID,
				PageID: pageID, ContainerID: containerID, OpenIn: action.OpenIn})
		case interactiveAutomationActionOpenUrl:
			plan.Outcomes = append(plan.Outcomes, interactiveAutomationOutcome{Type: "openUrl", URL: action.URL})
		case interactiveAutomationActionSendNotification:
			resolved, err := plan.formulas.resolveNotificationRecipients(action, variables)
			if err == nil {
				// Its words from their formula (interactive_automation_message.go).
				resolved, err = plan.formulas.resolveMessage(resolved, variables)
			}
			if err != nil {
				return interactiveAutomationPlan{}, err
			}
			plan.Deliveries = append(plan.Deliveries, resolved)
		case interactiveAutomationActionSendEmail,
			interactiveAutomationActionSendWebhook, interactiveAutomationActionSendSlack, interactiveAutomationActionRunAppAction:
			// Database automation delivery actions — and an app's action — are
			// durably admitted after local mutations are planned and applied.
			resolved, err := plan.formulas.resolveMessage(action, variables)
			if err != nil {
				return interactiveAutomationPlan{}, err
			}
			plan.Deliveries = append(plan.Deliveries, resolved)
		default:
			return interactiveAutomationPlan{}, errInteractiveAutomationUnsupported
		}
	}
	progress.BlockIndex, progress.PageIndex = blockIndex, pageIndex
	plan.progress = progress
	return plan, nil
}

func loadInteractiveAutomationProperties(
	app core.App, authorization pageAuthorization, source interactiveAutomationSource, document interactiveAutomationDocument,
) (map[string]interactiveAutomationProperty, error) {
	databaseIDs := map[string]struct{}{}
	if source.TriggerDatabaseID != "" {
		databaseIDs[source.TriggerDatabaseID] = struct{}{}
	}
	for _, action := range document.Actions {
		// A form it opens is opened, not written: whoever may answer it may
		// open it (interactiveAutomationFormTarget).
		if action.DatabaseID != "" && action.Type != interactiveAutomationActionOpenForm {
			databaseIDs[action.DatabaseID] = struct{}{}
		}
		if interactiveAutomationAimsAtPages(action) {
			var target struct {
				DatabaseID string `json:"databaseId"`
			}
			_ = json.Unmarshal(action.Target, &target)
			databaseIDs[target.DatabaseID] = struct{}{}
		}
	}
	if len(databaseIDs) == 0 {
		return map[string]interactiveAutomationProperty{}, nil
	}
	ordered := make([]string, 0, len(databaseIDs))
	for id := range databaseIDs {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	params := dbx.Params{"owner": authorization.Workspace.OwnerID}
	bindings := make([]string, len(ordered))
	for index, id := range ordered {
		key := fmt.Sprintf("database%d", index)
		params[key] = id
		bindings[index] = "{:" + key + "}"
	}
	// A database on another page of the workspace is one the person running
	// may write to there, as Notion's button adds to and edits any database
	// one can edit (seen 2026-10-09); the trigger's own is the run's page's.
	sources := []struct {
		ID     string `db:"id"`
		PageID string `db:"page_id"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[source]].[[id]], [[source]].[[page_id]] FROM {{leafen_data_sources}} AS [[source]]
		WHERE [[source]].[[owner]] = {:owner} AND [[source]].[[id]] IN (` + strings.Join(bindings, ",") + `)
			AND ` + databaseSourceNotTrashedSQL("[[source]].[[id]]")).Bind(params).All(&sources); err != nil {
		return nil, err
	}
	if len(sources) != len(ordered) {
		return nil, errInteractiveAutomationNotFound
	}
	for _, found := range sources {
		if found.PageID == authorization.Page.Id {
			continue
		}
		if found.ID == source.TriggerDatabaseID {
			return nil, errInteractiveAutomationNotFound
		}
		if err := authorizeInteractiveAutomationTargetPage(app, authorization, found.PageID); err != nil {
			return nil, err
		}
	}
	rows := []interactiveAutomationProperty{}
	if err := app.DB().NewQuery(`SELECT [[property]].[[id]], [[property]].[[data_source_id]], [[property]].[[type]], [[source]].[[page_id]]
		FROM {{leafen_database_properties}} AS [[property]]
		JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[id]] = [[property]].[[data_source_id]]
			AND [[source]].[[owner]] = [[property]].[[owner]] AND [[source]].[[page_id]] = [[property]].[[page_id]]
		WHERE [[property]].[[owner]] = {:owner} AND [[property]].[[data_source_id]] IN (` + strings.Join(bindings, ",") + `)`).Bind(params).All(&rows); err != nil {
		return nil, err
	}
	result := map[string]interactiveAutomationProperty{}
	for _, row := range rows {
		result[row.ID] = row
	}
	return result, nil
}

func loadInteractiveAutomationTriggerValues(
	app core.App, authorization pageAuthorization, source interactiveAutomationSource,
) (map[string]json.RawMessage, error) {
	result := map[string]json.RawMessage{}
	if source.TriggerRowID == "" {
		return result, nil
	}
	if source.TriggerSnapshotJSON != "" {
		values := map[string]graphRowValue{}
		if json.Unmarshal([]byte(source.TriggerSnapshotJSON), &values) != nil {
			return nil, errInteractiveAutomationInvalid
		}
		for propertyID, value := range values {
			result[propertyID] = append(json.RawMessage(nil), value.Value...)
		}
		return result, nil
	}
	rows := []interactiveAutomationRowValue{}
	if err := app.DB().NewQuery(`SELECT [[property_id]], [[type]], [[value]] FROM {{leafen_database_row_values}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[row_id]] = {:rowId}`).Bind(dbx.Params{
		"owner": authorization.Workspace.OwnerID, "pageId": authorization.Page.Id, "rowId": source.TriggerRowID,
	}).All(&rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.PropertyID] = json.RawMessage(row.Value)
	}
	return result, nil
}

func evaluateInteractiveAutomationValue(
	expression interactiveAutomationValueExpression,
	variables map[string]json.RawMessage,
	triggerValues map[string]json.RawMessage,
	executionTime time.Time,
	formulas *interactiveAutomationFormulas,
) (json.RawMessage, error) {
	switch expression.Kind {
	case "literal":
		if len(expression.Spans) > 0 {
			return formulas.resolveTextValue(expression.Spans, variables)
		}
		return append(json.RawMessage(nil), expression.Value...), nil
	case "variable":
		value, ok := variables[expression.Name]
		if !ok {
			return nil, errInteractiveAutomationInvalid
		}
		return append(json.RawMessage(nil), value...), nil
	case "trigger_property":
		value, ok := triggerValues[expression.PropertyID]
		if !ok {
			return nil, errInteractiveAutomationNotFound
		}
		return append(json.RawMessage(nil), value...), nil
	case "execution_time":
		value, _ := json.Marshal(executionTime.Format(time.RFC3339Nano))
		return value, nil
	case "execution_date":
		// The day the run began, without a time: Notion's "자동 실행된 날짜".
		day := executionTime.UTC().Format("2006-01-02")
		if formulas != nil {
			day = formulas.executionDay()
		}
		value, _ := json.Marshal(day)
		return value, nil
	case "formula":
		return formulas.evaluate(expression.Expression, variables)
	default:
		return nil, errInteractiveAutomationUnsupported
	}
}

// interactiveAutomationValueForType is the value an action sets as the graph
// saves it for the property's type: anything a cell of that type can hold
// (a checkbox's true, a status option, a date, people, ...), or null to
// clear it. Values the graph would refuse are refused before the run. An
// empty text — what a formula gives where it gives nothing, as
// if(done, now()) when not done — clears it, as Notion's automations do.
// Words set to a number are read as Notion's automations read a number
// line's words ("12abc" 12, "5+5+1" 551, "abc" empty — seen 2026-11-10).
func interactiveAutomationValueForType(value json.RawMessage, propertyType string) (json.RawMessage, bool) {
	var stored map[string]any
	if json.Unmarshal(value, &stored) == nil {
		if rich, ok := databaseFormulaStoredRichText(stored); ok {
			value, _ = json.Marshal(rich.shown())
		}
	}
	var words string
	if propertyType == graphValueTypeNumber && json.Unmarshal(value, &words) == nil {
		value = graphRowValueCleared
		if number, ok := databasePropertyConversionNumber(words); ok {
			value, _ = json.Marshal(number)
		}
	}
	if _, clearable := graphClearableValueTypes[propertyType]; clearable && string(bytes.TrimSpace(value)) == `""` {
		value = graphRowValueCleared
	}
	normalized, err := normalizeGraphRowValue("", propertyType, value, nil)
	return normalized, err == nil
}

// interactiveAutomationCellForType is a value as the property's cell keeps
// it: rich text in a title or text cell is its text, its mentions and
// formatting beside it, as Notion's automations set it (seen 2026-10-25);
// elsewhere rich text is the text it shows.
func interactiveAutomationCellForType(property interactiveAutomationProperty, value json.RawMessage) (interactiveAutomationRowValue, bool) {
	cell := interactiveAutomationRowValue{PropertyID: property.ID, Type: property.Type}
	var stored map[string]any
	rich := databaseFormulaRichText(nil)
	if property.Type == graphValueTypeTitle || property.Type == graphValueTypeRichText {
		if json.Unmarshal(value, &stored) == nil {
			rich, _ = databaseFormulaStoredRichText(stored)
		}
	}
	if len(rich) == 0 {
		normalized, valid := interactiveAutomationValueForType(value, property.Type)
		cell.Value = string(normalized)
		return cell, valid
	}
	var spelled strings.Builder
	for _, span := range rich {
		spelled.WriteString(span.Text)
	}
	text, _ := json.Marshal(spelled.String())
	normalized, valid := interactiveAutomationValueForType(text, property.Type)
	if !valid {
		return cell, false
	}
	spans, err := normalizeGraphRowValueSpans(property.Type, normalized, rich)
	cell.Value, cell.Spans = string(normalized), spans
	return cell, err == nil
}
