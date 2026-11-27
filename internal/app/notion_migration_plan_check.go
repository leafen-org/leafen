package app

import (
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"
)

// The checks a design-first migration plan must pass before it runs
// (decision "설계 우선 Notion 이사" 4절): every source database planned and
// each of its properties mapped, preserved or reported, each conversion one
// that keeps the values, no two target properties sharing a name, places
// and people that exist, and the counts the plan expects matching the scan.
// Each problem names its reason and source; any problem stops the run. The
// scan's own loss reasons come back as the agenda to talk over.

// notionMigrationTypeConversions are the type changes a run makes that keep
// every value, beside keeping the type: option types move between each
// other and text becomes options. Any stored value but a file may also
// become text. Leafen's property conversion converts the cells
// (database_property_conversion_rules.go); computed values, titles, places
// and relations keep their type.
var notionMigrationTypeConversions = map[string][]string{
	graphValueTypeSelect:   {graphValueTypeMultiSelect, graphValueTypeStatus},
	graphValueTypeStatus:   {graphValueTypeSelect, graphValueTypeMultiSelect},
	graphValueTypeRichText: {graphValueTypeSelect, graphValueTypeMultiSelect, graphValueTypeStatus},
}

func notionMigrationTypeConvertible(from, to string) bool {
	if from == "" || to == "" {
		return false
	}
	if from == to {
		return true
	}
	if _, stored := databasePropertyConversionStoredTypes[from]; !stored || !databasePropertyTypeChangeAllowed(from, to) {
		return false
	}
	if to == graphValueTypeRichText {
		return from != graphValueTypeFiles
	}
	for _, allowed := range notionMigrationTypeConversions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

var notionMigrationOptionTypes = map[string]bool{
	graphValueTypeSelect: true, graphValueTypeMultiSelect: true, graphValueTypeStatus: true,
}

// notionMigrationSourceKey compares Notion IDs written with or without dashes.
func notionMigrationSourceKey(id string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(id), "-", ""))
}

func notionMigrationPropertyKey(id string) string {
	if decoded, err := url.PathUnescape(id); err == nil {
		return decoded
	}
	return id
}

type notionMigrationPlanChecker struct {
	check     notionMigrationPlanCheck
	pages     map[string]notionMigrationPage
	databases map[string]notionMigrationDatabase
	// Each page's and database's place in the plan: its parent and whether
	// the plan names it, and whether it comes over.
	parents  map[string]string
	explicit map[string]bool
	skipped  map[string]bool
	included map[string]bool
	// The databases merged into another, by key: the key of each target.
	merges map[string]string
	// The plan's databases, by key.
	planned map[string]notionMigrationPlanDatabase
	// The migration; err is a read that failed.
	app    core.App
	stored storedNotionMigration
	err    error
}

// problem records one of the plan's problems, naming the page or database
// it is about. There are no more than the plan's own entries give.
func (c *notionMigrationPlanChecker) problem(code, sourceID, propertyID, detail string) {
	problem := notionMigrationIssue{Code: code, SourceID: sourceID, PropertyID: propertyID, Detail: detail}
	key := notionMigrationSourceKey(sourceID)
	if database, ok := c.databases[key]; ok {
		problem.SourceTitle = database.Title
	} else if page, ok := c.pages[key]; ok {
		problem.SourceTitle = page.Title
	}
	c.check.Problems = append(c.check.Problems, problem)
}

// sourceID gives back the scan's own ID for a source key.
func (c *notionMigrationPlanChecker) sourceID(key string) string {
	if page, ok := c.pages[key]; ok {
		return page.ID
	}
	if database, ok := c.databases[key]; ok {
		return database.ID
	}
	return key
}

func notionMigrationPlanName(value string) (string, bool) {
	name := strings.TrimSpace(value)
	return name, utf8.RuneCountInString(name) <= notionMigrationPlanNameRunes
}

