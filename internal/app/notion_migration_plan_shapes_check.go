package app

import "strings"

// The checks of a plan's splits and objects. A split
// (notion_migration_split.go): a database that comes on its own sends the
// rows whose property holds one of the values to another database that
// comes on its own; the property is an
// option, a checkbox or text, the values name its options ("true" or
// "false" for a checkbox), and the columns the rows bring agree with the
// target's of their name. An object property (notion_migration_objects.go)
// is checked by checkObject, a gathered relation
// (notion_migration_roles.go) by checkRole.

var notionMigrationSplitTypes = map[string]bool{
	graphValueTypeSelect: true, graphValueTypeStatus: true, graphValueTypeMultiSelect: true,
	graphValueTypeCheckbox: true, graphValueTypeTitle: true, graphValueTypeRichText: true,
}

func (c *notionMigrationPlanChecker) checkSplit(key string, database notionMigrationPlanDatabase, planned map[string]notionMigrationPlanDatabase) {
	if len(database.Split) == 0 {
		return
	}
	if database.Action != "import" {
		c.problem(notionMigrationProblemSplitInvalid, database.SourceID, "", "action")
		return
	}
	properties := map[string]notionMigrationProperty{}
	for _, property := range c.databases[key].Properties {
		properties[notionMigrationPropertyKey(property.ID)] = property
	}
	for _, rule := range database.Split {
		into := notionMigrationSourceKey(rule.Into)
		if target, ok := planned[into]; !ok || into == key || target.Action != "import" {
			c.problem(notionMigrationProblemSplitInvalid, database.SourceID, "", rule.Into)
			continue
		}
		property, ok := properties[notionMigrationPropertyKey(rule.Property)]
		if !ok || !notionMigrationSplitTypes[property.LeafenType] {
			c.problem(notionMigrationProblemSplitInvalid, database.SourceID, rule.Property, "property")
			continue
		}
		if len(rule.Values) == 0 {
			c.problem(notionMigrationProblemSplitInvalid, database.SourceID, rule.Property, "values")
			continue
		}
		known := map[string]bool{}
		switch {
		case notionMigrationOptionTypes[property.LeafenType]:
			for _, option := range property.Options {
				known[strings.ToLower(strings.TrimSpace(option))] = true
			}
		case property.LeafenType == graphValueTypeCheckbox:
			known["true"], known["false"] = true, true
		}
		for _, value := range rule.Values {
			if folded := strings.ToLower(strings.TrimSpace(value)); folded == "" || len(known) > 0 && !known[folded] {
				c.problem(notionMigrationProblemSplitInvalid, database.SourceID, rule.Property, "value:"+value)
			}
		}
	}
}

// checkSplitColumns finds the columns a split's rows bring that differ from
// the target's of their name — its own, a merged database's or another
// split's — and adds the others to the target's, as the run makes one column
// of a name whichever comes first (notionMergeAlongside; 2026-11-24: two
// splits into one target each brought a column of a name, one a text and
// one a number, and the run made both). A relation gathered into another
// has that one's column (notion_migration_roles.go); objects are a relation
// to theirs.
func (c *notionMigrationPlanChecker) checkSplitColumns(
	source notionMigrationDatabase, database notionMigrationPlanDatabase, columns map[string]notionMigrationTargetColumn,
) {
	properties := map[string]notionMigrationProperty{}
	for _, property := range source.Properties {
		properties[notionMigrationPropertyKey(property.ID)] = property
	}
	for _, entry := range database.Properties {
		property, known := properties[notionMigrationPropertyKey(entry.SourceID)]
		if !known || entry.Action != "map" && entry.Action != "preserve" && entry.Action != "object" || strings.TrimSpace(entry.Into) != "" {
			continue
		}
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			name = property.Name
		}
		kind, from := strings.TrimSpace(entry.Type), property.LeafenType
		switch entry.Action {
		case "preserve":
			kind = graphValueTypeRichText
		case "object":
			kind, from = graphValueTypeRelation, graphValueTypeRelation
		}
		if kind == "" {
			kind = property.LeafenType
		}
		if kind == graphValueTypeTitle {
			continue
		}
		target := ""
		if property.LeafenType == graphValueTypeRelation || entry.Action == "object" {
			target = notionMigrationSourceKey(property.RelationDatabaseID)
			if entry.Action == "object" {
				target = notionMigrationSourceKey(entry.Database)
			}
			if into, merged := c.merges[target]; merged {
				target = into
			}
		}
		folded := strings.ToLower(name)
		if column, ok := columns[folded]; ok {
			if column.conflicts(name, kind, from, target) {
				c.problem(notionMigrationProblemMergeTypeConflict, database.SourceID, entry.SourceID, name)
			}
			continue
		}
		columns[folded] = notionMigrationTargetColumn{Type: kind + "|" + from + "|" + target, SourceID: database.SourceID}
	}
}

// notionMigrationObjectTypes are the property types whose values name
// objects: text, options and people.
var notionMigrationObjectTypes = map[string]bool{
	graphValueTypeRichText: true, graphValueTypeSelect: true, graphValueTypeStatus: true,
	graphValueTypeMultiSelect: true, graphValueTypePerson: true,
}

// checkObject checks a property the plan turns into objects: a text, an
// option or people, into a database that comes (or merges into one that
// does).
func (c *notionMigrationPlanChecker) checkObject(
	database notionMigrationPlanDatabase, property notionMigrationProperty, entry notionMigrationPlanProperty,
) bool {
	if !notionMigrationObjectTypes[property.LeafenType] {
		c.problem(notionMigrationProblemObjectInvalid, database.SourceID, entry.SourceID, "type")
		return false
	}
	into := notionMigrationSourceKey(entry.Database)
	if target, merged := c.merges[into]; merged {
		into = target
	}
	if target, ok := c.planned[into]; !ok || target.Action != "import" {
		c.problem(notionMigrationProblemObjectInvalid, database.SourceID, entry.SourceID, entry.Database)
		return false
	}
	return true
}

// checkRole checks a relation the plan gathers into another: that one a
// relation of the same database the plan maps, gathered into none, relating
// to the same database once merged.
func (c *notionMigrationPlanChecker) checkRole(
	database notionMigrationPlanDatabase, properties map[string]notionMigrationProperty,
	property notionMigrationProperty, entry notionMigrationPlanProperty,
) {
	role := notionMigrationPropertyKey(strings.TrimSpace(entry.Into))
	var planned *notionMigrationPlanProperty
	for index := range database.Properties {
		if notionMigrationPropertyKey(database.Properties[index].SourceID) == role {
			planned = &database.Properties[index]
		}
	}
	main, known := properties[role]
	resolve := func(id string) string {
		key := notionMigrationSourceKey(id)
		if into, merged := c.merges[key]; merged {
			return into
		}
		return key
	}
	switch {
	case property.LeafenType != graphValueTypeRelation:
		c.problem(notionMigrationProblemRoleInvalid, database.SourceID, entry.SourceID, "type")
	case !known || planned == nil || role == notionMigrationPropertyKey(entry.SourceID) || planned.Action != "map" ||
		strings.TrimSpace(planned.Into) != "" || main.LeafenType != graphValueTypeRelation:
		c.problem(notionMigrationProblemRoleInvalid, database.SourceID, entry.SourceID, entry.Into)
	case resolve(property.RelationDatabaseID) != resolve(main.RelationDatabaseID):
		c.problem(notionMigrationProblemRoleInvalid, database.SourceID, entry.SourceID, "target")
	}
}
