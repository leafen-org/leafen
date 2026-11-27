package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An app's data takes the names of the people it is for (docs/apps.md
// Data): a setup file's steps are written in one language — its "language"
// — and its "names" say what a written name becomes in each other language.
// What an organization shares is made in its owner's language and a
// person's own data in theirs, as the app's name is (app_platform_names.go);
// a language without names takes the English ones, else the steps as
// written. The app keeps reading and writing its data by the names it wrote:
// GET /app-data answers with them, an option a query's condition names and
// the names a data event waits for are read as written. The people's
// consent lines name the data in each language.

// appDataWords are a setup file's language and its names in other languages.
type appDataWords struct {
	Language string
	Names    map[string]map[string]string
}

// appDataMade is what a workspace's data was made from, kept with it: the
// step as the app wrote it, the language it was made in and the written
// names it took another name for. A step kept before names had neither.
type appDataMade struct {
	Key      string            `json:"key"`
	Tool     string            `json:"tool"`
	Args     json.RawMessage   `json:"args"`
	Language string            `json:"language,omitempty"`
	Names    map[string]string `json:"names,omitempty"`
}

var (
	appDataLanguagePattern = regexp.MustCompile(`^[a-z]{2,3}(-[a-z0-9]{2,8})?$`)
	appDataFormulaProp     = regexp.MustCompile(`prop\("((?:[^"\\]|\\.)*)"\)`)
)

// appDataNamesProblems checks a setup file's names: they say which language
// the steps are written in, give other languages only, and make no two
// written names one.
func appDataNamesProblems(file string, setup appDataSetup) []string {
	problems := []string{}
	written := strings.ToLower(setup.Language)
	if setup.Language != "" && !appDataLanguagePattern.MatchString(written) {
		problems = append(problems, fmt.Sprintf("%s: language is a language tag such as \"en\" or \"ko\"", file))
	}
	if len(setup.Names) > 0 && setup.Language == "" {
		problems = append(problems, fmt.Sprintf("%s: names need the language the steps are written in", file))
	}
	for _, language := range appDataSortedKeys(setup.Names) {
		if lower := strings.ToLower(language); !appDataLanguagePattern.MatchString(lower) || lower == written {
			problems = append(problems, fmt.Sprintf("%s: names %q: a language tag other than the steps' own", file, language))
		}
		made := map[string]string{}
		for _, name := range appDataSortedKeys(setup.Names[language]) {
			as := setup.Names[language][name]
			if strings.TrimSpace(as) == "" {
				problems = append(problems, fmt.Sprintf("%s: names %q: %q becomes no name", file, language, name))
			}
			if other, taken := made[as]; taken {
				problems = append(problems, fmt.Sprintf("%s: names %q: %q and %q both become %q", file, language, other, name, as))
			}
			made[as] = name
		}
	}
	return problems
}

func appDataSortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// appDataNamesIn is what a step's written names become in a language, and
// the language they are: none in the steps' own, else that language's
// names (or its primary language's), else the English ones.
func appDataNamesIn(words *appDataWords, language string) (map[string]string, string) {
	want := strings.ToLower(strings.TrimSpace(language))
	if words == nil || len(words.Names) == 0 || want == "" {
		return nil, ""
	}
	written := strings.ToLower(words.Language)
	primary := func(tag string) string {
		head, _, _ := strings.Cut(tag, "-")
		return head
	}
	if want == written || primary(want) == primary(written) {
		return nil, ""
	}
	for _, candidate := range []string{want, primary(want), "en"} {
		if primary(candidate) == primary(written) {
			continue
		}
		for tag, names := range words.Names {
			if strings.ToLower(tag) == candidate {
				return names, candidate
			}
		}
	}
	return nil, ""
}

