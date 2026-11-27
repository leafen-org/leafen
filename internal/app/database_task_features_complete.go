package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// What the task settings' screens never leave out, held to where a
// database's task settings are stored (configureTaskFeatures), whoever sends
// them (2026-11-13: an agent's tasks {"enabled": true} with no status,
// assignee or due date, and dependencies with no date, were kept half-made).
// A feature turned on names properties of this database of the kind each
// role takes, as the screens let a person pick them: the task database its
// status (a select the screen turns into one after, with statusWasSelect),
// person and date; dependencies their date, or their start and end. What a
// screen fills for a person who only turns a feature on, it fills here too:
// dependencies' date shifting ("overlap"), weekends avoided and one date —
// the database's first date property, or, where it has none, one made with
// their pair, named 날짜 (Date), as Notion's (seen 2026-11-18: a database and
// a wiki with no date turned theirs on and got 날짜, 선행 작업 and 후속 작업)
// — and sub-items' title property with
// its toggle shown. Anything else missing is refused with what it needs. A
// feature sent back as it is stored isn't checked again, so settings kept
// from before don't stop a change to another feature.

// databaseTaskFeaturesError is task settings a screen would never send, and
// why.
type databaseTaskFeaturesError struct{ reason string }

func (failure *databaseTaskFeaturesError) Error() string { return failure.reason }

func (failure *databaseTaskFeaturesError) Is(target error) bool {
	return target == errDatabaseToolbarConfigInvalid
}

var databaseDependencyShiftModes = []string{"overlap", "maintain_spacing", "none"}

