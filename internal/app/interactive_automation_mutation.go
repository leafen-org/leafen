package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// pageOf is the page holding a database the run writes: its own page
// unless the database is another page's.
func (plan *interactiveAutomationPlan) pageOf(databaseID string) string {
	if page := plan.databasePages[databaseID]; page != "" {
		return page
	}
	return plan.pageID
}

func (plan *interactiveAutomationPlan) row(
	id string, databaseID string, position string, insert bool,
) *plannedInteractiveAutomationRow {
	if current, exists := plan.Rows[id]; exists {
		return current
	}
	row := &plannedInteractiveAutomationRow{
		ID: id, DatabaseID: databaseID, PageID: plan.pageOf(databaseID), Position: position, Insert: insert,
		Values: map[string]interactiveAutomationRowValue{},
	}
	plan.Rows[id] = row
	return row
}

// addBlock plans a block under parentID right after afterID (first when
// empty), its children after it in order, and returns its id.
func (plan *interactiveAutomationPlan) addBlock(
	template interactiveAutomationBlockTemplate,
	parentID string,
	afterID string,
	sourcePosition string,
	executionID string,
	actionID string,
	index *int,
) (string, error) {
	id := automationStableID("hab", executionID, actionID, fmt.Sprintf("%d", *index))
	content := map[string]any{}
	if json.Unmarshal(template.Content, &content) != nil {
		return "", errInteractiveAutomationInvalid
	}
	text, _ := content["text"].(string)
	var spans []graphTextSpan
	if rich, ok := content["rich"]; ok {
		encoded, _ := json.Marshal(rich)
		if json.Unmarshal(encoded, &spans) != nil {
			return "", errInteractiveAutomationInvalid
		}
	}
	delete(content, "text")
	delete(content, "rich")
	canonical, err := json.Marshal(content)
	if err != nil {
		return "", errInteractiveAutomationInvalid
	}
	position := automationPositionAfter(sourcePosition, executionID, actionID, *index)
	plan.Blocks = append(plan.Blocks, plannedInteractiveAutomationBlock{
		ID: id, ParentID: parentID, AfterID: afterID, Position: position,
		Type: template.Type, Content: string(canonical), Text: text, TextSpans: spans, Start: afterID == "",
	})
	plan.Effects.Blocks = append(plan.Effects.Blocks, interactiveAutomationEffectBlock{ID: id})
	previous := ""
	for _, child := range template.Children {
		*index = *index + 1
		if previous, err = plan.addBlock(child, id, previous, sourcePosition, executionID, actionID, index); err != nil {
			return "", err
		}
	}
	return id, nil
}

func (plan *interactiveAutomationPlan) addPage(
	action interactiveAutomationAction,
	properties map[string]interactiveAutomationProperty,
	variables map[string]json.RawMessage,
	triggerValues map[string]json.RawMessage,
	executionTime time.Time,
	executionID string,
	index int,
) error {
	var titleExpression interactiveAutomationValueExpression
	if json.Unmarshal(action.Title, &titleExpression) != nil {
		return errInteractiveAutomationInvalid
	}
	evaluatedTitle, err := evaluateInteractiveAutomationValue(titleExpression, variables, triggerValues, executionTime, plan.formulas)
	title, valid := interactiveAutomationCellForType(interactiveAutomationProperty{Type: graphValueTypeTitle}, evaluatedTitle)
	if err != nil || !valid {
		return errInteractiveAutomationInvalid
	}
	changes := append([]interactiveAutomationPropertyChange(nil), action.Properties...)
	titleProperty := ""
	for _, property := range properties {
		if property.DatabaseID == action.DatabaseID && property.Type == graphValueTypeTitle {
			if titleProperty != "" {
				return errInteractiveAutomationInvalid
			}
			titleProperty = property.ID
		}
	}
	if titleProperty == "" {
		return errInteractiveAutomationNotFound
	}
	id := derivedPageID("automation-add-page", executionID, action.ID, fmt.Sprintf("%d", index))
	row := plan.row(id, action.DatabaseID, fmt.Sprintf("automation~%s~%03d", executionID, index), true)
	title.PropertyID = titleProperty
	row.Values[titleProperty] = title
	for _, change := range changes {
		property, ok := properties[change.PropertyID]
		if !ok || property.DatabaseID != action.DatabaseID || property.Type == graphValueTypeButton {
			return errInteractiveAutomationNotFound
		}
		cell, err := plan.cellFor(property, change.Value, row, variables, triggerValues, executionTime)
		if err != nil {
			return err
		}
		row.Values[property.ID] = cell
	}
	plan.Effects.Pages = append(plan.Effects.Pages, interactiveAutomationEffectPage{ID: id})
	if action.TemplateID != "" {
		plan.TemplateRows = append(plan.TemplateRows, interactiveAutomationTemplateRow{RowID: id, TemplateID: action.TemplateID})
	}
	return nil
}

