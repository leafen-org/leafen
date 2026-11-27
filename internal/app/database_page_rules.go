package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// Page-level rules (schema_baseline.json): people with full access
// on a database's page add, change and delete them; a rule names a person,
// created-by or last-edited-by property and a role (view, comment or edit,
// as Notion's offers). A rule's grants are its row pages' shares marked
// with it, kept in step whenever its rows' values or the rules change. A
// share someone gave by hand wins over a rule's and keeps the person's
// share as given; a rule's share comes back when that one is taken away,
// and only the rule changes or takes it. A rule gives a share only to
// someone in the workspace now — its owner, a member, or a guest with a share
// there already — so a guest taken out of the workspace doesn't come back by
// a rule naming them: as Notion's removal says, they need a request or a new
// invitation (databasePageRuleReachesSQL). Like Notion's, every data source
// also offers "생성자" — who made each row page — with no property for it.
// Adding, changing and taking away a rule is a permission change in the
// organization's audit log, with the role before and after (the audit
// decision of 2026-10-21 20:05, 5).

// databasePageRuleCreator is the property ID of the built-in "생성자".
const databasePageRuleCreator = "created_by"

var databasePageRuleTypes = map[string]bool{"person": true, "created_by": true, "last_edited_by": true}

var databasePageRuleRoles = map[string]bool{"view": true, "comment": true, "edit": true}

type storedDatabasePageRule struct {
	ID           string `db:"id"`
	Owner        string `db:"owner"`
	PageID       string `db:"page_id"`
	DataSourceID string `db:"data_source_id"`
	PropertyID   string `db:"property_id"`
	Role         string `db:"role"`
	CreatedBy    string `db:"created_by"`
}

func registerDatabasePageRuleRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+publicMutationPageRulesPath, readDatabasePageRules)
	event.Router.POST(apiRoot+publicMutationPageRuleChangePath, changeDatabasePageRule).
		Bind(apis.BodyLimit(publicMutationMaxBodyBytes))
}

// loadDatabasePageRules lists a database page's rules and the properties a
// rule may name, those of its own data sources.
func loadDatabasePageRules(app core.App, workspaceID, pageID string) (publicMutationPageRules, error) {
	response := publicMutationPageRules{Rules: []publicMutationPageRule{}, Properties: []publicMutationRuleProperty{}}
	properties := []struct {
		DataSourceID string `db:"data_source_id"`
		ID           string `db:"id"`
		Name         string `db:"name"`
		Type         string `db:"type"`
	}{}
	// The page's sources are found by the page; the workspace is read after
	// (+), or SQLite, which keeps no statistics, reads the workspace's every
	// source.
	if err := app.DB().NewQuery(`
		SELECT [[property]].[[data_source_id]],[[property]].[[id]],[[property]].[[name]],[[property]].[[type]]
		FROM {{leafen_database_properties}} [[property]]
		JOIN {{leafen_data_sources}} [[source]] ON [[source]].[[id]]=[[property]].[[data_source_id]]
			AND [[source]].[[page_id]]={:pageId} AND +[[source]].[[workspace_id]]={:workspaceId}
		WHERE [[property]].[[workspace_id]]={:workspaceId} AND [[property]].[[page_id]]={:pageId}
			AND [[property]].[[type]] IN ('person','created_by','last_edited_by')
		ORDER BY [[property]].[[data_source_id]],[[property]].[[position]],[[property]].[[id]]
	`).Bind(dbx.Params{"workspaceId": workspaceID, "pageId": pageID}).All(&properties); err != nil {
		return response, err
	}
	sources := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_data_sources}} WHERE [[page_id]]={:pageId} AND [[workspace_id]]={:workspaceId} ORDER BY [[id]]
	`).Bind(dbx.Params{"workspaceId": workspaceID, "pageId": pageID}).Column(&sources); err != nil {
		return response, err
	}
	known := map[string]bool{}
	for _, sourceID := range sources {
		for _, property := range properties {
			if property.DataSourceID == sourceID {
				known[sourceID+"\x00"+property.ID] = true
				response.Properties = append(response.Properties, publicMutationRuleProperty{
					DataSourceID: property.DataSourceID, PropertyID: property.ID, Name: property.Name, Type: property.Type,
				})
			}
		}
		known[sourceID+"\x00"+databasePageRuleCreator] = true
		response.Properties = append(response.Properties, publicMutationRuleProperty{
			DataSourceID: sourceID, PropertyID: databasePageRuleCreator, Type: "created_by",
		})
	}
	rules := []storedDatabasePageRule{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[owner]],[[page_id]],[[data_source_id]],[[property_id]],[[role]],[[created_by]]
		FROM {{leafen_database_page_rules}} WHERE [[workspace_id]]={:workspaceId} AND [[page_id]]={:pageId}
		ORDER BY [[created_at]],[[id]]
	`).Bind(dbx.Params{"workspaceId": workspaceID, "pageId": pageID}).All(&rules); err != nil {
		return response, err
	}
	for _, rule := range rules {
		if known[rule.DataSourceID+"\x00"+rule.PropertyID] {
			response.Rules = append(response.Rules, publicMutationPageRule{
				ID: rule.ID, DataSourceID: rule.DataSourceID, PropertyID: rule.PropertyID, Role: rule.Role,
			})
		}
	}
	return response, nil
}

