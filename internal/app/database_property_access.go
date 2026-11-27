package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What a database property shows a reader under its access rule, as
// Notion's "속성 사용 권한" decides it (its help "Database property access",
// read 2026-10-12; schema_baseline.json). Someone with full
// access to the database always edits every property. For anyone else the
// widest of the exceptions that name them — themselves, a group of theirs,
// their app account, or a person property that names them in that row —
// applies, or else the property's level for everyone; "inherit" follows
// their access to the database. Access to the page caps it: who may only
// view the database views at most. A property no level shows is gone; one
// shown without its values shows "no access" in its cells.

const (
	propertyAccessInherit      = "inherit"
	propertyAccessEdit         = "edit"
	propertyAccessEditValues   = "edit_values"
	propertyAccessView         = "view"
	propertyAccessViewProperty = "view_property"
	propertyAccessNone         = "none"

	propertyAccessPrincipalUser        = "user"
	propertyAccessPrincipalGroup       = "group"
	propertyAccessPrincipalProperty    = "person_property"
	propertyAccessPrincipalIntegration = "integration"
	// The person property for "페이지 생성자": whoever made the row.
	propertyAccessPageCreator = "page_creator"
)

var propertyAccessRank = map[string]int{
	propertyAccessNone: 0, propertyAccessViewProperty: 1, propertyAccessView: 2, propertyAccessEditValues: 3, propertyAccessEdit: 4,
}

type storedDatabasePropertyAccessRule struct {
	DataSourceID string `db:"data_source_id" json:"-"`
	PropertyID   string `db:"property_id" json:"propertyId"`
	DefaultLevel string `db:"default_level" json:"defaultLevel"`
}

type storedDatabasePropertyAccessException struct {
	DataSourceID  string `db:"data_source_id" json:"-"`
	PropertyID    string `db:"property_id" json:"-"`
	PrincipalType string `db:"principal_type" json:"principalType"`
	PrincipalID   string `db:"principal_id" json:"principalId"`
	Level         string `db:"level" json:"level"`
}

type databasePropertyAccessRule struct {
	storedDatabasePropertyAccessRule
	Exceptions []storedDatabasePropertyAccessException `json:"exceptions"`
}

// databasePropertyAccessRow is what a rule reads of a row: who made it and
// the people its person properties name.
type databasePropertyAccessRow struct {
	ID        string
	CreatedBy string
	People    func(propertyID string) []string
}

// databasePropertyAccessReader applies the data sources' rules for one
// reader; nil where none of the data sources has a rule.
type databasePropertyAccessReader struct {
	app      core.App
	readerID string
	groups   map[string]bool
	rules    map[string]map[string]*databasePropertyAccessRule
	pages    map[string]string
	roles    map[string]pageAccessRole
	// reads and derived: what formulas and rollups read, and those that
	// read a restricted property (database_property_access_computed.go).
	reads   map[string]map[string][]databasePropertyAccessRead
	derived map[string]map[string]bool
	// rows: the reader's role in the data sources' unlinked items
	// (database_row_unlinked_access.go), which stands for the database's.
	rows *databaseRowAccess
}

// loadDatabasePropertyAccessRules reads the data sources' rules.
func loadDatabasePropertyAccessRules(app core.App, dataSourceIDs []string) (map[string]map[string]*databasePropertyAccessRule, error) {
	rules := map[string]map[string]*databasePropertyAccessRule{}
	if len(dataSourceIDs) == 0 {
		return rules, nil
	}
	encoded, err := json.Marshal(dataSourceIDs)
	if err != nil {
		return nil, err
	}
	stored := []storedDatabasePropertyAccessRule{}
	if err := app.DB().NewQuery(`
		SELECT [[data_source_id]], [[property_id]], [[default_level]] FROM {{leafen_database_property_access}}
		WHERE [[data_source_id]] IN (SELECT [[value]] FROM json_each({:ids}))
		ORDER BY [[data_source_id]], [[property_id]]
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&stored); err != nil || len(stored) == 0 {
		return rules, err
	}
	for _, rule := range stored {
		if rules[rule.DataSourceID] == nil {
			rules[rule.DataSourceID] = map[string]*databasePropertyAccessRule{}
		}
		rules[rule.DataSourceID][rule.PropertyID] = &databasePropertyAccessRule{storedDatabasePropertyAccessRule: rule,
			Exceptions: []storedDatabasePropertyAccessException{}}
	}
	exceptions := []storedDatabasePropertyAccessException{}
	if err := app.DB().NewQuery(`
		SELECT [[data_source_id]], [[property_id]], [[principal_type]], [[principal_id]], [[level]]
		FROM {{leafen_database_property_access_exceptions}}
		WHERE [[data_source_id]] IN (SELECT [[value]] FROM json_each({:ids}))
		ORDER BY [[data_source_id]], [[property_id]], [[principal_type]], [[principal_id]]
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&exceptions); err != nil {
		return nil, err
	}
	for _, exception := range exceptions {
		if rule := rules[exception.DataSourceID][exception.PropertyID]; rule != nil {
			rule.Exceptions = append(rule.Exceptions, exception)
		}
	}
	return rules, nil
}