func (plan *interactiveAutomationPlan) editPages(
	app core.App,
	authorization pageAuthorization,
	action interactiveAutomationAction,
	properties map[string]interactiveAutomationProperty,
	variables map[string]json.RawMessage,
	triggerValues map[string]json.RawMessage,
	executionTime time.Time,
) error {
	databaseID, ids, err := plan.targetRows(app, authorization, action)
	if err != nil {
		return err
	}
	for _, rowID := range ids {
		row := plan.row(rowID, databaseID, "", false)
		for _, change := range action.Changes {
			property, ok := properties[change.PropertyID]
			if !ok || property.DatabaseID != databaseID || property.Type == graphValueTypeButton {
				return errInteractiveAutomationNotFound
			}
			cell, err := plan.cellFor(property, change.Value, row, variables, triggerValues, executionTime)
			if err != nil {
				return err
			}
			row.Values[property.ID] = cell
		}
	}
	return nil
}

// editThisPage sets the changes on the row the button is on.
func (plan *interactiveAutomationPlan) editThisPage(
	source interactiveAutomationSource,
	action interactiveAutomationAction,
	properties map[string]interactiveAutomationProperty,
	variables map[string]json.RawMessage,
	triggerValues map[string]json.RawMessage,
	executionTime time.Time,
) error {
	if source.TriggerRowID == "" {
		return errAutomationNoTriggerRow
	}
	row := plan.row(source.TriggerRowID, source.TriggerDatabaseID, "", false)
	for _, change := range action.Changes {
		property, ok := properties[change.PropertyID]
		if !ok || property.DatabaseID != source.TriggerDatabaseID || property.Type == graphValueTypeButton {
			return errInteractiveAutomationNotFound
		}
		cell, err := plan.cellFor(property, change.Value, row, variables, triggerValues, executionTime)
		if err != nil {
			return err
		}
		row.Values[property.ID] = cell
	}
	return nil
}

// interactiveAutomationFormTarget: a view of the button's page, or a form of
// the workspace on a page whoever presses can read, as Notion's open action
// offers the workspace's forms (seen 2026-11-02) — with the page and the
// database block it is on, for the screen to open it there.
func interactiveAutomationFormTarget(app core.App, authorization pageAuthorization, databaseID, viewID string) (string, string, bool) {
	view := struct {
		PageID      string `db:"page_id"`
		Type        string `db:"type"`
		ContainerID string `db:"container_id"`
	}{}
	err := app.DB().NewQuery(`SELECT [[view]].[[page_id]], [[view]].[[type]], [[view]].[[container_id]] FROM {{leafen_database_views}} AS [[view]]
		WHERE [[view]].[[owner]] = {:owner}
			AND [[view]].[[data_source_id]] = {:databaseId} AND [[view]].[[id]] = {:viewId}
			AND ` + databaseSourceNotTrashedSQL("[[view]].[[data_source_id]]")).Bind(dbx.Params{
		"owner": authorization.Workspace.OwnerID, "databaseId": databaseID, "viewId": viewID,
	}).One(&view)
	if err != nil {
		return "", "", false
	}
	if view.PageID == authorization.Page.Id {
		return view.PageID, view.ContainerID, true
	}
	if view.Type != "form" {
		return "", "", false
	}
	target, err := authorizePage(app, authorization.Workspace.PrincipalID, view.PageID, permissionWorkspaceRead)
	if err == nil && target.Workspace.OwnerID == authorization.Workspace.OwnerID && !target.Page.GetBool("in_trash") {
		return view.PageID, view.ContainerID, true
	}
	// A form of the workspace opens for whoever pressed though they can't
	// read its database: answered, if they may (its link comes with the run,
	// withAnswerableFormTokens), or found not available, as a closed form's
	// page says (Notion's, seen 2026-11-05).
	return view.PageID, view.ContainerID, true
}

func applyInteractiveAutomationPlan(
	app core.App,
	doc *crdt.Doc,
	authorization pageAuthorization,
	plan interactiveAutomationPlan,
	dlpScanners ...organizationDLPContentScanner,
) (bool, error) {
	changed, _, err := applyInteractiveAutomationPlanSaying(app, doc, authorization, plan, dlpScanners...)
	return changed, err
}

