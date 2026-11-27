package app

import (
	"fmt"
	"strings"
)

// The plan's side of a formula or rollup kept beside its target's of the
// same name (notion_migration_merge_computed.go): by the scan, as the import
// matches them, whether a merged database's property stands for its
// target's column (notionMergeAliasedEntry), and the agenda's item for each
// formula or rollup of a merged database, or of the rows a split sends to
// another, that comes beside the target's of its name, or another merged or
// split database's, for computing otherwise, so the person hears it before
// they approve and the report says it again. The candidates are the run's —
// the target's own columns, then those of each other database merged into
// it or splitting rows into it — judged by the run's rule
// (notionMergeStandsFor); a scan from before the scan read rollups compares
// rollups by name alone.

// notionMigrationAgendaComputedBeside is the agenda's item for a formula or
// rollup kept beside its target's: Detail is the name of its column there.
const notionMigrationAgendaComputedBeside = "computed_kept_beside"

// notionMergePlanColumns compares, by the scan, the columns a plan's merges
// and splits bring into their targets.
type notionMergePlanColumns struct {
	databases   map[string]notionMigrationDatabase
	planned     map[string]notionMigrationPlanDatabase
	merges      map[string]string
	splits      map[string][]notionMigrationPlanSplit
	definitions map[string]string
}

func newNotionMergePlanColumns(plan notionMigrationPlan, databases map[string]notionMigrationDatabase) *notionMergePlanColumns {
	c := &notionMergePlanColumns{
		databases: databases, planned: map[string]notionMigrationPlanDatabase{},
		merges: map[string]string{}, splits: map[string][]notionMigrationPlanSplit{}, definitions: map[string]string{},
	}
	for _, database := range plan.Databases {
		key := notionMigrationSourceKey(database.SourceID)
		c.planned[key] = database
		if database.Action == "merge" {
			c.merges[key] = notionMigrationSourceKey(database.Into)
		}
		if database.Action == "import" && len(database.Split) > 0 {
			c.splits[key] = database.Split
		}
	}
	return c
}

// property is a database's scanned property, by its ID, with its plan entry.
func (c *notionMergePlanColumns) property(database, propertyID string) (notionMigrationProperty, notionMigrationPlanProperty, bool) {
	for _, property := range c.databases[database].Properties {
		if notionMigrationPropertyKey(property.ID) != notionMigrationPropertyKey(propertyID) {
			continue
		}
		for _, entry := range c.planned[database].Properties {
			if notionMigrationPropertyKey(entry.SourceID) == notionMigrationPropertyKey(propertyID) {
				return property, entry, true
			}
		}
		return property, notionMigrationPlanProperty{SourceID: property.ID, Action: "map"}, true
	}
	return notionMigrationProperty{}, notionMigrationPlanProperty{}, false
}

// column is a property as the plan leaves it, as mergeColumn makes it of
// the staged property.
func (c *notionMergePlanColumns) column(property notionMigrationProperty, entry notionMigrationPlanProperty) notionMergeColumn {
	column := notionMergeColumn{
		Name: strings.TrimSpace(entry.Name), Type: strings.TrimSpace(entry.Type), Source: property.LeafenType,
		Kept: entry.Action == "map" || entry.Action == "preserve",
	}
	if column.Name == "" {
		column.Name = property.Name
	}
	if entry.Action == "preserve" {
		column.Type = graphValueTypeRichText
	}
	if column.Type == "" {
		column.Type = property.LeafenType
	}
	if property.LeafenType == graphValueTypeRelation {
		column.Target = notionMigrationSourceKey(property.RelationDatabaseID)
		if into, found := c.merges[column.Target]; found {
			column.Target = into
		}
	}
	return column
}