func readDatabasePageRules(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	authorization, err := authorizePublicMutationManager(event.App, user.Id, strings.TrimSpace(event.Request.PathValue("pageId")))
	if err != nil {
		return publicMutationRouteError(event, err, "page-rules-authorization")
	}
	response, err := loadDatabasePageRules(event.App, authorization.Workspace.WorkspaceID, authorization.Page.Id)
	if err == nil {
		response, err = withPeoplePageRule(event.App, authorization.Page, response)
	}
	if err != nil {
		return publicMutationRouteError(event, err, "page-rules-read")
	}
	return event.JSON(http.StatusOK, response)
}

func changeDatabasePageRule(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	input := publicMutationPageRuleInput{}
	if err := event.BindBody(&input); err != nil {
		return publicMutationRouteError(event, bodyRefusal(errPublicMutationInvalid, "page rule change", err), "page-rule-input")
	}
	if strings.TrimSpace(input.Action) == "" {
		return refuseMissingInput(event, errorCodePublicMutationInvalid, "action", "create, update or delete.")
	}
	authorization, err := authorizePublicMutationManager(event.App, user.Id, pageID)
	if err != nil {
		return publicMutationRouteError(event, err, "page-rule-authorization")
	}
	release := acquireProductOperationLock(publicMutationLockNamespace, authorization.Workspace.WorkspaceID+"\x00"+pageID)
	defer release()
	response := publicMutationPageRules{}
	change := func(txApp core.App) error {
		current, err := authorizePublicMutationManager(txApp, user.Id, pageID)
		if err != nil {
			return err
		}
		workspaceID := current.Workspace.WorkspaceID
		rules, err := loadDatabasePageRules(txApp, workspaceID, pageID)
		if err != nil {
			return err
		}
		sourceID, err := applyDatabasePageRuleChange(txApp, current, rules, input, user.Id)
		if err != nil {
			return err
		}
		if err := auditDatabasePageRuleChange(txApp, current, rules, input, user.Id); err != nil {
			return err
		}
		if err := reconcileDatabasePageRuleGrants(txApp, sourceID, ""); err != nil {
			return err
		}
		if response, err = loadDatabasePageRules(txApp, workspaceID, pageID); err != nil {
			return err
		}
		response, err = withPeoplePageRule(txApp, current.Page, response)
		return err
	}
	// A key's call asks its person only for a change that would be made
	// (user_key_acting_ask.go).
	if err := rehearseBeforeAsking(event, change); err != nil {
		return publicMutationRouteError(event, err, "page-rule-change")
	}
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	err = event.App.RunInTransaction(change)
	if err != nil {
		return publicMutationRouteError(event, err, "page-rule-change")
	}
	return event.JSON(http.StatusOK, response)
}

