package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Once every source is applied, the import links what points across
// databases: it makes each relation property toward the imported database
// it relates to, pairs the two sides of a two-way relation as the relation
// pair authority keeps them (each naming the other in relatedPropertyId),
// makes each rollup over its relation, and sets each row's related rows. A
// relation to a database the import did not bring over is not made, and a
// related page it did not bring over is left out; both are reported.

type notionImportLinker struct {
	app        core.App
	job        storedJob
	payload    notionImportJobPayload
	resolver   *notionImportReferenceResolver
	mappings   *notionApplyMappings
	linked     map[string]string
	staged     map[string]*notionDatabasePropertyPlan
	pages      map[string]string
	documents  map[string]*structuralMutationBatch
	views      map[string]*notionImportViewPlan
	converters map[string]*notionImportViewConverter
	// The plan the migration runs, for its merges; nil for an import on its own.
	run *notionMigrationRunPlan
	// Whether a merged database's relation stands for its target's column (notion_migration_shared_pairs.go).
	shared map[string]bool
}

// lookup returns what the import made from a Notion object, whether Notion
// wrote its ID with or without dashes.
func (linker *notionImportLinker) lookup(notionID, kind string) (string, error) {
	nativeID, err := linker.resolver.lookup(notionID, kind)
	if err != nil || nativeID != "" {
		return nativeID, err
	}
	compact := strings.ReplaceAll(notionID, "-", "")
	if compact != notionID {
		return linker.resolver.lookup(compact, kind)
	}
	if len(compact) == 32 {
		return linker.resolver.lookup(
			compact[:8]+"-"+compact[8:12]+"-"+compact[12:16]+"-"+compact[16:20]+"-"+compact[20:], kind,
		)
	}
	return "", nil
}

// property makes the relation or rollup property a staged property source
// stands for.
func (linker *notionImportLinker) property(source storedNotionSource) error {
	plan := notionDatabasePropertyPlan{}
	if err := json.Unmarshal([]byte(source.Text), &plan); err != nil {
		return err
	}
	if !plan.linked() {
		return nil
	}
	if existing, err := linker.propertyID(source.SourceID); err != nil || existing != "" {
		return err
	}
	// A relation the plan gathers into another (notion_migration_roles.go).
	if role, gathered := linker.run.roleColumn(source.SourceID); gathered {
		return linker.gatherRelation(source, role)
	}
	// A merged database's relation or rollup stands for its target's of the
	// same name, type and related database, or comes as a new column under
	// the plan's name (notion_migration_merge.go).
	if _, merged := linker.run.mergeTarget(source.ParentSourceID); merged {
		// The target's own column is made first when it isn't yet.
		targetID, column, err := notionMergeTargetProperty(linker.app, linker.job.ID, linker.run, source,
			func(candidate storedNotionSource, key string, own bool) (string, error) {
				nativeID, err := linker.propertyID(key)
				if err != nil || nativeID != "" || !own {
					return nativeID, err
				}
				if err := linker.property(candidate); err != nil {
					return "", err
				}
				return linker.propertyID(candidate.SourceID)
			})
		if err != nil {
			return err
		}
		if targetID != "" {
			linker.linked[source.SourceID] = targetID
			return linker.mappings.insert(source.SourceID, targetID, "property")
		}
		source.Title = column.Name
	}
	dataSourceID, err := linker.lookup(source.ParentSourceID, "data_source")
	if err != nil {
		return err
	}
	pageID, err := linker.dataSourcePage(dataSourceID)
	if err != nil {
		return err
	}
	config := maps.Clone(plan.Config)
	if config == nil {
		config = map[string]any{}
	}
	sources := map[string]graphEntityReference{}
	unresolved := func() error {
		return recordNotionImportPropertyIssue(
			linker.app, linker.job.ID, source.SourceID, notionImportIssuePropertyTargetUnresolved, false,
		)
	}
	if plan.Link == nil {
		return unresolved()
	}
	switch plan.Type {
	case graphValueTypeRelation:
		target, err := linker.relationTarget(*plan.Link)
		if err != nil {
			return err
		}
		if target == "" {
			return unresolved()
		}
		config["relationDatabaseId"] = target
		if plan.Link.SyncedPropertyID != "" {
			pair, err := linker.relationPair(source, plan)
			if err != nil {
				return err
			}
			if pair == "" {
				if err := recordNotionImportPropertyIssue(
					linker.app, linker.job.ID, source.SourceID, notionImportIssuePropertySettingOmitted, false,
				); err != nil {
					return err
				}
			} else {
				config["relatedPropertyId"] = pair
			}
		}
		references, err := loadGraphReferences(linker.app, "leafen_data_sources", map[string]struct{}{target: {}})
		if err != nil {
			return err
		}
		for id, reference := range references {
			if reference.Owner == linker.payload.AuthorityOwnerID {
				sources[id] = reference
			}
		}
	case graphValueTypeRollup:
		relationID, targetID, err := linker.rollupReferences(*plan.Link)
		if err != nil {
			return err
		}
		if relationID == "" || targetID == "" {
			return unresolved()
		}
		config["rollupRelationPropertyId"], config["rollupTargetPropertyId"] = relationID, targetID
	}
	propertyID := notionImportLinkedPropertyID(linker.job.ID, source.SourceID)
	var taken int
	if err := linker.app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_database_properties}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": propertyID}).Row(&taken); err != nil {
		return err
	}
	if taken != 0 {
		return errors.New(errorCodeNotionImportCreateFailed)
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return err
	}
	batch, err := loadStructuralMutationBatch(linker.app, pageID, linker.documents)
	if err != nil {
		return err
	}
	operations := []graphOperation{{
		Kind: graphOperationKindUpsertProperty,
		Property: &graphProperty{
			ID: propertyID, DataSourceID: dataSourceID, Type: plan.Type, Name: source.Title,
			Position: fmt.Sprintf("p%010d", source.Position), Config: encoded,
		},
	}}
	if err := applyNotionGraphOperationsWith(
		linker.app, batch, linker.payload.AuthorityOwnerID, linker.job.WorkspaceID, pageID, linker.job.Owner,
		operations, notionGraphApplyReferences{sources: sources},
	); err != nil {
		return err
	}
	linker.linked[source.SourceID] = propertyID
	return linker.mappings.insert(source.SourceID, propertyID, "property")
}

