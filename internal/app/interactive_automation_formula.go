package app

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A formula value in an automation or button is computed when its action
// runs, as Notion's are. prop() reads the page that triggered it: its stored
// values, with an automation's values as they were when it was triggered,
// and its formulas and rollups as they read now. The automation's variables
// are names the formula can use. A page button has no triggering page, so
// prop() finds nothing there; a People automation's reads the person's row,
// their Email from their account (peopleAutomationSnapshotEmails).

type interactiveAutomationFormulas struct {
	app           core.App
	authorization pageAuthorization
	source        interactiveAutomationSource
	now           time.Time

	loaded   bool
	err      error
	resolver *databaseComputedResolver
	row      storedDatabaseQueryRow
}

func newInteractiveAutomationFormulas(
	app core.App, authorization pageAuthorization, source interactiveAutomationSource, now time.Time,
) *interactiveAutomationFormulas {
	return &interactiveAutomationFormulas{app: app, authorization: authorization, source: source, now: now}
}

// clock is the run's clock: the person's whose authority it runs on
// (personLocation), whose day its execution date, today() and the days of
// its formulas are — not UTC's, which in Seoul is yesterday until 09:00.
func (formulas *interactiveAutomationFormulas) clock() *time.Location {
	return personLocation(formulas.app, formulas.authorization.Workspace.PrincipalID)
}

// normalizeInteractiveAutomationFormula checks a formula's text reads as a
// formula.
func normalizeInteractiveAutomationFormula(value *interactiveAutomationValueExpression) error {
	value.Expression = strings.TrimSpace(value.Expression)
	if value.Expression == "" || len([]rune(value.Expression)) > maxDatabaseFormulaExpressionRunes {
		return automationRefused("expression is a formula of 1–%d characters", maxDatabaseFormulaExpressionRunes)
	}
	if _, err := parseDatabaseFormula(value.Expression); err != nil {
		var syntax databaseFormulaSyntaxError
		if errors.As(err, &syntax) {
			return automationRefused("expression doesn't read as a formula: %s", strings.TrimSuffix(syntax.description, "."))
		}
		return automationRefused("expression doesn't read as a formula")
	}
	return nil
}

// automationVariableFormulaName is a variable's name in a formula: its own
// where formulas take it as a name (letters, digits and _), else one made
// from its bytes, as the editor writes it (automationFormulaNames.ts), which
// shows the variable's own name on its chip.
func automationVariableFormulaName(name string) string {
	plain := name != ""
	for index, character := range name {
		if !(databaseFormulaIdentifierStart(character) || (index > 0 && databaseFormulaASCIIDigit(character))) {
			plain = false
			break
		}
	}
	if plain {
		return name
	}
	return "var_" + hex.EncodeToString([]byte(name))
}

// load reads the triggering row and what its formulas and rollups need, once.
func (formulas *interactiveAutomationFormulas) load() error {
	if formulas.loaded {
		return formulas.err
	}
	formulas.loaded = true
	if formulas.source.TriggerRowID == "" {
		return nil
	}
	owner, pageID := formulas.authorization.Workspace.OwnerID, formulas.authorization.Page.Id
	context, err := loadDatabaseQueryContext(formulas.app, storedDatabaseQueryJob{
		PrincipalID: formulas.authorization.Workspace.PrincipalID, WorkspaceID: formulas.authorization.Workspace.WorkspaceID,
		ContentOwnerID: owner, PageID: pageID, DataSourceID: formulas.source.TriggerDatabaseID,
	})
	if err != nil {
		formulas.err = err
		return err
	}
	context.FormulaTime.Location = formulas.clock()
	rows, err := loadDatabaseQueryRowsByID(formulas.app, owner, []string{pageID}, []string{formulas.source.TriggerRowID})
	if err != nil {
		formulas.err = err
		return err
	}
	row, found := rows[formulas.source.TriggerRowID]
	if !found {
		formulas.err = errInteractiveAutomationNotFound
		return formulas.err
	}
	if formulas.source.TriggerSnapshotJSON != "" {
		snapshot := map[string]graphRowValue{}
		if json.Unmarshal([]byte(formulas.source.TriggerSnapshotJSON), &snapshot) != nil {
			formulas.err = errInteractiveAutomationInvalid
			return formulas.err
		}
		// An absent value at the event is absent, even if it was set later.
		row.Values = snapshot
	}
	dependencies := map[string]storedDatabaseQueryRow{}
	if _, dependencyIDs, boundErr := boundedDatabaseQueryRows([]storedDatabaseQueryRow{row}, context); boundErr == nil {
		dependencies, err = loadDatabaseQueryRowsByID(formulas.app, owner, context.readablePages(pageID), dependencyIDs)
		if err != nil {
			formulas.err = err
			return err
		}
	}
	// Its formulas read the row and its related rows as the person running
	// it reads them, so what it writes names no page they can't open
	// (database_query_reader_mentions.go).
	row = context.rowAsRead(row)
	dependencies[row.ID] = row
	formulas.row = row
	formulas.resolver = &databaseComputedResolver{
		context: context, rows: dependencies, computed: map[string]map[string]databaseComputedValue{},
		visiting: map[string]bool{}, cyclic: map[string]bool{}, now: formulas.now,
	}
	return nil
}

// evaluate computes a formula value with the run's variables.
func (formulas *interactiveAutomationFormulas) evaluate(
	expression string,
	variables map[string]json.RawMessage,
) (json.RawMessage, error) {
	result, err := formulas.evaluateValue(expression, variables)
	if err != nil {
		return nil, err
	}
	// Rich text stays rich: its mentions stay mentions in a title or text
	// cell, a variable and a message, as Notion's do (seen 2026-10-25).
	return json.Marshal(databaseFormulaOutput(result, true))
}

// Recipient formulas consume typed people before serialization, never names or IDs as text.
func (formulas *interactiveAutomationFormulas) evaluateValue(
	expression string, variables map[string]json.RawMessage,
) (any, error) {
	if formulas == nil {
		return nil, errInteractiveAutomationUnsupported
	}
	if err := formulas.load(); err != nil {
		return nil, err
	}
	environment := &databaseFormulaEnvironment{
		now: formulas.now, location: formulas.clock(), property: func(string) (any, error) { return "", nil },
		related: formulas.buttonPageField,
	}
	if formulas.resolver != nil {
		environment = formulas.resolver.formulaEnvironment(
			formulas.row, storedDatabaseQueryProperty{DataSourceID: formulas.source.TriggerDatabaseID},
		)
	}
	scope, err := formulas.contextScope()
	if err != nil {
		return nil, err
	}
	for name, value := range variables {
		scope[name] = databaseFormulaInput(databaseQueryRawValue(value))
		scope[automationVariableFormulaName(name)] = scope[name]
	}
	result, err := evaluateDatabaseFormulaInScope(expression, environment, scope)
	if err != nil {
		return nil, errInteractiveAutomationInvalid
	}
	return result, nil
}
