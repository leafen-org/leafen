package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// A row a split sends to another database keeps there every column its own
// database has (2026-11-24: a column was made there only for a property a
// row there held a value of, so a formula's, a rollup's and the created and
// edited columns were never made — the rows a split sent away lost what
// Notion showed of them — and a value Notion gave since to a property no row
// there had held was dropped on a run again). The first row a run sends to a
// target, or finds there, makes them (ensure): each property as column makes
// it — a formula reading the target's columns in place of its database's
// (formula), a formula or rollup the plan preserves as the text Notion shows
// — and the link phase makes each relation, each rollup reading through the
// relation's column there, and the formulas that read them
// (splitLinkedColumns). A property the plan reports, a relation it gathers
// into another (the role's column stands for it) and one it turns into
// objects (made with its values, notion_migration_objects.go) aren't made
// ahead.

// notionSplitColumns makes a split database's columns in one target: into
// is the database the plan names, target the data source it became.
type notionSplitColumns struct {
	app        core.App
	job        storedJob
	payload    notionImportJobPayload
	run        *notionMigrationRunPlan
	into       string
	target     string
	documents  map[string]*structuralMutationBatch
	mappings   *notionApplyMappings
	references *notionApplyPropertyReferences
	// gone says, in the link phase, whether a relation or rollup of the
	// database won't be made at all, so a formula reads it as its own does.
	gone func(propertySource string) (bool, error)
}

// splitAhead says whether a split database's property has its column in a
// target before a row there holds a value of it.
func (run *notionMigrationRunPlan) splitAhead(propertySource string) bool {
	databaseID, propertyID, found := strings.Cut(propertySource, ":property:")
	if run == nil || !found {
		return false
	}
	entry := run.columns[notionMigrationSourceKey(databaseID)][notionMigrationPropertyKey(propertyID)]
	return entry.Action != "report" && entry.Action != "object" && strings.TrimSpace(entry.Into) == ""
}

// notionSplitProperties are a staged database's properties, in staging order.
func notionSplitProperties(app core.App, jobID, database string) ([]storedNotionSource, error) {
	properties := []storedNotionSource{}
	err := app.DB().NewQuery(`
		SELECT [[sequence]], [[source_id]], [[kind]], [[parent_source_id]], [[parent_kind]], [[position]], [[title]], [[text]]
		FROM {{leafen_notion_import_sources}} WHERE [[job_id]]={:jobId} AND [[kind]]='property' AND [[parent_source_id]]={:database}
		ORDER BY [[sequence]]
	`).Bind(dbx.Params{"jobId": jobID, "database": database}).All(&properties)
	return properties, err
}

// ensure makes in the target the columns of a database's properties it
// doesn't have yet, once a run's window.
func (columns *notionSplitColumns) ensure(database string) error {
	made := database + "\x00" + columns.target
	if columns.run.splitMade[made] {
		return nil
	}
	properties, err := notionSplitProperties(columns.app, columns.job.ID, database)
	if err != nil {
		return err
	}
	for _, property := range properties {
		if !columns.run.splitAhead(property.SourceID) {
			continue
		}
		if _, err := columns.column(property.SourceID); err != nil {
			return err
		}
	}
	if columns.run.splitMade == nil {
		columns.run.splitMade = map[string]bool{}
	}
	columns.run.splitMade[made] = true
	return nil
}

// made is the column a property, by its ledger key, has in the target.
func (columns *notionSplitColumns) made(key string) (string, error) {
	if nativeID, found := columns.mappings.nativeID(key, "property"); found {
		return nativeID, nil
	}
	return notionImportMappedID(columns.app, columns.job.ID, key, "property")
}

// column is the column a split database's property has in the target: the
// target's of its name and type, taking the options it has none of, or a new
// column of the target under the plan's name, a formula reading the target's
// columns (formula) — "" while one it reads isn't there yet. A formula or
// rollup the plan preserves is the text Notion shows, as in its own database
// (keepShownAsText). A relation or rollup is made in the link phase.
func (columns *notionSplitColumns) column(propertySource string) (string, error) {
	app, job, run := columns.app, columns.job, columns.run
	key := notionSplitColumnSource(propertySource, columns.target)
	if nativeID, err := columns.made(key); err != nil || nativeID != "" {
		return nativeID, err
	}
	source, found, err := notionImportStagedSource(app, job.ID, propertySource)
	if err != nil || !found {
		return "", err
	}
	if source, err = run.plannedOptions(app, job.ID, source); err != nil {
		return "", err
	}
	source = run.keepShownAsText([]storedNotionSource{source})[0]
	plan := notionDatabasePropertyPlan{}
	if err := json.Unmarshal([]byte(source.Text), &plan); err != nil {
		return "", err
	}
	if plan.linked() {
		return "", nil
	}
	column, err := run.mergeColumn(source.ParentSourceID, source)
	if err != nil {
		return "", err
	}
	nativeID, apart := "", false
	if column.Kept {
		if nativeID, apart, err = run.targetColumnApart(app, job.ID, column, columns.into, source.ParentSourceID,
			func(_ storedNotionSource, key string, _ bool) (string, error) {
				if nativeID, found := columns.mappings.nativeID(key, "property"); found {
					return nativeID, nil
				}
				return notionImportMappedID(app, job.ID, key, "property")
			}); err != nil {
			return "", err
		}
	}
	if nativeID != "" {
		if err := joinNotionMergedOptions(app, job, columns.payload, plan, nativeID, columns.documents, columns.references); err != nil {
			return "", err
		}
		return nativeID, columns.mappings.insert(key, nativeID, "property")
	}
	if plan.Type == graphValueTypeFormula {
		if ready, err := columns.formula(source, plan.Config); err != nil || !ready {
			return "", err
		}
	}
	// A formula computing otherwise than the target's of its name comes
	// beside it (notion_migration_merge_computed.go).
	name := column.Name
	if apart {
		if name, err = run.apartName(app, job.ID, column.Name, source.ParentSourceID); err != nil {
			return "", err
		}
	}
	return columns.write(key, source, plan, name)
}