// completeDatabaseTaskFeatures checks the features turned on and fills what
// their screens fill.
func completeDatabaseTaskFeatures(app core.App, authority databaseToolbarConfigAuthority, features *databaseToolbarTaskFeatureWire) error {
	properties := []struct {
		ID   string `db:"id"`
		Type string `db:"type"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[id]], [[type]] FROM {{leafen_database_properties}}
		WHERE [[data_source_id]] = {:source} AND [[page_id]] = {:page} AND ` + databasePropertyNotTrashedSQL("[[id]]") + `
		ORDER BY [[position]], [[id]]`).Bind(dbx.Params{"source": authority.DataSourceID, "page": authority.PageID}).All(&properties); err != nil {
		return err
	}
	types := map[string]string{}
	first := map[string]string{}
	for _, property := range properties {
		types[property.ID] = property.Type
		if first[property.Type] == "" {
			first[property.Type] = property.ID
		}
	}
	var keptSubitems, keptDependencies, keptTasks sql.NullString
	if err := app.DB().NewQuery(`SELECT [[subitems_json]], [[dependencies_json]], [[tasks_json]] FROM {{leafen_database_task_features}}
		WHERE [[data_source_id]] = {:source}`).Bind(dbx.Params{"source": authority.DataSourceID}).
		Row(&keptSubitems, &keptDependencies, &keptTasks); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	refuse := func(format string, args ...any) error {
		return &databaseTaskFeaturesError{reason: fmt.Sprintf(format, args...)}
	}
	// role checks that settings name a property of one of the kinds.
	role := func(feature string, settings map[string]any, key string, kinds ...string) error {
		id, _ := settings[key].(string)
		if strings.TrimSpace(id) == "" {
			return refuse("%s turned on needs %q, a %s property of this database (make one with a content graph upsertProperty first)",
				feature, key, strings.Join(kinds, " or "))
		}
		kind, found := types[id]
		switch {
		case !found:
			return refuse("%s: %q names %q, which isn't a property of this database", feature, key, id)
		case !slices.Contains(kinds, kind):
			return refuse("%s: %q names a %s property; it takes a %s property", feature, key, kind, strings.Join(kinds, " or "))
		}
		return nil
	}

	if tasks, on, err := databaseTaskFeatureOn(features.Tasks, keptTasks.String); err != nil {
		return refuse("tasks is an object")
	} else if on {
		statusKinds := []string{graphValueTypeStatus}
		if tasks["statusWasSelect"] == true {
			statusKinds = append(statusKinds, graphValueTypeSelect)
		}
		for _, check := range []struct {
			key   string
			kinds []string
		}{{"statusPropertyId", statusKinds}, {"assigneePropertyId", []string{graphValueTypePerson}}, {"dueDatePropertyId", []string{graphValueTypeDate}}} {
			if err := role("tasks", tasks, check.key, check.kinds...); err != nil {
				return err
			}
		}
	}

	if dependencies, on, err := databaseTaskFeatureOn(features.Dependencies, keptDependencies.String); err != nil {
		return refuse("dependencies is an object or null")
	} else if on {
		if _, set := dependencies["shiftMode"]; !set {
			dependencies["shiftMode"] = "overlap"
		}
		if mode, _ := dependencies["shiftMode"].(string); !slices.Contains(databaseDependencyShiftModes, mode) {
			return refuse(`dependencies: "shiftMode" is "overlap", "maintain_spacing" or "none"`)
		}
		if _, set := dependencies["avoidWeekends"]; !set {
			dependencies["avoidWeekends"] = true
		}
		if _, isBool := dependencies["avoidWeekends"].(bool); !isBool {
			return refuse(`dependencies: "avoidWeekends" is true or false`)
		}
		if _, set := dependencies["dateMode"]; !set {
			dependencies["dateMode"] = "single"
		}
		// Any mode but "separate" reads one date, as the dates are shifted.
		if dependencies["dateMode"] == "separate" {
			for _, key := range []string{"startDatePropertyId", "endDatePropertyId"} {
				if err := role("dependencies", dependencies, key, graphValueTypeDate); err != nil {
					return err
				}
			}
		} else if id, _ := dependencies["datePropertyId"].(string); id == "" && first[graphValueTypeDate] == "" {
			// None to shift: the change turning them on makes one with their
			// pair (ensureDatabaseDependencyPair).
			dependencies["datePropertyId"] = databaseDependencyMadeDatePrefix +
				security.RandomStringWithAlphabet(24, "abcdefghijklmnopqrstuvwxyz0123456789")
		} else {
			if id == "" {
				dependencies["datePropertyId"] = first[graphValueTypeDate]
			}
			if err := role("dependencies", dependencies, "datePropertyId", graphValueTypeDate); err != nil {
				return err
			}
		}
		if features.Dependencies, err = json.Marshal(dependencies); err != nil {
			return err
		}
	}

	if subitems, on, err := databaseTaskFeatureOn(features.Subitems, keptSubitems.String); err != nil {
		return refuse("subitems is an object or null")
	} else if on {
		if id, _ := subitems["titlePropertyId"].(string); id == "" {
			subitems["titlePropertyId"] = first[graphValueTypeTitle]
		}
		if err := role("subitems", subitems, "titlePropertyId", graphValueTypeTitle); err != nil {
			return err
		}
		// Turned on, their toggle stands in the table's first column, Notion's
		// "제목에 중첩 토글 표시" off (seen 2026-11-22, three times).
		if _, set := subitems["showToggleOnTitle"]; !set {
			subitems["showToggleOnTitle"] = false
		}
		if features.Subitems, err = json.Marshal(subitems); err != nil {
			return err
		}
	}
	return nil
}

// databaseTaskFeatureOn reads one feature's settings and whether they turn
// it on and so are checked: none and null leave it off, and settings the
// same as kept stay as they are.
func databaseTaskFeatureOn(raw json.RawMessage, kept string) (map[string]any, bool, error) {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return nil, false, nil
	}
	settings := map[string]any{}
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil, false, err
	}
	stored := map[string]any{}
	if json.Unmarshal([]byte(kept), &stored) == nil && reflect.DeepEqual(settings, stored) {
		return settings, false, nil
	}
	return settings, settings["enabled"] == true, nil
}
