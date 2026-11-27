package app

import (
	"database/sql"
	"errors"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
	"github.com/reearth/ygo/crdt"
)

// Reading and writing a property's access rule, which the organization
// module offers (database_property_access.go). Only someone with full
// access to the database reads or changes its rules, as Notion's; a rule
// that keeps the level for everyone at "inherit" with no exceptions is no
// rule. A database takes rules on at most 20 properties, never on its
// title, IDs, who made or last edited a row and when, nor its Parent item
// and Sub-items, and a wiki takes none.

var (
	errDatabasePropertyAccessInvalid  = errors.New("LEAFEN_DATABASE_PROPERTY_ACCESS_INVALID")
	errDatabasePropertyAccessNotFound = errors.New("LEAFEN_DATABASE_PROPERTY_ACCESS_NOT_FOUND")
)

var propertyAccessExceptionLevels = map[string][]string{
	propertyAccessPrincipalUser:        {propertyAccessEdit, propertyAccessEditValues, propertyAccessView, propertyAccessViewProperty, propertyAccessNone},
	propertyAccessPrincipalGroup:       {propertyAccessEdit, propertyAccessEditValues, propertyAccessView, propertyAccessViewProperty, propertyAccessNone},
	propertyAccessPrincipalIntegration: {propertyAccessEdit, propertyAccessView, propertyAccessNone},
	propertyAccessPrincipalProperty:    {propertyAccessEditValues, propertyAccessView, propertyAccessNone},
}

// DatabasePropertyAccessRule is a property's rule as its editor sees it.
// FullAccess names who has full access to its database, whom its fixed first
// exception always lets edit it (database_property_access_holders.go); a
// rule read gives it, one written or previewed leaves it out.
type DatabasePropertyAccessRule struct {
	DataSourceID string                                  `json:"dataSourceId"`
	PropertyID   string                                  `json:"propertyId"`
	DefaultLevel string                                  `json:"defaultLevel"`
	Exceptions   []storedDatabasePropertyAccessException `json:"exceptions"`
	FullAccess   *DatabasePropertyAccessHolders          `json:"fullAccess,omitempty"`
}

type databasePropertyAccessTarget struct {
	OwnerID      string `db:"owner"`
	WorkspaceID  string `db:"workspace_id"`
	PageID       string `db:"page_id"`
	Wiki         bool   `db:"wiki"`
	PropertyType string `db:"type"`
}

// authorizeDatabasePropertyAccessEditor finds the data source's property and
// checks the person has full access to its database.
func authorizeDatabasePropertyAccessEditor(app core.App, actorID, dataSourceID, propertyID string) (databasePropertyAccessTarget, error) {
	target := databasePropertyAccessTarget{}
	err := app.DB().NewQuery(`
		SELECT [[source]].[[owner]], [[source]].[[workspace_id]], [[source]].[[page_id]], [[source]].[[wiki]], [[property]].[[type]]
		FROM {{leafen_data_sources}} [[source]]
		JOIN {{leafen_database_properties}} [[property]]
			ON [[property]].[[data_source_id]] = [[source]].[[id]] AND [[property]].[[id]] = {:propertyId}
		WHERE [[source]].[[id]] = {:dataSourceId}
	`).Bind(dbx.Params{"dataSourceId": dataSourceID, "propertyId": propertyID}).One(&target)
	if errors.Is(err, sql.ErrNoRows) {
		return target, errDatabasePropertyAccessNotFound
	}
	if err != nil {
		return target, err
	}
	if _, err := authorizePageAccessRole(app, actorID, target.PageID, pageAccessRoleFullAccess); isPermissionDenied(err) {
		return target, errDatabasePropertyAccessNotFound
	} else if err != nil {
		return target, err
	}
	return target, nil
}