// checkNotionMigrationPlan checks a plan against its migration's scan.
func checkNotionMigrationPlan(
	app core.App, stored storedNotionMigration, summary notionMigrationSummary, plan notionMigrationPlan,
) (notionMigrationPlanCheck, error) {
	c := &notionMigrationPlanChecker{
		check: notionMigrationPlanCheck{Problems: []notionMigrationIssue{}, Agenda: []notionMigrationIssue{}},
		pages: map[string]notionMigrationPage{}, databases: map[string]notionMigrationDatabase{},
		parents: map[string]string{}, explicit: map[string]bool{}, skipped: map[string]bool{}, included: map[string]bool{},
		app: app, stored: stored,
	}
	if plan.Format != notionMigrationPlanFormat {
		c.problem(notionMigrationProblemFormat, "", "", fmt.Sprint(plan.Format))
	}
	if plan.Comments != "import" && plan.Comments != "skip" {
		c.problem(notionMigrationProblemValueInvalid, "", "", "comments")
	}
	if plan.Files != "copy" && plan.Files != "skip" {
		c.problem(notionMigrationProblemValueInvalid, "", "", "files")
	}
	if destination := strings.TrimSpace(plan.DestinationPageID); destination != "" {
		if code, err := notionMigrationDestinationProblem(app, stored.Owner, stored.WorkspaceID, destination); err != nil {
			return notionMigrationPlanCheck{}, err
		} else if code != "" {
			c.problem(code, destination, "", "")
		}
	}
	for _, page := range summary.Pages {
		key := notionMigrationSourceKey(page.ID)
		c.pages[key] = page
		c.parents[key] = notionMigrationSourceKey(page.ParentID)
	}
	for _, database := range summary.Databases {
		key := notionMigrationSourceKey(database.ID)
		c.databases[key] = database
		c.parents[key] = notionMigrationSourceKey(database.ParentID)
	}
	planned := c.planPlaces(plan)
	c.checkPlaces()
	c.settleAgenda(append(notionMigrationScanAgenda(plan, summary.Issues), c.notionMigrationLeftItems(plan, summary)...))
	c.checkProperties(plan, planned)
	if err := c.checkPeople(app, stored.Owner, stored.WorkspaceID, plan.People, summary.People); err != nil {
		return notionMigrationPlanCheck{}, err
	}
	c.countExpected(planned)
	if expect := plan.Expect; expect != nil && *expect != c.check.Expected {
		c.problem(notionMigrationProblemCountMismatch, "", "", fmt.Sprintf("pages %d/%d, databases %d/%d, rows %d/%d",
			expect.Pages, c.check.Expected.Pages, expect.Databases, c.check.Expected.Databases, expect.Rows, c.check.Expected.Rows))
	}
	if c.err != nil {
		return notionMigrationPlanCheck{}, c.err
	}
	c.check.OK = len(c.check.Problems) == 0
	return c.check, nil
}

// settleAgenda keeps the scan's issues, what Notion can't give, that are about
// what the plan brings, to settle with the person: an issue about something
// left out, or under it, is not. Something the scan could not read goes by the
// page that holds it.
func (c *notionMigrationPlanChecker) settleAgenda(issues []notionMigrationIssue) {
	for _, issue := range issues {
		key := notionMigrationSourceKey(issue.SourceID)
		if _, scanned := c.parents[key]; !scanned && issue.ParentID != "" {
			key = notionMigrationSourceKey(issue.ParentID)
		}
		if included, known := c.included[key]; known && !included {
			continue
		}
		if issue.SourceTitle == "" {
			issue.SourceTitle = c.title(notionMigrationSourceKey(issue.SourceID))
		}
		c.check.Agenda = append(c.check.Agenda, issue)
	}
}

// title gives the scan's title of a page or database.
func (c *notionMigrationPlanChecker) title(key string) string {
	if database, ok := c.databases[key]; ok {
		return database.Title
	}
	return c.pages[key].Title
}

// planPlaces reads the plan's pages and databases against the scan, and
// returns its databases by source key.
func (c *notionMigrationPlanChecker) planPlaces(plan notionMigrationPlan) map[string]notionMigrationPlanDatabase {
	for _, page := range plan.Pages {
		key := notionMigrationSourceKey(page.SourceID)
		switch _, known := c.pages[key]; {
		case !known:
			c.problem(notionMigrationProblemSourceUnknown, page.SourceID, "", "page")
			continue
		case c.explicit[key]:
			c.problem(notionMigrationProblemSourceDuplicate, page.SourceID, "", "")
			continue
		}
		c.explicit[key] = true
		if page.Action != "import" && page.Action != "skip" {
			c.problem(notionMigrationProblemValueInvalid, page.SourceID, "", "action")
		}
		if _, ok := notionMigrationPlanName(page.Title); !ok {
			c.problem(notionMigrationProblemValueInvalid, page.SourceID, "", "title")
		}
		c.skipped[key] = page.Action == "skip"
		c.reparent(key, page.SourceID, page.ParentSourceID)
	}
	planned := map[string]notionMigrationPlanDatabase{}
	for _, database := range plan.Databases {
		key := notionMigrationSourceKey(database.SourceID)
		switch _, known := c.databases[key]; {
		case !known:
			c.problem(notionMigrationProblemSourceUnknown, database.SourceID, "", "database")
			continue
		case c.explicit[key]:
			c.problem(notionMigrationProblemSourceDuplicate, database.SourceID, "", "")
			continue
		}
		c.explicit[key] = true
		planned[key] = database
		if database.Action != "import" && database.Action != "skip" && database.Action != "merge" {
			c.problem(notionMigrationProblemValueInvalid, database.SourceID, "", "action")
		}
		if _, ok := notionMigrationPlanName(database.Title); !ok {
			c.problem(notionMigrationProblemValueInvalid, database.SourceID, "", "title")
		}
		c.skipped[key] = database.Action == "skip"
		c.reparent(key, database.SourceID, database.ParentSourceID)
	}
	for _, database := range c.databases {
		if !c.explicit[notionMigrationSourceKey(database.ID)] {
			c.problem(notionMigrationProblemDatabaseUnplanned, database.ID, "", "")
		}
	}
	// A merged database comes over into another that comes on its own; its
	// relations, and those to it, go on pointing at the same rows
	// (notion_migration_merge.go).
	c.merges, c.planned = map[string]string{}, planned
	for key, database := range planned {
		if database.Action != "merge" {
			continue
		}
		into := notionMigrationSourceKey(database.Into)
		if target, ok := planned[into]; !ok || into == key || target.Action != "import" {
			c.problem(notionMigrationProblemMergeInvalid, database.SourceID, "", database.Into)
			continue
		}
		c.merges[key] = into
	}
	// A split sends the rows of a database coming on its own that hold a
	// property's values to another such database (notion_migration_split.go).
	for key, database := range planned {
		c.checkSplit(key, database, planned)
	}
	return planned
}

