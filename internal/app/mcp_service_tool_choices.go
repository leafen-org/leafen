package app

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's custom MCP connections (help "MCP connections for Custom Agents",
// read 2026-10-12 and 2026-10-13): each of a connection's tools has a toggle
// and runs "자동 실행" or "항상 묻기" — write tools ask by default — and
// "항상 허용" lets all of a server's tools run without asking. The person who
// connected the service changes them, on their screen or through a key of
// theirs in the conversation, which asks them before a tool runs without
// asking (connection_settings_asks.go). A tool turned off is neither listed to agents
// nor run; a tool left as it was keeps Leafen's rule — one the service
// doesn't mark read-only or harmless asks first.

const (
	mcpServiceRunAuto = "auto"
	mcpServiceRunAsk  = "ask"
)

// MCPServiceToolSetting is a service's tool with how its person set it:
// Run is how it runs ("auto" or "ask") when the service isn't always allowed.
type MCPServiceToolSetting struct {
	MCPServiceTool
	Enabled bool   `json:"enabled"`
	Run     string `json:"run"`
}

// mcpServiceToolRun is how the tool runs by its person's choice, or else by
// Leafen's rule.
func mcpServiceToolRun(tool MCPServiceTool, chosen string) string {
	if chosen == mcpServiceRunAuto || chosen == mcpServiceRunAsk {
		return chosen
	}
	if tool.Destructive {
		return mcpServiceRunAsk
	}
	return mcpServiceRunAuto
}

// mcpServiceToolChoice is the person's choice for one tool: on, and the run
// chosen ("" for Leafen's rule).
func mcpServiceToolChoice(app core.App, serviceID, tool string) (bool, string, error) {
	choice := struct {
		Enabled bool   `db:"enabled"`
		Run     string `db:"run"`
	}{}
	err := app.DB().NewQuery(`SELECT [[enabled]], [[run]] FROM {{leafen_mcp_service_tool_choices}}
		WHERE [[service_id]] = {:service} AND [[tool]] = {:tool}`).Bind(dbx.Params{"service": serviceID, "tool": tool}).One(&choice)
	if errors.Is(err, sql.ErrNoRows) {
		return true, "", nil
	}
	return choice.Enabled, choice.Run, err
}

func setMCPServiceToolChoice(app core.App, serviceID, tool string, enabled *bool, run *string) error {
	on, chosen, err := mcpServiceToolChoice(app, serviceID, tool)
	if err != nil {
		return err
	}
	if enabled != nil {
		on = *enabled
	}
	if run != nil {
		if *run != mcpServiceRunAuto && *run != mcpServiceRunAsk {
			return errMCPServiceInvalid
		}
		chosen = *run
	}
	_, err = app.DB().NewQuery(`INSERT INTO {{leafen_mcp_service_tool_choices}} ([[service_id]], [[tool]], [[enabled]], [[run]], [[updated_at]])
		VALUES ({:service}, {:tool}, {:enabled}, {:run}, {:now})
		ON CONFLICT ([[service_id]], [[tool]]) DO UPDATE SET [[enabled]] = excluded.[[enabled]], [[run]] = excluded.[[run]],
			[[updated_at]] = excluded.[[updated_at]]`).Bind(dbx.Params{
		"service": serviceID, "tool": tool, "enabled": on, "run": chosen, "now": time.Now().UnixMilli(),
	}).Execute()
	return err
}

// setMCPServiceAlwaysAllowed lets all the service's tools run without asking, or ask again as each is set.
func setMCPServiceAlwaysAllowed(app core.App, userID, serviceID string, allowed bool) error {
	result, err := app.DB().NewQuery(`UPDATE {{leafen_mcp_services}} SET [[always_allow]] = {:allowed}, [[updated_at]] = {:now}
		WHERE [[id]] = {:id} AND [[user_id]] = {:user}`).Bind(dbx.Params{
		"allowed": allowed, "now": time.Now().UnixMilli(), "id": serviceID, "user": userID,
	}).Execute()
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return errMCPServiceNotFound
	}
	return nil
}

// The tools' paths start with "tools": "{serviceId}/tools" would read as an
// approval's address ("approvals/{approvalId}").
func registerMCPServiceToolChoiceRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/mcp-services/tools/{serviceId}", listMCPServiceToolsRoute)
	event.Router.PATCH(apiRoot+"/mcp-services/tools/{serviceId}/{tool}", setMCPServiceToolRoute).Bind(apis.BodyLimit(1024))
	event.Router.PATCH(apiRoot+"/mcp-services/{serviceId}", setMCPServiceRunRoute).Bind(apis.BodyLimit(1024))
}

