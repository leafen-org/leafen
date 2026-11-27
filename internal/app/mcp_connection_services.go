package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// Which of the person's connected services each of their agent connections
// may use: every one (also those connected later), or only chosen ones. The
// connection's first instructions name them, so the agent uses them through
// Leafen rather than asking the person to set them up again.

const mcpServiceInstructionServices = 20

// mcpConnectionServiceWindow is a window of the person's services the
// connection may use, with their keys, which stay the same whatever the
// connection chose.
func mcpConnectionServiceWindow(app core.App, authority mcpAuthority, cursor string, limit int) ([]MCPService, map[string]string, string, error) {
	services, next, err := listMCPServiceWindow(app, authority.owner(), authority.ConnectionID, cursor, limit)
	if err != nil {
		return nil, nil, "", err
	}
	keys, err := mcpServiceKeysFor(app, authority.owner())
	if err != nil {
		return nil, nil, "", err
	}
	return services, keys, next, nil
}

// The services' short lines: with none, and with a count.
const mcpServicesNoneLine = "Outside services join Leafen once for all agents (leafen_services): suggest that."

func mcpServicesCountLine(count string) string {
	return "Via Leafen: " + count + " external services (leafen_services), no separate connectors."
}

// mcpServicesNotice is the instructions' paragraph naming the services the
// connection may use, or saying where the person connects them, and the
// line saying so in short, for instructions without room for the names.
func mcpServicesNotice(app core.App, authority mcpAuthority) (string, string) {
	services, keys, next, err := mcpConnectionServiceWindow(app, authority, "", mcpServiceInstructionServices)
	if err != nil {
		return "", ""
	}
	if len(services) == 0 {
		return "Outside services (remote MCP servers) connect to Leafen once, in its settings, " +
				"and every agent uses them through it (leafen_services): suggest that, not a separate connector.",
			mcpServicesNoneLine
	}
	count := fmt.Sprint(len(services))
	if next != "" {
		count = fmt.Sprintf("more than %d", mcpServiceInstructionServices)
	}
	line := mcpServicesCountLine(count)
	if next != "" {
		return line, line
	}
	lines := []string{}
	for _, service := range services {
		summary := mcpServiceSummaryOf(service, keys[service.ID])
		line := fmt.Sprintf("- %s — %s (%s), %d tools", summary.Service, summary.Name, summary.Address, summary.Tools)
		if service.Status != mcpServiceStatusConnected {
			line += mcpServiceNotConnected(mcpConnectionKind(authority), service)
		}
		lines = append(lines, line)
	}
	return "Outside services the person connected to Leafen, which you use through Leafen — Leafen logs in for them, so don't ask them " +
		"to set these up as separate connectors (see a service's tools with leafen_services, call one with leafen_service_call; " +
		"the names are the services' own):\n" + strings.Join(lines, "\n"), line
}

type mcpConnectionServiceView struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	Status  string `json:"status"`
	Tools   int    `json:"tools"`
	Chosen  bool   `json:"chosen"`
}

type mcpConnectionServicesView struct {
	Mode       string                     `json:"mode"`
	Services   []mcpConnectionServiceView `json:"services"`
	NextCursor string                     `json:"nextCursor,omitempty"`
}

func registerMCPConnectionServiceRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/mcp/connections/{connectionId}/services", func(event *core.RequestEvent) error {
		return mcpConnectionServicesRoute(event, false)
	})
	event.Router.PUT(apiRoot+"/mcp/connections/{connectionId}/services", func(event *core.RequestEvent) error {
		return mcpConnectionServicesRoute(event, true)
	}).Bind(apis.BodyLimit(16 << 10))
}