// nameIn is the name of the column a property's values have in the database
// into (nameIn of the staged property).
func (c *notionMergePlanColumns) nameIn(database string, property notionMigrationProperty, entry notionMigrationPlanProperty, into string) string {
	if role := strings.TrimSpace(entry.Into); role != "" {
		if read, readEntry, found := c.property(database, role); found {
			property, entry = read, readEntry
		}
	}
	column := c.column(property, entry)
	if !column.Kept {
		return property.Name
	}
	if into == "" || into == database || !workspaceExportComputedType(column.Type) {
		return column.Name
	}
	return c.columnName(database, property, entry, into)
}

// definition is a formula's or rollup's definition as it computes in the
// database into (computedDefinition of the staged property).
func (c *notionMergePlanColumns) definition(database string, property notionMigrationProperty, entry notionMigrationPlanProperty, into string) string {
	key := database + "\x00" + notionMigrationPropertyKey(property.ID) + "\x00" + into
	if definition, found := c.definitions[key]; found {
		return definition
	}
	c.definitions[key] = key
	definition := ""
	switch property.LeafenType {
	case graphValueTypeFormula:
		byName := map[string]notionMigrationProperty{}
		for _, other := range c.databases[database].Properties {
			if _, taken := byName[other.Name]; !taken {
				byName[other.Name] = other
			}
		}
		definition = notionMergeFormulaDefinition(property.Expression, func(name string) (string, bool) {
			read, found := byName[name]
			if !found {
				return "", false
			}
			_, readEntry, _ := c.property(database, read.ID)
			return c.nameIn(database, read, readEntry, into), true
		})
	case graphValueTypeRollup:
		if property.Rollup == nil {
			definition = graphValueTypeRollup
			break
		}
		relationName, related, propertyName := "", "", ""
		if relation, relationEntry, found := c.property(database, property.Rollup.RelationID); found {
			relationName = c.nameIn(database, relation, relationEntry, into)
			related = c.column(relation, relationEntry).Target
			reads := notionMigrationSourceKey(relation.RelationDatabaseID)
			if target, targetEntry, found := c.property(reads, property.Rollup.PropertyID); found {
				propertyName = c.nameIn(reads, target, targetEntry, "")
			}
		}
		definition = notionMergeRollupDefinition(relationName, related, propertyName, property.Rollup.Function)
	}
	c.definitions[key] = definition
	return definition
}

// notionMergePlanCandidate is a scanned property a property may stand for
// in its target, with its plan entry and its database's key.
type notionMergePlanCandidate struct {
	database string
	property notionMigrationProperty
	entry    notionMigrationPlanProperty
}

// candidates are the columns a property of from may stand for in the target
// into, as the run finds them (targetColumnApart): the target's own, then
// each other database's merged into it or splitting rows into it
// (notionMergeAlongside).
func (c *notionMergePlanColumns) candidates(into, from string) []notionMergePlanCandidate {
	list := []notionMergePlanCandidate{}
	add := func(database string) {
		scanned := map[string]notionMigrationProperty{}
		for _, property := range c.databases[database].Properties {
			scanned[notionMigrationPropertyKey(property.ID)] = property
		}
		for _, entry := range c.planned[database].Properties {
			if property, found := scanned[notionMigrationPropertyKey(entry.SourceID)]; found {
				list = append(list, notionMergePlanCandidate{database: database, property: property, entry: entry})
			}
		}
	}
	add(into)
	for _, beside := range notionMergeAlongside(c.merges, c.splits, into, from) {
		add(beside.Key)
	}
	return list
}

// match says whether a property stands for a column of the target into's
// own, and whether a formula or rollup comes apart, beside the candidates of
// its name and type computing otherwise (notionMergeStandsFor).
func (c *notionMergePlanColumns) match(database string, property notionMigrationProperty, entry notionMigrationPlanProperty, into string) (bool, bool) {
	candidates := c.candidates(into, database)
	columns := make([]notionMergeCandidate, len(candidates))
	for index, candidate := range candidates {
		columns[index] = notionMergeCandidate{column: c.column(candidate.property, candidate.entry), own: candidate.database == into}
	}
	standsFor, apart, _ := notionMergeStandsFor(c.column(property, entry), columns, func(index int) (bool, error) {
		mine, theirs := c.definition(database, property, entry, into), candidates[index]
		return mine != "" && mine == c.definition(theirs.database, theirs.property, theirs.entry, into), nil
	})
	return len(standsFor) > 0 && columns[standsFor[0]].own, apart
}

