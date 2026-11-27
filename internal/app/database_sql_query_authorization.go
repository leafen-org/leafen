package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A query's table is the one fetch describes for the data source, the page's
// id before it as Notion's SELECT * gives (seen 2026-11-05, 2026-11-11): id,
// url and createdTime, then each property's columns as fetch names them and
// in its order, a date's and a place's parts apart; formulas and rollups
// have none.
const (
	databaseSQLSyntheticIDColumn      = "id"
	databaseSQLSyntheticURLColumn     = "url"
	databaseSQLSyntheticCreatedColumn = "createdTime"
	// databaseSQLColumnShape marks columns laid out as fetch's table, their
	// times as SQL reads them; a query an older server began with other
	// columns is refused, not mixed.
	databaseSQLColumnShape = 4
)

type databaseSQLLogicalColumn struct {
	Name         string         `json:"name"`
	PropertyID   string         `json:"propertyId,omitempty"`
	PropertyType string         `json:"propertyType"`
	Config       map[string]any `json:"config,omitempty"`
	Synthetic    bool           `json:"synthetic,omitzero"`
	// Part is which of the property's columns this is: a date's "start",
	// "end" or "is_datetime", a place's "name"…; empty for its one column.
	Part  string `json:"part,omitempty"`
	Shape int    `json:"shape"`
	// Written is the column's name in fetch's table where Name isn't it:
	// one of two columns whose names differ only by case, which SQLite
	// can't tell apart (database_sql_query_names.go).
	Written string `json:"written,omitempty"`
}

type databaseSQLAuthorizedSource struct {
	URL               string                     `json:"url"`
	ID                string                     `json:"id"`
	SourceContainerID string                     `json:"sourceContainerId"`
	OwnerID           string                     `json:"ownerId"`
	WorkspaceID       string                     `json:"workspaceId"`
	PageID            string                     `json:"pageId"`
	Revision          int                        `json:"revision"`
	Columns           []databaseSQLLogicalColumn `json:"columns"`
	// Unqueried are its properties without a column, a refusal's hint.
	Unqueried []databaseSQLUnqueried `json:"unqueried,omitempty"`
}

type databaseSQLAuthorizedPlan struct {
	Plan           databaseSQLQueryPlan
	OrganizationID string
	OwnerID        string
	WorkspaceID    string
	Sources        []databaseSQLAuthorizedSource
	CanonicalHash  string
}

type databaseSQLSourceContainerRow struct {
	ID string `db:"id"`
}

func authorizeDatabaseSQLQueryPlan(
	app core.App,
	authority mcpAuthority,
	plan databaseSQLQueryPlan,
) (databaseSQLAuthorizedPlan, error) {
	authorized := databaseSQLAuthorizedPlan{Plan: plan}
	err := runReadSnapshot(context.Background(), app, func(txApp core.App) error {
		var err error
		authorized, err = authorizeDatabaseSQLQueryPlanTransaction(txApp, authority, plan)
		return err
	})
	return authorized, err
}

