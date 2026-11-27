package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// storedDatabaseFormulaRewrite is how a migration's content writes the
// formulas Leafen keeps anew: formula properties and database buttons,
// database automations, and the buttons of pages (migrations 869 and 870).
type storedDatabaseFormulaRewrite struct {
	// rewrite is a formula written anew and whether it changed.
	rewrite func(string) (string, bool)
	// A document that holds none of needles holds no formula rewrite changes.
	needles []string
	// why names the change in the pages' history; logged says it in the log.
	why, logged string
}

// storedFormulaNeedles is the condition that column holds one of the needles,
// each bound in params.
func storedFormulaNeedles(column string, needles []string, params dbx.Params) string {
	conditions := make([]string, len(needles))
	for index, needle := range needles {
		name := fmt.Sprintf("needle%d", index)
		params[name] = needle
		conditions[index] = fmt.Sprintf("instr(%s, {:%s}) > 0", column, name)
	}
	return "(" + strings.Join(conditions, " OR ") + ")"
}

// rewriteDatabaseFormulaDocuments rewrites each formula value ({"kind":
// "formula", "expression": …}) of a button's or an automation's document.
func rewriteDatabaseFormulaDocuments(value any, rewrite func(string) (string, bool)) (any, bool) {
	changed := false
	switch current := value.(type) {
	case map[string]any:
		if expression, ok := current["expression"].(string); ok && current["kind"] == "formula" {
			if next, rewritten := rewrite(expression); rewritten {
				current["expression"] = next
				changed = true
			}
		}
		for key, child := range current {
			if next, rewritten := rewriteDatabaseFormulaDocuments(child, rewrite); rewritten {
				current[key] = next
				changed = true
			}
		}
	case []any:
		for index, child := range current {
			if next, rewritten := rewriteDatabaseFormulaDocuments(child, rewrite); rewritten {
				current[index] = next
				changed = true
			}
		}
	}
	return value, changed
}

// databaseFormulaRewrittenJSON is a stored document with its formulas
// written anew — a formula property's own (under "formula", or the older
// "expression"), a button's and an automation's formula values — or "" when
// rewrite changes none of them.
func databaseFormulaRewrittenJSON(raw string, own bool, rewrite func(string) (string, bool)) (string, error) {
	// Numbers keep their text (an integer past 2^53, a 1.50) where only a
	// formula changes.
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var document any
	if decoder.Decode(&document) != nil {
		return "", nil
	}
	changed := false
	if config, ok := document.(map[string]any); ok && own {
		for _, key := range []string{"formula", "expression"} {
			if expression, ok := config[key].(string); ok {
				if next, rewritten := rewrite(expression); rewritten {
					config[key] = next
					changed = true
				}
			}
		}
	}
	if _, rewritten := rewriteDatabaseFormulaDocuments(document, rewrite); rewritten {
		changed = true
	}
	if !changed {
		return "", nil
	}
	// A formula's < and > stay as they are written, not escaped for HTML.
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(document); err != nil {
		return "", err
	}
	return strings.TrimSuffix(encoded.String(), "\n"), nil
}

// rewriteStoredDatabaseFormulas writes the formulas Leafen keeps anew.
func rewriteStoredDatabaseFormulas(app core.App, rewrite storedDatabaseFormulaRewrite) error {
	now := time.Now().UTC().UnixMilli()
	properties := []struct {
		ID     string `db:"id"`
		Type   string `db:"type"`
		Config string `db:"config"`
	}{}
	params := dbx.Params{"formula": graphValueTypeFormula, "button": graphValueTypeButton}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[type]], [[config]] FROM {{leafen_database_properties}}
		WHERE [[type]] IN ({:formula}, {:button}) AND ` + storedFormulaNeedles("[[config]]", rewrite.needles, params) + `
		ORDER BY [[id]]
	`).Bind(params).All(&properties); err != nil {
		return err
	}
	rewritten := 0
	for _, property := range properties {
		config, err := databaseFormulaRewrittenJSON(property.Config, property.Type == graphValueTypeFormula, rewrite.rewrite)
		if err != nil || config == "" {
			if err != nil {
				return fmt.Errorf("property %s: %w", property.ID, err)
			}
			continue
		}
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_database_properties}} SET [[config]] = {:config}, [[updated]] = {:updated} WHERE [[id]] = {:id}
		`).Bind(dbx.Params{"config": config, "updated": now, "id": property.ID}).Execute(); err != nil {
			return err
		}
		rewritten++
	}
	automations, err := rewriteDatabaseAutomationFormulas(app, rewrite, now)
	if err != nil {
		return err
	}
	buttons, err := rewritePageButtonFormulas(app, rewrite)
	if err != nil {
		return err
	}
	if rewritten+automations+buttons > 0 {
		app.Logger().Info(rewrite.logged, "properties", rewritten, "automations", automations, "pageButtons", buttons)
	}
	return nil
}

