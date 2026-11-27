package app

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
)

// The catalog is exported from real native databases. It contains only
// synthetic examples; the declaration chooses features and views.
//
//go:embed database_starter_templates.json
var databaseStarterFiles embed.FS

type databaseStarterFeature struct {
	Key        string   `json:"key"`
	Name       string   `json:"name"`
	DefaultOn  bool     `json:"defaultOn"`
	Properties []string `json:"properties"`
}
type databaseStarterViewChoice struct {
	Key                string   `json:"key"`
	ID                 string   `json:"id"`
	RetainedProperties []string `json:"retainedProperties"`
}
type databaseStarterTemplate struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	// TaskDatabase is a template that makes a task database, as Notion's
	// task tracker does (seen 2026-11-05).
	TaskDatabase bool `json:"taskDatabase,omitempty"`
	// UntitledName is what its database calls its pages without a title, as
	// Notion's template gives its database an item name (task tracker "신규
	// 작업", projects "신규 프로젝트", docs hub "신규 문서", seen 2026-11-11;
	// brainstorm "신규  아이디어", its item name " 아이디어" written with a
	// space before it, which a title's place shows and a button's folds, seen
	// 2026-11-12).
	UntitledName string                      `json:"untitledName,omitempty"`
	Features     []databaseStarterFeature    `json:"features"`
	ViewChoices  []databaseStarterViewChoice `json:"viewChoices"`
	Blocks       []graphBlock                `json:"blocks"`
	Sources      []graphDataSource           `json:"sources"`
	Properties   []graphProperty             `json:"properties"`
	Views        []graphDatabaseView         `json:"views"`
	Rows         []graphDatabaseRow          `json:"rows"`
	RowBodies    []struct {
		ID       string         `json:"id"`
		RowID    string         `json:"rowId"`
		ParentID string         `json:"parentId"`
		Position string         `json:"position"`
		Type     string         `json:"type"`
		Content  map[string]any `json:"content"`
	} `json:"rowBodies"`
	Templates []archiveTemplateValue `json:"templates"`
	// PageLayout lays its pages out, as Notion's template does (task
	// tracker, seen 2026-11-06); none leaves them as any database's.
	PageLayout json.RawMessage `json:"pageLayout,omitempty"`
}
type databaseStarterCatalog struct {
	Version   int                       `json:"version"`
	Templates []databaseStarterTemplate `json:"templates"`
	// Translations are the words a template is made with in each language;
	// one a language leaves out stays as written, as Korean leaves the task
	// tracker's and projects' "Done" (Notion's Korean starters read "시작 전",
	// "진행 중", "Done", seen 2026-11-12, in the gallery and once made).
	Translations map[string]map[string]string `json:"translations"`
}

func readDatabaseStarterCatalog() (databaseStarterCatalog, error) {
	raw, err := databaseStarterFiles.ReadFile("database_starter_templates.json")
	catalog := databaseStarterCatalog{}
	if err == nil {
		err = json.Unmarshal(raw, &catalog)
	}
	return catalog, err
}

func databaseStarterIdentity(pageID, mutationID, sourceID string) string {
	digest := sha256.Sum256([]byte("leafen:database-starter\x00" + pageID + "\x00" + mutationID + "\x00" + sourceID))
	return "starter-" + hex.EncodeToString(digest[:16])
}

// Remap exact identities and declared display strings. Formula property
// references use the same parser as property renames, never text replacement.
func remapDatabaseStarterValue(value any, ids map[string]string, labels map[string]string) any {
	switch value := value.(type) {
	case string:
		if next, ok := ids[value]; ok {
			return next
		}
		if next, ok := labels[value]; ok {
			return next
		}
		next, _ := rewriteDatabaseFormulaReferences(value, func(name string) (string, bool) {
			next, ok := labels[name]
			return next, ok
		})
		return next
	case []any:
		result := make([]any, 0, len(value))
		for _, item := range value {
			result = append(result, remapDatabaseStarterValue(item, ids, labels))
		}
		return result
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, item := range value {
			if next, ok := ids[key]; ok {
				key = next
			}
			result[key] = remapDatabaseStarterValue(item, ids, labels)
		}
		return result
	default:
		return value
	}
}