// newDatabasePropertyAccessReader reads the rules of the data sources for
// one reader ("" for a visitor through a public link, who views); nil when
// none of them has a rule.
func newDatabasePropertyAccessReader(app core.App, readerID string, dataSourceIDs []string) (*databasePropertyAccessReader, error) {
	return newDatabasePropertyAccessReaderRows(app, readerID, dataSourceIDs, loadDatabaseRowAccess)
}

// newDatabasePropertyAccessReaderRows reads them with the reader's role in
// their items as rows reads it (database_row_window_access.go).
func newDatabasePropertyAccessReaderRows(app core.App, readerID string, dataSourceIDs []string,
	rows func(core.App, string, []string) (*databaseRowAccess, error),
) (*databasePropertyAccessReader, error) {
	sources, err := loadDatabasePropertyAccessSources(app, dataSourceIDs)
	if err != nil {
		return nil, err
	}
	rules, err := loadDatabasePropertyAccessRules(app, sources)
	if err != nil || len(rules) == 0 {
		return nil, err
	}
	reads, err := loadDatabasePropertyAccessReads(app, sources)
	if err != nil {
		return nil, err
	}
	reader, err := newDatabasePropertyAccessReaderWith(app, readerID, rules)
	if err != nil {
		return nil, err
	}
	reader.setReads(reads)
	if reader.rows, err = rows(app, readerID, sources); err != nil {
		return nil, err
	}
	return reader, nil
}

// newDatabasePropertyAccessReaderWith applies the given rules for one reader.
func newDatabasePropertyAccessReaderWith(app core.App, readerID string, rules map[string]map[string]*databasePropertyAccessRule) (*databasePropertyAccessReader, error) {
	reader := &databasePropertyAccessReader{app: app, readerID: readerID, groups: map[string]bool{}, rules: rules,
		pages: map[string]string{}, roles: map[string]pageAccessRole{}}
	if readerID != "" {
		groups := []string{}
		if err := app.DB().NewQuery(`
			SELECT [[member]].[[group_id]] FROM {{leafen_workspace_permission_group_members}} [[member]]
			JOIN {{leafen_workspace_permission_groups}} [[group]] ON [[group]].[[id]] = [[member]].[[group_id]]
			WHERE [[member]].[[user_id]] = {:readerId} AND [[group]].[[deleted_at]] IS NULL
		`).Bind(dbx.Params{"readerId": readerID}).Column(&groups); err != nil {
			return nil, err
		}
		for _, group := range groups {
			reader.groups[group] = true
		}
	}
	return reader, nil
}

// restricted is whether the data source's property has a rule, or reads one that has.
func (reader *databasePropertyAccessReader) restricted(dataSourceID, propertyID string) bool {
	return reader != nil && (reader.rules[dataSourceID][propertyID] != nil || reader.derived[dataSourceID][propertyID])
}

// role is the reader's access to the data source's database, or to the row
// where the database itself isn't open to them (a row shared on its own) or
// the row is unlinked from it.
func (reader *databasePropertyAccessReader) role(dataSourceID, rowID string) (pageAccessRole, error) {
	if reader.readerID == "" {
		return pageAccessRoleView, nil
	}
	if role, unlinked := reader.rows.role(rowID); unlinked {
		return role, nil
	}
	key := dataSourceID
	if role, known := reader.roles[key]; known && (role > pageAccessRoleNone || rowID == "") {
		return role, nil
	}
	pageID, known := reader.pages[dataSourceID]
	if !known {
		if err := reader.app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": dataSourceID}).Row(&pageID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return pageAccessRoleNone, err
		}
		reader.pages[dataSourceID] = pageID
	}
	role, err := reader.accessRole(pageID)
	if err != nil {
		return pageAccessRoleNone, err
	}
	reader.roles[key] = role
	if role == pageAccessRoleNone && rowID != "" {
		rowKey := dataSourceID + "\x00" + rowID
		if rowRole, known := reader.roles[rowKey]; known {
			return rowRole, nil
		}
		if role, err = reader.accessRole(rowID); err != nil {
			return pageAccessRoleNone, err
		}
		reader.roles[rowKey] = role
	}
	return role, nil
}

func (reader *databasePropertyAccessReader) accessRole(pageID string) (pageAccessRole, error) {
	if pageID == "" {
		return pageAccessRoleNone, nil
	}
	authorization, err := authorizePageAccessRole(reader.app, reader.readerID, pageID, pageAccessRoleView)
	if isPermissionDenied(err) {
		return pageAccessRoleNone, nil
	}
	return authorization.AccessRole, err
}

