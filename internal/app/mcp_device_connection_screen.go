package app

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// An agent's request to connect (mcp_device_connections.go) may also be
// allowed on Leafen's own screen — in its phone app's settings, where a
// person with no agent connected yet is — with the code the agent showed:
// signed in themselves, never with a key, the screen's confirm is their
// yes, as for what they do there. The connection reaches all their
// workspaces with everything they may do, as `leafen connect` makes one.

func registerMCPDeviceConnectionScreenRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+mcpDeviceConnectionPath+"/find", findMCPDeviceConnectionOnScreen)
	event.Router.POST(apiRoot+mcpDeviceConnectionPath+"/decide", decideMCPDeviceConnectionOnScreen)
}

type mcpDeviceConnectionScreenInput struct {
	Code  string `json:"code"`
	Allow bool   `json:"allow"`
}

type mcpDeviceConnectionScreenView struct {
	Name      string `json:"name"`
	From      string `json:"from"`
	AskedAt   int64  `json:"askedAt"`
	ExpiresAt int64  `json:"expiresAt"`
	Question  string `json:"question"`
	Effect    string `json:"effect"`
}

// mcpDeviceConnectionOnScreen is the request with the code typed and what
// allowing it gives, for the person signed in; handled when it answered.
func mcpDeviceConnectionOnScreen(event *core.RequestEvent, input *mcpDeviceConnectionScreenInput) (mcpDeviceConnection, mcpDevicePlan, personAsk, bool, error) {
	user, ok, err := signedInPerson(event)
	if !ok {
		return mcpDeviceConnection{}, mcpDevicePlan{}, personAsk{}, true, err
	}
	_ = event.BindBody(input)
	limiter := mcpDeviceConnectionLimiterOf(event.App)
	now := time.Now()
	tooMany := func() error {
		event.Response.Header().Set("Retry-After", "900")
		apiErrorBecause(event, "", mcpDeviceRefusedTooManyWrongCodes)
		return apiError(event, http.StatusTooManyRequests, errorCodeMCPDeviceConnectionLimited,
			"Too many wrong codes were tried. Wait a while and try again.")
	}
	if admitted, _ := limiter.wrongCodes.admit("person:"+user.Id, now); !admitted {
		return mcpDeviceConnection{}, mcpDevicePlan{}, personAsk{}, true, tooMany()
	}
	if admitted, _ := limiter.serverWrongs.admit("server", now); !admitted {
		limiter.wrongCodes.refund("person:" + user.Id)
		return mcpDeviceConnection{}, mcpDevicePlan{}, personAsk{}, true, tooMany()
	}
	request, found, err := findWaitingMCPDeviceConnection(event.App, input.Code)
	if err != nil {
		return request, mcpDevicePlan{}, personAsk{}, true, apiErrorFrom(event, http.StatusInternalServerError, errorCodeMCPDeviceConnectionFailed,
			"Couldn't read the request to connect.", err)
	}
	if !found {
		return request, mcpDevicePlan{}, personAsk{}, true, mcpDeviceScreenRefused(event, http.StatusNotFound, mcpDeviceRefusedCodeUnknown,
			"No agent waits with this code: it may be mistyped, used, or past its ten minutes.")
	}
	limiter.wrongCodes.refund("person:" + user.Id)
	limiter.serverWrongs.refund("server")
	connect, err := planAgentConnection(event.App, request.ClientName, "", user.Id)
	if errors.Is(err, errAgentConnectNoWorkspace) {
		return request, mcpDevicePlan{}, personAsk{}, true, mcpDeviceScreenRefused(event, http.StatusConflict, mcpDeviceRefusedNoWorkspace,
			"You have no workspace for an agent to reach yet.")
	}
	if err != nil {
		return request, mcpDevicePlan{}, personAsk{}, true, apiErrorFrom(event, http.StatusInternalServerError, errorCodeMCPDeviceConnectionFailed,
			"Couldn't read the workspaces an agent would reach.", err)
	}
	plan := mcpDevicePlan{OwnerID: user.Id}
	for _, row := range connect.Workspaces {
		plan.WorkspaceIDs = append(plan.WorkspaceIDs, row.ID)
	}
	return request, plan, mcpDeviceAsk(event.Request.Context(), event.App, request, plan), false, nil
}

// findMCPDeviceConnectionOnScreen shows the request with the code typed.
func findMCPDeviceConnectionOnScreen(event *core.RequestEvent) error {
	request, _, ask, handled, err := mcpDeviceConnectionOnScreen(event, &mcpDeviceConnectionScreenInput{})
	if handled {
		return err
	}
	// The question's head is the confirm's title; where it asked from leads its body.
	lines := strings.SplitN(ask.Question, "\n", 2)
	effect := ask.Effect
	if len(lines) == 2 {
		effect = lines[1] + " " + effect
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, mcpDeviceConnectionScreenView{Name: request.ClientName, From: request.RequestIP,
		AskedAt: request.CreatedAt, ExpiresAt: request.ExpiresAt, Question: lines[0], Effect: effect})
}

// decideMCPDeviceConnectionOnScreen keeps the person's answer to it.
func decideMCPDeviceConnectionOnScreen(event *core.RequestEvent) error {
	input := mcpDeviceConnectionScreenInput{}
	request, plan, _, handled, err := mcpDeviceConnectionOnScreen(event, &input)
	if handled {
		return err
	}
	if input.Allow {
		if err := checkMCPDevicePlan(event.App, request, plan); err != nil {
			return mcpDeviceScreenRefusal(event, err)
		}
	}
	err = decideMCPDeviceConnection(event.App, request, input.Allow, plan, "", "screen")
	if errors.Is(err, errMCPDeviceConnectionMissing) {
		return mcpDeviceScreenRefused(event, http.StatusNotFound, mcpDeviceRefusedDecidedMeanwhile, "The request ran out or was decided meanwhile.")
	}
	if cause, said := mcpDeviceRefusalOf(err); cause != "" {
		// Allowing widens at once, checked again as it does (mcp_device_connection_widen.go).
		return mcpDeviceScreenRefused(event, http.StatusForbidden, cause, said)
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeMCPDeviceConnectionFailed, "Couldn't keep the answer.", err)
	}
	return event.NoContent(http.StatusNoContent)
}