// applyInteractiveAutomationPlanSaying applies a run's plan to its page and
// says what its change of the page changed in the page's graph, as a
// person's change of the same operations says it (graphChangeParts): a
// database automation's run records it (database_automation_changed_parts.go).
func applyInteractiveAutomationPlanSaying(
	app core.App,
	doc *crdt.Doc,
	authorization pageAuthorization,
	plan interactiveAutomationPlan,
	dlpScanners ...organizationDLPContentScanner,
) (bool, graphChangedParts, error) {
	if len(dlpScanners) == 0 {
		if err := enforceOrganizationDLPContent(
			app, authorization.Workspace.Organization.OrganizationID, plan,
		); err != nil {
			return false, graphChangedParts{}, err
		}
	} else if len(dlpScanners) != 1 ||
		dlpScanners[0].organizationID != authorization.Workspace.Organization.OrganizationID {
		return false, graphChangedParts{}, errDLPPolicyUnavailable
	} else if err := dlpScanners[0].enforce(plan); err != nil {
		return false, graphChangedParts{}, err
	}
	input, starts, copies, err := buildInteractiveAutomationMutation(app, authorization, plan)
	if err != nil {
		return false, graphChangedParts{}, err
	}
	operations := input.Operations
	if len(operations) == 0 {
		return false, graphChangedParts{}, applyInteractiveAutomationRowPageWrites(app, authorization, plan)
	}
	if doc == nil {
		// A run without a page's room (People's) writes no page of its own.
		return false, graphChangedParts{}, errInteractiveAutomationUnavailable
	}
	if _, err := validateGraphMutation(app, authorization.Workspace.OwnerID, authorization.Workspace.WorkspaceID,
		authorization.Page.Id, operations, nil); err != nil {
		return false, graphChangedParts{}, err
	}
	before, err := interactiveAutomationGraphChangeBefore(app, authorization.Workspace.OwnerID, authorization.Page.Id, operations)
	if err != nil {
		return false, graphChangedParts{}, err
	}
	author := authorization.Workspace.PrincipalID
	if plan.SystemAuthored {
		author = leafenSystemAuthorID
	}
	if _, err := applyStructuralGraphMutation(app, doc, authorization.Workspace.OwnerID,
		authorization.Workspace.WorkspaceID, authorization.Page.Id, author,
		operations, false, starts); err != nil {
		return false, graphChangedParts{}, err
	}
	if err := applyDatabaseGraphMutation(app, doc, authorization.Workspace.OwnerID, authorization.Workspace.WorkspaceID,
		authorization.Page.Id, author, operations); err != nil {
		return false, graphChangedParts{}, err
	}
	if _, err := applyContentGraphTextUpdates(app, authorization.Page.Id, author,
		input.TextUpdates, nil, nil, nil, false, copies); err != nil {
		return false, graphChangedParts{}, err
	}
	if err := markSearchDirty(app, authorization.Page); err != nil {
		return false, graphChangedParts{}, err
	}
	// Its rows' icons and archives, written after, record changes of their own (announceDatabaseRowPageChange).
	parts := graphChangeParts(input, contentGraphMutationHooks{}, graphMutationReceipt{}, nil, before, nil)
	// Icons set and pages archived once what the run writes is written, so a refused write leaves them.
	return true, parts, applyInteractiveAutomationRowPageWrites(app, authorization, plan)
}

// Both interactive phases and automatic runs use the native graph and text
// shape. This is the run's own page's change: its blocks and the rows of its
// own databases; rows of another page's database are that page's change
// (interactiveAutomationOtherPageInputs).
func buildInteractiveAutomationMutation(app core.App, authorization pageAuthorization, plan interactiveAutomationPlan) (
	graphMutationInput, map[string]struct{}, map[string]string, error,
) {
	input := graphMutationInput{}
	starts := map[string]struct{}{}
	copies := map[string]string{}
	operations := make([]graphOperation, 0, len(plan.Blocks)+len(plan.Rows))
	for _, block := range plan.Blocks {
		if block.Start {
			starts[block.ID] = struct{}{}
		}
		var parentID, afterID *string
		if block.ParentID != "" {
			value := block.ParentID
			parentID = &value
		}
		if block.AfterID != "" {
			value := block.AfterID
			afterID = &value
		}
		value := &graphBlock{ID: block.ID, ParentID: parentID, AfterID: afterID, Position: block.Position,
			Type: block.Type, Content: json.RawMessage(block.Content)}
		if err := normalizeGraphBlock(value); err != nil {
			return input, nil, nil, err
		}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertBlock, Block: value})
		if block.Text != "" || len(block.TextSpans) > 0 || block.SourceTextID != "" {
			spans := block.TextSpans
			if len(spans) == 0 && block.Text != "" {
				spans = []graphTextSpan{{Text: block.Text}}
			}
			input.TextUpdates = append(input.TextUpdates, graphTextUpdate{BlockID: block.ID, Spans: spans})
			if block.SourceTextID != "" {
				copies[block.ID] = block.SourceTextID
			}
		}
	}
	rows, err := interactiveAutomationRowOperations(app, authorization.Workspace.OwnerID, authorization.Page.Id, authorization.Page.Id, plan)
	if err != nil {
		return input, nil, nil, err
	}
	input.Operations = append(operations, rows...)
	return input, starts, copies, nil
}