// write makes a new column of the target for a property.
func (columns *notionSplitColumns) write(key string, source storedNotionSource, plan notionDatabasePropertyPlan, name string) (string, error) {
	pageID, err := notionMigrationDataSourcePage(columns.app, columns.target)
	if err != nil {
		return "", err
	}
	config, err := json.Marshal(plan.Config)
	if err != nil {
		return "", err
	}
	if string(config) == "null" {
		config = []byte("{}")
	}
	propertyID := security.RandomString(15)
	batch, err := loadStructuralMutationBatch(columns.app, pageID, columns.documents)
	if err != nil {
		return "", err
	}
	owner := columns.payload.AuthorityOwnerID
	if err := applyNotionGraphOperationsWithReferences(columns.app, batch, owner, columns.job.WorkspaceID, pageID, columns.job.Owner,
		[]graphOperation{{Kind: graphOperationKindUpsertProperty, Property: &graphProperty{
			ID: propertyID, DataSourceID: columns.target, Type: plan.Type, Name: name,
			Position: fmt.Sprintf("z%010d", source.Position), Config: config,
		}}}, nil, columns.references, nil,
	); err != nil {
		return "", err
	}
	if err := columns.references.insert(owner, pageID, columns.target, propertyID, plan.Type, string(config)); err != nil {
		return "", err
	}
	return propertyID, columns.mappings.insert(key, propertyID, "property")
}

// formula makes a formula's expression read, in place of each property of
// its database, the column that property has in the target — by its name
// there, or by its ID while another column there shares the name — and says
// false while a column it reads isn't there yet: a relation's or a rollup's,
// which the link phase makes, or a formula's waiting for one. A name no
// property of the database has, or one the plan reports, is left as it is.
func (columns *notionSplitColumns) formula(source storedNotionSource, config map[string]any) (bool, error) {
	expression, _ := config["formula"].(string)
	if expression == "" {
		return true, nil
	}
	properties, err := notionSplitProperties(columns.app, columns.job.ID, source.ParentSourceID)
	if err != nil {
		return false, err
	}
	byName := map[string]storedNotionSource{}
	for _, property := range properties {
		if _, taken := byName[property.Title]; !taken {
			byName[property.Title] = property
		}
	}
	// A formula reading itself, which Notion doesn't let one do, waits.
	if columns.run.splitReading == nil {
		columns.run.splitReading = map[string]bool{}
	}
	if columns.run.splitReading[source.SourceID] {
		return false, nil
	}
	columns.run.splitReading[source.SourceID] = true
	defer delete(columns.run.splitReading, source.SourceID)
	names := map[string]string{}
	for _, call := range databaseFormulaPropCalls([]rune(expression)) {
		read, found := byName[call.Name]
		if _, done := names[call.Name]; done || !found {
			continue
		}
		nativeID, err := columns.read(read)
		if err != nil || nativeID == "-" {
			return false, err
		}
		if nativeID == "" {
			continue
		}
		reference := struct {
			Name   string `db:"name"`
			Shared int    `db:"shared"`
		}{}
		if err := columns.app.DB().NewQuery(`
			SELECT [[property]].[[name]], (SELECT COUNT(*) FROM {{leafen_database_properties}} AS [[other]]
				WHERE [[other]].[[data_source_id]]=[[property]].[[data_source_id]] AND [[other]].[[name]]=[[property]].[[name]]) AS [[shared]]
			FROM {{leafen_database_properties}} AS [[property]] WHERE [[property]].[[id]]={:id}
		`).Bind(dbx.Params{"id": nativeID}).One(&reference); errors.Is(err, sql.ErrNoRows) {
			continue
		} else if err != nil {
			return false, err
		}
		names[call.Name] = reference.Name
		if reference.Shared > 1 {
			names[call.Name] = nativeID
		}
	}
	rewritten, _ := rewriteDatabaseFormulaReferences(expression, func(name string) (string, bool) {
		next, found := names[name]
		return next, found && next != name
	})
	config["formula"] = rewritten
	return true, nil
}

// read is the column a property a formula reads has in the target: "" for
// one left as it is — as its database's own formula reads it, one the link
// phase won't make there either — "-" for one not there yet.
func (columns *notionSplitColumns) read(property storedNotionSource) (string, error) {
	databaseID, propertyID, _ := strings.Cut(property.SourceID, ":property:")
	entry := columns.run.columns[notionMigrationSourceKey(databaseID)][notionMigrationPropertyKey(propertyID)]
	if entry.Action == "report" {
		return "", nil
	}
	held := property.SourceID
	if role, gathered := columns.run.roleColumn(held); gathered {
		held = role
	}
	plan := notionDatabasePropertyPlan{}
	_ = json.Unmarshal([]byte(columns.run.keepShownAsText([]storedNotionSource{property})[0].Text), &plan)
	nativeID := ""
	var err error
	if plan.linked() || entry.Action == "object" || held != property.SourceID {
		nativeID, err = columns.made(notionSplitColumnSource(held, columns.target))
		if err == nil && nativeID == "" && columns.gone != nil && entry.Action != "object" {
			if gone, err := columns.gone(held); err != nil || gone {
				return "", err
			}
		}
	} else {
		nativeID, err = columns.column(property.SourceID)
	}
	if err == nil && nativeID == "" {
		nativeID = "-"
	}
	return nativeID, err
}
