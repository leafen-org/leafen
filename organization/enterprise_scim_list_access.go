package organization

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/elimity-com/scim"
	scimErrors "github.com/elimity-com/scim/errors"
	scimFilter "github.com/elimity-com/scim/filter"
	"github.com/elimity-com/scim/optional"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
	filterParser "github.com/scim2/filter-parser/v2"
)

type scimListAccessWindow struct {
	Anchor string
	ListID int64
	Total  int
}

type storedSCIMListUser struct {
	ID          string `db:"id"`
	UserID      string `db:"user_id"`
	Status      string `db:"status"`
	ExternalID  string `db:"external_id"`
	Created     string `db:"created"`
	Updated     string `db:"updated"`
	Email       string `db:"email"`
	DisplayName string `db:"display_name"`
}

type storedSCIMListGroupMember struct {
	GroupID              string `db:"group_id"`
	OrganizationMemberID string `db:"organization_member_id"`
}

func loadSCIMListAccessWindow(
	app core.App,
	organizationID, resourceType, filterKey, filterValue string,
	params scim.ListRequestParams,
) (scimListAccessWindow, error) {
	window := scimListAccessWindow{}
	err := app.DB().NewQuery(`
		SELECT [[list_id]], [[total]] FROM {{leafen_scim_list_access_state}}
		WHERE [[organization_id]] = {:organizationId} AND [[resource_type]] = {:resourceType}
			AND [[filter_key]] = {:filterKey} AND [[filter_value]] = {:filterValue}
	`).Bind(dbx.Params{
		"organizationId": organizationID, "resourceType": resourceType,
		"filterKey": filterKey, "filterValue": filterValue,
	}).Row(&window.ListID, &window.Total)
	if errors.Is(err, sql.ErrNoRows) {
		return window, nil
	}
	if err != nil || params.Count <= 0 {
		return window, err
	}
	rank := max(1, params.StartIndex)
	if rank > window.Total {
		return window, nil
	}
	err = app.DB().NewQuery(`
		WITH RECURSIVE [[search]] ([[depth]], [[prefix]], [[remaining]], [[done]]) AS (
			VALUES (0, '', {:rank}, 0)
			UNION ALL
			SELECT [[search]].[[depth]] + 1, [[candidate]].[[prefix]],
				[[search]].[[remaining]] - COALESCE((
					SELECT SUM([[prior]].[[active_count]])
					FROM {{leafen_scim_list_access_tree}} [[prior]]
					WHERE [[prior]].[[list_id]] = {:listId}
						AND [[prior]].[[parent_prefix]] = [[search]].[[prefix]]
						AND [[prior]].[[prefix]] < [[candidate]].[[prefix]]
				), 0) - CASE WHEN [[candidate]].[[terminal_count]] >=
					[[search]].[[remaining]] - COALESCE((
						SELECT SUM([[prior]].[[active_count]])
						FROM {{leafen_scim_list_access_tree}} [[prior]]
						WHERE [[prior]].[[list_id]] = {:listId}
							AND [[prior]].[[parent_prefix]] = [[search]].[[prefix]]
							AND [[prior]].[[prefix]] < [[candidate]].[[prefix]]
					), 0) THEN 0 ELSE [[candidate]].[[terminal_count]] END,
				CASE WHEN [[candidate]].[[terminal_count]] >=
					[[search]].[[remaining]] - COALESCE((
						SELECT SUM([[prior]].[[active_count]])
						FROM {{leafen_scim_list_access_tree}} [[prior]]
						WHERE [[prior]].[[list_id]] = {:listId}
							AND [[prior]].[[parent_prefix]] = [[search]].[[prefix]]
							AND [[prior]].[[prefix]] < [[candidate]].[[prefix]]
					), 0) THEN 1 ELSE 0 END
			FROM [[search]]
			JOIN {{leafen_scim_list_access_tree}} [[candidate]]
				ON [[candidate]].[[list_id]] = {:listId}
				AND [[candidate]].[[parent_prefix]] = [[search]].[[prefix]]
			WHERE [[search]].[[done]] = 0 AND [[candidate]].[[prefix]] = (
				SELECT [[choice]].[[prefix]]
				FROM {{leafen_scim_list_access_tree}} [[choice]]
				WHERE [[choice]].[[list_id]] = {:listId}
					AND [[choice]].[[parent_prefix]] = [[search]].[[prefix]]
					AND COALESCE((
						SELECT SUM([[covered]].[[active_count]])
						FROM {{leafen_scim_list_access_tree}} [[covered]]
						WHERE [[covered]].[[list_id]] = {:listId}
							AND [[covered]].[[parent_prefix]] = [[search]].[[prefix]]
							AND [[covered]].[[prefix]] <= [[choice]].[[prefix]]
					), 0) >= [[search]].[[remaining]]
				ORDER BY [[choice]].[[prefix]] LIMIT 1
			)
		)
		SELECT [[prefix]] FROM [[search]] WHERE [[done]] = 1 LIMIT 1
	`).Bind(dbx.Params{
		"listId": window.ListID, "rank": rank,
	}).Row(&window.Anchor)
	return window, err
}