// propertyID returns the property a staged property source became, or ""
// when the import has not made it.
func (linker *notionImportLinker) propertyID(sourceID string) (string, error) {
	if propertyID, found := linker.linked[sourceID]; found {
		return propertyID, nil
	}
	return linker.resolver.lookup(sourceID, "property")
}

// stagedProperty reads a staged property's plan, or nil when the import did
// not stage the property.
func (linker *notionImportLinker) stagedProperty(sourceID string) (*notionDatabasePropertyPlan, error) {
	if plan, found := linker.staged[sourceID]; found {
		return plan, nil
	}
	texts := []string{}
	if err := linker.app.DB().NewQuery(`
		SELECT [[text]] FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]]={:jobId} AND [[source_id]]={:sourceId} AND [[kind]]='property'
	`).Bind(dbx.Params{"jobId": linker.job.ID, "sourceId": sourceID}).Column(&texts); err != nil {
		return nil, err
	}
	var plan *notionDatabasePropertyPlan
	if len(texts) == 1 {
		plan = &notionDatabasePropertyPlan{}
		if json.Unmarshal([]byte(texts[0]), plan) != nil {
			plan = nil
		}
	}
	linker.staged[sourceID] = plan
	return plan, nil
}

// relationPair returns the property on the other side of a two-way relation,
// when the import brings that side over pointing back at this one. Within
// one database both properties stay on that database, as the relation pair
// authority keeps them.
func (linker *notionImportLinker) relationPair(
	source storedNotionSource, plan notionDatabasePropertyPlan,
) (string, error) {
	pairSource, err := linker.keyedPropertySource(
		plan.Link.RelationDatabaseID, plan.Link.RelationDataSourceID, plan.Link.SyncedPropertyID,
	)
	if err != nil {
		return "", err
	}
	// The other side standing for another column, gathered or merged, isn't one.
	if aliased, err := linker.aliased(pairSource); err != nil || aliased {
		return "", err
	}
	pair, err := linker.stagedProperty(pairSource)
	if err != nil || pair == nil || pair.Type != graphValueTypeRelation || pair.Link == nil {
		return "", err
	}
	notionID, _ := plan.Config["notionPropertyId"].(string)
	// The other side points back at this property's database, and at its
	// data source when the database has several.
	databaseID, dataSourceID := notionImportKeyedIDs(source.ParentSourceID)
	if !notionImportSameID(pair.Link.RelationDatabaseID, databaseID) || pair.Link.SyncedPropertyID != notionID ||
		dataSourceID != "" && pair.Link.RelationDataSourceID != "" && !notionImportSameID(pair.Link.RelationDataSourceID, dataSourceID) {
		return "", nil
	}
	return notionImportLinkedPropertyID(linker.job.ID, pairSource), nil
}