// applyDatabasePageRuleChange makes one change to a page's rules and names
// the data source whose rows it touches.
func applyDatabasePageRuleChange(
	app core.App, authorization pageAuthorization, current publicMutationPageRules, input publicMutationPageRuleInput, actorID string,
) (string, error) {
	now := time.Now().UTC().UnixMilli()
	switch input.Action {
	case "create":
		var property *publicMutationRuleProperty
		for index := range current.Properties {
			candidate := current.Properties[index]
			if candidate.PropertyID == input.PropertyID && candidate.DataSourceID == input.DataSourceID {
				property = &candidate
			}
		}
		if property == nil {
			return "", publicMutationRefused("propertyId and dataSourceId name no person property of this page's databases; the page rules list them")
		}
		for _, rule := range current.Rules {
			if rule.PropertyID == input.PropertyID {
				return "", publicMutationRefused("propertyId has a rule already: change it with update and its ruleId")
			}
		}
		if !databasePageRuleRoles[input.Role] {
			return "", publicMutationRefused("role is view, comment or edit, not %q", input.Role)
		}
		_, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_database_page_rules}}
				([[id]],[[owner]],[[workspace_id]],[[page_id]],[[data_source_id]],[[property_id]],[[role]],
				 [[created_by]],[[created_at]],[[updated_at]])
			VALUES ({:id},{:owner},{:workspaceId},{:pageId},{:sourceId},{:propertyId},{:role},{:actorId},{:now},{:now})
		`).Bind(dbx.Params{
			"id": securityRandomID(), "owner": authorization.Page.GetString("owner"),
			"workspaceId": authorization.Workspace.WorkspaceID, "pageId": authorization.Page.Id,
			"sourceId": property.DataSourceID, "propertyId": property.PropertyID, "role": input.Role,
			"actorId": actorID, "now": now,
		}).Execute()
		return property.DataSourceID, err
	case "update", "delete":
		for _, rule := range current.Rules {
			if rule.ID != input.RuleID {
				continue
			}
			if input.Action == "delete" {
				return rule.DataSourceID, deleteDatabasePageRule(app, rule.ID)
			}
			if !databasePageRuleRoles[input.Role] {
				return "", publicMutationRefused("role is view, comment or edit, not %q", input.Role)
			}
			_, err := app.DB().NewQuery(`
				UPDATE {{leafen_database_page_rules}} SET [[role]]={:role},[[updated_at]]={:now} WHERE [[id]]={:id}
			`).Bind(dbx.Params{"role": input.Role, "now": now, "id": rule.ID}).Execute()
			return rule.DataSourceID, err
		}
		return "", publicMutationRefused("ruleId names no rule of this page; the page rules list them")
	}
	return "", publicMutationRefused("action is create, update or delete, not %q", input.Action)
}

// auditDatabasePageRuleChange keeps a change to a page's rules in the
// organization's audit log: the database, the property, the role before and
// after.
func auditDatabasePageRuleChange(
	app core.App, authorization pageAuthorization, before publicMutationPageRules, input publicMutationPageRuleInput, actorID string,
) error {
	organizationID := authorization.Workspace.Organization.OrganizationID
	if organizationID == "" {
		return nil
	}
	action, propertyID, from, to := "", input.PropertyID, "", input.Role
	switch input.Action {
	case "create":
		action = "database.page_rule.created"
	case "update", "delete":
		action = "database.page_rule.changed"
		if input.Action == "delete" {
			action, to = "database.page_rule.removed", ""
		}
		for _, rule := range before.Rules {
			if rule.ID == input.RuleID {
				propertyID, from = rule.PropertyID, rule.Role
			}
		}
	}
	property := ""
	for _, candidate := range before.Properties {
		if candidate.PropertyID == propertyID {
			property = candidate.Name
		}
	}
	metadata := map[string]any{"name": authorization.Page.GetString("title"), "propertyId": propertyID, "property": property, "from": from, "to": to}
	if propertyID == databasePageRuleCreator {
		// The built-in "생성자", named in the reader's language.
		metadata["creator"] = true
	}
	return insertOrganizationAuditEvent(app, organizationAuditEvent{OrganizationID: organizationID, WorkspaceID: authorization.Workspace.WorkspaceID,
		ActorID: actorID, Action: action, TargetType: "page", TargetID: authorization.Page.Id, Metadata: metadata})
}

// deleteDatabasePageRule takes a rule away with what it gave.
func deleteDatabasePageRule(app core.App, ruleID string) error {
	if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_page_permissions}} WHERE ` + partialIndexKey(`[[rule_id]]`, `={:id}`)).
		Bind(dbx.Params{"id": ruleID}).Execute(); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`DELETE FROM {{leafen_database_page_rules}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": ruleID}).Execute()
	return err
}

// retireDatabasePageRulesOfProperty takes away the rules of a property that
// is gone or no longer names people.
func retireDatabasePageRulesOfProperty(app core.App, propertyID, propertyType string) error {
	if databasePageRuleTypes[propertyType] || propertyID == databasePageRuleCreator {
		return nil
	}
	ids := []string{}
	if err := app.DB().NewQuery(`SELECT [[id]] FROM {{leafen_database_page_rules}} WHERE [[property_id]]={:id}`).
		Bind(dbx.Params{"id": propertyID}).Column(&ids); err != nil {
		return err
	}
	for _, id := range ids {
		if err := deleteDatabasePageRule(app, id); err != nil {
			return err
		}
	}
	return nil
}

// reconcileDatabasePageRuleGrantsOfPage keeps a row page's rule shares in
// step, for a page whose own shares changed.
func reconcileDatabasePageRuleGrantsOfPage(app core.App, pageID string) error {
	var sourceID string
	err := app.DB().NewQuery(`SELECT [[data_source_id]] FROM {{leafen_pages}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": pageID}).Row(&sourceID)
	if errors.Is(err, sql.ErrNoRows) || err == nil && sourceID == "" {
		return nil
	}
	if err != nil {
		return err
	}
	return reconcileDatabasePageRuleGrants(app, sourceID, pageID)
}

