package app

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A query reads through its relations to the data sources they point to: a
// rollup reads a property of the related source, and a formula there reads
// that source's properties by name. The related sources' properties join
// the query context, and their rows may be read from the query's page and
// from the related pages the person querying can read and the query's
// reading reaches — a Public API connection's grant, an agent's token
// (database_query_reader_mentions.go). Rows on any other page stay
// invisible, as rows they cannot open, and a relation to a source that is
// gone or in the trash reads no rows.

func databaseQueryPagesJSON(pages []string) string {
	encoded, err := json.Marshal(pages)
	if err != nil || len(pages) == 0 {
		return "[]"
	}
	return string(encoded)
}

type databaseQueryRelatedSources struct {
	// Targets maps each relation property the query's computed values
	// follow to the data source it points to: the query's own relations and
	// those of the related sources whose formulas and rollups they read.
	Targets map[string]string
	// Sources are the target sources that exist and are not in the trash,
	// each with its page, the query's own related sources first.
	Sources []databaseQueryRelatedSource
	// Properties are the properties read of each source, every one with
	// everyProperty.
	Properties map[string][]storedDatabaseQueryProperty
	// ReadablePages start with the query's page.
	ReadablePages []string
}

type databaseQueryRelatedSource struct {
	ID     string `db:"id"`
	PageID string `db:"page_id"`
}

// resolveDatabaseQueryRelatedSources finds the sources a query reads: those
// its relations point to, and past them those the computed values it reads
// there read (databaseComputedReach), as far as databaseRollupReachLimit.
// With everyProperty every such source's properties are read, as the query
// context holds them; without, only the relations and computed values of
// those whose computed values are read, as the query's revision follows them.
func resolveDatabaseQueryRelatedSources(
	app core.App,
	job storedDatabaseQueryJob,
	properties []storedDatabaseQueryProperty,
	everyProperty bool,
) (databaseQueryRelatedSources, error) {
	result := databaseQueryRelatedSources{
		Targets: map[string]string{}, Properties: map[string][]storedDatabaseQueryProperty{},
		ReadablePages: []string{job.PageID},
	}
	reach := newDatabaseComputedReach()
	reach.start(job.DataSourceID, properties)
	found := map[string]databaseQueryRelatedSource{}
	looked := map[string]bool{job.DataSourceID: true}
	for hop := 0; hop <= databaseRollupReachLimit; hop++ {
		level := []string{}
		for sourceID := range reach.Reached {
			if !looked[sourceID] {
				looked[sourceID] = true
				level = append(level, sourceID)
			}
		}
		sort.Strings(level)
		if len(level) > 0 {
			sources, err := loadDatabaseQueryRelatedSourceRecords(app, job.ContentOwnerID, level)
			if err != nil {
				return result, err
			}
			for _, source := range sources {
				found[source.ID] = source
				result.Sources = append(result.Sources, source)
			}
			for _, sourceID := range level {
				if _, exists := found[sourceID]; !exists {
					// A source gone or in the trash reads no rows, and nothing
					// is read past it.
					reach.load(sourceID, nil)
				}
			}
		}
		loading := []databaseQueryRelatedSource{}
		for _, source := range result.Sources {
			if !reach.loaded(source.ID) && (everyProperty || len(reach.pending[source.ID]) > 0) {
				loading = append(loading, source)
			}
		}
		if len(level) == 0 && len(loading) == 0 {
			break
		}
		for _, source := range loading {
			sourceProperties, err := loadDatabaseQueryRelatedSourceProperties(app, job.ContentOwnerID, source, everyProperty)
			if err != nil {
				return result, err
			}
			result.Properties[source.ID] = sourceProperties
			reach.load(source.ID, sourceProperties)
		}
	}
	for propertyID, target := range reach.Targets {
		if target != job.DataSourceID {
			result.Targets[propertyID] = target
		}
	}
	pageAccess := map[string]bool{job.PageID: true}
	for _, source := range result.Sources {
		if _, checked := pageAccess[source.PageID]; checked {
			continue
		}
		allowed := false
		if job.PrincipalID != "" {
			page, err := authorizePage(app, job.PrincipalID, source.PageID, permissionWorkspaceRead)
			if err != nil && !isPermissionDenied(err) {
				return result, err
			}
			allowed = err == nil && page.Workspace.WorkspaceID == job.WorkspaceID &&
				job.mentions.reachesRelatedPage(app, page)
		}
		pageAccess[source.PageID] = allowed
		if allowed {
			result.ReadablePages = append(result.ReadablePages, source.PageID)
		}
	}
	return result, nil
}