// listMCPServiceToolsRoute is a window of the service's tools, as its person sets them.
func listMCPServiceToolsRoute(event *core.RequestEvent) error {
	// Reading a service's tools changes nothing: the person's agent reads
	// them too.
	personID, ok := mcpServiceRequestUser(event)
	if !ok {
		return nil
	}
	stored, err := loadMCPService(event.App, personID, event.Request.PathValue("serviceId"))
	if err != nil {
		return mcpServiceFailure(event, err)
	}
	from := 0
	if cursor := event.Request.URL.Query().Get("cursor"); cursor != "" {
		if from, err = strconv.Atoi(cursor); err != nil || from <= 0 {
			return mcpServiceFailure(event, errMCPServiceInvalid)
		}
	}
	tools, next, err := mcpServiceToolWindow(event.App, stored.ID, from, mcpServicesWindow, false)
	if err != nil {
		return mcpServiceFailure(event, err)
	}
	answer := map[string]any{"service": stored.view(), "tools": tools}
	if next > 0 {
		answer["nextCursor"] = strconv.Itoa(next)
	}
	return event.JSON(http.StatusOK, answer)
}

func setMCPServiceToolRoute(event *core.RequestEvent) error {
	personID, ok := mcpServiceRequestUser(event)
	if !ok {
		return nil
	}
	stored, err := loadMCPService(event.App, personID, event.Request.PathValue("serviceId"))
	if err != nil {
		return mcpServiceFailure(event, err)
	}
	input := struct {
		Enabled *bool   `json:"enabled"`
		Run     *string `json:"run"`
	}{}
	if event.BindBody(&input) != nil || input.Enabled == nil && input.Run == nil {
		return mcpServiceFailure(event, errMCPServiceInvalid)
	}
	tool, found, err := mcpServiceToolNamed(event.App, stored.ID, event.Request.PathValue("tool"))
	if err != nil {
		return mcpServiceFailure(event, err)
	}
	if !found {
		return mcpServiceFailure(event, errMCPServiceNotFound)
	}
	if input.Run != nil && *input.Run != mcpServiceRunAuto && *input.Run != mcpServiceRunAsk {
		return mcpServiceFailure(event, errMCPServiceInvalid)
	}
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	if err := setMCPServiceToolChoice(event.App, stored.ID, tool.Name, input.Enabled, input.Run); err != nil {
		return mcpServiceFailure(event, err)
	}
	enabled, chosen, err := mcpServiceToolChoice(event.App, stored.ID, tool.Name)
	if err != nil {
		return mcpServiceFailure(event, err)
	}
	return event.JSON(http.StatusOK, MCPServiceToolSetting{MCPServiceTool: tool, Enabled: enabled, Run: mcpServiceToolRun(tool, chosen)})
}

// setMCPServiceRunRoute sets whether all the service's tools run without asking.
func setMCPServiceRunRoute(event *core.RequestEvent) error {
	personID, ok := mcpServiceRequestUser(event)
	if !ok {
		return nil
	}
	input := struct {
		AlwaysAllow *bool `json:"alwaysAllow"`
	}{}
	if event.BindBody(&input) != nil || input.AlwaysAllow == nil {
		return mcpServiceFailure(event, errMCPServiceInvalid)
	}
	serviceID := event.Request.PathValue("serviceId")
	if _, err := loadMCPService(event.App, personID, serviceID); err != nil {
		return mcpServiceFailure(event, err)
	}
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	if err := setMCPServiceAlwaysAllowed(event.App, personID, serviceID, *input.AlwaysAllow); err != nil {
		return mcpServiceFailure(event, err)
	}
	stored, err := loadMCPService(event.App, personID, serviceID)
	if err != nil {
		return mcpServiceFailure(event, err)
	}
	return event.JSON(http.StatusOK, mcpServiceAnswer{Service: stored.view()})
}

// mcpServiceToolOff is the agent's answer for a tool its person turned off:
// an agent connection turns it on itself, as its person's screen does
// (PATCH /mcp-services/tools/{serviceId}/{tool}; 2026-11-26: no way is
// their screen's alone); an MCP token hears that takes an agent connection
// (connection_kind_ways.go); an app hears where its person turns it on, in
// their screen's words (mcp_screen_names.go).
func mcpServiceToolOff(kind connectionKind, serviceID, key, tool, language string) error {
	switch kind {
	case connectionKindAgent:
		return fmt.Errorf("%w: %s's tool %q is turned off by the person; turn it back on yourself with leafen_call PATCH "+
			"/mcp-services/tools/%s/%s with {\"enabled\": true}", errMCPToolInvalid, key, tool, serviceID, url.PathEscape(tool))
	case connectionKindToken:
		return fmt.Errorf("%w: %s's tool %q is turned off by the person; %s", errMCPToolInvalid, key, tool, mcpTokenNeedsAgent("turning it back on"))
	}
	return fmt.Errorf("%w: %s's tool %q is turned off by the person; they turn it on in %s", errMCPToolInvalid, key, tool,
		mcpScreen(language, "settings", "features", "connections"))
}
