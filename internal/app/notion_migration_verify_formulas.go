package app

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
)

// The check's computed cells: each formula and rollup cell of a row Notion
// gave the run, as Leafen computes it now, against what Notion showed of it
// when the run read it (2026-11-24: a plan renaming the properties a formula
// read left the formula reading names no column had, its values 0 and blank
// where Notion showed 2 and "상대 1 1", and the check said nothing differed).
// Both sides are the value as Notion's API gives it, said as the run keeps
// what Notion showed (notionImportShownText): Leafen's as its own API
// computes a row page's (loadDatabaseRowPageComputedValuesFor), so a number
// is a number and a date a time on either side, to the minute Notion gives
// times in. A formula reading the clock or the page's own ID shows another
// value in Leafen by what it is (now(), today(), id(), and a formula reading
// one), and a rollup showing its related values as they are is a list the
// API gives no value of; neither is compared. What differs says why when it
// is known: its own import issue, one of a property it reads, or that the
// plan changes what it reads (a type, option names, kept as text, sent to
// the report, a relation gathered into another or one others are gathered
// into, or a column it shares with its merge target's). A row standing for
// a template, one Notion has in its trash and one in Leafen's trash aren't
// compared; a row whose related rows are more than a row page reads at once
// has no value to compare.

// A formula's or a rollup's value has the setting notionMigrationVerifyComputed
// (contracts/notion-import-wire.json); notionMigrationVerifyPlanChanged is
// why: the plan changes what it reads.
const notionMigrationVerifyPlanChanged = "plan_changed"

// notionMigrationVerifyClockFunctions are the functions whose value is
// another in Leafen by what they read.
var notionMigrationVerifyClockFunctions = map[string]bool{"now": true, "today": true, "id": true}

type notionMigrationVerifyFormulaRow struct {
	RowID        string `db:"row_id"`
	Database     string `db:"parent_source_id"`
	Title        string `db:"row_title"`
	DataSourceID string `db:"data_source_id"`
	PageID       string `db:"page_id"`
	Owner        string `db:"owner"`
	WorkspaceID  string `db:"workspace_id"`
	Shown        string `db:"shown"`
}

type notionMigrationVerifyFormulaProperty struct {
	SourceID string `db:"source_id"`
	Database string `db:"parent_source_id"`
	Title    string `db:"title"`
	Text     string `db:"text"`
	Issues   string `db:"issues"`
}

// notionMigrationVerifyColumn is a column a row's formula has in Leafen.
type notionMigrationVerifyColumn struct {
	SourceID     string `db:"source_id"`
	ID           string `db:"id"`
	DataSourceID string `db:"data_source_id"`
	Type         string `db:"type"`
	Name         string `db:"name"`
	Config       string `db:"config"`
	Shared       int    `db:"shared"`
}

