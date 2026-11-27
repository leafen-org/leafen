package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// Turning dependencies on gives the data source its Blocked by and Blocking,
// as Notion does: Blocked by lists the rows a row waits on and Blocking the
// rows waiting on it, and the source's dependency binding names the two. The
// two are named in the language of the person turning them on. A source with
// no date to shift gets one before them in the same change, Date (날짜),
// which its settings name (completeDatabaseTaskFeatures), as Notion's (seen
// 2026-11-18: a wiki's "모든 페이지" read 페이지 … 최종 편집 일시, 날짜, 선행
// 작업, 후속 작업 after turning them on).

// databaseDependencyMadeDatePrefix starts the id of a date the settings name
// before the change turning dependencies on makes it.
const databaseDependencyMadeDatePrefix = "depends-date-"

//
// Turned off keeping the two and turned on again, the source gets a new
// pair, as Notion's does (seen 2026-10-10): the kept pair stays the
// relations it is, values and all, and the binding moves to the new pair,
// named apart from the kept one ("Blocked by 1"), whose links start empty
// under a data key of their own.

// databaseDependencyPairKept says whether a data source's dependencies are
// off with their pair kept, still bound: turning them on again makes a new
// pair.
func databaseDependencyPairKept(app core.App, pageID, sourceID string) (bool, error) {
	var kept int
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_database_task_features}} AS [[features]]
		WHERE [[features]].[[data_source_id]] = {:source} AND [[features]].[[page_id]] = {:page}
			AND COALESCE(json_extract([[features]].[[dependencies_json]], '$.enabled'), 0) = 0
			AND json_extract([[features]].[[dependencies_json]], '$.propertyDisposition') = 'keep'
			AND EXISTS (SELECT 1 FROM {{leafen_database_dependency_bindings}} WHERE [[data_source_id]] = {:source})
	`).Bind(dbx.Params{"source": sourceID, "page": pageID}).Row(&kept)
	return kept > 0, err
}

// ensureDatabaseDependencyPair gives a data source whose dependencies are on
// its Blocked by and Blocking after its last property, and binds them, in one
// change: when it has no binding, or, replacing a kept pair, when it has.
func (p *pocketBaseYgoPersistence) ensureDatabaseDependencyPair(
	ctx context.Context, actorID, pageID, sourceID string, authorize func(core.App) (pageAuthorization, error), replaceKept bool,
) error {
	// Each turning on makes the pair as its own change, as for sub-items.
	state := struct {
		Turn     int64  `db:"turn"`
		Bound    bool   `db:"bound"`
		Revision int    `db:"revision"`
		Date     string `db:"date"`
	}{}
	if err := p.app.DB().NewQuery(`
		SELECT [[features]].[[authority_revision]] AS [[turn]],
			[[binding]].[[data_source_id]] IS NOT NULL AS [[bound]], COALESCE([[binding]].[[revision]], 0) AS [[revision]],
			COALESCE(json_extract([[features]].[[dependencies_json]], '$.datePropertyId'), '') AS [[date]]
		FROM {{leafen_database_task_features}} AS [[features]]
		LEFT JOIN {{leafen_database_dependency_bindings}} AS [[binding]]
			ON [[binding]].[[data_source_id]] = [[features]].[[data_source_id]]
		WHERE [[features]].[[data_source_id]] = {:source} AND [[features]].[[page_id]] = {:page}
			AND json_extract([[features]].[[dependencies_json]], '$.enabled') = 1
	`).Bind(dbx.Params{"source": sourceID, "page": pageID}).One(&state); errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	if state.Bound && !replaceKept {
		return nil
	}
	// The first pair keeps the names it always had; a later one is named and
	// keyed by its turn — replacing a kept pair, or whenever the source still
	// has a property of the first pair's names (kept when dependencies went
	// off, or still being deleted), so turning them on never takes another
	// pair over, nor the links it drew.
	id := func(part, salt string) string {
		digest := sha256.Sum256([]byte("leafen:dependency-pair\x00" + sourceID + "\x00" + part + salt))
		return "depends-" + part + "-" + hex.EncodeToString(digest[:12])
	}
	var held, dated int
	if err := p.app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_database_properties}} WHERE [[id]] IN ({:blocked}, {:blocking})`).
		Bind(dbx.Params{"blocked": id("blocked-by", ""), "blocking": id("blocking", "")}).Row(&held); err != nil {
		return err
	}
	// The date the settings name before it is made (completeDatabaseTaskFeatures).
	makeDate := strings.HasPrefix(state.Date, databaseDependencyMadeDatePrefix)
	if makeDate {
		if err := p.app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_database_properties}} WHERE [[id]] = {:date}`).
			Bind(dbx.Params{"date": state.Date}).Row(&dated); err != nil {
			return err
		}
		makeDate = dated == 0
	}
	salt, dataKey := "", "dependencies"
	if state.Bound || held > 0 {
		salt = "\x00" + strconv.FormatInt(state.Turn, 10)
		dataKey = "dependencies-" + strconv.FormatInt(state.Turn, 10)
	}
	blockedBy, blocking := id("blocked-by", salt), id("blocking", salt)
	mutationID := "dependency-pair:" + sourceID + ":" + strconv.FormatInt(state.Turn, 10)
	digest := sha256.Sum256([]byte("leafen:dependency-pair\x00" + pageID + "\x00" + mutationID))
	_, err := p.applyContentGraphMutationContext(ctx, actorID, pageID,
		graphMutationInput{MutationID: mutationID},
		contentGraphMutationHooks{
			// Authorized by its caller, it is the job runner's move of a
			// source's dependencies (database_dependency_pair_upkeep.go):
			// Leafen's own upkeep, no one's act.
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
				operations := []graphOperation{}
				names := generatedNamesFor(ctx, txApp, actorID)
				taken := []string{}
				if err := txApp.DB().NewQuery(`
					SELECT [[name]] FROM {{leafen_database_properties}}
					WHERE [[data_source_id]] = {:source} AND ` + databasePropertyNotTrashedSQL("[[id]]"),
				).Bind(dbx.Params{"source": sourceID}).Column(&taken); err != nil {
					return contentGraphMutationPreparation{}, err
				}
				used := map[string]bool{}
				for _, name := range taken {
					used[name] = true
				}
				// A name the source has gets the first free number, as Notion's
				// "Blocked by 1".
				unique := func(base string) string {
					name := base
					for number := 1; used[name]; number++ {
						name = fmt.Sprintf("%s %d", base, number)
					}
					used[name] = true
					return name
				}
				next := func() (string, error) {
					position, err := databasePositionAfter(tail)
					tail = position
					return position, err
				}
				if makeDate {
					position, err := next()
					if err != nil {
						return contentGraphMutationPreparation{}, err
					}
					operations = append(operations, graphOperation{Kind: graphOperationKindUpsertProperty, Property: &graphProperty{
						ID: state.Date, DataSourceID: sourceID, Type: graphValueTypeDate, Name: unique(names.Date),
						Position: position, Config: json.RawMessage(`{}`),
					}})
				}
				first, err := next()
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				second, err := next()
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				for _, property := range []struct{ id, other, name, position string }{
					{blockedBy, blocking, unique(names.BlockedBy), first}, {blocking, blockedBy, unique(names.Blocking), second},
				} {
					config, err := json.Marshal(map[string]any{"relationDatabaseId": sourceID, "relatedPropertyId": property.other})
					if err != nil {
						return contentGraphMutationPreparation{}, err
					}
					operations = append(operations, graphOperation{Kind: graphOperationKindUpsertProperty, Property: &graphProperty{
						ID: property.id, DataSourceID: sourceID, Type: graphValueTypeRelation, Name: property.name,
						Position: property.position, Config: config,
					}})
				}
				operations = append(operations, graphOperation{Kind: graphOperationKindConfigureDependency,
					DependencyBinding: &graphDependencyBinding{
						DataSourceID: sourceID, PredecessorPropertyID: blockedBy, SuccessorPropertyID: blocking,
						DataKey: dataKey, ExpectedRevision: state.Revision,
					}})
				return contentGraphMutationPreparation{Operations: operations}, nil
			},
		})
	return err
}

// dropDatabaseDependencyPair deletes the Blocked by and Blocking of a data
// source whose dependencies were turned off with their properties removed,
// through the deletion lifecycle, which lets the dependency binding and the
// links it drew go with them; turned off keeping them, they stay as the
// relations they are.
func dropDatabaseDependencyPair(app core.App, actorID, pageID, sourceID string) error {
	pair := struct {
		BlockedBy string `db:"blocked_by"`
		Blocking  string `db:"blocking"`
		Turn      int64  `db:"turn"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[binding]].[[predecessor_property_id]] AS [[blocked_by]], [[binding]].[[successor_property_id]] AS [[blocking]],
			[[features]].[[authority_revision]] AS [[turn]]
		FROM {{leafen_database_task_features}} AS [[features]]
		JOIN {{leafen_database_dependency_bindings}} AS [[binding]]
			ON [[binding]].[[data_source_id]] = [[features]].[[data_source_id]] AND [[binding]].[[page_id]] = [[features]].[[page_id]]
		WHERE [[features]].[[data_source_id]] = {:source} AND [[features]].[[page_id]] = {:page}
			AND COALESCE(json_extract([[features]].[[dependencies_json]], '$.enabled'), 0) = 0
			AND json_extract([[features]].[[dependencies_json]], '$.propertyDisposition') = 'remove'
	`).Bind(dbx.Params{"source": sourceID, "page": pageID}).One(&pair)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return removeDatabasePropertyPair(app, actorID, pageID, "dependency-pair-remove", pair.Turn, pair.BlockedBy, pair.Blocking)
}