// columnName is the name a property's column has in the target into
// (columnName of the staged property).
func (c *notionMergePlanColumns) columnName(database string, property notionMigrationProperty, entry notionMigrationPlanProperty, into string) string {
	column := c.column(property, entry)
	if _, apart := c.match(database, property, entry, into); !apart {
		return column.Name
	}
	title := strings.TrimSpace(c.planned[database].Title)
	if title == "" {
		title = c.databases[database].Title
	}
	return notionMergeApartName(column.Name, title)
}

// besideItems are the agenda's items for each formula or rollup a merge or a
// split brings beside its target's of the name, or another merged
// database's.
func (c *notionMergePlanColumns) besideItems() []notionMigrationIssue {
	items := []notionMigrationIssue{}
	add := func(database, into string) {
		source := c.databases[database]
		for _, property := range source.Properties {
			if !workspaceExportComputedType(property.LeafenType) {
				continue
			}
			_, entry, _ := c.property(database, property.ID)
			if _, apart := c.match(database, property, entry, into); !apart {
				continue
			}
			items = append(items, notionMigrationIssue{
				Code: notionMigrationAgendaComputedBeside, SourceID: source.ID, SourceTitle: source.Title, PropertyID: property.ID,
				Detail: c.columnName(database, property, entry, into),
			})
		}
	}
	for _, key := range workspaceExportSortedKeys(c.planned) {
		database := c.planned[key]
		if into, merged := c.merges[key]; merged {
			add(key, into)
			continue
		}
		if database.Action != "import" {
			continue
		}
		seen := map[string]bool{}
		for _, rule := range database.Split {
			if into := notionMigrationSourceKey(rule.Into); !seen[into] {
				seen[into] = true
				add(key, into)
			}
		}
	}
	return items
}

// notionMigrationComputedBesideText is the report's line for a formula or
// rollup kept beside the column of its name its target has or another
// merged or split database brings, among the structure's changes: its
// database, its name and its column's.
var notionMigrationComputedBesideText = map[string]string{
	accountLanguageKo: "%s의 '%s' — 대상에 있거나 다른 데이터베이스에서 함께 합쳐 오거나 나눠 오는 같은 이름 열과 계산이 달라 '%s' 열로 따로 옵니다. 그 행들은 Notion에서처럼 계산됩니다",
	accountLanguageEn: "%s’s ‘%s’ computes otherwise than the column of that name its target has or another merged or split database brings, so it comes as ‘%s’ — those rows compute as in Notion",
}

// notionMigrationComputedBesideLines are the report's lines for the
// agenda's formulas and rollups kept beside their target's.
func notionMigrationComputedBesideLines(language string, summary notionMigrationSummary, name func(string) string) []string {
	text := cmpOr(notionMigrationComputedBesideText[language], notionMigrationComputedBesideText[accountLanguageEn])
	lines := []string{}
	for _, issue := range summary.Issues {
		if issue.Code != notionMigrationAgendaComputedBeside {
			continue
		}
		property := issue.PropertyID
		for _, database := range summary.Databases {
			if notionMigrationSourceKey(database.ID) != notionMigrationSourceKey(issue.SourceID) {
				continue
			}
			for _, scanned := range database.Properties {
				if notionMigrationPropertyKey(scanned.ID) == notionMigrationPropertyKey(issue.PropertyID) {
					property = scanned.Name
				}
			}
		}
		lines = append(lines, fmt.Sprintf(text, name(issue.SourceTitle), property, issue.Detail))
	}
	return lines
}