// rollupReferences returns the relation a rollup reads through and the
// property it reads, or "" for either the import does not make.
func (linker *notionImportLinker) rollupReferences(link notionImportPropertyLink) (string, string, error) {
	relationID, err := linker.linkedPropertyID(link.RelationSourceID, 0)
	if err != nil || relationID == "" {
		return "", "", err
	}
	targetSource := link.TargetSourceID
	if link.RelationDataSourceID != "" && targetSource != "" {
		// The property it reads sits in the data source its relation names.
		_, propertyID, _ := strings.Cut(targetSource, ":property:")
		if targetSource, err = linker.keyedPropertySource(
			link.RelationDatabaseID, link.RelationDataSourceID, propertyID,
		); err != nil {
			return "", "", err
		}
	}
	targetID, err := linker.linkedPropertyID(targetSource, 0)
	return relationID, targetID, err
}

// linkedPropertyID returns the property a staged property source becomes,
// even before the link phase makes it, or "" when it will not exist.
func (linker *notionImportLinker) linkedPropertyID(sourceID string, depth int) (string, error) {
	if sourceID == "" || depth > 2 {
		return "", nil
	}
	if propertyID, err := linker.propertyID(sourceID); err != nil || propertyID != "" {
		return propertyID, err
	}
	plan, err := linker.stagedProperty(sourceID)
	if err != nil || plan == nil || !plan.linked() || plan.Link == nil {
		return "", err
	}
	switch plan.Type {
	case graphValueTypeRelation:
		target, err := linker.relationTarget(*plan.Link)
		if err != nil || target == "" {
			return "", err
		}
		// One standing for another column, gathered or merged, is that column, made now
		// (2026-11-24: a merged rollup before its relation read a relation no column was).
		if aliased, err := linker.aliased(sourceID); err != nil || aliased {
			source, found, err := notionImportStagedSource(linker.app, linker.job.ID, sourceID)
			if err != nil || !found {
				return "", err
			}
			if err := linker.property(source); err != nil {
				return "", err
			}
			return linker.propertyID(sourceID)
		}
	case graphValueTypeRollup:
		for _, reference := range []string{plan.Link.RelationSourceID, plan.Link.TargetSourceID} {
			resolved, err := linker.linkedPropertyID(reference, depth+1)
			if err != nil || resolved == "" {
				return "", err
			}
		}
	}
	return notionImportLinkedPropertyID(linker.job.ID, sourceID), nil
}

func (linker *notionImportLinker) dataSourcePage(dataSourceID string) (string, error) {
	if pageID, found := linker.pages[dataSourceID]; found {
		return pageID, nil
	}
	pageIDs := []string{}
	if dataSourceID != "" {
		if err := linker.app.DB().NewQuery(`
			SELECT [[page_id]] FROM {{leafen_data_sources}} WHERE [[id]]={:id} AND [[owner]]={:owner}
		`).Bind(dbx.Params{"id": dataSourceID, "owner": linker.payload.AuthorityOwnerID}).Column(&pageIDs); err != nil {
			return "", err
		}
	}
	if len(pageIDs) != 1 {
		return "", errors.New(errorCodeNotionImportCreateFailed)
	}
	linker.pages[dataSourceID] = pageIDs[0]
	return pageIDs[0], nil
}

