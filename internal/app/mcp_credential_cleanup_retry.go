package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	mcpCredentialCleanupConnection = "connection"
	mcpCredentialCleanupClient     = "client"
)

type mcpCredentialCleanupOutcome uint8

const (
	mcpCredentialCleanupNoChange mcpCredentialCleanupOutcome = iota
	mcpCredentialCleanupDeletedConnection
	mcpCredentialCleanupDeletedClient
)

type mcpCredentialCleanupCandidate struct {
	Kind  string `db:"kind"`
	ID    string `db:"id"`
	DueAt int64  `db:"due_at"`
}

func mcpConnectionCleanupDueExpression() string {
	return "MAX([[retire_after]], [[cleanup_retry_at]])"
}

func mcpOAuthClientCleanupDueExpression() string {
	return fmt.Sprintf(
		"MAX([[last_used_at]] + %d, [[cleanup_retry_at]])",
		mcpCredentialRetentionMilliseconds,
	)
}

func listMCPCredentialCleanupCandidates(
	ctx context.Context,
	app core.App,
	now int64,
	limit int,
) ([]mcpCredentialCleanupCandidate, error) {
	if limit < 1 {
		return nil, errors.New("MCP credential registry cleanup requires a positive limit")
	}
	connectionDue := mcpConnectionCleanupDueExpression()
	connections := []mcpCredentialCleanupCandidate{}
	if err := app.DB().NewQuery(`
		SELECT '` + mcpCredentialCleanupConnection + `' AS [[kind]], [[id]],
			` + connectionDue + ` AS [[due_at]]
		FROM {{leafen_mcp_connections}}
		WHERE [[retire_after]] > 0 AND ` + connectionDue + ` <= {:now}
		ORDER BY ` + connectionDue + `, [[id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{"now": now, "limit": limit}).All(&connections); err != nil {
		return nil, err
	}
	clients, err := listMCPOAuthClientCleanupCandidates(ctx, app, now, limit)
	if err != nil {
		return nil, err
	}
	return mergeMCPCredentialCleanupCandidates(connections, clients, limit), nil
}

func listMCPOAuthClientCleanupCandidates(
	ctx context.Context,
	app core.App,
	now int64,
	limit int,
) ([]mcpCredentialCleanupCandidate, error) {
	clientDue := mcpOAuthClientCleanupDueExpression()
	clients := []mcpCredentialCleanupCandidate{}
	if err := app.DB().NewQuery(`
		SELECT '` + mcpCredentialCleanupClient + `' AS [[kind]], [[id]],
			` + clientDue + ` AS [[due_at]]
		FROM {{leafen_mcp_oauth_clients}}
		WHERE ` + clientDue + ` <= {:now}
		ORDER BY ` + clientDue + `, [[id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{"now": now, "limit": limit}).All(&clients); err != nil {
		return nil, err
	}
	return clients, nil
}

func mergeMCPCredentialCleanupCandidates(
	connections []mcpCredentialCleanupCandidate,
	clients []mcpCredentialCleanupCandidate,
	limit int,
) []mcpCredentialCleanupCandidate {
	merged := make([]mcpCredentialCleanupCandidate, 0, limit)
	connectionIndex := 0
	clientIndex := 0
	for len(merged) < limit &&
		(connectionIndex < len(connections) || clientIndex < len(clients)) {
		if connectionIndex >= len(connections) {
			merged = append(merged, clients[clientIndex])
			clientIndex++
			continue
		}
		if clientIndex >= len(clients) || mcpCredentialCleanupCandidateBefore(
			connections[connectionIndex], clients[clientIndex],
		) {
			merged = append(merged, connections[connectionIndex])
			connectionIndex++
			continue
		}
		merged = append(merged, clients[clientIndex])
		clientIndex++
	}
	return merged
}

func mcpCredentialCleanupCandidateBefore(
	left mcpCredentialCleanupCandidate,
	right mcpCredentialCleanupCandidate,
) bool {
	if left.DueAt != right.DueAt {
		return left.DueAt < right.DueAt
	}
	if left.Kind != right.Kind {
		return left.Kind < right.Kind
	}
	return left.ID < right.ID
}

func cleanupMCPCredentialCandidate(
	ctx context.Context,
	app core.App,
	candidate mcpCredentialCleanupCandidate,
	now int64,
) (mcpCredentialCleanupOutcome, error) {
	outcome := mcpCredentialCleanupNoChange
	err := app.RunInTransaction(func(txApp core.App) error {
		var err error
		switch candidate.Kind {
		case mcpCredentialCleanupConnection:
			outcome, err = cleanupMCPConnectionCandidate(ctx, txApp, candidate.ID, now)
		case mcpCredentialCleanupClient:
			outcome, err = cleanupMCPOAuthClientCandidate(ctx, txApp, candidate.ID, now)
		default:
			err = fmt.Errorf("unknown MCP credential cleanup kind %q", candidate.Kind)
		}
		return err
	})
	return outcome, err
}

func cleanupMCPConnectionCandidate(
	ctx context.Context,
	app core.App,
	id string,
	now int64,
) (mcpCredentialCleanupOutcome, error) {
	due := mcpConnectionCleanupDueExpression()
	params := dbx.Params{"id": id, "now": now}
	if _, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_mcp_oauth_retired_refresh_tokens}}
		WHERE [[connection_id]] = {:id} AND EXISTS (
			SELECT 1 FROM {{leafen_mcp_connections}}
			WHERE [[id]] = {:id} AND [[retire_after]] > 0
				AND ` + due + ` <= {:now}
		)
	`).WithContext(ctx).Bind(params).Execute(); err != nil {
		return mcpCredentialCleanupNoChange, err
	}
	if _, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_mcp_connection_workspaces}}
		WHERE [[connection_id]] = {:id} AND EXISTS (
			SELECT 1 FROM {{leafen_mcp_connections}}
			WHERE [[id]] = {:id} AND [[retire_after]] > 0
				AND ` + due + ` <= {:now}
		)
	`).WithContext(ctx).Bind(params).Execute(); err != nil {
		return mcpCredentialCleanupNoChange, err
	}
	result, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_mcp_connections}}
		WHERE [[id]] = {:id} AND [[retire_after]] > 0
			AND ` + due + ` <= {:now}
	`).WithContext(ctx).Bind(params).Execute()
	if err != nil {
		return mcpCredentialCleanupNoChange, err
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return mcpCredentialCleanupNoChange, err
	}
	if deleted == 1 {
		return mcpCredentialCleanupDeletedConnection, nil
	}
	return mcpCredentialCleanupNoChange, nil
}

func cleanupMCPOAuthClientCandidate(
	ctx context.Context,
	app core.App,
	id string,
	now int64,
) (mcpCredentialCleanupOutcome, error) {
	due := mcpOAuthClientCleanupDueExpression()
	state := struct {
		DueAt        int64 `db:"due_at"`
		ReferenceDue int64 `db:"reference_due"`
	}{}
	err := app.DB().NewQuery(`
		SELECT ` + due + ` AS [[due_at]], MAX(
			COALESCE((
				SELECT MAX([[connection]].[[refresh_expires_at]])
				FROM {{leafen_mcp_connections}} [[connection]]
				WHERE [[connection]].[[oauth_client_id]] = [[client]].[[id]]
					AND [[connection]].[[revoked_at]] = 0
			), 0),
			COALESCE((
				SELECT MAX([[authorization]].[[expires_at]])
				FROM {{leafen_mcp_oauth_authorizations}} [[authorization]]
				WHERE [[authorization]].[[client_id]] = [[client]].[[id]]
			), 0)
		) AS [[reference_due]]
		FROM {{leafen_mcp_oauth_clients}} [[client]] WHERE [[id]] = {:id}
	`).WithContext(ctx).Bind(dbx.Params{"id": id}).One(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return mcpCredentialCleanupNoChange, nil
	}
	if err != nil {
		return mcpCredentialCleanupNoChange, err
	}
	if state.DueAt > now {
		return mcpCredentialCleanupNoChange, nil
	}
	if state.ReferenceDue > now {
		_, err := app.DB().NewQuery(`
			UPDATE {{leafen_mcp_oauth_clients}}
			SET [[cleanup_retry_at]] = {:retryAt}
			WHERE [[id]] = {:id} AND [[cleanup_retry_at]] < {:retryAt}
		`).WithContext(ctx).Bind(dbx.Params{
			"id": id, "retryAt": state.ReferenceDue,
		}).Execute()
		return mcpCredentialCleanupNoChange, err
	}
	result, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_mcp_oauth_clients}}
		WHERE [[id]] = {:id} AND ` + due + ` <= {:now}
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_mcp_connections}} [[connection]]
				WHERE [[connection]].[[oauth_client_id]] = {:id}
					AND [[connection]].[[revoked_at]] = 0
					AND [[connection]].[[refresh_expires_at]] > {:now}
			)
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_mcp_oauth_authorizations}} [[authorization]]
				WHERE [[authorization]].[[client_id]] = {:id}
					AND [[authorization]].[[expires_at]] > {:now}
			)
	`).WithContext(ctx).Bind(dbx.Params{"id": id, "now": now}).Execute()
	if err != nil {
		return mcpCredentialCleanupNoChange, err
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return mcpCredentialCleanupNoChange, err
	}
	if deleted == 1 {
		return mcpCredentialCleanupDeletedClient, nil
	}
	return mcpCredentialCleanupNoChange, nil
}