// loadDatabaseQueryRelatedSourceRecords are those of the sources that exist
// and are not in the trash, by ID; the owner is read after (+), or SQLite,
// which keeps no statistics, reads the owner's every source.
func loadDatabaseQueryRelatedSourceRecords(app core.App, owner string, ids []string) ([]databaseQueryRelatedSource, error) {
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	sources := []databaseQueryRelatedSource{}
	err = app.DB().NewQuery(`
		SELECT [[source]].[[id]], [[source]].[[page_id]] FROM {{leafen_data_sources}} AS [[source]]
		WHERE +[[source]].[[owner]] = {:owner} AND [[source]].[[id]] IN (SELECT [[value]] FROM json_each({:ids}))
			AND ` + databaseSourceNotTrashedSQL("[[source]].[[id]]") + `
		ORDER BY [[source]].[[id]]
	`).Bind(dbx.Params{"owner": owner, "ids": string(encoded)}).All(&sources)
	return sources, err
}

// loadDatabaseQueryRelatedSourceProperties are a related source's
// properties not in the trash, or with everyProperty false only those a
// computed value is followed through.
func loadDatabaseQueryRelatedSourceProperties(
	app core.App, owner string, source databaseQueryRelatedSource, everyProperty bool,
) ([]storedDatabaseQueryProperty, error) {
	kinds := "TRUE"
	if !everyProperty {
		kinds = "[[type]] IN ('relation', 'formula', 'rollup')"
	}
	properties := []storedDatabaseQueryProperty{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[data_source_id]], [[type]], [[name]], [[position]], [[config]]
		FROM {{leafen_database_properties}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:dataSourceId}
			AND ` + kinds + ` AND ` + databasePropertyNotTrashedSQL("[[id]]") + `
		ORDER BY [[position]], [[id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"owner": owner, "pageId": source.PageID, "dataSourceId": source.ID, "limit": maxDatabaseQueryProperties,
	}).All(&properties)
	return properties, err
}

func addDatabaseQueryRelatedSources(app core.App, job storedDatabaseQueryJob, context *databaseQueryContext) error {
	context.ReadablePages = []string{job.PageID}
	context.UnavailableRelations = map[string]bool{}
	context.ClosedRelations = map[string]bool{}
	context.PropertiesByNameBySource = map[string]map[string]storedDatabaseQueryProperty{job.DataSourceID: {}}
	for _, property := range context.Properties {
		if _, exists := context.PropertiesByNameBySource[job.DataSourceID][property.Name]; !exists {
			context.PropertiesByNameBySource[job.DataSourceID][property.Name] = property
		}
	}
	related, err := resolveDatabaseQueryRelatedSources(app, job, context.Properties, true)
	if err != nil {
		return err
	}
	context.ReadablePages = related.ReadablePages
	hidden, err := databaseQueryHiddenRelatedRows(app, job, related)
	if err != nil {
		return err
	}
	context.Hidden = make(map[string]bool, len(hidden))
	for _, id := range hidden {
		context.Hidden[id] = true
	}
	available := make(map[string]struct{}, len(related.Sources))
	for _, source := range related.Sources {
		available[source.ID] = struct{}{}
	}
	for propertyID, target := range related.Targets {
		if _, ok := available[target]; !ok {
			context.UnavailableRelations[propertyID] = true
		}
	}
	// A reading that names the related pages it reaches — a connection's,
	// an agent's — reads a relation to a source on a page it doesn't read as
	// empty, as its row page shows it: its conditions and formulas never
	// learn which rows link there.
	if job.mentions.reaches != nil {
		readable := make(map[string]bool, len(related.ReadablePages))
		for _, pageID := range related.ReadablePages {
			readable[pageID] = true
		}
		closed := map[string]bool{}
		for _, source := range related.Sources {
			closed[source.ID] = !readable[source.PageID]
		}
		for propertyID, target := range related.Targets {
			if closed[target] {
				context.ClosedRelations[propertyID] = true
			}
		}
	}
	// Every available source's properties join the context so its rollups
	// and formulas resolve; only rows on readable pages are ever loaded.
	properties := []storedDatabaseQueryProperty{}
	for _, source := range related.Sources {
		properties = append(properties, related.Properties[source.ID]...)
	}
	for _, property := range properties {
		if _, exists := context.PropertiesByID[property.ID]; exists {
			continue
		}
		context.PropertiesByID[property.ID] = property
		byName := context.PropertiesByNameBySource[property.DataSourceID]
		if byName == nil {
			byName = map[string]storedDatabaseQueryProperty{}
			context.PropertiesByNameBySource[property.DataSourceID] = byName
		}
		if _, exists := byName[property.Name]; !exists {
			byName[property.Name] = property
		}
	}
	return nil
}

