package app

import (
	"encoding/json"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// rewriteDatabaseAutomationDefinitionReferences keeps a database's saved
// automations reading the same properties when those are renamed, as its
// formulas and buttons do: each formula value's prop() arguments move to
// the new names.
func rewriteDatabaseAutomationDefinitionReferences(
	app core.App,
	databaseID string,
	before, after databaseFormulaReferenceNames,
) error {
	definitions := []struct {
		ID     string `db:"id"`
		Action string `db:"action_json"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[action_json]] FROM {{leafen_database_automation_definitions}}
		WHERE [[database_id]] = {:databaseId} AND instr([[action_json]], '"formula"') > 0
	`).Bind(dbx.Params{"databaseId": databaseID}).All(&definitions); err != nil {
		return err
	}
	replace := databaseFormulaReferenceReplacement(false, before, after)
	for _, definition := range definitions {
		var document any
		if json.Unmarshal([]byte(definition.Action), &document) != nil {
			continue
		}
		rewritten, changed := rewriteDatabaseAutomationFormulaReferences(document, replace)
		if !changed {
			continue
		}
		encoded, err := json.Marshal(rewritten)
		if err != nil {
			return err
		}
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_database_automation_definitions}}
			SET [[action_json]] = {:action}, [[revision]] = [[revision]] + 1, [[updated]] = {:updated}
			WHERE [[id]] = {:id}
		`).Bind(dbx.Params{
			"action": string(encoded), "updated": time.Now().UTC().UnixMilli(), "id": definition.ID,
		}).Execute(); err != nil {
			return err
		}
	}
	return nil
}