// mcpConnectionServicesRoute shows, a window at a time, or changes, one
// service at a time, for the connection's owner — signed in, or through a
// key of theirs (user_key_account_routes.go) — which of their services the
// connection uses.
func mcpConnectionServicesRoute(event *core.RequestEvent, change bool) error {
	personID, ok := mcpServiceRequestUser(event)
	if !ok {
		return nil
	}
	connectionID := event.Request.PathValue("connectionId")
	choice := ""
	if err := event.App.DB().NewQuery(`SELECT [[service_choice]] FROM {{leafen_mcp_connections}}
		WHERE [[id]] = {:id} AND [[owner_id]] = {:owner} AND [[revoked_at]] = 0 AND [[app_install_id]] = ''`).
		Bind(dbx.Params{"id": connectionID, "owner": personID}).Row(&choice); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return apiError(event, http.StatusNotFound, errorCodeMCPServiceNotFound, "No such connection.")
		}
		return mcpServiceFailure(event, err)
	}
	if change {
		input := struct {
			Mode      string `json:"mode"`
			ServiceID string `json:"serviceId"`
			Chosen    bool   `json:"chosen"`
		}{}
		if event.BindBody(&input) != nil || input.Mode != "all" && input.Mode != "chosen" {
			return apiError(event, http.StatusBadRequest, errorCodeMCPServiceInvalid, "Choose all services or the chosen ones.")
		}
		if input.ServiceID != "" {
			if _, err := loadMCPService(event.App, personID, input.ServiceID); err != nil {
				if errors.Is(err, errMCPServiceNotFound) {
					return apiError(event, http.StatusBadRequest, errorCodeMCPServiceInvalid, "Choose among your services.")
				}
				return mcpServiceFailure(event, err)
			}
		}
		if err := event.App.RunInTransaction(func(tx core.App) error {
			if _, err := tx.DB().Update("leafen_mcp_connections", dbx.Params{"service_choice": input.Mode}, dbx.HashExp{"id": connectionID}).Execute(); err != nil {
				return err
			}
			if input.ServiceID == "" {
				return nil
			}
			if !input.Chosen {
				_, err := tx.DB().Delete("leafen_mcp_connection_services", dbx.HashExp{"connection_id": connectionID, "service_id": input.ServiceID}).Execute()
				return err
			}
			_, err := tx.DB().NewQuery(`INSERT OR IGNORE INTO {{leafen_mcp_connection_services}} ([[connection_id]], [[service_id]], [[created]])
				VALUES ({:connection}, {:service}, {:now})`).Bind(dbx.Params{"connection": connectionID, "service": input.ServiceID, "now": time.Now().UnixMilli()}).Execute()
			return err
		}); err != nil {
			return mcpServiceFailure(event, err)
		}
		return event.JSON(http.StatusOK, mcpConnectionServicesView{Mode: input.Mode, Services: []mcpConnectionServiceView{}})
	}
	services, next, err := ListMCPServices(event.App, personID, event.Request.URL.Query().Get("cursor"))
	if err != nil {
		return mcpServiceFailure(event, err)
	}
	ids := make([]string, 0, len(services))
	for _, service := range services {
		ids = append(ids, service.ID)
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return mcpServiceFailure(event, err)
	}
	chosen := []string{}
	if err := event.App.DB().NewQuery(`SELECT [[service_id]] FROM {{leafen_mcp_connection_services}}
		WHERE [[connection_id]] = {:id} AND [[service_id]] IN (SELECT [[value]] FROM json_each({:ids}))`).
		Bind(dbx.Params{"id": connectionID, "ids": string(encoded)}).Column(&chosen); err != nil {
		return mcpServiceFailure(event, err)
	}
	view := mcpConnectionServicesView{Mode: "all", Services: []mcpConnectionServiceView{}, NextCursor: next}
	if choice == "chosen" {
		view.Mode = "chosen"
	}
	for _, service := range services {
		address, _ := parseMCPServiceAddress(service.URL)
		view.Services = append(view.Services, mcpConnectionServiceView{ID: service.ID, Name: service.Name, Address: address,
			Status: service.Status, Tools: service.ToolCount, Chosen: slices.Contains(chosen, service.ID)})
	}
	return event.JSON(http.StatusOK, view)
}