func (v *notionMigrationVerifier) formulas() error {
	properties := []notionMigrationVerifyFormulaProperty{}
	if err := v.app.DB().NewQuery(`
		SELECT [[property]].[[source_id]], [[property]].[[parent_source_id]], [[property]].[[title]], [[property]].[[text]],
			COALESCE((SELECT group_concat([[issue]].[[text]], ',') FROM {{leafen_notion_import_sources}} AS [[issue]]
				WHERE [[issue]].[[job_id]]={:jobId} AND [[issue]].[[kind]]='issue'
					AND [[issue]].[[source_id]] > [[property]].[[source_id]] || '#' AND [[issue]].[[source_id]] < [[property]].[[source_id]] || '$'), '') AS [[issues]]
		FROM {{leafen_notion_import_sources}} AS [[property]]
		WHERE [[property]].[[job_id]]={:jobId} AND [[property]].[[kind]]='property' AND json_valid([[property]].[[text]])
		ORDER BY [[property]].[[sequence]]
	`).Bind(dbx.Params{"jobId": v.jobID}).All(&properties); err != nil {
		return err
	}
	byDatabase, formulas := map[string][]notionMigrationVerifyFormulaProperty{}, map[string]bool{}
	for _, property := range properties {
		byDatabase[property.Database] = append(byDatabase[property.Database], property)
		plan := notionDatabasePropertyPlan{}
		if json.Unmarshal([]byte(property.Text), &plan) == nil && workspaceExportComputedType(plan.Type) {
			formulas[property.Database] = true
		}
	}
	if len(formulas) == 0 {
		return nil
	}
	databases, err := json.Marshal(workspaceExportSortedKeys(formulas))
	if err != nil {
		return err
	}
	rows := []notionMigrationVerifyFormulaRow{}
	if err := v.app.DB().NewQuery(`
		SELECT [[row_map]].[[native_id]] AS [[row_id]], [[source]].[[parent_source_id]], COALESCE([[page]].[[title]], '') AS [[row_title]],
			[[row]].[[data_source_id]], [[row]].[[page_id]], [[row]].[[owner]], [[row]].[[workspace_id]],
			COALESCE(json_extract([[source]].[[text]], '$."` + notionImportRowShownKey + `".value'), '{}') AS [[shown]]
		FROM {{leafen_notion_import_sources}} AS [[source]]
		JOIN {{leafen_notion_import_mappings}} AS [[row_map]] ON [[row_map]].[[job_id]]=[[source]].[[job_id]]
			AND [[row_map]].[[kind]]='row' AND [[row_map]].[[source_id]]=[[source]].[[source_id]]
		JOIN {{leafen_database_rows}} AS [[row]] ON [[row]].[[id]]=[[row_map]].[[native_id]]
		JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[id]]=[[row]].[[id]]
		WHERE [[source]].[[job_id]]={:jobId} AND [[source]].[[kind]]='row' AND json_valid([[source]].[[text]])
			AND [[source]].[[parent_source_id]] IN (SELECT [[value]] FROM json_each({:databases}))
			AND instr([[source]].[[text]], {:countedTemplate})=0 AND instr([[source]].[[text]], {:countedTrashed})=0
			AND NOT ` + pageInTrashSQL("[[page]]") + `
		ORDER BY [[source]].[[sequence]]
	`).Bind(notionImportCountedParams(dbx.Params{"jobId": v.jobID, "databases": string(databases)})).All(&rows); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	// Each formula's and rollup's column, its own or a split's elsewhere, by its key.
	columns := []notionMigrationVerifyColumn{}
	if err := v.app.DB().NewQuery(`
		SELECT [[mapping]].[[source_id]], [[column]].[[id]], [[column]].[[data_source_id]], [[column]].[[type]],
			[[column]].[[name]], [[column]].[[config]],
			(SELECT COUNT(*) FROM {{leafen_notion_import_mappings}} AS [[other]] WHERE [[other]].[[job_id]]=[[mapping]].[[job_id]]
				AND [[other]].[[kind]]='property' AND [[other]].[[native_id]]=[[mapping]].[[native_id]]) AS [[shared]]
		FROM {{leafen_notion_import_mappings}} AS [[mapping]]
		JOIN {{leafen_database_properties}} AS [[column]] ON [[column]].[[id]]=[[mapping]].[[native_id]]
		WHERE [[mapping]].[[job_id]]={:jobId} AND [[mapping]].[[kind]]='property' AND [[column]].[[type]] IN ({:formula}, {:rollup})
			AND ` + databasePropertyNotTrashedSQL("[[column]].[[id]]") + `
	`).Bind(dbx.Params{"jobId": v.jobID, "formula": graphValueTypeFormula, "rollup": graphValueTypeRollup}).All(&columns); err != nil {
		return err
	}
	bySource := map[string]notionMigrationVerifyColumn{}
	for _, column := range columns {
		bySource[column.SourceID] = column
	}
	principal := ""
	_ = v.app.DB().NewQuery(`SELECT [[owner]] FROM {{leafen_notion_migrations}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": v.migrationID}).Row(&principal)
	plan := notionMigrationPlan{}
	planJSON := ""
	_ = v.app.DB().NewQuery(`SELECT [[plan_json]] FROM {{leafen_notion_migrations}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": v.migrationID}).Row(&planJSON)
	_ = json.Unmarshal([]byte(planJSON), &plan)
	others, err := v.sharedPairSources()
	if err != nil {
		return err
	}
	reasons := notionMigrationVerifyComputedReasons{
		into: v.into, columns: notionMigrationPlanColumns(plan), properties: byDatabase, others: others,
		bySource: map[string]notionMigrationVerifyFormulaProperty{}, said: map[string]string{},
	}
	for _, property := range properties {
		reasons.bySource[property.SourceID] = property
	}
	untitled := notionMigrationVerifyText[accountLanguageEn]["untitled"]
	if text := notionMigrationVerifyText[v.language]; text != nil {
		untitled = text["untitled"]
	}
	computing := map[string]*notionMigrationVerifyComputing{}
	for _, row := range rows {
		shown := map[string]string{}
		_ = json.Unmarshal([]byte(row.Shown), &shown)
		computed := computing[row.DataSourceID]
		if computed == nil {
			if computed, err = newNotionMigrationVerifyComputing(v.app, principal, row); err != nil {
				return err
			}
			computing[row.DataSourceID] = computed
		}
		var values map[string]databaseComputedValue
		computedRow := false
		for _, property := range byDatabase[row.Database] {
			column, found := bySource[notionSplitColumnSource(property.SourceID, row.DataSourceID)]
			if own, ownFound := bySource[property.SourceID]; ownFound && own.DataSourceID == row.DataSourceID {
				column, found = own, true
			}
			if !found || column.DataSourceID != row.DataSourceID || !computed.compared(column.ID) {
				continue
			}
			if !computedRow {
				computedRow = true
				unsupported := false
				if values, unsupported, err = computed.values(v.app, row); err != nil {
					return err
				}
				if unsupported {
					break
				}
			}
			leafen, comparable, err := computed.shown(column.ID, values[column.ID])
			if err != nil {
				return err
			}
			if !comparable {
				continue
			}
			notion := shown[property.SourceID]
			if notionMigrationVerifySameShown(notion, leafen) {
				continue
			}
			v.differ(notionMigrationVerifyDifference{
				Kind: notionMigrationVerifyKindCell, Setting: notionMigrationVerifyComputed, NotionID: notionMigrationVerifyDatabase(row.Database), ID: row.RowID,
				Title:  cmpOr(strings.TrimSpace(row.Title), untitled) + " · " + column.Name,
				Notion: notionMigrationVerifyCut(notion), Leafen: notionMigrationVerifyCut(leafen),
				Reason: reasons.of(property, column.Shared > 1),
			})
		}
	}
	return nil
}

// notionMigrationVerifySameShown says whether two computed values shown are
// the same: the same words, numbers equal to the last digits a float
// keeps, or dates and times within the minute Notion gives times to.
func notionMigrationVerifySameShown(notion, leafen string) bool {
	notion, leafen = strings.TrimSpace(notion), strings.TrimSpace(leafen)
	if notion == leafen {
		return true
	}
	if left, err := strconv.ParseFloat(notion, 64); err == nil {
		right, err := strconv.ParseFloat(leafen, 64)
		return err == nil && math.Abs(left-right) <= 1e-9*math.Max(1, math.Max(math.Abs(left), math.Abs(right)))
	}
	leftParts, rightParts := strings.Split(notion, " → "), strings.Split(leafen, " → ")
	if len(leftParts) != len(rightParts) {
		return false
	}
	for index := range leftParts {
		left, leftTimed, leftOK := notionMigrationVerifyTime(leftParts[index])
		right, rightTimed, rightOK := notionMigrationVerifyTime(rightParts[index])
		if !leftOK || !rightOK || leftTimed != rightTimed || math.Abs(left.Sub(right).Seconds()) >= 60 {
			return false
		}
	}
	return true
}

// notionMigrationVerifyTime reads a date or a time a computed value shows.
func notionMigrationVerifyTime(text string) (time.Time, bool, bool) {
	if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(text)); err == nil {
		return parsed, true, true
	}
	parsed, err := time.Parse(time.DateOnly, strings.TrimSpace(text))
	return parsed, false, err == nil
}

// notionMigrationVerifyComputedReasons says why a computed value differs.
type notionMigrationVerifyComputedReasons struct {
	into    map[string]map[string]string
	columns map[string]map[string]notionMigrationPlanProperty
	// The other sides of two-way columns holding another property's rows
	// (notion_migration_verify_shared_pairs.go).
	others     map[string]bool
	properties map[string][]notionMigrationVerifyFormulaProperty
	bySource   map[string]notionMigrationVerifyFormulaProperty
	said       map[string]string
}

// of is why a formula's or rollup's value differs: its import issue, its
// column shared with a merge target's, or what it reads; unexplained else.
func (reasons notionMigrationVerifyComputedReasons) of(property notionMigrationVerifyFormulaProperty, shared bool) string {
	if reason, known := reasons.said[property.SourceID]; known {
		return reason
	}
	// One reading itself through others reads nothing more.
	reasons.said[property.SourceID] = notionMigrationVerifyUnexplained
	reason := property.Issues
	if reason == "" && shared {
		reason = notionMigrationVerifyPlanChanged
	}
	plan := notionDatabasePropertyPlan{}
	_ = json.Unmarshal([]byte(property.Text), &plan)
	reads := []string{}
	if expression, _ := plan.Config["formula"].(string); plan.Type == graphValueTypeFormula {
		for _, call := range databaseFormulaPropCalls([]rune(expression)) {
			for _, read := range reasons.properties[property.Database] {
				if read.Title == call.Name {
					reads = append(reads, read.SourceID)
					break
				}
			}
		}
	} else if plan.Link != nil {
		reads = append(reads, plan.Link.RelationSourceID, plan.Link.TargetSourceID)
	}
	for _, sourceID := range reads {
		if reason != "" {
			break
		}
		read, found := reasons.bySource[sourceID]
		if !found {
			continue
		}
		switch {
		case read.Issues != "":
			reason = read.Issues
		case reasons.planChanges(read.SourceID):
			reason = notionMigrationVerifyPlanChanged
		default:
			// One it reads that differs differs for its reason.
			if said := reasons.of(read, false); said != notionMigrationVerifyUnexplained {
				reason = said
			}
		}
	}
	reasons.said[property.SourceID] = cmpOr(reason, notionMigrationVerifyUnexplained)
	return reasons.said[property.SourceID]
}

// planChanges says whether the plan changes a property as more than its
// name: its type or option names, kept as text, sent to the report, turned
// into objects, a relation gathered into another or one others are gathered
// into, or the other side of a column holding another property's rows.
func (reasons notionMigrationVerifyComputedReasons) planChanges(propertySource string) bool {
	if reasons.others[propertySource] {
		return true
	}
	databaseID, propertyID, _ := strings.Cut(propertySource, ":property:")
	key := notionMigrationPropertyKey(propertyID)
	entry := reasons.columns[notionMigrationSourceKey(databaseID)][key]
	if strings.TrimSpace(entry.Type) != "" || len(entry.Values) > 0 || strings.TrimSpace(entry.Into) != "" ||
		entry.Action != "" && entry.Action != "map" {
		return true
	}
	for _, into := range reasons.into[notionMigrationSourceKey(databaseID)] {
		if notionMigrationPropertyKey(into) == key {
			return true
		}
	}
	return false
}