func authorizeDatabaseSQLQueryPlanTransaction(
	app core.App,
	authority mcpAuthority,
	plan databaseSQLQueryPlan,
) (databaseSQLAuthorizedPlan, error) {
	authorized := databaseSQLAuthorizedPlan{Plan: plan}
	if len(plan.DataSourceURLs) == 0 || authority.PrincipalID == "" {
		return databaseSQLAuthorizedPlan{}, mcpInvalid("data_source_urls names the data sources the query reads, as collection://… URLs")
	}
	for _, sourceURL := range plan.DataSourceURLs {
		dataSourceID, ok := parseMCPDataSourceURL(sourceURL)
		if !ok {
			return databaseSQLAuthorizedPlan{}, mcpInvalid("each of data_source_urls is a collection://… URL, as notion-fetch gives it")
		}
		// A data source in the trash reads as any (mcp_fetch_trash.go).
		location, err := loadMCPDataSourceLocationToRead(app, dataSourceID)
		if err != nil {
			return databaseSQLAuthorizedPlan{}, mcpDataSourceQueryError(err)
		}
		page, err := authorizePage(app, authority.PrincipalID, location.PageID, permissionWorkspaceRead)
		if err != nil || page.Workspace.WorkspaceID != location.WorkspaceID ||
			page.Workspace.OwnerID != location.OwnerID {
			return databaseSQLAuthorizedPlan{}, mcpRefusedUnlessStorage(errMCPCantRead, err)
		}
		workspace, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{
			WorkspaceID: location.WorkspaceID,
		}, permissionWorkspaceRead)
		if err != nil || workspace.WorkspaceID != location.WorkspaceID {
			return databaseSQLAuthorizedPlan{}, mcpRefusedUnlessStorage(errMCPOutsideConnection, err)
		}
		if authorized.WorkspaceID == "" {
			authorized.OrganizationID = page.Workspace.Organization.OrganizationID
			authorized.OwnerID = location.OwnerID
			authorized.WorkspaceID = location.WorkspaceID
		} else if authorized.OrganizationID != page.Workspace.Organization.OrganizationID ||
			authorized.OwnerID != location.OwnerID || authorized.WorkspaceID != location.WorkspaceID {
			return databaseSQLAuthorizedPlan{}, mcpInvalid("one query reads data sources of one workspace")
		}
		containerID, err := mcpQueriedSourceContainerID(app, location)
		if err != nil {
			return databaseSQLAuthorizedPlan{}, err
		}
		properties, _, err := loadMCPDataSourceSchema(app, location, authority.PrincipalID)
		if err != nil {
			return databaseSQLAuthorizedPlan{}, err
		}
		ids, err := loadNotionPropertyIDs(app, location.ID)
		if err != nil {
			return databaseSQLAuthorizedPlan{}, err
		}
		// The table fetch describes, its columns in its order.
		visible := mcpSQLiteVisibleProperties(properties)
		columns, err := databaseSQLLogicalColumns(mcpSQLiteTableOrder(visible, ids))
		if err != nil {
			return databaseSQLAuthorizedPlan{}, err
		}
		revision, err := databaseQuerySourceRevision(app, location.OwnerID, location.PageID)
		if err != nil {
			return databaseSQLAuthorizedPlan{}, err
		}
		authorized.Sources = append(authorized.Sources, databaseSQLAuthorizedSource{
			URL: sourceURL, ID: dataSourceID, SourceContainerID: containerID,
			OwnerID: location.OwnerID, WorkspaceID: location.WorkspaceID,
			PageID: location.PageID, Revision: revision, Columns: columns,
			Unqueried: databaseSQLUnqueriedProperties(visible),
		})
	}
	// A query is the one it was while its sources change: one being taken
	// goes on (a count of 19 sources never came while an import wrote to
	// them, 2026-11-16), and a finished one asked anew is taken again when
	// they have (admitDatabaseSQLQueryJob).
	unrevised := slices.Clone(authorized.Sources)
	for index := range unrevised {
		unrevised[index].Revision = 0
	}
	encoded, err := json.Marshal(struct {
		PrincipalID    string                        `json:"principalId"`
		OrganizationID string                        `json:"organizationId"`
		Plan           databaseSQLQueryPlan          `json:"plan"`
		Sources        []databaseSQLAuthorizedSource `json:"sources"`
		Build          string                        `json:"build"`
	}{
		PrincipalID: authority.PrincipalID, OrganizationID: authorized.OrganizationID,
		Plan: plan, Sources: unrevised, Build: serverBuildIdentity,
	})
	if err != nil {
		return databaseSQLAuthorizedPlan{}, err
	}
	digest := sha256.Sum256(encoded)
	authorized.CanonicalHash = hex.EncodeToString(digest[:])
	return authorized, nil
}