func loadSCIMUserList(
	app core.App,
	organizationID string,
	params scim.ListRequestParams,
) (scim.Page, error) {
	var page scim.Page
	err := app.RunInTransaction(func(txApp core.App) error {
		var err error
		page, err = loadSCIMUserListSnapshot(txApp, organizationID, params)
		return err
	})
	return page, err
}

func loadSCIMUserListSnapshot(app core.App, organizationID string, params scim.ListRequestParams) (scim.Page, error) {
	window, err := loadSCIMListAccessWindow(
		app, organizationID, leafen.SCIMListAccessResourceTypeUser, leafen.SCIMListAccessAllFilterKey, "", params,
	)
	if err != nil || window.Anchor == "" {
		return scim.Page{TotalResults: window.Total, Resources: []scim.Resource{}}, err
	}
	rows := []storedSCIMListUser{}
	err = app.DB().NewQuery(`
		SELECT [[membership]].[[id]], [[membership]].[[user]] AS [[user_id]],
			[[membership]].[[status]], [[membership]].[[scim_external_id]] AS [[external_id]],
			[[membership]].[[created]], [[membership]].[[updated]],
			[[user]].[[email]], [[user]].[[name]] AS [[display_name]]
		FROM (
			SELECT [[resource_id]] FROM {{leafen_scim_list_access_entries}}
			WHERE [[list_id]] = {:listId} AND [[resource_id]] >= {:anchor}
			ORDER BY [[resource_id]] LIMIT {:limit}
		) [[selected]]
		JOIN {{leafen_organization_memberships}} [[membership]]
			ON [[membership]].[[id]] = [[selected]].[[resource_id]]
		JOIN {{users}} [[user]] ON [[user]].[[id]] = [[membership]].[[user]]
		ORDER BY [[selected]].[[resource_id]]
	`).Bind(dbx.Params{
		"listId": window.ListID, "anchor": window.Anchor, "limit": params.Count,
	}).All(&rows)
	if err != nil {
		return scim.Page{}, err
	}
	addresses, err := loadSCIMUserEmails(app, organizationID, rows)
	if err != nil {
		return scim.Page{}, err
	}
	resources := make([]scim.Resource, 0, len(rows))
	for _, row := range rows {
		resource, err := scimUserResource(row, addresses[row.UserID])
		if err != nil {
			return scim.Page{}, err
		}
		resources = append(resources, resource)
	}
	return scim.Page{TotalResults: window.Total, Resources: resources}, nil
}