// appDataMakeArgs is a step's arguments with its names in a language, and
// what is kept of it: the step as written, the language and the names that
// changed — an update compares with what the app wrote and changes the data
// by the names it took (app_platform_data_catch_up.go).
func appDataMakeArgs(step appDataStep, language string) (json.RawMessage, string) {
	names, madeIn := appDataNamesIn(step.words, language)
	args, used := appDataNamed(step.Args, names)
	if len(used) == 0 {
		madeIn = ""
	}
	if madeIn == "" && step.words != nil {
		madeIn = strings.ToLower(step.words.Language)
	}
	return args, appDataSpecWith(step, madeIn, used)
}

// appDataSpecWith is the kept record of a step made in a language with names.
func appDataSpecWith(step appDataStep, language string, names map[string]string) string {
	encoded, _ := json.Marshal(appDataMade{Key: step.Key, Tool: step.Tool, Args: step.Args, Language: language, Names: names})
	return string(encoded)
}

// appDataNamed gives a step's arguments other names: every string value and
// every key of a "properties" object that is a written name, and in a
// "schema" its quoted column names, the quoted texts that are a written
// name (an option, a relation or rollup's column, a comment) and the
// columns a formula's prop("…") names. It answers which names changed.
func appDataNamed(args json.RawMessage, names map[string]string) (json.RawMessage, map[string]string) {
	if len(names) == 0 {
		return args, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(args))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return args, nil
	}
	used := map[string]string{}
	rename := func(text string) string {
		if as, found := names[text]; found && as != text {
			used[text] = as
			return as
		}
		return text
	}
	var walk func(value any, key string) any
	walk = func(value any, key string) any {
		switch typed := value.(type) {
		case string:
			if key == "schema" {
				return appDataNamedSchema(typed, rename)
			}
			return rename(typed)
		case []any:
			for index := range typed {
				typed[index] = walk(typed[index], "")
			}
		case map[string]any:
			named := make(map[string]any, len(typed))
			for inner, held := range typed {
				as := inner
				if key == "properties" {
					as = rename(inner)
				}
				named[as] = walk(held, inner)
			}
			return named
		}
		return value
	}
	if value = walk(value, ""); len(used) == 0 {
		return args, nil
	}
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if encoder.Encode(value) != nil {
		return args, nil
	}
	return json.RawMessage(bytes.TrimSpace(encoded.Bytes())), used
}

// appDataNamedSchema renames a CREATE TABLE schema's quoted names.
func appDataNamedSchema(schema string, rename func(string) string) string {
	var named strings.Builder
	for offset := 0; offset < len(schema); {
		switch schema[offset] {
		case '"':
			if name, next, ok := readMCPDatabaseQuotedIdentifier(schema, offset); ok {
				named.WriteString(appDataQuoted(rename(name)))
				offset = next
				continue
			}
		case '\'':
			if text, next, ok := readMCPDatabaseSchemaQuoted(schema, offset); ok {
				as := rename(text)
				if as == text {
					as = appDataFormulaProp.ReplaceAllStringFunc(text, func(prop string) string {
						column := appDataFormulaProp.FindStringSubmatch(prop)[1]
						return `prop("` + strings.ReplaceAll(rename(strings.ReplaceAll(column, `\"`, `"`)), `"`, `\"`) + `")`
					})
				}
				named.WriteString("'" + strings.ReplaceAll(as, "'", "''") + "'")
				offset = next
				continue
			}
		}
		named.WriteByte(schema[offset])
		offset++
	}
	return named.String()
}

// appDataStepNames are what people know a step's data by in each language
// its file names, for the consent and review lines; a file that doesn't
// say its language is read as English, as before.
func appDataStepNames(step appDataStep) map[string]string {
	name := appDataStepName(step)
	if step.words == nil || step.words.Language == "" {
		return map[string]string{"en": name}
	}
	names := map[string]string{strings.ToLower(step.words.Language): name}
	for language, table := range step.words.Names {
		args, _ := appDataNamed(step.Args, table)
		names[strings.ToLower(language)] = appDataStepName(appDataStep{Key: step.Key, Tool: step.Tool, Args: args})
	}
	return names
}