// reparent moves a page or database under another page of the scan.
func (c *notionMigrationPlanChecker) reparent(key, sourceID, parentSourceID string) {
	if strings.TrimSpace(parentSourceID) == "" {
		return
	}
	parent := notionMigrationSourceKey(parentSourceID)
	if _, ok := c.pages[parent]; !ok || parent == key {
		c.problem(notionMigrationProblemParentInvalid, sourceID, "", parentSourceID)
		return
	}
	c.parents[key] = parent
}

// checkPlaces finds moves that loop and what the plan brings over from
// under a page it leaves out. A page the plan doesn't name follows its
// parent; one it names to bring over must have a parent that comes too.
func (c *notionMigrationPlanChecker) checkPlaces() {
	limit := len(c.parents) + 1
	for key := range c.parents {
		at, steps := c.parents[key], 0
		for at != "" && at != key && steps < limit {
			at, steps = c.parents[at], steps+1
		}
		if at == key {
			c.problem(notionMigrationProblemParentInvalid, c.sourceID(key), "", "loop")
			c.parents[key] = ""
		}
	}
	var comes func(key string, depth int) bool
	comes = func(key string, depth int) bool {
		if included, ok := c.included[key]; ok {
			return included
		}
		included := !c.skipped[key]
		if parent := c.parents[key]; included && parent != "" && depth < limit {
			if _, scanned := c.pages[parent]; scanned && !comes(parent, depth+1) {
				if c.explicit[key] {
					c.problem(notionMigrationProblemParentSkipped, c.sourceID(key), "", c.sourceID(parent))
				}
				included = false
			}
		}
		c.included[key] = included
		return included
	}
	for key := range c.parents {
		comes(key, 0)
	}
}

// notionMigrationTargetColumn is a target's column of a name: its type,
// the type it comes as and the database it relates to.
type notionMigrationTargetColumn struct {
	Type     string
	SourceID string
}

// conflicts says whether a property of a name differs from the target's
// column of that name.
func (column notionMigrationTargetColumn) conflicts(name, kind, source, target string) bool {
	return column.Type != kind+"|"+source+"|"+target
}