func scimUserResource(row storedSCIMListUser, addresses []string) (scim.Resource, error) {
	created, err := types.ParseDateTime(row.Created)
	if err != nil {
		return scim.Resource{}, err
	}
	updated, err := types.ParseDateTime(row.Updated)
	if err != nil {
		return scim.Resource{}, err
	}
	createdAt, updatedAt := created.Time(), updated.Time()
	emails := []interface{}{}
	for _, email := range addresses {
		emails = append(emails, map[string]interface{}{"value": email, "type": "work", "primary": email == row.Email})
	}
	resource := scim.Resource{
		ID: row.ID,
		Attributes: scim.ResourceAttributes{
			"userName": row.Email, "displayName": row.DisplayName,
			"active": row.Status == leafen.OrganizationMemberStatusActive,
			"emails": emails,
		},
		Meta: scim.Meta{
			Created: &createdAt, LastModified: &updatedAt,
			Version: fmt.Sprintf("W/\"%s\"", row.Updated),
		},
	}
	if row.ExternalID != "" {
		resource.ExternalID = optional.NewString(row.ExternalID)
	}
	return resource, nil
}

func loadSCIMGroupList(
	app core.App,
	organizationID string,
	params scim.ListRequestParams,
) (scim.Page, error) {
	filterKey, filterValue, pointColumn, pointValue, err := scimGroupListFilter(params.FilterValidator)
	if err != nil {
		return scim.Page{}, err
	}
	if pointColumn != "" {
		return loadSCIMGroupPoint(app, organizationID, pointColumn, pointValue, params)
	}
	window, err := loadSCIMListAccessWindow(
		app, organizationID, leafen.SCIMListAccessResourceTypeGroup, filterKey, filterValue, params,
	)
	if err != nil || window.Anchor == "" {
		return scim.Page{TotalResults: window.Total, Resources: []scim.Resource{}}, err
	}
	rows := []storedSCIMGroup{}
	err = app.DB().NewQuery(`
		SELECT [[group]].[[id]], [[group]].[[organization_id]], [[group]].[[display_name]],
			[[group]].[[normalized_name]], [[group]].[[external_id]],
			[[group]].[[active_generation]], [[group]].[[current_payload_hash]],
			[[group]].[[deleted_at]], [[group]].[[created_at]], [[group]].[[updated_at]]
		FROM (
			SELECT [[resource_id]] FROM {{leafen_scim_list_access_entries}}
			WHERE [[list_id]] = {:listId} AND [[resource_id]] >= {:anchor}
			ORDER BY [[resource_id]] LIMIT {:limit}
		) [[selected]]
		JOIN {{leafen_scim_groups}} [[group]] ON [[group]].[[id]] = [[selected]].[[resource_id]]
		ORDER BY [[selected]].[[resource_id]]
	`).Bind(dbx.Params{
		"listId": window.ListID, "anchor": window.Anchor, "limit": params.Count,
	}).All(&rows)
	if err != nil {
		return scim.Page{}, err
	}
	resources, err := scimGroupResources(app, rows)
	return scim.Page{TotalResults: window.Total, Resources: resources}, err
}

func scimGroupListFilter(
	validator *scimFilter.Validator,
) (filterKey, filterValue, pointColumn, pointValue string, err error) {
	if validator == nil {
		return leafen.SCIMListAccessAllFilterKey, "", "", "", nil
	}
	attribute, ok := validator.GetFilter().(*filterParser.AttributeExpression)
	if !ok || attribute.Operator != filterParser.EQ {
		return "", "", "", "", scimRefusal(scimErrors.ScimErrorTooMany, "The filter takes one eq comparison.")
	}
	value, ok := attribute.CompareValue.(string)
	if !ok {
		return "", "", "", "", scimRefusal(scimErrors.ScimErrorInvalidFilter, "The filter compares with a string.")
	}
	switch attribute.AttributePath.AttributeName {
	case "id":
		return "", "", "id", value, nil
	case "externalId":
		return leafen.SCIMListAccessExternalFilterKey(value), value, "", "", nil
	case "displayName":
		return "", "", "normalized_name", normalizeSCIMGroupName(value), nil
	default:
		return "", "", "", "", scimRefusal(scimErrors.ScimErrorTooMany, "The filter takes id, externalId or displayName.")
	}
}