func scheduleMCPCredentialCleanupRetry(
	ctx context.Context,
	app core.App,
	candidate mcpCredentialCleanupCandidate,
	retryAt int64,
) error {
	table := ""
	switch candidate.Kind {
	case mcpCredentialCleanupConnection:
		table = mcpConnectionsTable
	case mcpCredentialCleanupClient:
		table = mcpOAuthClientsTable
	default:
		return fmt.Errorf("unknown MCP credential cleanup kind %q", candidate.Kind)
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{` + table + `}} SET [[cleanup_retry_at]] = {:retryAt}
		WHERE [[id]] = {:id} AND [[cleanup_retry_at]] < {:retryAt}
	`).WithContext(ctx).Bind(dbx.Params{
		"id": candidate.ID, "retryAt": retryAt,
	}).Execute()
	return err
}

func nextMCPCredentialCleanupDue(
	ctx context.Context,
	app core.App,
) (int64, bool, error) {
	connectionDue := mcpConnectionCleanupDueExpression()
	clientDue := mcpOAuthClientCleanupDueExpression()
	frontier := []mcpCredentialCleanupCandidate{}
	if err := app.DB().NewQuery(`
		SELECT '` + mcpCredentialCleanupConnection + `' AS [[kind]], [[id]], [[due_at]] FROM (
			SELECT [[id]], ` + connectionDue + ` AS [[due_at]]
			FROM {{leafen_mcp_connections}} WHERE [[retire_after]] > 0
			ORDER BY ` + connectionDue + `, [[id]] LIMIT 1
		)
		UNION ALL
		SELECT '` + mcpCredentialCleanupClient + `' AS [[kind]], [[id]], [[due_at]] FROM (
			SELECT [[id]], ` + clientDue + ` AS [[due_at]]
			FROM {{leafen_mcp_oauth_clients}}
			ORDER BY ` + clientDue + `, [[id]] LIMIT 1
		)
	`).WithContext(ctx).All(&frontier); err != nil {
		return 0, false, err
	}
	if len(frontier) == 0 {
		return 0, false, nil
	}
	first := frontier[0]
	for _, candidate := range frontier[1:] {
		if mcpCredentialCleanupCandidateBefore(candidate, first) {
			first = candidate
		}
	}
	return first.DueAt, true, nil
}