func (c *notionMigrationPlanChecker) checkDatabaseProperties(
	source notionMigrationDatabase, database notionMigrationPlanDatabase,
	columns map[string]notionMigrationTargetColumn, merging bool,
) {
	properties := map[string]notionMigrationProperty{}
	for _, property := range source.Properties {
		properties[notionMigrationPropertyKey(property.ID)] = property
	}
	seen, own, titles := map[string]bool{}, map[string]bool{}, 0
	for _, entry := range database.Properties {
		key := notionMigrationPropertyKey(entry.SourceID)
		property, known := properties[key]
		switch {
		case !known:
			c.problem(notionMigrationProblemSourceUnknown, database.SourceID, entry.SourceID, "property")
			continue
		case seen[key]:
			c.problem(notionMigrationProblemSourceDuplicate, database.SourceID, entry.SourceID, "")
			continue
		}
		seen[key] = true
		name, ok := notionMigrationPlanName(entry.Name)
		if !ok {
			c.problem(notionMigrationProblemValueInvalid, database.SourceID, entry.SourceID, "name")
		}
		if name == "" {
			name = property.Name
		}
		kind := strings.TrimSpace(entry.Type)
		switch entry.Action {
		case "map":
			// A relation gathered into another has that one's column (notion_migration_roles.go).
			if strings.TrimSpace(entry.Into) != "" {
				c.checkRole(database, properties, property, entry)
				continue
			}
			if kind == "" {
				kind = property.LeafenType
			}
			if !notionMigrationTypeConvertible(property.LeafenType, kind) {
				c.problem(notionMigrationProblemTypeIncompatible, database.SourceID, entry.SourceID, property.Type+"→"+kind)
			}
			c.checkValues(database.SourceID, property, entry, kind)
		case "preserve":
			// Kept as text, which a file can't be; a formula's or rollup's as
			// Notion shows it (notion_migration_property_choices.go).
			if !notionMigrationPreservable(property.LeafenType) {
				c.problem(notionMigrationProblemTypeIncompatible, database.SourceID, entry.SourceID, property.Type+"→preserve")
			}
			kind = graphValueTypeRichText
		case "report":
			continue
		case "object":
			// A relation to the rows its names become (notion_migration_objects.go).
			if !c.checkObject(database, property, entry) {
				continue
			}
			kind = graphValueTypeRelation
		default:
			c.problem(notionMigrationProblemValueInvalid, database.SourceID, entry.SourceID, "action")
			continue
		}
		if kind == graphValueTypeTitle {
			titles++
			if merging {
				continue
			}
		}
		folded := strings.ToLower(name)
		if own[folded] {
			c.problem(notionMigrationProblemNameConflict, database.SourceID, entry.SourceID, name)
			continue
		}
		own[folded] = true
		// Columns of one name become one: of one type, coming as one type
		// and, for a relation, relating to one database once merged; objects
		// are a relation to theirs.
		source, target := property.LeafenType, ""
		if property.LeafenType == graphValueTypeRelation || entry.Action == "object" {
			target = notionMigrationSourceKey(property.RelationDatabaseID)
			if entry.Action == "object" {
				source, target = graphValueTypeRelation, notionMigrationSourceKey(entry.Database)
			}
			if into, merged := c.merges[target]; merged {
				target = into
			}
		}
		if column, ok := columns[folded]; ok {
			if column.conflicts(name, kind, source, target) {
				c.problem(notionMigrationProblemMergeTypeConflict, database.SourceID, entry.SourceID, name)
			}
			if column.conflicts(name, kind, source, target) {
				continue
			}
		}
		columns[folded] = notionMigrationTargetColumn{Type: kind + "|" + source + "|" + target, SourceID: database.SourceID}
	}
	for key, property := range properties {
		if !seen[key] {
			c.problem(notionMigrationProblemPropertyUnplanned, database.SourceID, property.ID, property.Name)
		}
	}
	if titles != 1 {
		c.problem(notionMigrationProblemTitle, database.SourceID, "", fmt.Sprint(titles))
	}
}

func (c *notionMigrationPlanChecker) checkValues(
	databaseID string, property notionMigrationProperty, entry notionMigrationPlanProperty, kind string,
) {
	if len(entry.Values) == 0 {
		return
	}
	if !notionMigrationOptionTypes[property.LeafenType] || !notionMigrationOptionTypes[kind] {
		c.problem(notionMigrationProblemValueInvalid, databaseID, entry.SourceID, "values")
		return
	}
	options := map[string]bool{}
	for _, option := range property.Options {
		options[option] = true
	}
	// Options given one name become one (notion_migration_values.go).
	for _, value := range entry.Values {
		if !options[value.From] {
			c.problem(notionMigrationProblemOptionUnknown, databaseID, entry.SourceID, value.From)
		}
		if to, ok := notionMigrationPlanName(value.To); !ok || to == "" {
			c.problem(notionMigrationProblemValueInvalid, databaseID, entry.SourceID, "values")
		}
	}
}

// countExpected counts what the plan brings over: its pages and databases
// and the rows of each database that comes, or that merges into one that
// does.
func (c *notionMigrationPlanChecker) countExpected(planned map[string]notionMigrationPlanDatabase) {
	for key := range c.pages {
		if c.included[key] {
			c.check.Expected.Pages++
		}
	}
	for key, database := range planned {
		source := c.databases[key]
		switch database.Action {
		case "import":
			if c.included[key] {
				c.check.Expected.Databases++
				c.check.Expected.Rows += source.Rows
			}
		case "merge":
			into := notionMigrationSourceKey(database.Into)
			if planned[into].Action != "import" {
				continue
			}
			if !c.included[into] {
				// Its rows would go nowhere.
				c.problem(notionMigrationProblemMergeInvalid, database.SourceID, "", database.Into)
				continue
			}
			c.check.Expected.Rows += source.Rows
		}
	}
}
