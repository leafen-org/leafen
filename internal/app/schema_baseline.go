package app

import (
	"bytes"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// The schema baseline is the schema migrations 1 to 831 left on a fresh
// PocketBase database — its objects, collections and rows — taken as one
// step: a new database applies it and records it as migration 831, and the
// migrations after it run one by one on top (user decision 2026-11-13, made
// 2026-11-18). It was recorded at product commit fd31bb2c1, the last whose
// migrations ran one by one, by that commit's `go run
// ./cmd/leafen-fresh-schema`; those migrations are in the history before it.
//
// It stays as it is: a new migration changes nothing here. It changes
// PocketBase's own users table and collection, which it holds as PocketBase
// had them then (replaced); a PocketBase that has them otherwise stops a new
// database's start rather than lose what it changed, and the baseline is
// updated for it, then recorded again with `go run ./cmd/leafen-schema-baseline
// -write` (schema_baseline_check.go). An installation keeps its own auth
// secrets: a collection the baseline changes takes its fields, indexes and
// rules, never its options.

const (
	schemaBaselineFormat  = 2
	schemaBaselineVersion = 831
	// schemaBaselineChecksum names the migrations the baseline holds: the
	// digest of versions 1 to 831 with their names and checksums, as their
	// schema snapshot recorded it.
	schemaBaselineChecksum = "6f3eafc8ec4d23a5d7adfd6a8b5f2a0130d8cbcdb8c9dea9da957676e0fce90d"
)

//go:embed schema_baseline.json
var schemaBaselineJSON []byte

type schemaBaseline struct {
	Format     int    `json:"format"`
	Version    int    `json:"version"`
	Migrations string `json:"migrations"`
	// Replaced is what of PocketBase's own the baseline drops or changes,
	// as PocketBase made it when the baseline was recorded.
	Replaced    []schemaBaselineReplaced `json:"replaced"`
	Drop        []string                 `json:"drop"`
	Create      []string                 `json:"create"`
	Collections []map[string]any         `json:"collections"`
	Rows        []schemaBaselineRow      `json:"rows"`
}

type schemaBaselineReplaced struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// schemaBaselineRow is a row the migrations left; its Now columns held the
// time they ran, and take the time the baseline applies.
type schemaBaselineRow struct {
	Table   string   `json:"table"`
	Columns []string `json:"columns"`
	Values  []any    `json:"values"`
	Now     []string `json:"now,omitempty"`
}

// readSchemaBaseline is the baseline this build holds, read once.
var readSchemaBaseline = sync.OnceValues(func() (schemaBaseline, error) {
	baseline, err := decodeSchemaBaseline(schemaBaselineJSON)
	if err != nil {
		return baseline, fmt.Errorf("schema baseline: %w", err)
	}
	if baseline.Format != schemaBaselineFormat || baseline.Version != schemaBaselineVersion ||
		baseline.Migrations != schemaBaselineChecksum {
		return baseline, errors.New("schema baseline: schema_baseline.json is not the baseline this build names")
	}
	return baseline, nil
})

func decodeSchemaBaseline(source []byte) (schemaBaseline, error) {
	baseline := schemaBaseline{}
	decoder := json.NewDecoder(bytes.NewReader(source))
	decoder.UseNumber()
	if err := decoder.Decode(&baseline); err != nil {
		return baseline, err
	}
	for _, row := range baseline.Collections {
		for column, value := range row {
			row[column] = schemaBaselineValue(value)
		}
	}
	for _, row := range baseline.Rows {
		for index, value := range row.Values {
			row.Values[index] = schemaBaselineValue(value)
		}
	}
	return baseline, nil
}

// schemaBaselineValue reads a JSON number back as SQLite's integer or real.
func schemaBaselineValue(value any) any {
	number, ok := value.(json.Number)
	if !ok {
		return value
	}
	if integer, err := number.Int64(); err == nil {
		return integer
	}
	real, _ := number.Float64()
	return real
}

// schemaBaselineInstallationOwn are the values a migration made at random
// for each installation — the server's own number, its data's epoch — which
// a database made from the baseline takes anew (2026-10-18: every new server
// had the same data epoch, so a device couldn't tell a server set up again
// at its address from the one it knew).
var schemaBaselineInstallationOwn = []struct {
	table   string
	columns []string
	renew   func(core.App) error
}{
	// The alias is picked from the number (server_name.go).
	{"leafen_server_identity", []string{"server_id", "created_at", "alias"}, func(app core.App) error {
		if err := renewServerID(app); err != nil {
			return err
		}
		return renewServerAlias(app)
	}},
	{"leafen_data_epoch", []string{"epoch", "changed_at"}, func(app core.App) error {
		_, err := renewDataEpoch(app, time.Now())
		return err
	}},
}

// applySchemaBaseline gives a new database the baseline's schema. It runs
// only on a database PocketBase just made, with no product tables and no
// people, which runProductMigrations sees to; it refuses one whose own
// PocketBase tables aren't those the baseline was recorded over.
func applySchemaBaseline(txApp core.App) error {
	baseline, err := readSchemaBaseline()
	if err != nil {
		return err
	}
	var users int
	if err := txApp.DB().NewQuery(`SELECT COUNT(*) FROM {{users}}`).Row(&users); err != nil {
		return err
	}
	if users > 0 {
		return errors.New("schema baseline: the database already holds accounts")
	}
	if err := checkSchemaBaselineReplaced(txApp, baseline); err != nil {
		return err
	}
	for _, statement := range slices.Concat(baseline.Drop, baseline.Create) {
		if _, err := txApp.DB().NewQuery(statement).Execute(); err != nil {
			return fmt.Errorf("schema baseline: %s: %w", strings.SplitN(statement, "\n", 2)[0], err)
		}
	}
	stamp := types.NowDateTime().String()
	for _, row := range baseline.Collections {
		id, _ := row["id"].(string)
		var existing int
		if err := txApp.DB().NewQuery(`SELECT COUNT(*) FROM {{_collections}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": id}).Row(&existing); err != nil {
			return err
		}
		params := dbx.Params{}
		for column, value := range row {
			params[column] = value
		}
		params["updated"] = stamp
		if existing > 0 {
			delete(params, "id")
			delete(params, "options")
			delete(params, "created")
			_, err = txApp.DB().Update("_collections", params, dbx.HashExp{"id": id}).Execute()
		} else {
			params["created"] = stamp
			_, err = txApp.DB().Insert("_collections", params).Execute()
		}
		if err != nil {
			return fmt.Errorf("schema baseline collection %s: %w", id, err)
		}
	}
	now := time.Now().UnixMilli()
	for _, row := range baseline.Rows {
		params := dbx.Params{}
		for index, column := range row.Columns {
			params[column] = row.Values[index]
		}
		for _, column := range row.Now {
			params[column] = now
		}
		if _, err := txApp.DB().Insert(row.Table, params).Execute(); err != nil {
			return fmt.Errorf("schema baseline row %s: %w", row.Table, err)
		}
	}
	// What the migrations made at random for each installation is a new
	// server's own, not the baseline's.
	for _, own := range schemaBaselineInstallationOwn {
		held := slices.ContainsFunc(baseline.Rows, func(row schemaBaselineRow) bool { return row.Table == own.table })
		if !held {
			continue
		}
		if err := own.renew(txApp); err != nil {
			return fmt.Errorf("schema baseline: a new %s: %w", own.table, err)
		}
	}
	if err := txApp.ReloadCachedCollections(); err != nil {
		return err
	}
	return checkSchemaBaselineApplied(txApp, baseline)
}

// schemaBaselineReplacedNow reads what of PocketBase's own the baseline
// replaces, as the database holds it: every object on a table it drops, and
// every collection of PocketBase's that it changes, without the
// collection's options (its auth secrets) and times.
func schemaBaselineReplacedNow(app core.App, baseline schemaBaseline) ([]schemaBaselineReplaced, error) {
	replaced := []schemaBaselineReplaced{}
	for _, statement := range baseline.Drop {
		fields := strings.Fields(statement)
		table := strings.Trim(fields[len(fields)-1], "\"`[]")
		objects := []struct {
			Type string `db:"type"`
			Name string `db:"name"`
			SQL  string `db:"sql"`
		}{}
		if err := app.DB().NewQuery(`
			SELECT [[type]], [[name]], COALESCE([[sql]], '') AS [[sql]] FROM [[sqlite_master]]
			WHERE [[tbl_name]] = {:table} ORDER BY [[type]], [[name]]
		`).Bind(dbx.Params{"table": table}).All(&objects); err != nil {
			return nil, err
		}
		lines := []string{}
		for _, object := range objects {
			lines = append(lines, object.Type+"\t"+object.Name+"\t"+object.SQL)
		}
		replaced = append(replaced, schemaBaselineReplaced{Key: "objects on " + table, Value: strings.Join(lines, "\n")})
	}
	for _, row := range baseline.Collections {
		id, _ := row["id"].(string)
		var held struct {
			System  bool   `db:"system"`
			Type    string `db:"type"`
			Name    string `db:"name"`
			Fields  string `db:"fields"`
			Indexes string `db:"indexes"`
			Rules   string `db:"rules"`
		}
		err := app.DB().NewQuery(`
			SELECT [[system]], [[type]], [[name]], [[fields]], [[indexes]],
				COALESCE([[listRule]], '') || '|' || COALESCE([[viewRule]], '') || '|' || COALESCE([[createRule]], '') || '|' ||
				COALESCE([[updateRule]], '') || '|' || COALESCE([[deleteRule]], '') AS [[rules]]
			FROM {{_collections}} WHERE [[id]] = {:id}
		`).Bind(dbx.Params{"id": id}).One(&held)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(held)
		if err != nil {
			return nil, err
		}
		replaced = append(replaced, schemaBaselineReplaced{Key: "collection " + id, Value: string(encoded)})
	}
	return replaced, nil
}

// checkSchemaBaselineReplaced refuses a database whose own PocketBase tables
// and collections differ from those the baseline was recorded over: the
// baseline would drop what PocketBase added to them.
func checkSchemaBaselineReplaced(app core.App, baseline schemaBaseline) error {
	now, err := schemaBaselineReplacedNow(app, baseline)
	if err != nil {
		return err
	}
	recorded := map[string]string{}
	for _, entry := range baseline.Replaced {
		recorded[entry.Key] = entry.Value
	}
	for _, entry := range now {
		if value, ok := recorded[entry.Key]; !ok || value != entry.Value {
			return fmt.Errorf("schema baseline: this PocketBase made %s otherwise than the PocketBase the baseline was recorded over; "+
				"update the baseline for it and record it again (go run ./cmd/leafen-schema-baseline -write)", entry.Key)
		}
		delete(recorded, entry.Key)
	}
	for key := range recorded {
		return fmt.Errorf("schema baseline: this PocketBase has no %s, which the baseline was recorded over", key)
	}
	return nil
}

// checkSchemaBaselineApplied compares a database just given the baseline
// with the baseline: every object it creates as it is written, every
// collection it holds as it is, and its rows.
func checkSchemaBaselineApplied(app core.App, baseline schemaBaseline) error {
	objects := []string{}
	if err := app.DB().NewQuery(`SELECT COALESCE([[sql]], '') FROM [[sqlite_master]]`).Column(&objects); err != nil {
		return err
	}
	held := map[string]bool{}
	for _, statement := range objects {
		held[statement] = true
	}
	for _, statement := range baseline.Create {
		if !held[statement] {
			return fmt.Errorf("schema baseline: %s is not as the baseline writes it", strings.SplitN(statement, "\n", 2)[0])
		}
	}
	for _, row := range baseline.Collections {
		id, _ := row["id"].(string)
		collection, err := app.FindCollectionByNameOrId(id)
		if err != nil {
			return fmt.Errorf("schema baseline collection %s: %w", id, err)
		}
		if collection.Name != row["name"] {
			return fmt.Errorf("schema baseline collection %s is named %s", id, collection.Name)
		}
	}
	counts := map[string]int{}
	for _, row := range baseline.Rows {
		counts[row.Table]++
	}
	for table, want := range counts {
		var count int
		if err := app.DB().NewQuery("SELECT COUNT(*) FROM " + quoteSchemaBaselineName(table)).Row(&count); err != nil {
			return err
		}
		if count != want {
			return fmt.Errorf("schema baseline: table %s holds %d rows, not %d", table, count, want)
		}
	}
	return nil
}

func quoteSchemaBaselineName(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