type databasePageRuleGrant struct {
	PageID      string `db:"page_id"`
	PrincipalID string `db:"principal_id"`
}

// reconcileDatabasePageRuleGrants makes the rule shares of a data source's
// rows — or of one row — what its rules give now: each person named gets
// the widest role the rules give, unless a share given by hand is theirs.
func reconcileDatabasePageRuleGrants(app core.App, dataSourceID, rowID string) error {
	if dataSourceID == "" {
		return nil
	}
	rules := []storedDatabasePageRule{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[owner]],[[page_id]],[[data_source_id]],[[property_id]],[[role]],[[created_by]]
		FROM {{leafen_database_page_rules}} WHERE [[data_source_id]]={:sourceId}
		ORDER BY CASE [[role]] WHEN 'edit' THEN 0 WHEN 'comment' THEN 1 ELSE 2 END,[[created_at]],[[id]]
	`).Bind(dbx.Params{"sourceId": dataSourceID}).All(&rules); err != nil || len(rules) == 0 {
		// A rule taken away took its shares with it.
		return err
	}
	type grant struct{ role, ruleID, createdBy string }
	desired := map[databasePageRuleGrant]grant{}
	workspaceID := ""
	if err := app.DB().NewQuery(`SELECT [[workspace_id]] FROM {{leafen_data_sources}} WHERE [[id]]={:sourceId}`).
		Bind(dbx.Params{"sourceId": dataSourceID}).Row(&workspaceID); errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	for _, rule := range rules {
		propertyType := "created_by"
		if rule.PropertyID != databasePageRuleCreator {
			if err := app.DB().NewQuery(`SELECT [[type]] FROM {{leafen_database_properties}} WHERE [[id]]={:id} AND [[data_source_id]]={:sourceId}`).
				Bind(dbx.Params{"id": rule.PropertyID, "sourceId": dataSourceID}).Row(&propertyType); errors.Is(err, sql.ErrNoRows) {
				continue
			} else if err != nil {
				return err
			}
		}
		named := []databasePageRuleGrant{}
		params := dbx.Params{"owner": rule.Owner, "pageId": rule.PageID, "sourceId": dataSourceID, "rowId": rowID, "propertyId": rule.PropertyID}
		rows := `[[row]].[[owner]]={:owner} AND [[row]].[[page_id]]={:pageId} AND [[row]].[[data_source_id]]={:sourceId}
			AND ({:rowId}='' OR [[row]].[[id]]={:rowId})`
		query := `
			SELECT [[row]].[[id]] AS [[page_id]],[[person]].[[value]] AS [[principal_id]]
			FROM {{leafen_database_rows}} [[row]]
			JOIN {{leafen_database_row_values}} [[cell]] ON [[cell]].[[row_id]]=[[row]].[[id]] AND [[cell]].[[property_id]]={:propertyId}
				AND json_valid([[cell]].[[value]]) AND json_type([[cell]].[[value]])='array'
			JOIN json_each([[cell]].[[value]]) [[person]]
			WHERE ` + rows + ` AND [[person]].[[type]]='text'`
		if propertyType == "created_by" || propertyType == "last_edited_by" {
			query = `
				SELECT [[row]].[[id]] AS [[page_id]],[[page]].[[` + propertyType + `]] AS [[principal_id]]
				FROM {{leafen_database_rows}} [[row]] JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]]=[[row]].[[id]]
				WHERE ` + rows + ` AND [[page]].[[` + propertyType + `]]!=''`
		} else if propertyType != "person" {
			continue
		}
		if err := app.DB().NewQuery(query).Bind(params).All(&named); err != nil {
			return err
		}
		for _, key := range named {
			if _, taken := desired[key]; !taken {
				desired[key] = grant{role: rule.Role, ruleID: rule.ID, createdBy: rule.CreatedBy}
			}
		}
	}
	existing := []struct {
		ID          string `db:"id"`
		PageID      string `db:"page_id"`
		PrincipalID string `db:"principal_id"`
		Role        string `db:"role"`
		RuleID      string `db:"rule_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[permission]].[[id]],[[permission]].[[page_id]],[[permission]].[[principal_id]],[[permission]].[[role]],[[permission]].[[rule_id]]
		FROM {{leafen_page_permissions}} [[permission]]
		JOIN {{leafen_database_page_rules}} [[rule]] ON [[rule]].[[id]]=[[permission]].[[rule_id]]
		WHERE [[permission]].[[rule_id]]!='' AND [[rule]].[[data_source_id]]={:sourceId}
			AND ({:rowId}='' OR [[permission]].[[page_id]]={:rowId})
	`).Bind(dbx.Params{"sourceId": dataSourceID, "rowId": rowID}).All(&existing); err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	for _, share := range existing {
		key := databasePageRuleGrant{PageID: share.PageID, PrincipalID: share.PrincipalID}
		want, kept := desired[key]
		delete(desired, key)
		var err error
		switch {
		case !kept:
			_, err = app.DB().NewQuery(`DELETE FROM {{leafen_page_permissions}} WHERE [[id]]={:id}`).
				Bind(dbx.Params{"id": share.ID}).Execute()
		case want.role != share.Role || want.ruleID != share.RuleID:
			_, err = app.DB().NewQuery(`
				UPDATE {{leafen_page_permissions}} SET [[role]]={:role},[[rule_id]]={:ruleId},[[updated_at]]={:now} WHERE [[id]]={:id}
			`).Bind(dbx.Params{"role": want.role, "ruleId": want.ruleID, "now": now, "id": share.ID}).Execute()
		}
		if err != nil {
			return err
		}
	}
	for key, want := range desired {
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_page_permissions}}
				([[id]],[[workspace_id]],[[page_id]],[[principal_type]],[[principal_id]],[[role]],
				 [[created_by]],[[created_at]],[[updated_at]],[[rule_id]])
			SELECT {:id},{:workspaceId},{:pageId},'user',{:principalId},{:role},{:createdBy},{:now},{:now},{:ruleId}
			WHERE EXISTS (SELECT 1 FROM {{users}} WHERE [[id]]={:principalId}) AND ` + databasePageRuleReachesSQL + `
			ON CONFLICT ([[page_id]],[[principal_type]],[[principal_id]]) DO NOTHING
		`).Bind(dbx.Params{
			"id": securityRandomID(), "workspaceId": workspaceID, "pageId": key.PageID, "principalId": key.PrincipalID,
			"role": want.role, "createdBy": want.createdBy, "now": now, "ruleId": want.ruleID,
		}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// databasePageRuleReachesSQL says a rule may share with {:principalId}: the
// workspace's owner, a member, or a guest who holds a share in it already.
var databasePageRuleReachesSQL = `(
	EXISTS (SELECT 1 FROM {{leafen_workspaces}} WHERE [[id]]={:workspaceId} AND [[workspace_owner]]={:principalId})
	OR EXISTS (SELECT 1 FROM {{leafen_workspace_memberships}} WHERE [[workspace_id]]={:workspaceId} AND [[user_id]]={:principalId})
	OR EXISTS (SELECT 1 FROM {{leafen_page_permissions}} WHERE [[workspace_id]]={:workspaceId} AND ` + pageShareNamesAccountSQL("principalId") + `))`