func databaseStarterLanguage(catalog databaseStarterCatalog, language string) map[string]string {
	return catalog.Translations[strings.Split(strings.ToLower(language), "-")[0]]
}

// databaseStarterItemNameLabel keys the words an item name reads in a
// language where the same English reads otherwise elsewhere in its template:
// the brainstorm's item name "New idea" is "신규  아이디어" in Korean, its
// status of that name "새 아이디어" (Notion's, seen 2026-11-12).
const databaseStarterItemNameLabel = "item:"

// databaseStarterItemName is what a template's item name reads as in a
// language: its own words, or as any of the template's words are.
func databaseStarterItemName(name string, labels map[string]string) string {
	if next, ok := labels[databaseStarterItemNameLabel+name]; ok {
		return next
	}
	return name
}

// After selected views retain their declared schema dependencies, remove
// optional references to absent properties, including entire filter/sort items.
func pruneDatabaseStarterConfig(value any, removed map[string]bool) any {
	switch value := value.(type) {
	case []any:
		result := []any{}
		for _, item := range value {
			if id, ok := item.(string); ok && removed[id] {
				continue
			}
			if object, ok := item.(map[string]any); ok {
				if id, _ := object["propertyId"].(string); removed[id] {
					continue
				}
				if id, _ := object["id"].(string); removed[id] {
					continue
				}
			}
			result = append(result, pruneDatabaseStarterConfig(item, removed))
		}
		return result
	case map[string]any:
		result := map[string]any{}
		for key, item := range value {
			if removed[key] {
				continue
			}
			if id, ok := item.(string); ok && removed[id] {
				continue
			}
			result[key] = pruneDatabaseStarterConfig(item, removed)
		}
		return result
	default:
		return value
	}
}

// shiftDatabaseStarterDates moves the sample rows' dates so the earliest is
// today, keeping their spacing: a tracker made now shows examples around
// now, not long overdue (2026-10-19: one made in 2026 showed 2025 due dates).
// Today is now's day on its own clock, the person's who starts it: on UTC's,
// one started at 03:00 in Seoul began on yesterday (2026-11-26).
func shiftDatabaseStarterDates(rows []graphDatabaseRow, now time.Time) {
	day := func(text string) (time.Time, bool) {
		if len(text) < 10 {
			return time.Time{}, false
		}
		parsed, err := time.Parse(time.DateOnly, text[:10])
		return parsed, err == nil
	}
	var earliest time.Time
	for _, row := range rows {
		for _, cell := range row.Values {
			value := map[string]any{}
			if cell.Type != "date" || json.Unmarshal(cell.Value, &value) != nil {
				continue
			}
			for _, key := range []string{"start", "end"} {
				text, _ := value[key].(string)
				if parsed, ok := day(text); ok && (earliest.IsZero() || parsed.Before(earliest)) {
					earliest = parsed
				}
			}
		}
	}
	if earliest.IsZero() {
		return
	}
	today, _ := time.Parse(time.DateOnly, now.Format(time.DateOnly))
	days := int(today.Sub(earliest).Hours() / 24)
	for _, row := range rows {
		for key, cell := range row.Values {
			value := map[string]any{}
			if cell.Type != "date" || json.Unmarshal(cell.Value, &value) != nil {
				continue
			}
			for _, field := range []string{"start", "end"} {
				text, _ := value[field].(string)
				if parsed, ok := day(text); ok {
					value[field] = parsed.AddDate(0, 0, days).Format(time.DateOnly) + text[10:]
				}
			}
			if encoded, err := json.Marshal(value); err == nil {
				cell.Value = encoded
				row.Values[key] = cell
			}
		}
	}
}