// appDataChangesIn names an update's data changes in the reviewer's
// language, as the steps' names make them: a step and the columns it gains
// or changes by the new version's names, what goes by the installed one's.
func appDataChangesIn(changes []appDataChangeWire, before map[string]appDataStep, steps []appDataStep, language string) []appDataChangeWire {
	after := map[string]appDataStep{}
	for _, step := range steps {
		after[step.Key] = step
	}
	tableOf := func(step appDataStep) map[string]string {
		names, _ := appDataNamesIn(step.words, language)
		return names
	}
	rename := func(names map[string]string, list []string) {
		for index, name := range list {
			list[index] = appDataWrittenName(names, name)
		}
	}
	for index := range changes {
		change := &changes[index]
		next, old := tableOf(after[change.Key]), tableOf(before[change.Key])
		named := next
		if _, kept := after[change.Key]; !kept {
			named = old
		}
		head, more, counted := strings.Cut(change.Name, " +")
		if counted {
			change.Name = appDataWrittenName(named, head) + " +" + more
		} else {
			change.Name = appDataWrittenName(named, change.Name)
		}
		rename(next, change.Added)
		rename(next, change.Converted)
		rename(old, change.Removed)
	}
	return changes
}

// appDataWritten is what the names a step's data took were written as.
func appDataWritten(spec string) map[string]string {
	made := appDataMade{}
	if json.Unmarshal([]byte(spec), &made) != nil || len(made.Names) == 0 {
		return nil
	}
	written := make(map[string]string, len(made.Names))
	for name, as := range made.Names {
		written[as] = name
	}
	return written
}

func appDataWrittenName(written map[string]string, name string) string {
	if as, found := written[name]; found {
		return as
	}
	return name
}

// appDataWrittenProperties gives a database's columns and their options the
// names the app wrote.
func appDataWrittenProperties(properties []appDataPropertyWire, written map[string]string) {
	if len(written) == 0 {
		return
	}
	for index := range properties {
		properties[index].Name = appDataWrittenName(written, properties[index].Name)
		appDataWrittenOptions(properties[index].Config, written)
	}
}

// appDataWrittenOptions renames a column's options by a table of names.
func appDataWrittenOptions(config map[string]any, names map[string]string) {
	options, _ := config["options"].([]any)
	for _, option := range options {
		if fields, ok := option.(map[string]any); ok {
			if name, ok := fields["name"].(string); ok {
				fields["name"] = appDataWrittenName(names, name)
			}
		}
	}
}

// appDataWrittenConfig is a column's settings with its options renamed by
// a table of names: written ones for the app to read, made ones for it to write.
func appDataWrittenConfig(config string, names map[string]string) string {
	if len(names) == 0 || config == "" {
		return config
	}
	fields := map[string]any{}
	if json.Unmarshal([]byte(config), &fields) != nil {
		return config
	}
	appDataWrittenOptions(fields, names)
	encoded, err := json.Marshal(fields)
	if err != nil {
		return config
	}
	return string(encoded)
}

// appGraphWriteInMadeNames reads an app's change to a column of its own
// data by the names it wrote: the column and its options keep the names
// they were made with (an app writes back what GET /app-data gave it — the
// Approvals app's people columns took their written names back, 2026-11-15).
func appGraphWriteInMadeNames(app core.App, held any, input *graphMutationInput) {
	authority, ok := held.(userKeyAuthority)
	if !ok || authority.AppInstallID == "" || authority.TenancyKey == "" {
		return
	}
	made := map[string]map[string]string{}
	for _, operation := range input.Operations {
		property := operation.Property
		if property == nil || property.DataSourceID == "" {
			continue
		}
		names, read := made[property.DataSourceID]
		if !read {
			spec, _ := appDataSourceSpec(app, authority.AppInstallID, authority.TenancyKey, "", property.DataSourceID)
			kept := appDataMade{}
			_ = json.Unmarshal([]byte(spec), &kept)
			names, made[property.DataSourceID] = kept.Names, kept.Names
		}
		if len(names) == 0 {
			continue
		}
		property.Name = appDataWrittenName(names, property.Name)
		if len(property.Config) > 0 {
			property.Config = json.RawMessage(appDataWrittenConfig(string(property.Config), names))
		}
	}
}