// interactiveAutomationOtherPageInputs are the changes the run makes to other
// pages' databases, a change for each page, in page order.
func interactiveAutomationOtherPageInputs(app core.App, authorization pageAuthorization, plan interactiveAutomationPlan) (
	[]interactiveAutomationPageInput, error,
) {
	pages := map[string]struct{}{}
	for _, row := range plan.Rows {
		if row.PageID != "" && row.PageID != authorization.Page.Id {
			pages[row.PageID] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(pages))
	for page := range pages {
		ordered = append(ordered, page)
	}
	sort.Strings(ordered)
	inputs := make([]interactiveAutomationPageInput, 0, len(ordered))
	for _, page := range ordered {
		operations, err := interactiveAutomationRowOperations(app, authorization.Workspace.OwnerID, page, authorization.Page.Id, plan)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, interactiveAutomationPageInput{PageID: page, Operations: operations})
	}
	return inputs, nil
}

type interactiveAutomationPageInput struct {
	PageID     string
	Operations []graphOperation
}

// interactiveAutomationRowOperations are the row writes of the run's plan on
// one page's databases; a row planned without a page is the run's own.
func interactiveAutomationRowOperations(app core.App, ownerID, pageID, runPageID string, plan interactiveAutomationPlan) ([]graphOperation, error) {
	operations := []graphOperation{}
	rowIDs := make([]string, 0, len(plan.Rows))
	for id, row := range plan.Rows {
		if row.PageID == pageID || row.PageID == "" && pageID == runPageID {
			rowIDs = append(rowIDs, id)
		}
	}
	sort.Strings(rowIDs)
	// Pages the run adds go after their database's last, in the order its
	// actions add them, as a page added in the table does.
	inserted := make([]*plannedInteractiveAutomationRow, 0, len(plan.Rows))
	for _, rowID := range rowIDs {
		if row := plan.Rows[rowID]; row.Insert {
			inserted = append(inserted, row)
		}
	}
	sort.SliceStable(inserted, func(left, right int) bool { return inserted[left].Position < inserted[right].Position })
	tails := map[string]string{}
	positions := map[string]string{}
	for _, row := range inserted {
		tail, known := tails[row.DatabaseID]
		if !known {
			var err error
			if tail, err = databaseRowTail(app, ownerID, pageID, row.DatabaseID); err != nil {
				return nil, err
			}
		}
		position, err := databasePositionAfter(tail)
		if err != nil {
			return nil, err
		}
		tails[row.DatabaseID], positions[row.ID] = position, position
	}
	for _, rowID := range rowIDs {
		row := plan.Rows[rowID]
		position := row.Position
		// A row the automation edits stays where it is, under its parent
		// item too; one it adds goes at the end.
		var parent *string
		if row.Insert {
			position = positions[row.ID]
		} else {
			stored := ""
			if err := app.DB().NewQuery(`
				SELECT [[position]], COALESCE([[subitem_parent_id]], '') FROM {{leafen_database_rows}}
				WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
					AND [[data_source_id]] = {:databaseId} AND [[id]] = {:id}
			`).Bind(dbx.Params{
				"owner": ownerID, "pageId": pageID,
				"databaseId": row.DatabaseID, "id": row.ID,
			}).Row(&position, &stored); err != nil {
				return nil, err
			}
			parent = optionalDatabaseRowParent(stored)
		}
		values := make(map[string]graphRowValue, len(row.Values))
		for propertyID, value := range row.Values {
			values[propertyID] = graphRowValue{Type: value.Type, Value: json.RawMessage(value.Value), Spans: value.Spans}
		}
		operations = append(operations, graphOperation{
			Kind: graphOperationKindUpsertRow,
			Row: &graphDatabaseRow{
				ID: row.ID, DataSourceID: row.DatabaseID, SubitemParentID: parent, Position: position, Values: values,
			},
		})
	}
	return operations, nil
}
