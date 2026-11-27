package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// Turning sub-items on gives the data source its Parent item and Sub-items,
// as Notion does, named in its sub-item settings so that rows nest by them.
// The two are named in the language of the person turning them on. A wiki's
// are its Parent page and Sub-pages, as Notion's wiki names them (seen
// 2026-11-11): a relation pair of its own, apart from the pages' places in
// the wiki, which stay as they are (database_subitem_relations.go).

// databaseSubitemPairIDs names a data source's Parent item and Sub-items
// the same way wherever they are made, so that making them again is the
// same change.
func databaseSubitemPairIDs(sourceID string) databaseSubitemPair {
	id := func(part string) string {
		digest := sha256.Sum256([]byte("leafen:subitem-pair\x00" + sourceID + "\x00" + part))
		return "subitems-" + part + "-" + hex.EncodeToString(digest[:12])
	}
	return databaseSubitemPair{Parent: id("parent"), Children: id("children")}
}

// wikiSubpagePairIDs names a wiki's Parent page and Sub-pages, apart from
// the pair a wiki's pages once nested by, which the job runner takes away
// (dropWikiNestingPairs).
func wikiSubpagePairIDs(sourceID string) databaseSubitemPair {
	id := func(part string) string {
		digest := sha256.Sum256([]byte("leafen:wiki-subpage-pair\x00" + sourceID + "\x00" + part))
		return "subpages-" + part + "-" + hex.EncodeToString(digest[:12])
	}
	return databaseSubitemPair{Parent: id("parent"), Children: id("children")}
}

// databaseSubitemPairIDsFor names the pair turning sub-items on gives a
// data source: a wiki's Parent page and Sub-pages, another's Parent item and
// Sub-items.
func databaseSubitemPairIDsFor(app core.App, sourceID string) (databaseSubitemPair, bool, error) {
	_, wiki, err := loadWikiSourceProperties(app, sourceID)
	if err != nil {
		return databaseSubitemPair{}, false, err
	}
	if wiki {
		return wikiSubpagePairIDs(sourceID), true, nil
	}
	return databaseSubitemPairIDs(sourceID), false, nil
}

// databaseSubitemPairProperties are the Parent item and Sub-items of a data
// source at the given places, each the other's pair.
func databaseSubitemPairProperties(
	sourceID string, pair databaseSubitemPair, parentPosition, childrenPosition, parentName, childrenName string,
) ([]graphOperation, error) {
	property := func(id, other, name, position string) (graphOperation, error) {
		settings := map[string]any{"relationDatabaseId": sourceID, "relatedPropertyId": other}
		if id == pair.Parent {
			// A row has one parent item.
			settings["limit"] = 1
		}
		config, err := json.Marshal(settings)
		return graphOperation{Kind: graphOperationKindUpsertProperty, Property: &graphProperty{
			ID: id, DataSourceID: sourceID, Type: graphValueTypeRelation, Name: name, Position: position, Config: config,
		}}, err
	}
	parent, err := property(pair.Parent, pair.Children, parentName, parentPosition)
	if err != nil {
		return nil, err
	}
	children, err := property(pair.Children, pair.Parent, childrenName, childrenPosition)
	return []graphOperation{parent, children}, err
}