func rewriteDatabaseAutomationFormulas(app core.App, rewrite storedDatabaseFormulaRewrite, now int64) (int, error) {
	definitions := []struct {
		ID      string `db:"id"`
		Trigger string `db:"trigger_json"`
		Action  string `db:"action_json"`
	}{}
	params := dbx.Params{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[trigger_json]], [[action_json]] FROM {{leafen_database_automation_definitions}}
		WHERE instr([[trigger_json]] || [[action_json]], '"formula"') > 0 AND ` +
		storedFormulaNeedles("[[trigger_json]] || [[action_json]]", rewrite.needles, params) + `
		ORDER BY [[id]]
	`).Bind(params).All(&definitions); err != nil {
		return 0, err
	}
	rewritten := 0
	for _, definition := range definitions {
		trigger, err := databaseFormulaRewrittenJSON(definition.Trigger, false, rewrite.rewrite)
		if err != nil {
			return 0, err
		}
		action, err := databaseFormulaRewrittenJSON(definition.Action, false, rewrite.rewrite)
		if err != nil {
			return 0, err
		}
		if trigger == "" && action == "" {
			continue
		}
		if trigger == "" {
			trigger = definition.Trigger
		}
		if action == "" {
			action = definition.Action
		}
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_database_automation_definitions}}
			SET [[trigger_json]] = {:trigger}, [[action_json]] = {:action}, [[revision]] = [[revision]] + 1, [[updated]] = {:updated}
			WHERE [[id]] = {:id}
		`).Bind(dbx.Params{"trigger": trigger, "action": action, "updated": now, "id": definition.ID}).Execute(); err != nil {
			return 0, err
		}
		rewritten++
	}
	return rewritten, nil
}

// rewritePageButtonFormulas writes the formula values of the buttons of
// pages anew, through each page's blocks, keeping who edited them last and
// when.
func rewritePageButtonFormulas(app core.App, rewrite storedDatabaseFormulaRewrite) (int, error) {
	blocks := []movedUntitledLinkedView{}
	params := dbx.Params{"button": graphBlockTypeButton}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[owner]], [[workspace_id]], [[page_id]], [[parent_id]], [[position]], [[content]], [[last_edited_by]], [[updated]]
		FROM {{leafen_blocks}}
		WHERE [[type]] = {:button} AND json_valid([[content]]) AND instr([[content]], '"formula"') > 0 AND ` +
		storedFormulaNeedles("[[content]]", rewrite.needles, params) + `
		ORDER BY [[page_id]], [[id]]
	`).Bind(params).All(&blocks); err != nil {
		return 0, err
	}
	rewritten := 0
	for start := 0; start < len(blocks); {
		end := start + 1
		for end < len(blocks) && blocks[end].PageID == blocks[start].PageID {
			end++
		}
		count, err := rewritePageButtonFormulasOnPage(app, rewrite, blocks[start:end])
		if err != nil {
			return 0, fmt.Errorf("page %s: %w", blocks[start].PageID, err)
		}
		rewritten += count
		start = end
	}
	return rewritten, nil
}

func rewritePageButtonFormulasOnPage(app core.App, rewrite storedDatabaseFormulaRewrite, blocks []movedUntitledLinkedView) (int, error) {
	first := blocks[0]
	operations := []graphOperation{}
	changed := []movedUntitledLinkedView{}
	for _, block := range blocks {
		content, err := databaseFormulaRewrittenJSON(block.Content, false, rewrite.rewrite)
		if err != nil {
			return 0, err
		}
		if content == "" {
			continue
		}
		upsert := &graphBlock{
			ID: block.ID, Type: graphBlockTypeButton, Position: block.Position, Content: json.RawMessage(content),
			BaseContent: json.RawMessage(block.Content),
		}
		if block.ParentID != "" {
			parentID := block.ParentID
			upsert.ParentID = &parentID
		}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertBlock, Block: upsert})
		changed = append(changed, block)
	}
	if len(operations) == 0 {
		return 0, nil
	}
	batch, err := loadStructuralMutationBatch(app, first.PageID, map[string]*structuralMutationBatch{})
	if err != nil {
		return 0, err
	}
	if _, err := applyStructuralGraphMutation(
		app, batch.doc, first.Owner, first.WorkspaceID, first.PageID, first.Owner, operations, false, nil,
	); err != nil {
		return 0, err
	}
	if err := saveServerStructuralBatch(app, first.Owner, first.PageID, first.Owner, rewrite.why, batch, nil); err != nil {
		return 0, err
	}
	for _, block := range changed {
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_blocks}} SET [[last_edited_by]] = {:by}, [[updated]] = {:updated}
			WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[id]] = {:id}
		`).Bind(dbx.Params{
			"by": block.LastEditedBy, "updated": block.Updated, "owner": block.Owner, "pageId": block.PageID, "id": block.ID,
		}).Execute(); err != nil {
			return 0, err
		}
	}
	// Its history keeps the blocks as they now stand.
	return len(changed), recordPageHistoryRewrite(app, first.PageID, first.Owner, true)
}

// validateStoredDatabaseFormulas is the check of a migration that writes
// them anew: the tables it reads and writes are there.
func validateStoredDatabaseFormulas(app core.App) error {
	for _, table := range []string{
		"leafen_database_properties", "leafen_database_automation_definitions", "leafen_blocks", "leafen_ygo_structural_documents",
	} {
		if !app.HasTable(table) {
			return fmt.Errorf("stored formulas: the table %s is missing", table)
		}
	}
	return nil
}
