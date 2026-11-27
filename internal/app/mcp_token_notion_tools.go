package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's tools on a connection's list (the Notion migration decision's
// change 4, 2026-11-20): the native endpoint lists its core tools and
// Notion's as Notion's own MCP lists them (mcp_core_tools.go), and an agent
// that needs every notion-* name Leafen has — one written against the
// claude.ai connector's names, or a person asking for them — turns them on
// for its own connection here, in its conversation. That changes only what
// its list shows: every tool still runs by name or through leafen_call, so
// turning them on or off asks no one. The setting stays with the connection
// (its endpoint keeps no session to tell of a changed list), kept as
// notion_tools, 0 for every connection until its agent turns it on; a
// token's page turning on Notion's tools Leafen doesn't have lists these too
// (mcp_notion_only_tools.go).

const (
	mcpNotionToolsPath      = "/mcp/connections/current/notion-tools"
	mcpNotionToolsBodyBytes = 1024
)

type mcpNotionToolsInput struct {
	NotionTools *bool `json:"notionTools"`
}

type mcpNotionToolsView struct {
	NotionTools bool `json:"notionTools"`
	// Next says when the list shows the change and what runs a tool before.
	Next string `json:"next"`
}

const (
	mcpNotionToolsOnNext = "Notion's tools (notion-*) are on this connection's list from the next time your program reads Leafen's tools: " +
		"have it read them again (in Claude Code, /mcp and reconnect Leafen). Until then run one with leafen_call, its name as tool and its input as arguments."
	mcpNotionToolsOffNext = "Notion's tools are off this connection's list from the next time your program reads Leafen's tools; " +
		"leafen_call still runs one by its name."
	mcpNotionToolsPageNext = " Its token's page in Leafen lists Notion's tool names, these too, while its “Notion-only tool names” is on."
)

// mcpConnectionSwitchesUsage is how an agent turns its own connection's
// switches, which the instructions name (mcp_instructions.go): told in
// leafen_call's description, as the instructions stay within what Claude
// Code keeps (mcpInstructionsLimit).
const mcpConnectionSwitchesUsage = `PUT ` + mcpNotionToolsPath + ` {"notionTools": true} puts every notion-* tool on your list, ` +
	`those Notion's MCP doesn't list too, and false takes those off; PUT ` + mcpHandsAllOverPath + ` {"handsAllOver": true} ` +
	`stops Leafen asking before irreversible actions — only if the person says to stop asking.`

func registerMCPNotionToolsRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+mcpNotionToolsPath, readOwnMCPTokenNotionTools)
	event.Router.PUT(apiRoot+mcpNotionToolsPath, setOwnMCPTokenNotionTools).Bind(apis.BodyLimit(mcpNotionToolsBodyBytes))
}

// ownMCPTokenNotionTools reads the request's own token's two Notion
// settings, or answered says it was refused: no token sends it, or the
// token is gone.
func ownMCPTokenNotionTools(event *core.RequestEvent) (connectionID string, on, pageOn, answered bool, err error) {
	key, keyed := userKeyOf(event)
	if !keyed || key.AppInstallID != "" {
		return "", false, false, true, apiError(event, http.StatusNotFound, errorCodeMCPConnectionMissing,
			"No MCP token sends this: an agent sends it with its own token.")
	}
	stored := struct {
		On     int64 `db:"notion_tools"`
		PageOn int64 `db:"notion_only_tools"`
	}{}
	err = event.App.DB().NewQuery(`SELECT [[notion_tools]], [[notion_only_tools]] FROM {{leafen_mcp_connections}}
		WHERE [[id]] = {:id} AND [[revoked_at]] = 0 AND [[app_install_id]] = ''`).
		Bind(dbx.Params{"id": key.ConnectionID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, false, true, apiError(event, http.StatusNotFound, errorCodeMCPConnectionMissing, "MCP connection not found.")
	}
	if err != nil {
		return "", false, false, true, mcpTokenSettingsFailure(event, err)
	}
	return key.ConnectionID, stored.On == 1, stored.PageOn == 1, false, nil
}

func mcpNotionToolsViewOf(on, pageOn bool) mcpNotionToolsView {
	view := mcpNotionToolsView{NotionTools: on, Next: mcpNotionToolsOffNext}
	if on {
		view.Next = mcpNotionToolsOnNext
	}
	if pageOn {
		view.Next += mcpNotionToolsPageNext
	}
	return view
}

func readOwnMCPTokenNotionTools(event *core.RequestEvent) error {
	_, on, pageOn, answered, err := ownMCPTokenNotionTools(event)
	if answered {
		return err
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, mcpNotionToolsViewOf(on, pageOn))
}

func setOwnMCPTokenNotionTools(event *core.RequestEvent) error {
	connectionID, _, pageOn, answered, err := ownMCPTokenNotionTools(event)
	if answered {
		return err
	}
	body, err := io.ReadAll(io.LimitReader(event.Request.Body, mcpNotionToolsBodyBytes+1))
	input := mcpNotionToolsInput{}
	if err != nil || len(body) > mcpNotionToolsBodyBytes || json.Unmarshal(body, &input) != nil || input.NotionTools == nil {
		return apiError(event, http.StatusBadRequest, errorCodeMCPTokenInvalid, `Send {"notionTools": true} or {"notionTools": false}.`)
	}
	if _, err := event.App.DB().Update(mcpConnectionsTable, dbx.Params{"notion_tools": *input.NotionTools},
		dbx.HashExp{"id": connectionID}).Execute(); err != nil {
		return mcpTokenSettingsFailure(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, mcpNotionToolsViewOf(*input.NotionTools, pageOn))
}

// Migration 834 adds the setting, off for every connection.

func ensureNativeMCPNotionToolsSchema(app core.App) error {
	if _, err := app.DB().NewQuery(`
		ALTER TABLE {{leafen_mcp_connections}} ADD COLUMN [[notion_tools]] INTEGER NOT NULL DEFAULT 0
			CHECK ([[notion_tools]] IN (0, 1));
	`).Execute(); err != nil {
		return err
	}
	return validateNativeMCPNotionToolsSchema(app)
}

func validateNativeMCPNotionToolsSchema(app core.App) error {
	var columns int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM pragma_table_info('leafen_mcp_connections')
		WHERE [[name]] = 'notion_tools' AND [[notnull]] = 1 AND [[dflt_value]] = '0'`).Row(&columns); err != nil {
		return err
	}
	if columns != 1 {
		return errors.New(errorCodeSchemaMigrationHistoryInvalid)
	}
	return nil
}