// level is what the property shows the reader in the row (nil row: in the
// database as a whole, which names nobody through a person property).
func (reader *databasePropertyAccessReader) level(dataSourceID, propertyID string, row *databasePropertyAccessRow) (string, error) {
	level, err := reader.ownLevel(dataSourceID, propertyID, row)
	if err != nil || !propertyAccessShowsValues(level) {
		return level, err
	}
	hidden, err := reader.readsHidden(dataSourceID, propertyID, row)
	if err != nil {
		return "", err
	}
	if hidden {
		return propertyAccessViewProperty, nil
	}
	return level, nil
}

func (reader *databasePropertyAccessReader) ownLevel(dataSourceID, propertyID string, row *databasePropertyAccessRow) (string, error) {
	rule := reader.rules[dataSourceID][propertyID]
	if rule == nil && !reader.derived[dataSourceID][propertyID] {
		return propertyAccessEdit, nil
	}
	if rule == nil {
		rule = &databasePropertyAccessRule{storedDatabasePropertyAccessRule: storedDatabasePropertyAccessRule{DefaultLevel: propertyAccessInherit}}
	}
	rowID := ""
	if row != nil {
		rowID = row.ID
	}
	role, err := reader.role(dataSourceID, rowID)
	if err != nil || role == pageAccessRoleNone {
		return propertyAccessNone, err
	}
	if role >= pageAccessRoleFullAccess {
		return propertyAccessEdit, nil
	}
	best := -1
	for _, exception := range rule.Exceptions {
		if reader.names(exception, row) && propertyAccessRank[exception.Level] > best {
			best = propertyAccessRank[exception.Level]
		}
	}
	level := rule.DefaultLevel
	if best >= 0 {
		for name, rank := range propertyAccessRank {
			if rank == best {
				level = name
			}
		}
	}
	if level == propertyAccessInherit {
		level = propertyAccessView
		if role >= pageAccessRoleEdit {
			level = propertyAccessEdit
		}
	}
	// Who may only view or comment on the database changes nothing in it.
	if role < pageAccessRoleEdit && propertyAccessRank[level] > propertyAccessRank[propertyAccessView] {
		level = propertyAccessView
	}
	return level, nil
}

// names is whether an exception names the reader (in the row, for a person property).
func (reader *databasePropertyAccessReader) names(exception storedDatabasePropertyAccessException, row *databasePropertyAccessRow) bool {
	if reader.readerID == "" {
		return false
	}
	switch exception.PrincipalType {
	case propertyAccessPrincipalUser, propertyAccessPrincipalIntegration:
		return exception.PrincipalID == reader.readerID
	case propertyAccessPrincipalGroup:
		return reader.groups[exception.PrincipalID]
	case propertyAccessPrincipalProperty:
		if row == nil {
			return false
		}
		if exception.PrincipalID == propertyAccessPageCreator {
			return row.CreatedBy == reader.readerID
		}
		if row.People == nil {
			return false
		}
		for _, person := range row.People(exception.PrincipalID) {
			if person == reader.readerID {
				return true
			}
		}
	}
	return false
}

// propertyShown is whether the property's column shows at all: some row may
// show it, through a person property that may name the reader.
func (reader *databasePropertyAccessReader) propertyShown(dataSourceID, propertyID string) (bool, error) {
	level, err := reader.level(dataSourceID, propertyID, nil)
	if err != nil || level != propertyAccessNone {
		return err == nil, err
	}
	role, err := reader.role(dataSourceID, "")
	if err != nil || role == pageAccessRoleNone {
		return false, err
	}
	if rule := reader.rules[dataSourceID][propertyID]; rule != nil {
		for _, exception := range rule.Exceptions {
			if exception.PrincipalType == propertyAccessPrincipalProperty && exception.Level != propertyAccessNone {
				return true, nil
			}
		}
	}
	return false, nil
}

func propertyAccessShowsValues(level string) bool {
	return propertyAccessRank[level] >= propertyAccessRank[propertyAccessView]
}

func propertyAccessEditsValues(level string) bool {
	return propertyAccessRank[level] >= propertyAccessRank[propertyAccessEditValues]
}

// peopleInValue reads the people a person value names: their IDs.
func peopleInValue(raw []byte) []string {
	people := []string{}
	if err := json.Unmarshal(raw, &people); err == nil {
		return people
	}
	objects := []struct {
		ID string `json:"id"`
	}{}
	if err := json.Unmarshal(raw, &objects); err == nil {
		for _, object := range objects {
			people = append(people, object.ID)
		}
	}
	return people
}

// databasePropertyAccessUnrestrictable is whether a property can take no
// rule, as Notion's: the title, an ID, who made or last edited a row and
// when, and the Parent item and Sub-items.
func databasePropertyAccessUnrestrictable(propertyType, propertyID string) bool {
	switch propertyType {
	case "title", "unique_id", "created_by", "created_time", "last_edited_by", "last_edited_time":
		return true
	}
	return propertyType == "relation" && strings.HasPrefix(propertyID, "subitems-")
}
