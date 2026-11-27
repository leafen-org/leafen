package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// An app brings its data structure as a setup file in data/: steps run in
// order as the app's account when the app's workspace is first made, each
// one Leafen's own MCP creation call — a database from a CREATE TABLE
// schema, a view of it, pages under a parent or as a data source's rows —
// with {{root}} for where the step's data goes and {{key.field}} for what an
// earlier step made. What the manifest's data entries mark locked goes where
// people only view it, what they mark hidden stays the app account's own,
// and the rest goes where the workspace's people edit (docs/app-platform.md 6).

const (
	appDataTable            = "leafen_app_data"
	appDataMaxSteps         = 200
	appDataMaxStepBytes     = 64 << 10
	appDataToolDatabase     = "create-database"
	appDataToolView         = "create-view"
	appDataToolPages        = "create-pages"
	appDataPlaceEdit        = "edit"
	appDataPlaceView        = "view"
	appDataPlaceHidden      = "hidden"
	appDataSetupDone        = "done"
	appDataSetupFailed      = "failed"
	appAuditDataSetupFailed = "data_setup_failed"
	appDataSetupNotNeeded   = "none"
)

var (
	appDataPlaceholder = regexp.MustCompile(`\{\{([a-z][a-zA-Z0-9_-]{0,47}|root)(?:\.(id|data_source_id|view_id))?\}\}`)
	appDataFields      = map[string][]string{
		appDataToolDatabase: {"id", "data_source_id", "view_id"},
		appDataToolView:     {"id"},
		appDataToolPages:    {"id"},
	}
)

// appDataSetup is a setup file: its steps, the language they are written in
// and what their names become in other languages (app_platform_data_names.go).
type appDataSetup struct {
	Language string                       `json:"language,omitempty"`
	Names    map[string]map[string]string `json:"names,omitempty"`
	Steps    []appDataStep                `json:"steps"`
}

type appDataStep struct {
	Key  string          `json:"key"`
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args"`
	// words are its file's language and names.
	words *appDataWords
}

// appDataRow is what a step, a place's root page or a teamspace was made as.
type appDataRow struct {
	Key    string `db:"data_key"`
	Page   string `db:"page_id"`
	Source string `db:"data_source_id"`
	View   string `db:"view_id"`
	Spec   string `db:"spec"`
}

// appDataProblems checks every setup file the manifest's data entries name:
// known tools, keys in their form and order, placeholders naming earlier
// steps' results, and each data entry made by a step of its file.
func appDataProblems(manifest appManifest, files map[string][]byte) []string {
	problems := []string{}
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	byFile := map[string][]string{}
	perPerson := map[string]bool{}
	for _, data := range manifest.Data {
		byFile[data.File] = append(byFile[data.File], data.Key)
		if data.PerPerson && (data.Locked || data.Hidden) {
			add("data %q: a person's own data is theirs, neither locked nor hidden", data.Key)
		}
		if seen, known := perPerson[data.File]; known && seen != data.PerPerson {
			add("%s: its data entries are all perPerson or none", data.File)
		}
		perPerson[data.File] = data.PerPerson
	}
	for file, keys := range byFile {
		setup, err := parseAppDataSetup(files[file])
		if err != nil {
			add("%s", appDataSetupProblem(file, files[file], err))
			continue
		}
		problems = append(problems, appDataNamesProblems(file, setup)...)
		made := map[string]string{}
		for index, step := range setup.Steps {
			label := fmt.Sprintf("%s step %d (%s)", file, index+1, step.Key)
			if !appKeyPattern.MatchString(step.Key) || step.Key == "root" || made[step.Key] != "" ||
				strings.HasPrefix(step.Key, "root-") || strings.HasPrefix(step.Key, "teamspace-") {
				add("%s: key must be unique, in key form and not root or begin root- or teamspace-", label)
			}
			if _, known := appDataFields[step.Tool]; !known {
				add("%s: tool is create-database, create-view or create-pages", label)
			}
			if len(step.Args) == 0 || len(step.Args) > appDataMaxStepBytes || !appJSONObject(step.Args) {
				add("%s: args is a JSON object up to 64 KB", label)
			}
			for _, match := range appDataPlaceholder.FindAllStringSubmatch(string(step.Args), -1) {
				if match[1] == "root" {
					continue
				}
				tool, earlier := made[match[1]]
				if !earlier || match[2] != "" && !appOneOf(match[2], appDataFields[tool]) {
					add("%s: {{%s.%s}} names no earlier result", label, match[1], match[2])
				}
			}
			if bytes.Contains(appDataPlaceholder.ReplaceAll(step.Args, nil), []byte("{{")) {
				add("%s: only {{root}} and {{key.field}} fill values", label)
			}
			if step.Tool == appDataToolView {
				view := struct {
					Database string `json:"database_id"`
					Parent   string `json:"parent_page_id"`
				}{}
				if json.Unmarshal(step.Args, &view) == nil && view.Database != "" && view.Parent != "" {
					add("%s: database_id makes a tab of that database and parent_page_id a linked view on a page, so give one", label)
				}
			}
			made[step.Key] = step.Tool
		}
		for _, key := range keys {
			if made[key] == "" {
				add("%s: no step has the key %q — a data entry's key names the step in its file that makes it", file, key)
			}
		}
	}
	return problems
}

// errAppDataSetupShape is a setup file that is no list of steps at all.
var errAppDataSetupShape = errors.New("isn't a setup file of steps")

func parseAppDataSetup(raw []byte) (appDataSetup, error) {
	setup := appDataSetup{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if len(raw) == 0 {
		return setup, errAppDataSetupShape
	}
	if err := decoder.Decode(&setup); err != nil {
		return setup, &appDataSetupDecodeError{err}
	}
	if len(setup.Steps) == 0 || len(setup.Steps) > appDataMaxSteps {
		return setup, fmt.Errorf("has 1-%d steps", appDataMaxSteps)
	}
	words := &appDataWords{Language: setup.Language, Names: setup.Names}
	for index := range setup.Steps {
		setup.Steps[index].words = words
	}
	return setup, nil
}

type appDataSetupDecodeError struct{ cause error }

func (failure *appDataSetupDecodeError) Error() string { return errAppDataSetupShape.Error() }

// appDataSetupProblem says what is wrong with a setup file: where its JSON
// breaks, or how its steps don't fit.
func appDataSetupProblem(file string, raw []byte, err error) string {
	var decoding *appDataSetupDecodeError
	if errors.As(err, &decoding) {
		return appJSONProblem(file, raw, decoding.cause)
	}
	return fmt.Sprintf("%s: %v", file, err)
}

// appDataPlace says where a step's data goes: with the people who edit,
// where they only view, or the app account's own.
func appDataPlace(manifest appManifest, key string) string {
	for _, data := range manifest.Data {
		if data.Key != key {
			continue
		}
		if data.PerPerson {
			return appDataPlacePerson
		}
		if data.Hidden {
			return appDataPlaceHidden
		}
		if data.Locked {
			return appDataPlaceView
		}
	}
	return appDataPlaceEdit
}

// fillAppDataPlaceholders writes earlier results into a step's arguments.
func fillAppDataPlaceholders(args json.RawMessage, root string, results map[string]map[string]string) json.RawMessage {
	return appDataPlaceholder.ReplaceAllFunc(args, func(match []byte) []byte {
		parts := appDataPlaceholder.FindSubmatch(match)
		name, field := string(parts[1]), string(parts[2])
		if name == "root" {
			return []byte(root)
		}
		if field == "" {
			field = "id"
		}
		return []byte(strings.ReplaceAll(results[name][field], `"`, ""))
	})
}