func loadSCIMGroupPoint(
	app core.App,
	organizationID, column, value string,
	params scim.ListRequestParams,
) (scim.Page, error) {
	if column != "id" && column != "normalized_name" {
		return scim.Page{}, scimRefusal(scimErrors.ScimErrorInvalidFilter, "The filter takes id or displayName here.")
	}
	row := storedSCIMGroup{}
	query := `SELECT [[id]], [[organization_id]], [[display_name]], [[normalized_name]],
		[[external_id]], [[active_generation]], [[current_payload_hash]],
		[[deleted_at]], [[created_at]], [[updated_at]]
		FROM {{leafen_scim_groups}}
		WHERE [[organization_id]] = {:organizationId} AND [[deleted_at]] = 0 AND [[` + column + `]] = {:value}
		LIMIT 1`
	err := app.DB().NewQuery(query).Bind(dbx.Params{
		"organizationId": organizationID, "value": value,
	}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return scim.Page{Resources: []scim.Resource{}}, nil
	}
	if err != nil {
		return scim.Page{}, err
	}
	if params.Count <= 0 || params.StartIndex > 1 {
		return scim.Page{TotalResults: 1, Resources: []scim.Resource{}}, nil
	}
	resources, err := scimGroupResources(app, []storedSCIMGroup{row})
	return scim.Page{TotalResults: 1, Resources: resources}, err
}

func scimGroupResources(app core.App, groups []storedSCIMGroup) ([]scim.Resource, error) {
	if len(groups) == 0 {
		return []scim.Resource{}, nil
	}
	params := dbx.Params{}
	placeholders := make([]string, len(groups))
	for index, group := range groups {
		key := fmt.Sprintf("group%d", index)
		params[key], placeholders[index] = group.ID, "{:"+key+"}"
	}
	rows := []storedSCIMListGroupMember{}
	if err := app.DB().NewQuery(`
		SELECT [[member]].[[group_id]], [[member]].[[organization_member_id]]
		FROM {{leafen_scim_group_members}} [[member]]
		JOIN {{leafen_scim_groups}} [[group]]
			ON [[group]].[[id]] = [[member]].[[group_id]]
			AND [[group]].[[active_generation]] = [[member]].[[generation]]
		WHERE [[member]].[[group_id]] IN (` + strings.Join(placeholders, ", ") + `)
	`).Bind(params).All(&rows); err != nil {
		return nil, err
	}
	sort.Slice(rows, func(left, right int) bool {
		if rows[left].GroupID == rows[right].GroupID {
			return rows[left].OrganizationMemberID < rows[right].OrganizationMemberID
		}
		return rows[left].GroupID < rows[right].GroupID
	})
	members := make(map[string][]string, len(groups))
	for _, row := range rows {
		members[row.GroupID] = append(members[row.GroupID], row.OrganizationMemberID)
	}
	resources := make([]scim.Resource, 0, len(groups))
	for _, group := range groups {
		resources = append(resources, scimGroupResource(group, members[group.ID]))
	}
	return resources, nil
}

func scimGroupResource(group storedSCIMGroup, members []string) scim.Resource {
	memberAttributes := make([]interface{}, 0, len(members))
	for _, memberID := range members {
		memberAttributes = append(memberAttributes, map[string]interface{}{
			"value": memberID, "$ref": "Users/" + memberID, "type": "User",
		})
	}
	created := time.UnixMilli(group.CreatedAt).UTC()
	updated := time.UnixMilli(group.UpdatedAt).UTC()
	resource := scim.Resource{
		ID: group.ID,
		Attributes: scim.ResourceAttributes{
			"displayName": group.DisplayName, "members": memberAttributes,
		},
		Meta: scim.Meta{
			Created: &created, LastModified: &updated,
			Version: fmt.Sprintf("W/\"%d\"", group.UpdatedAt),
		},
	}
	if group.ExternalID != "" {
		resource.ExternalID = optional.NewString(group.ExternalID)
	}
	return resource
}