// appDataSourceSpec is what the app's data source was made from, when it is
// one of the app's in the organization — its data or a person's own — and,
// when a key is given, the one that step made.
func appDataSourceSpec(app core.App, installID, tenancy, key, sourceID string) (string, bool) {
	var spec string
	err := app.DB().NewQuery(`SELECT [[spec]] FROM {{leafen_app_data}} WHERE [[install_id]] = {:install}
			AND [[tenancy_key]] = {:tenancy} AND ({:key} = '' OR [[data_key]] = {:key}) AND [[data_source_id]] = {:source}
		UNION ALL SELECT [[spec]] FROM {{leafen_app_person_data}} WHERE [[install_id]] = {:install}
			AND [[tenancy_key]] = {:tenancy} AND ({:key} = '' OR [[data_key]] = {:key}) AND [[data_source_id]] = {:source}
		LIMIT 1`).Bind(dbx.Params{"install": installID, "tenancy": tenancy, "key": key, "source": sourceID}).Row(&spec)
	return spec, err == nil
}

// appQueryInMadeNames reads an app's query of its own data by the names it
// wrote: an option a condition names (the SDKs send options by name) is the
// one it became. held is the request's authority as the request holds it.
func appQueryInMadeNames(app core.App, held any, input *databaseQueryRequest) {
	authority, ok := held.(userKeyAuthority)
	if !ok || authority.AppInstallID == "" || authority.TenancyKey == "" || input.FilterGroup == nil || len(input.DataSourceID) == 0 {
		return
	}
	spec, known := appDataSourceSpec(app, authority.AppInstallID, authority.TenancyKey, "", input.DataSourceID)
	made := appDataMade{}
	if !known || json.Unmarshal([]byte(spec), &made) != nil || len(made.Names) == 0 {
		return
	}
	rows := []struct {
		ID   string `db:"id"`
		Type string `db:"type"`
	}{}
	if app.DB().NewQuery(`SELECT [[id]], [[type]] FROM {{leafen_database_properties}} WHERE [[data_source_id]] = {:source}`).
		Bind(dbx.Params{"source": input.DataSourceID}).All(&rows) != nil {
		return
	}
	options := map[string]bool{}
	for _, row := range rows {
		options[row.ID] = row.Type == graphValueTypeSelect || row.Type == graphValueTypeMultiSelect || row.Type == graphValueTypeStatus
	}
	group := appQueryGroupInMadeNames(*input.FilterGroup, options, made.Names)
	input.FilterGroup = &group
}

func appQueryGroupInMadeNames(group databaseQueryFilterGroup, options map[string]bool, names map[string]string) databaseQueryFilterGroup {
	filters := make([]databaseQueryFilter, len(group.Filters))
	for index, filter := range group.Filters {
		if options[filter.PropertyID] {
			filter.Value = appDataMadeValue(filter.Value, names)
		}
		filters[index] = filter
	}
	group.Filters = filters
	if group.Groups != nil {
		groups := make([]databaseQueryFilterGroup, len(group.Groups))
		for index, inner := range group.Groups {
			groups[index] = appQueryGroupInMadeNames(inner, options, names)
		}
		group.Groups = groups
	}
	return group
}

// appDataMadeValue is a condition's option name, or names, as made.
func appDataMadeValue(value json.RawMessage, names map[string]string) json.RawMessage {
	var one string
	if json.Unmarshal(value, &one) == nil {
		if as, found := names[one]; found {
			encoded, _ := json.Marshal(as)
			return encoded
		}
		return value
	}
	var many []string
	if json.Unmarshal(value, &many) != nil {
		return value
	}
	for index, name := range many {
		if as, found := names[name]; found {
			many[index] = as
		}
	}
	encoded, _ := json.Marshal(many)
	return encoded
}