// databaseQueryHiddenRelatedRows are the unlinked items of the query's own
// and related sources that the person querying has no role in, sorted.
func databaseQueryHiddenRelatedRows(app core.App, job storedDatabaseQueryJob, related databaseQueryRelatedSources) ([]string, error) {
	sources := []string{job.DataSourceID}
	for _, source := range related.Sources {
		sources = append(sources, source.ID)
	}
	access, err := loadDatabaseRowAccess(app, job.PrincipalID, sources)
	if err != nil || access == nil {
		return nil, err
	}
	hidden := []string{}
	for rowID := range access.roles {
		if access.hides(rowID) {
			hidden = append(hidden, rowID)
		}
	}
	sort.Strings(hidden)
	return hidden, nil
}

// databaseQueryJobRevision fingerprints what a query reads: its own page and
// the related pages the person querying can read, and the related items
// hidden from them. An edit on a related page changes the query's rollups,
// and so does losing or gaining access to one or to an item, so any of these
// starts a fresh job instead of reusing the stored one.
func databaseQueryJobRevision(app core.App, job storedDatabaseQueryJob) (int, error) {
	own, err := loadDatabaseQueryRelatedSourceProperties(
		app, job.ContentOwnerID, databaseQueryRelatedSource{ID: job.DataSourceID, PageID: job.PageID}, false,
	)
	if err != nil {
		return 0, err
	}
	related, err := resolveDatabaseQueryRelatedSources(app, job, own, false)
	if err != nil {
		return 0, err
	}
	hidden, err := databaseQueryHiddenRelatedRows(app, job, related)
	if err != nil {
		return 0, err
	}
	if len(related.ReadablePages) == 1 && len(hidden) == 0 {
		return databaseQuerySourceRevision(app, job.ContentOwnerID, job.PageID)
	}
	cursors := []struct {
		PageID string `db:"page_id"`
		Cursor int    `db:"cursor"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[page_id]], COALESCE(MAX([[cursor]]), 0) AS [[cursor]] FROM {{leafen_graph_mutations}}
		WHERE [[owner]] = {:owner} AND [[page_id]] IN (SELECT [[value]] FROM json_each({:pages}))
		GROUP BY [[page_id]] ORDER BY [[page_id]]
	`).Bind(dbx.Params{
		"owner": job.ContentOwnerID, "pages": databaseQueryPagesJSON(related.ReadablePages),
	}).All(&cursors); err != nil {
		return 0, err
	}
	byPage := make(map[string]int, len(cursors))
	for _, cursor := range cursors {
		byPage[cursor.PageID] = cursor.Cursor
	}
	hash := fnv.New64a()
	for _, pageID := range related.ReadablePages {
		fmt.Fprintf(hash, "%s\x00%d\x00", pageID, byPage[pageID])
	}
	for _, rowID := range hidden {
		fmt.Fprintf(hash, "hidden\x00%s\x00", rowID)
	}
	return int(hash.Sum64() & (1<<62 - 1)), nil
}
