package app

import (
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// The window's side of the first run's steps (first_run_state.go): which are
// left for the person who set the server up, a step done or left for later,
// the steps over, and those left for later brought back from Settings; while
// an agent goes through the first run by commands, that person's window
// follows it and opens the screen a command asked for. And, for a setup
// screen open while an agent sets the server up from its conversation (user
// decision 2026-11-13, first_run_person_ask.go), that it is set up now and,
// to the screen that holds the setup code, the one sign-in that lets it go
// on as the first administrator the person said yes to.

type setupStepsResponse struct {
	Steps   []string `json:"steps"`
	Skipped []string `json:"skipped"`
	Done    bool     `json:"done"`
	// For the setup person (owner): an agent's command was taken a short
	// while ago, and the screen the last one asked to open.
	Owner  bool       `json:"owner,omitempty"`
	Follow bool       `json:"follow,omitzero"`
	Open   *setupOpen `json:"open,omitempty"`
}

type setupStepInput struct {
	Step string `json:"step"`
	Done bool   `json:"done"`
}

type setupAgentStartInput struct {
	SetupCode string `json:"setupCode"`
	// The asking screen's own claim, if it sent one (setup_claims.go).
	Claim string `json:"claim,omitempty"`
}

type setupAgentStartResponse struct {
	Configured bool `json:"configured,omitempty"`
	// The asking screen's own claim set the server up: nothing elsewhere to follow.
	Own bool `json:"own,omitempty"`
	// A one-time device code that signs the setup screen in.
	SignIn string `json:"signIn,omitempty"`
}

func registerSetupRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/first-run", readSetupSteps)
	event.Router.POST(apiRoot+"/first-run/steps", passSetupStep)
	event.Router.POST(apiRoot+"/first-run/finish", finishSetupSteps)
	event.Router.POST(apiRoot+"/first-run/reopen", reopenSetupSteps)
	event.Router.POST(apiRoot+"/setup/agent-start", readSetupAgentStart)
}

// setupStepsFor answers with the steps the signed-in person has: none for
// anyone but whoever set the server up.
func setupStepsFor(event *core.RequestEvent, state setupState) error {
	user, _ := authenticatedUser(event)
	response := setupStepsResponse{Steps: []string{}, Skipped: []string{}, Done: true}
	if state.SetupBy == user.Id {
		response = setupStepsResponse{Steps: state.stepsLeft(), Skipped: state.Skipped, Done: state.Done}
	}
	if setupOwner(event.App, state) == user.Id {
		response.Owner = true
		response.Follow = state.following()
		if state.Open != nil && time.Since(time.UnixMilli(state.Open.At)) < setupRequestLasting {
			response.Open = state.Open
		}
	}
	return event.JSON(http.StatusOK, response)
}

func setupSignedIn(event *core.RequestEvent) bool {
	if _, ok := authenticatedUser(event); ok {
		return true
	}
	_ = apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	return false
}

func readSetupSteps(event *core.RequestEvent) error {
	if !setupSignedIn(event) {
		return nil
	}
	return setupStepsFor(event, setupKeeperOf(event.App).read())
}

func passSetupStep(event *core.RequestEvent) error {
	if !setupSignedIn(event) {
		return nil
	}
	input := setupStepInput{}
	if err := event.BindBody(&input); err != nil || !slices.Contains(setupStepNames, input.Step) {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the step.")
	}
	keeper := setupKeeperOf(event.App)
	user, _ := authenticatedUser(event)
	if keeper.read().SetupBy != user.Id {
		return setupStepsFor(event, keeper.read())
	}
	keeper.pass(event.App, input.Step, input.Done)
	return setupStepsFor(event, keeper.read())
}

func finishSetupSteps(event *core.RequestEvent) error {
	if !setupSignedIn(event) {
		return nil
	}
	user, _ := authenticatedUser(event)
	state := setupKeeperOf(event.App).change(event.App, func(state *setupState) {
		if state.SetupBy == user.Id {
			state.Done = true
		}
	})
	return setupStepsFor(event, state)
}

// reopenSetupSteps brings back the steps left for later; on a server set up
// before the steps were the server's, by its administrator.
func reopenSetupSteps(event *core.RequestEvent) error {
	if !setupSignedIn(event) {
		return nil
	}
	user, _ := authenticatedUser(event)
	administrator, err := isAdministrator(event.App, user.Id)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the setup state.", err)
	}
	state := setupKeeperOf(event.App).change(event.App, func(state *setupState) {
		if state.SetupBy == "" && administrator {
			state.SetupBy = user.Id
		}
		if state.SetupBy == user.Id {
			state.Skipped = []string{}
			state.Done = false
		}
	})
	return setupStepsFor(event, state)
}

// readSetupAgentStart tells a setup screen that the server is set up — by
// that screen's own claim, or elsewhere — and, where the screen holds the
// setup code (a server that takes none can't tell its setup person's screen
// from anyone's), gives it the sign-in an agent's confirmed name left, once.
func readSetupAgentStart(event *core.RequestEvent) error {
	input := setupAgentStartInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the setup code.")
	}
	configured, err := administratorConfigured(event.App)
	if err != nil || !configured {
		return event.JSON(http.StatusOK, setupAgentStartResponse{})
	}
	if claims := setupClaimsOf(event.App); claims.own(input.Claim) {
		return event.JSON(http.StatusOK, setupAgentStartResponse{Configured: true, Own: true})
	} else if claims.agentStarting() {
		// Not yet: the agent's start is still making what this screen goes on with.
		return event.JSON(http.StatusOK, setupAgentStartResponse{})
	}
	if !setupCodeRequired() || strings.TrimSpace(input.SetupCode) == "" {
		return event.JSON(http.StatusOK, setupAgentStartResponse{Configured: true})
	}
	state, address := setupCodeStateOf(event.App), "address:"+requestLimitAddress(event.RealIP())
	if admitted, _ := state.failures.admit(address, time.Now()); !admitted {
		event.Response.Header().Set("Retry-After", "900")
		return apiError(event, http.StatusTooManyRequests, errorCodeSetupCodeLimited, "Too many setup codes were tried. Wait a while and try again.")
	}
	if !setupCodeAccepts(event.App, input.SetupCode) {
		return event.JSON(http.StatusOK, setupAgentStartResponse{Configured: true})
	}
	state.failures.refund(address)
	return event.JSON(http.StatusOK, setupAgentStartResponse{Configured: true, SignIn: setupKeeperOf(event.App).takeHandoff(event.App)})
}