// row sets an imported row's related rows; on a run again, also those the
// source emptied since (notion_migration_relation_pairs.go).
func (linker *notionImportLinker) row(source storedNotionSource) error {
	values, err := linker.rowValues(source)
	if err != nil {
		return err
	}
	propertySources := []string{}
	for propertySource, value := range values {
		if (value.Type == graphValueTypeRelation || linker.run.linkedLater(propertySource)) && !notionImportRowSpecialKey(propertySource) {
			propertySources = append(propertySources, propertySource)
		}
	}
	if len(propertySources) == 0 {
		return nil
	}
	sort.Strings(propertySources)
	rowID, err := linker.lookup(source.SourceID, "row")
	if err != nil || rowID == "" {
		return err
	}
	// A row a split sent elsewhere has its relations in its columns there
	// (notion_migration_split.go).
	dataSourceID, split, err := linker.rowPlace(source, rowID)
	if err != nil {
		return err
	}
	pageID, err := linker.dataSourcePage(dataSourceID)
	if err != nil {
		return err
	}
	rowValues := map[string]graphRowValue{}
	externalRows := map[string]graphEntityReference{}
	// The rows each column holds for another property (notion_migration_shared_pairs.go).
	sharedTargets := map[string][]string{}
	for _, propertySource := range propertySources {
		// A property the plan turns into objects (notion_migration_objects.go).
		if entry, object := linker.run.objectColumn(propertySource); object {
			if err := linker.objectValues(source, entry, propertySource, values[propertySource], dataSourceID, split, rowValues, externalRows); err != nil {
				return err
			}
			continue
		}
		// A gathered relation's values go into its role column (notion_migration_roles.go).
		holder, column, propertyID, shared, err := linker.relationColumnFor(propertySource, dataSourceID, split)
		if err != nil {
			return err
		}
		plan, err := linker.stagedProperty(propertySource)
		if err != nil {
			return err
		}
		if propertyID == "" || plan == nil || plan.Link == nil {
			// The relation was not made; the property's issue says why.
			continue
		}
		targetDataSourceID, err := linker.relationTarget(*plan.Link)
		if err != nil {
			return err
		}
		related, _ := values[propertySource].Value.([]any)
		candidates := map[string]struct{}{}
		ordered := []string{}
		unresolved := false
		for _, value := range related {
			notionID, _ := value.(string)
			nativeID, err := linker.lookup(notionID, "row")
			if err != nil {
				return err
			}
			if nativeID == "" {
				unresolved = true
				continue
			}
			if _, duplicate := candidates[nativeID]; !duplicate {
				candidates[nativeID] = struct{}{}
				ordered = append(ordered, nativeID)
			}
		}
		references, err := loadGraphReferences(linker.app, "leafen_database_rows", candidates)
		if err != nil {
			return err
		}
		targets := make([]string, 0, len(ordered))
		// A related row a split sent to another data source is kept in a
		// column beside this one, relating there.
		elsewhere, places := map[string][]string{}, []string{}
		for _, nativeID := range ordered {
			reference, found := references[nativeID]
			if !found || reference.Owner != linker.payload.AuthorityOwnerID {
				unresolved = true
				continue
			}
			if reference.DataSourceID != targetDataSourceID {
				if !linker.run.hasSecondPass() {
					unresolved = true
					continue
				}
				if elsewhere[reference.DataSourceID] == nil {
					places = append(places, reference.DataSourceID)
				}
				elsewhere[reference.DataSourceID] = append(elsewhere[reference.DataSourceID], nativeID)
				externalRows[nativeID] = reference
				continue
			}
			targets = append(targets, nativeID)
			externalRows[nativeID] = reference
		}
		if shared {
			sharedTargets[propertyID] = append(sharedTargets[propertyID], targets...)
		}
		if err := linker.siblingValues(rowValues, holder, column, dataSourceID, places, elsewhere); err != nil {
			return err
		}
		if unresolved {
			if err := recordNotionImportPropertyIssue(
				linker.app, linker.job.ID, propertySource, notionImportIssueRelationValueUnresolved, false,
			); err != nil {
				return err
			}
		}
		if err := linker.relationValue(rowValues, propertyID, targets); err != nil {
			return err
		}
	}
	if len(rowValues) == 0 {
		return nil
	}
	batch, err := loadStructuralMutationBatch(linker.app, pageID, linker.documents)
	if err != nil {
		return err
	}
	operations := []graphOperation{{
		Kind: graphOperationKindUpsertRow,
		Row: &graphDatabaseRow{
			ID: rowID, DataSourceID: dataSourceID,
			Position: fmt.Sprintf("p%010d", source.Position), Values: rowValues,
		},
	}}
	// A run again's two-way relations change both sides (notion_migration_relation_pairs.go),
	// and a first run's those a column holds for another property.
	return linker.writeRow(batch, pageID, source.SourceID, operations, externalRows, sharedTargets)
}

// notionImportSameID reports whether two Notion IDs name the same object,
// with or without dashes.
func notionImportSameID(left, right string) bool {
	normalize := func(value string) string {
		return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "-", ""))
	}
	return left != "" && normalize(left) == normalize(right)
}