// ensureDatabaseSubitemPair gives a data source whose sub-items are on its
// Parent item and Sub-items after its last property and names them in its
// sub-item settings, unless the settings name a pair already. The change is
// the actor's on the page, or authorized as the caller says.
func (p *pocketBaseYgoPersistence) ensureDatabaseSubitemPair(
	ctx context.Context, actorID, pageID, sourceID string, authorize func(core.App) (pageAuthorization, error),
) error {
	// Each turning on makes the pair as its own change: a pair removed
	// when sub-items went off comes back when they go on again.
	features := struct {
		Owner string `db:"owner"`
		Turn  int64  `db:"authority_revision"`
	}{}
	if err := p.app.DB().NewQuery(`
		SELECT [[owner]], [[authority_revision]] FROM {{leafen_database_task_features}}
		WHERE [[data_source_id]] = {:source} AND [[page_id]] = {:page}
			AND json_extract([[subitems_json]], '$.enabled') = 1
			AND json_type([[subitems_json]], '$.parentPropertyId') IS NULL
	`).Bind(dbx.Params{"source": sourceID, "page": pageID}).One(&features); errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	pair, wiki, err := databaseSubitemPairIDsFor(p.app, sourceID)
	if err != nil {
		return err
	}
	// A pair still being deleted since sub-items last went off is not named;
	// the job runner makes the pair again once it is gone.
	for _, propertyID := range []string{pair.Parent, pair.Children} {
		deleting, err := databaseMutationDeletionTargetFenced(
			p.app, features.Owner, pageID, sourceID, databaseMutationTargetProperty, propertyID,
		)
		if err != nil || deleting {
			return err
		}
	}
	name := func(app core.App) error {
		_, err := app.DB().NewQuery(`
			UPDATE {{leafen_database_task_features}}
			SET [[subitems_json]] = json_set([[subitems_json]], '$.parentPropertyId', {:parent}, '$.subitemsPropertyId', {:children})
			WHERE [[data_source_id]] = {:source} AND json_type([[subitems_json]], '$.parentPropertyId') IS NULL
		`).Bind(dbx.Params{"source": sourceID, "parent": pair.Parent, "children": pair.Children}).Execute()
		return err
	}
	var made int
	if err := p.app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_database_properties}} WHERE [[id]] IN ({:parent}, {:children})`).
		Bind(dbx.Params{"parent": pair.Parent, "children": pair.Children}).Row(&made); err != nil {
		return err
	}
	if made == 2 {
		return name(p.app)
	}
	mutationID := "subitem-pair:" + sourceID + ":" + strconv.FormatInt(features.Turn, 10)
	digest := sha256.Sum256([]byte("leafen:subitem-pair\x00" + pageID + "\x00" + mutationID))
	_, err = p.applyContentGraphMutationContext(ctx, actorID, pageID,
		graphMutationInput{MutationID: mutationID},
		contentGraphMutationHooks{
			// Authorized by its caller, it is the job runner's move of a
			// source's sub-items (database_subitem_pair_upkeep.go): Leafen's
			// own upkeep, no one's act.
			Authorize: authorize, LeafenOwn: authorize != nil,
			ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
				return hex.EncodeToString(digest[:]), nil
			},
			Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				var tail string
				if err := txApp.DB().NewQuery(`
					SELECT COALESCE(MAX([[position]]), '') FROM {{leafen_database_properties}} WHERE [[data_source_id]] = {:source}
				`).Bind(dbx.Params{"source": sourceID}).Row(&tail); err != nil {
					return contentGraphMutationPreparation{}, err
				}
				first, err := databasePositionAfter(tail)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				second, err := databasePositionAfter(first)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				names := generatedNamesFor(ctx, txApp, actorID)
				task, err := databaseIsTaskDatabase(txApp, sourceID)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				parentName, childrenName := names.ParentItem, names.SubItems
				switch {
				case wiki:
					parentName, childrenName = names.ParentPage, names.SubPages
				case task:
					parentName, childrenName = names.ParentTask, names.SubTasks
				}
				operations, err := databaseSubitemPairProperties(sourceID, pair, first, second, parentName, childrenName)
				if err != nil || !task {
					return contentGraphMutationPreparation{Operations: operations}, err
				}
				// A task database's pair starts hidden in its views, as Notion's
				// does (seen 2026-11-02: "표에서 숨기기" held both).
				hidden, err := databaseViewsHidingProperties(txApp, pageID, sourceID, pair.Parent, pair.Children)
				return contentGraphMutationPreparation{Operations: append(operations, hidden...)}, err
			},
			AfterApply: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
				return name(txApp)
			},
		})
	return err
}

// dropDatabaseSubitemPair deletes the Parent item and Sub-items of a data
// source whose sub-items were turned off with their properties removed,
// through the deletion lifecycle as any property is deleted, and stops
// naming them; turned off keeping them, they stay as the relations they are.
func dropDatabaseSubitemPair(app core.App, actorID, pageID, sourceID string) error {
	pair := struct {
		Parent   string `db:"parent"`
		Children string `db:"children"`
		Turn     int64  `db:"turn"`
	}{}
	err := app.DB().NewQuery(`
		SELECT json_extract([[subitems_json]], '$.parentPropertyId') AS [[parent]],
			json_extract([[subitems_json]], '$.subitemsPropertyId') AS [[children]], [[authority_revision]] AS [[turn]]
		FROM {{leafen_database_task_features}}
		WHERE [[data_source_id]] = {:source} AND [[page_id]] = {:page}
			AND COALESCE(json_extract([[subitems_json]], '$.enabled'), 0) = 0
			AND json_extract([[subitems_json]], '$.propertyDisposition') = 'remove'
			AND json_type([[subitems_json]], '$.parentPropertyId') = 'text'
	`).Bind(dbx.Params{"source": sourceID, "page": pageID}).One(&pair)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_database_task_features}}
		SET [[subitems_json]] = json_remove([[subitems_json]], '$.parentPropertyId', '$.subitemsPropertyId')
		WHERE [[data_source_id]] = {:source}
	`).Bind(dbx.Params{"source": sourceID}).Execute(); err != nil {
		return err
	}
	return removeDatabasePropertyPair(app, actorID, pageID, "subitem-pair-remove", pair.Turn, pair.Parent, pair.Children)
}