// loadDatabaseSQLSourceContainerID is the database a data source is its own,
// in the trash or not: a query, a data source's address and the ancestors
// fetch names read one in the trash as any (mcp_fetch_trash.go).
func loadDatabaseSQLSourceContainerID(
	app core.App,
	location mcpDataSourceLocation,
) (string, error) {
	rows := []databaseSQLSourceContainerRow{}
	if err := app.DB().NewQuery(`
		SELECT [[container]].[[id]]
		FROM {{leafen_database_containers}} AS [[container]]
		JOIN {{leafen_database_container_sources}} AS [[membership]]
			ON [[membership]].[[container_id]] = [[container]].[[id]]
		WHERE [[container]].[[kind]] = 'source'
			AND [[container]].[[owner]] = {:owner}
			AND [[container]].[[workspace_id]] = {:workspaceId}
			AND [[container]].[[page_id]] = {:pageId}
			AND [[membership]].[[owner]] = [[container]].[[owner]]
			AND [[membership]].[[workspace_id]] = [[container]].[[workspace_id]]
			AND [[membership]].[[page_id]] = [[container]].[[page_id]]
			AND [[membership]].[[data_source_id]] = {:dataSourceId}
			AND [[membership]].[[source_container_id]] = ''
		ORDER BY [[container]].[[position]], [[container]].[[id]] LIMIT 2
	`).Bind(dbx.Params{
		"owner": location.OwnerID, "workspaceId": location.WorkspaceID,
		"pageId": location.PageID, "dataSourceId": location.ID,
	}).All(&rows); err != nil {
		return "", err
	}
	if len(rows) != 1 {
		return "", mcpDenied("that data source is gone, or isn't one this connection reaches")
	}
	return rows[0].ID, nil
}

func databaseSQLLogicalColumns(
	properties []notionMCPDataSourceProperty,
) ([]databaseSQLLogicalColumn, error) {
	// Names that differ only by case are told apart where both have
	// columns; a formula or rollup has none, so its name takes none from a
	// column's (2026-11-19: a formula "price" turned the number "Price"
	// fetch shows into "Price [id]").
	counts := map[string]int{}
	for _, property := range properties {
		name := strings.TrimSpace(property.Name)
		if property.ID == "" || name == "" || property.Type == "" || property.Config == nil {
			return nil, databaseSQLQueryInvalid()
		}
		if len(mcpSQLiteColumns(property)) > 0 {
			counts[strings.ToLower(name)]++
		}
	}
	columns := []databaseSQLLogicalColumn{
		{Name: databaseSQLSyntheticIDColumn, PropertyType: "id", Synthetic: true, Shape: databaseSQLColumnShape},
		{Name: databaseSQLSyntheticURLColumn, PropertyType: "url", Synthetic: true, Shape: databaseSQLColumnShape},
		{Name: databaseSQLSyntheticCreatedColumn, PropertyType: graphValueTypeCreatedTime, Synthetic: true, Shape: databaseSQLColumnShape},
	}
	used := map[string]struct{}{
		databaseSQLSyntheticIDColumn:                   {},
		strings.ToLower(databaseSQLSyntheticURLColumn): {}, strings.ToLower(databaseSQLSyntheticCreatedColumn): {},
	}
	for _, property := range properties {
		for _, column := range mcpSQLiteColumns(property) {
			base := column.Name
			name := base
			if counts[strings.ToLower(strings.TrimSpace(property.Name))] > 1 {
				name = fmt.Sprintf("%s [%s]", base, property.ID)
			}
			for suffix := 0; ; suffix++ {
				folded := strings.ToLower(name)
				if _, exists := used[folded]; !exists {
					used[folded] = struct{}{}
					break
				}
				if suffix == 0 {
					name = fmt.Sprintf("%s [%s]", base, property.ID)
				} else {
					name = fmt.Sprintf("%s [%s:%d]", base, property.ID, suffix+1)
				}
			}
			written := ""
			if name != base {
				written = base
			}
			columns = append(columns, databaseSQLLogicalColumn{
				Name: name, PropertyID: property.ID, PropertyType: property.Type,
				Config: property.Config, Part: column.Part, Shape: databaseSQLColumnShape, Written: written,
			})
		}
	}
	return columns, nil
}

func databaseSQLAuthorizedPlanError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errMCPScopeDenied) {
		return err
	}
	if errors.Is(err, sql.ErrNoRows) {
		return errMCPGoneOrUnreached
	}
	if isPermissionDenied(err) {
		return errMCPCantRead
	}
	if errors.Is(err, errDatabaseSQLQueryInvalid) {
		return mcpSQLQueryInvalid(err)
	}
	return failedBecause(errMCPToolFailed, err)
}