// loadDatabasePropertyAccessRule reads a property's rule for someone with
// full access to its database, with who has that access; a property without
// one keeps "inherit".
func loadDatabasePropertyAccessRule(app core.App, actorID, dataSourceID, propertyID string) (DatabasePropertyAccessRule, error) {
	rule := DatabasePropertyAccessRule{DataSourceID: dataSourceID, PropertyID: propertyID, DefaultLevel: propertyAccessInherit,
		Exceptions: []storedDatabasePropertyAccessException{}}
	target, err := authorizeDatabasePropertyAccessEditor(app, actorID, dataSourceID, propertyID)
	if err != nil {
		return rule, err
	}
	if rule.FullAccess, err = loadDatabasePropertyAccessHolders(app, actorID, target.PageID); err != nil {
		return rule, err
	}
	rules, err := loadDatabasePropertyAccessRules(app, []string{dataSourceID})
	if err != nil {
		return rule, err
	}
	if stored := rules[dataSourceID][propertyID]; stored != nil {
		rule.DefaultLevel, rule.Exceptions = stored.DefaultLevel, stored.Exceptions
	}
	return rule, nil
}

// setDatabasePropertyAccessRule replaces a property's rule; it reports the
// workspace and whether anything changed.
func setDatabasePropertyAccessRule(app core.App, actorID string, rule DatabasePropertyAccessRule, now int64) (string, bool, error) {
	target, err := authorizeDatabasePropertyAccessEditor(app, actorID, rule.DataSourceID, rule.PropertyID)
	if err != nil {
		return "", false, err
	}
	if err := validateDatabasePropertyAccessRule(app, target, rule); err != nil {
		return "", false, err
	}
	for index := range rule.Exceptions {
		rule.Exceptions[index].DataSourceID, rule.Exceptions[index].PropertyID = rule.DataSourceID, rule.PropertyID
	}
	slices.SortFunc(rule.Exceptions, func(left, right storedDatabasePropertyAccessException) int {
		if left.PrincipalType != right.PrincipalType {
			return strings.Compare(left.PrincipalType, right.PrincipalType)
		}
		return strings.Compare(left.PrincipalID, right.PrincipalID)
	})
	before, err := loadDatabasePropertyAccessRules(app, []string{rule.DataSourceID})
	if err != nil {
		return "", false, err
	}
	empty := rule.DefaultLevel == propertyAccessInherit && len(rule.Exceptions) == 0
	had := before[rule.DataSourceID][rule.PropertyID] != nil
	if stored := before[rule.DataSourceID][rule.PropertyID]; stored == nil && empty {
		return target.WorkspaceID, false, nil
	} else if stored != nil && stored.DefaultLevel == rule.DefaultLevel && slices.Equal(stored.Exceptions, rule.Exceptions) {
		return target.WorkspaceID, false, nil
	}
	if err := writeDatabasePropertyAccessRule(app, target, actorID, rule, empty, now); err != nil {
		return "", false, err
	}
	// Search keeps a property's values only while no rule covers it.
	if had == empty && databaseRowSearchIndexesType(target.PropertyType) {
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_search_dirty}} ([[page_id]], [[workspace_id]], [[version]], [[enqueued_at]])
			SELECT [[row_id]], [[workspace_id]], 1, {:now} FROM {{leafen_database_row_pages}} WHERE [[data_source_id]] = {:dataSourceId}
			ON CONFLICT ([[page_id]]) DO UPDATE SET [[workspace_id]] = excluded.[[workspace_id]],
				[[version]] = [[leafen_search_dirty]].[[version]] + 1, [[enqueued_at]] = excluded.[[enqueued_at]]
		`).Bind(dbx.Params{"dataSourceId": rule.DataSourceID, "now": now}).Execute(); err != nil {
			return "", false, err
		}
	}
	// The database's page, and the pages showing it after it, read it again
	// under the rule as they take up another person's change.
	return target.WorkspaceID, true, recordServerGraphChange(app, target.OwnerID, target.PageID,
		"database-property-access:"+security.RandomString(20), crdt.EncodeStateAsUpdateV1(crdt.New(), nil), graphMutationCounts{})
}

func writeDatabasePropertyAccessRule(app core.App, target databasePropertyAccessTarget, actorID string, rule DatabasePropertyAccessRule, empty bool, now int64) error {
	params := dbx.Params{"dataSourceId": rule.DataSourceID, "propertyId": rule.PropertyID}
	if _, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_database_property_access}} WHERE [[data_source_id]] = {:dataSourceId} AND [[property_id]] = {:propertyId}
	`).Bind(params).Execute(); err != nil || empty {
		return err
	}
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_database_property_access}}
			([[data_source_id]], [[property_id]], [[workspace_id]], [[default_level]], [[updated_by]], [[updated_at]])
		VALUES ({:dataSourceId}, {:propertyId}, {:workspaceId}, {:level}, {:actorId}, {:now})
	`).Bind(dbx.Params{"dataSourceId": rule.DataSourceID, "propertyId": rule.PropertyID, "workspaceId": target.WorkspaceID,
		"level": rule.DefaultLevel, "actorId": actorID, "now": now}).Execute(); err != nil {
		return err
	}
	for _, exception := range rule.Exceptions {
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_database_property_access_exceptions}}
				([[data_source_id]], [[property_id]], [[workspace_id]], [[principal_type]], [[principal_id]], [[level]])
			VALUES ({:dataSourceId}, {:propertyId}, {:workspaceId}, {:type}, {:principal}, {:level})
		`).Bind(dbx.Params{"dataSourceId": rule.DataSourceID, "propertyId": rule.PropertyID, "workspaceId": target.WorkspaceID,
			"type": exception.PrincipalType, "principal": exception.PrincipalID, "level": exception.Level}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

func validateDatabasePropertyAccessRule(app core.App, target databasePropertyAccessTarget, rule DatabasePropertyAccessRule) error {
	if target.Wiki || databasePropertyAccessUnrestrictable(target.PropertyType, rule.PropertyID) {
		return errDatabasePropertyAccessInvalid
	}
	if rule.DefaultLevel != propertyAccessInherit && propertyAccessRank[rule.DefaultLevel] == 0 && rule.DefaultLevel != propertyAccessNone {
		return errDatabasePropertyAccessInvalid
	}
	seen := map[string]bool{}
	for _, exception := range rule.Exceptions {
		key := exception.PrincipalType + "\x00" + exception.PrincipalID
		if seen[key] || exception.PrincipalID == "" || len(exception.PrincipalID) > maxGraphIdentifierLength ||
			!slices.Contains(propertyAccessExceptionLevels[exception.PrincipalType], exception.Level) {
			return errDatabasePropertyAccessInvalid
		}
		seen[key] = true
		var found int
		var err error
		switch exception.PrincipalType {
		case propertyAccessPrincipalUser:
			err = app.DB().NewQuery(`SELECT COUNT(*) FROM {{users}} WHERE [[id]] = {:id}`).
				Bind(dbx.Params{"id": exception.PrincipalID}).Row(&found)
		case propertyAccessPrincipalIntegration:
			err = app.DB().NewQuery(`SELECT COUNT(*) FROM {{users}} [[user]] WHERE [[user]].[[id]] = {:id} AND ` +
				integrationAccountSQL("[[user]].[[id]]")).Bind(dbx.Params{"id": exception.PrincipalID}).Row(&found)
		case propertyAccessPrincipalGroup:
			err = app.DB().NewQuery(`
				SELECT COUNT(*) FROM {{leafen_workspace_permission_groups}}
				WHERE [[id]] = {:id} AND [[workspace_id]] = {:workspaceId} AND [[deleted_at]] IS NULL
			`).Bind(dbx.Params{"id": exception.PrincipalID, "workspaceId": target.WorkspaceID}).Row(&found)
		case propertyAccessPrincipalProperty:
			if exception.PrincipalID == propertyAccessPageCreator {
				found = 1
				break
			}
			err = app.DB().NewQuery(`
				SELECT COUNT(*) FROM {{leafen_database_properties}}
				WHERE [[id]] = {:id} AND [[data_source_id]] = {:dataSourceId} AND [[type]] = 'person'
			`).Bind(dbx.Params{"id": exception.PrincipalID, "dataSourceId": rule.DataSourceID}).Row(&found)
		}
		if err != nil {
			return err
		}
		if found == 0 {
			return errDatabasePropertyAccessInvalid
		}
	}
	return nil
}

// previewDatabasePropertyAccess is what a property shows a person in its
// database as a whole under the rule being drafted (or the saved one), and
// why, for someone with full access to it.
func previewDatabasePropertyAccess(app core.App, actorID, dataSourceID, propertyID, personID string, draft *DatabasePropertyAccessRule) (DatabasePropertyAccessPreview, error) {
	target, err := authorizeDatabasePropertyAccessEditor(app, actorID, dataSourceID, propertyID)
	if err != nil {
		return DatabasePropertyAccessPreview{}, err
	}
	reader, err := newDatabasePropertyAccessReader(app, personID, []string{dataSourceID})
	if err != nil {
		return DatabasePropertyAccessPreview{}, err
	}
	if draft != nil {
		if err := validateDatabasePropertyAccessRule(app, target, *draft); err != nil {
			return DatabasePropertyAccessPreview{}, err
		}
		if reader == nil {
			if reader, err = newDatabasePropertyAccessReaderWith(app, personID, map[string]map[string]*databasePropertyAccessRule{}); err != nil {
				return DatabasePropertyAccessPreview{}, err
			}
		}
		if reader.rules[dataSourceID] == nil {
			reader.rules[dataSourceID] = map[string]*databasePropertyAccessRule{}
		}
		delete(reader.rules[dataSourceID], propertyID)
		if draft.DefaultLevel != propertyAccessInherit || len(draft.Exceptions) > 0 {
			reader.rules[dataSourceID][propertyID] = &databasePropertyAccessRule{
				storedDatabasePropertyAccessRule: storedDatabasePropertyAccessRule{DataSourceID: dataSourceID, PropertyID: propertyID,
					DefaultLevel: draft.DefaultLevel},
				Exceptions: draft.Exceptions,
			}
		}
		// What formulas and rollups read follows the draft too.
		sources, err := loadDatabasePropertyAccessSources(app, []string{dataSourceID})
		if err != nil {
			return DatabasePropertyAccessPreview{}, err
		}
		reads, err := loadDatabasePropertyAccessReads(app, sources)
		if err != nil {
			return DatabasePropertyAccessPreview{}, err
		}
		reader.setReads(reads)
	}
	if reader == nil || !reader.restricted(dataSourceID, propertyID) {
		role, err := (&databasePropertyAccessReader{app: app, readerID: personID, pages: map[string]string{},
			roles: map[string]pageAccessRole{}}).role(dataSourceID, "")
		if err != nil {
			return DatabasePropertyAccessPreview{}, err
		}
		return explainPropertyAccessPreview(app, target, dataSourceID, personID, nil, nil, role, inheritedPropertyAccess(role))
	}
	level, err := reader.level(dataSourceID, propertyID, nil)
	if err != nil {
		return DatabasePropertyAccessPreview{}, err
	}
	role, err := reader.role(dataSourceID, "")
	if err != nil {
		return DatabasePropertyAccessPreview{}, err
	}
	return explainPropertyAccessPreview(app, target, dataSourceID, personID, reader, reader.rules[dataSourceID][propertyID], role, level)
}

func inheritedPropertyAccess(role pageAccessRole) string {
	switch {
	case role >= pageAccessRoleEdit:
		return propertyAccessEdit
	case role >= pageAccessRoleView:
		return propertyAccessView
	}
	return propertyAccessNone
}