// removeDatabasePropertyPair deletes a pair of properties a task feature was
// turned off with through the deletion lifecycle, once for each turning off:
// turn is the revision of the settings that turned it off.
func removeDatabasePropertyPair(app core.App, actorID, pageID, kind string, turn int64, propertyIDs ...string) error {
	for _, propertyID := range propertyIDs {
		requestID := kind + ":" + propertyID + ":" + strconv.FormatInt(turn, 10)
		_, err := admitDatabaseMutationDeletion(app, actorID, pageID, databaseMutationInput{
			RequestID: requestID, MutationID: requestID, TargetKind: databaseMutationTargetProperty, TargetID: propertyID,
		})
		// Deleting one of the pair deletes the other with it.
		if err != nil && !errors.Is(err, errDatabaseMutationNotFound) && !errors.Is(err, errDatabaseMutationConflict) {
			return err
		}
	}
	// The deletions run now, not when the server next starts.
	notifyDatabaseMutationDeletionAfterCommit(app)
	if runner, ok := app.Store().Get(durableJobRunnerStoreKey).(*durableJobRunner); ok {
		runner.taskPairsRequested.Store(true)
		runner.notify()
	}
	return nil
}

// databasePairRemovalsRunning reports whether the deletion of a pair of
// properties a task feature was turned off with is still running.
func databasePairRemovalsRunning(ctx context.Context, app core.App) (bool, error) {
	var running int
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM (SELECT 1 FROM {{leafen_database_mutation_jobs}}
			WHERE [[status]] = 'pending' AND [[target_kind]] = 'property'
				AND ([[request_id]] LIKE 'subitem-pair-remove:%' OR [[request_id]] LIKE 'dependency-pair-remove:%') LIMIT 1)
	`).WithContext(ctx).Row(&running)
	return running > 0, err
}

// databaseIsTaskDatabase reports whether a data source is a task database.
func databaseIsTaskDatabase(app core.App, sourceID string) (bool, error) {
	var enabled int
	err := app.DB().NewQuery(`
		SELECT COALESCE(json_extract([[tasks_json]], '$.enabled'), 0) FROM {{leafen_database_task_features}}
		WHERE [[data_source_id]] = {:source}
	`).Bind(dbx.Params{"source": sourceID}).Row(&enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return enabled == 1, err
}

// databaseViewsHidingProperties are the data source's own views on its page
// with the given properties added to what each hides; a linked database's
// views elsewhere and a dashboard's widgets keep theirs.
func databaseViewsHidingProperties(app core.App, pageID, sourceID string, propertyIDs ...string) ([]graphOperation, error) {
	views := []struct {
		ID       string `db:"id"`
		Type     string `db:"type"`
		Name     string `db:"name"`
		Position string `db:"position"`
		Config   string `db:"config"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[type]], [[name]], [[position]], [[config]] FROM {{leafen_database_views}}
		WHERE [[data_source_id]] = {:source} AND [[page_id]] = {:page} AND [[container_id]] = ''
		ORDER BY [[position]], [[id]]
	`).Bind(dbx.Params{"source": sourceID, "page": pageID}).All(&views); err != nil {
		return nil, err
	}
	operations := make([]graphOperation, 0, len(views))
	for _, view := range views {
		if dashboardWidgetOwner([]byte(view.Config)) != "" {
			continue
		}
		config := map[string]any{}
		if strings.TrimSpace(view.Config) != "" {
			if err := json.Unmarshal([]byte(view.Config), &config); err != nil {
				return nil, err
			}
		}
		hidden := []string{}
		if list, ok := config["hiddenProperties"].([]any); ok {
			for _, item := range list {
				if id, ok := item.(string); ok {
					hidden = append(hidden, id)
				}
			}
		}
		for _, id := range propertyIDs {
			if !slices.Contains(hidden, id) {
				hidden = append(hidden, id)
			}
		}
		config["hiddenProperties"] = hidden
		encoded, err := json.Marshal(config)
		if err != nil {
			return nil, err
		}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
			ID: view.ID, DataSourceID: sourceID, Type: view.Type, Name: view.Name, Position: view.Position, Config: encoded,
		}})
	}
	return operations, nil
}
