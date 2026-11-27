package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page a relation gives knows its data source ("block@<source>"), and its
// prop() reads that source's property, as Notion's check reads it (seen
// 2026-11-21 through its API and editor, synthetic databases under
// 한지테스트페이지): through a relation to the formula's own source,
// prop("Rel").first().prop("Text") * 2 is math on text and number and
// prop("Rel").first().prop("Nope") names Nope as a property not found; a
// formula a request writes can't name a property of another source's page —
// each such name is not found, as Notion's API refuses it, while a stored
// formula (one its editor saved) is of what it gives there. A value that
// isn't a page reading a property is refused as such where its name is
// valid — its own source's, a list of pages its pages' source's —, and a
// page's prop() with other than one written name as a function not found.

// notionFormulaPages gives the type of a page's property in a formula, by the
// page's source and the property's name, and whether that source has it.
type notionFormulaPages func(source, name string) (string, bool)

// notionFormulaRelated gives another data source's properties.
type notionFormulaRelated func(source string) []notionFormulaCheckProperty

const notionFormulaPagePrefix = "block@"

// notionFormulaPage is the type of a page of source ("" where not known).
func notionFormulaPage(source string) string {
	if source == "" {
		return "block"
	}
	return notionFormulaPagePrefix + source
}

// notionFormulaPageSource is a page type's source, and whether it is a page's.
func notionFormulaPageSource(kind string) (string, bool) {
	if kind == "block" {
		return "", true
	}
	if strings.HasPrefix(kind, notionFormulaPagePrefix) {
		return kind[len(notionFormulaPagePrefix):], true
	}
	return "", false
}

// notionFormulaRelationTarget is the data source a relation's pages are of.
func notionFormulaRelationTarget(config string) string {
	settings := map[string]any{}
	_ = json.Unmarshal([]byte(config), &settings)
	target, _ := settings["relationDatabaseId"].(string)
	return target
}

// pageProperty checks a page's prop(name) — values its page and the name —
// and gives the property's type.
func (checker *notionFormulaChecker) pageProperty(values []*databaseFormulaNode, scope map[string]string) string {
	page := checker.check(values[0], scope)
	if checker.lost && len(values) == 2 && values[1].kind == databaseFormulaNodeString {
		return checker.named(values[1].text, "", false)
	}
	if len(values) != 2 || values[1].kind != databaseFormulaNodeString {
		for _, value := range values[1:] {
			checker.check(value, scope)
		}
		checker.problem("Cannot find function prop().")
		return ""
	}
	name := values[1].text
	source, isPage := notionFormulaPageSource(page)
	if listed, listOfPages := notionFormulaPageSource(notionFormulaItem(page)); listOfPages && !isPage {
		source, page = listed, "array"
	}
	switch {
	case page == "" || isPage && source == "" || checker.pages == nil:
		return ""
	case notionFormulaIsList(page) && source == "" || !isPage && !notionFormulaIsList(page):
		// What isn't a page reads its name in the formula's own source first
		// ("a".prop("Text2") names Text2 as not found there).
		if _, exists := checker.properties[name]; !exists {
			checker.named(name, "", false)
		} else {
			checker.problem("Properties can only be read from a page.")
		}
		return ""
	}
	kind, valid := checker.pages(source, name)
	kind = checker.named(name, kind, valid)
	switch {
	case !valid || checker.lost:
	case !isPage:
		checker.problem("Properties can only be read from a page.")
		return ""
	}
	return kind
}

// requestPages are the pages a formula a request writes reads: its own
// source's, by the types properties gives; another source's name none.
func notionFormulaRequestPages(own string, properties map[string]string) notionFormulaPages {
	return func(source, name string) (string, bool) {
		if source != own {
			return "", false
		}
		kind, exists := properties[name]
		return kind, exists
	}
}

// withSources lets the kinds read the pages of their own source (own) and of
// others (related), each source's kinds worked out once.
func (kinds *notionFormulaPropertyKinds) withSources(own string, related notionFormulaRelated) *notionFormulaPropertyKinds {
	kinds.own, kinds.related = own, related
	kinds.sources = map[string]*notionFormulaPropertyKinds{own: kinds}
	return kinds
}

// storedPage is a page's property as a stored formula reads it: of its own
// source or another, as that source has it.
func (kinds *notionFormulaPropertyKinds) storedPage(source, name string) (string, bool) {
	other, known := kinds.sources[source]
	if !known {
		if kinds.related == nil || source == "" {
			return "", true
		}
		other = newNotionFormulaPropertyKinds(kinds.related(source))
		other.own, other.related, other.sources = source, kinds.related, kinds.sources
		kinds.sources[source] = other
	}
	index, found := other.byName[name]
	if !found {
		return "", false
	}
	return other.of(index), true
}

// notionFormulaPropertyKind is a property's type in a formula; a relation's
// pages are of its target.
func notionFormulaPropertyKind(nativeType, config string) string {
	if nativeType == graphValueTypeRelation {
		return notionFormulaListOf(notionFormulaPage(notionFormulaRelationTarget(config)))
	}
	return notionFormulaPropertyType(nativeType)
}

// notionFormulaRelatedLoader reads another data source's properties.
func notionFormulaRelatedLoader(app core.App) notionFormulaRelated {
	return func(source string) []notionFormulaCheckProperty {
		rows := []struct {
			ID     string `db:"id"`
			Name   string `db:"name"`
			Type   string `db:"type"`
			Config string `db:"config"`
		}{}
		if err := app.DB().NewQuery(`
			SELECT [[id]], [[name]], [[type]], [[config]] FROM {{leafen_database_properties}}
			WHERE [[data_source_id]] = {:source} AND ` + databasePropertyNotTrashedSQL("[[id]]") + `
			ORDER BY [[position]], [[id]]
		`).Bind(dbx.Params{"source": source}).All(&rows); err != nil {
			return nil
		}
		properties := make([]notionFormulaCheckProperty, 0, len(rows))
		for _, row := range rows {
			properties = append(properties, notionFormulaCheckProperty{ID: row.ID, Name: row.Name, Type: row.Type, Config: row.Config})
		}
		return properties
	}
}

// A property a formula names that isn't found leaves every property it names
// after it not found, a page's or its own, as Notion's check reads them (seen
// 2026-11-21 through its API: prop("Nope") + prop("Text") * 2 names both Nope
// and Text as not found, as does a chain after a page's —
// prop("Rel2").first().prop("Back").first().prop("Num") names Back and Num);
// its other problems are still named, and a refusal names the first three
// (notionFormulaProblemsSaid).

// named is the type of a property the formula names, kind where it exists
// and none was lost before, noting it not found otherwise.
func (checker *notionFormulaChecker) named(name, kind string, exists bool) string {
	if exists && !checker.lost {
		return kind
	}
	checker.lost = true
	checker.problem("There's no property called " + name + ".")
	return ""
}

// notionFormulaProblemsShown is how many problems a refusal names, as
// Notion's: "a" * 1 + foo + now() * 2 + bar names the math, foo and the date's
// math, not bar (seen 2026-11-21).
const notionFormulaProblemsShown = 3

// notionFormulaProblemsSaid are the problems a refusal names, in order.
func notionFormulaProblemsSaid(problems []string) string {
	return strings.Join(problems[:min(len(problems), notionFormulaProblemsShown)], " ")
}
