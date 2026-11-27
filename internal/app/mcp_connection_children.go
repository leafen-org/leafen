package app

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What a key or agent connection makes — another key or connection, a code
// or sign-in that becomes one, a sign-in session — ends with it: when its
// person disconnects it, nothing it left behind goes on working (agent
// full-permission decision of 2026-10-16, 2). Each such record names the
// connection that made it; it holds only while that connection, and the one
// that made that, still hold.

// connectionLineageHolds says whether the connection, and each connection
// that made it however far back (parent_chain.go), is still connected; a
// lineage that comes back on itself holds none.
func connectionLineageHolds(app core.App, connectionID string) (bool, error) {
	for chain := (parentChain[string]{}); connectionID != ""; {
		if !chain.climb(connectionID) {
			return false, nil
		}
		row := struct {
			RevokedAt int64  `db:"revoked_at"`
			CreatedBy string `db:"created_by_connection"`
		}{}
		err := app.DB().NewQuery(`SELECT [[revoked_at]], [[created_by_connection]] FROM {{leafen_mcp_connections}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": connectionID}).One(&row)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return false, nil
			}
			return false, err
		}
		if row.RevokedAt != 0 {
			return false, nil
		}
		connectionID = row.CreatedBy
	}
	return true, nil
}

// noteConnectionMaker records that the connection a call came with made the
// record, when it came with one.
func noteConnectionMaker(app core.App, table, id string, maker userKeyAuthority, keyed bool) error {
	if !keyed || maker.ConnectionID == "" || id == "" {
		return nil
	}
	_, err := app.DB().NewQuery(`UPDATE {{` + table + `}} SET [[created_by_connection]] = {:maker}
		WHERE [[id]] = {:id} AND [[created_by_connection]] = ''`).
		Bind(dbx.Params{"maker": maker.ConnectionID, "id": id}).Execute()
	return err
}

// revokeConnectionChildren ends what the connections made, and what that
// made in turn, however many makings down: each connection is ended once.
func revokeConnectionChildren(app core.App, connectionIDs []string, now int64) error {
	retireAfter := now + int64(mcpCredentialRetentionMilliseconds)
	ended := parentChain[string]{}
	for _, id := range connectionIDs {
		ended.climb(id)
	}
	for len(connectionIDs) > 0 {
		params := dbx.Params{"now": now, "retireAfter": retireAfter}
		placeholders := make([]string, 0, len(connectionIDs))
		for index, id := range connectionIDs {
			key := "c" + strconv.Itoa(index)
			params[key] = id
			placeholders = append(placeholders, "{:"+key+"}")
		}
		in := strings.Join(placeholders, ",")
		if _, err := app.DB().NewQuery(`UPDATE {{leafen_auth_sessions}} SET [[revoked_at]] = {:now}, [[updated_at]] = {:now}
			WHERE [[revoked_at]] = 0 AND [[created_by_connection]] IN (` + in + `)`).Bind(params).Execute(); err != nil {
			return err
		}
		children := []string{}
		if err := app.DB().NewQuery(`SELECT [[id]] FROM {{leafen_mcp_connections}}
			WHERE [[revoked_at]] = 0 AND [[created_by_connection]] IN (` + in + `)`).Bind(params).Column(&children); err != nil {
			return err
		}
		if len(children) == 0 {
			break
		}
		if _, err := app.DB().NewQuery(`UPDATE {{leafen_mcp_connections}} SET [[revoked_at]] = {:now}, [[retire_after]] = {:retireAfter}
			WHERE [[revoked_at]] = 0 AND [[created_by_connection]] IN (` + in + `)`).Bind(params).Execute(); err != nil {
			return err
		}
		connectionIDs = make([]string, 0, len(children))
		for _, child := range children {
			if ended.climb(child) {
				connectionIDs = append(connectionIDs, child)
			}
		}
	}
	// Their open collaboration sockets are checked again and closed.
	recheckCollaborationAccessAfterCommit(app)
	return nil
}
