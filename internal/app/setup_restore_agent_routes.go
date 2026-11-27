package app

import (
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// The routes of a first-setup restore left to an agent (setup_restore_agent.go):
// the screen's — making the restore code, following the restore, sending
// what the person typed, going back — each let through as the first setup
// is; and the agent's — status, find, run — each opened by the restore code
// in its body.

type setupRestoreAgentScreenInput struct {
	SetupCode string `json:"setupCode"`
	// The outside way the agent elsewhere needs: "" for an agent on a
	// computer of the server's network, "quick" or "tailscale".
	Outside string `json:"outside"`
	setupRestoreAgentSecrets
}

type setupRestoreAgentBegun struct {
	Code      string `json:"code"`
	SignIn    string `json:"signIn"`
	ExpiresAt int64  `json:"expiresAt"`
}

// setupRestoreAgentScreen is what the screen shows of the restore.
type setupRestoreAgentScreen struct {
	Active    bool                    `json:"active"`
	Ended     string                  `json:"ended,omitempty"`
	ExpiresAt int64                   `json:"expiresAt,omitempty"`
	Outside   *setupRestoreOutside    `json:"outside,omitempty"`
	Place     *setupRestoreAgentPlace `json:"place,omitempty"`
	Found     *setupRestoreAgentFound `json:"found,omitempty"`
	Typed     setupRestoreAgentTyped  `json:"typed"`
	AgentSeen bool                    `json:"agentSeen,omitzero"`
	Running   bool                    `json:"running,omitempty"`
	Failure   string                  `json:"failure,omitempty"`
	Restart   string                  `json:"restart,omitempty"`
	// How far the agent's restore has got, while it goes (backup_progress.go).
	Progress *administratorBackupProgress `json:"progress,omitempty"`
	// The address agents anywhere already reach the server at, when it has one.
	PublicAddress string `json:"publicAddress,omitempty"`
}

type setupRestoreAgentCall struct {
	RestoreCode string `json:"restoreCode"`
	setupRestoreAgentPlace
	Choice string `json:"choice"`
}

func registerSetupRestoreAgentRoutes(event *core.ServeEvent) {
	event.Router.POST(setupRestoreAgentPath, beginSetupRestoreAgent)
	event.Router.POST(setupRestoreAgentPath+"/state", readSetupRestoreAgentState)
	event.Router.POST(setupRestoreAgentPath+"/secrets", typeSetupRestoreAgentSecrets)
	event.Router.POST(setupRestoreAgentPath+"/end", endSetupRestoreAgent)
	event.Router.POST(setupRestoreAgentPath+"/status", callSetupRestoreAgentStatus)
	event.Router.POST(setupRestoreAgentPath+"/find", callSetupRestoreAgentFind)
	event.Router.POST(setupRestoreAgentPath+"/run", callSetupRestoreAgentRun)
	registerSetupRestoreAgentMCP(event)
	registerSetupRestoreOutsideScope(event)
}

// admitSetupRestoreAgentScreen binds the screen's input and lets it through as the first setup.
func admitSetupRestoreAgentScreen(event *core.RequestEvent) (setupRestoreAgentScreenInput, bool) {
	input := setupRestoreAgentScreenInput{}
	if err := event.BindBody(&input); err != nil {
		_ = apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the restore request.")
		return input, false
	}
	return input, admitFirstSetup(event, input.SetupCode)
}

func beginSetupRestoreAgent(event *core.RequestEvent) error {
	input, ok := admitSetupRestoreAgentScreen(event)
	if !ok {
		return nil
	}
	outside := strings.TrimSpace(input.Outside)
	switch outside {
	case "", serverTunnelQuick:
	case serverTunnelTailscale:
		if !tailscaleBuiltIn {
			return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Tailscale isn't built into Leafen here.")
		}
	default:
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Choose no outside way, a one-time link or Tailscale.")
	}
	if outside != "" && setupRestorePublicAddress(event.Request) != "" {
		// Reached from anywhere already: no other way is opened.
		outside = ""
	}
	code, pass, expires, err := setupRestoreAgentOf(event.App).begin(event.App, outside, time.Now())
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeScheduledBackupRestoreFailed, "Couldn't make the restore code.", err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, setupRestoreAgentBegun{Code: code, SignIn: pass, ExpiresAt: expires.UnixMilli()})
}

func (keeper *setupRestoreAgentKeeper) screen(app core.App, request *http.Request) setupRestoreAgentScreen {
	keeper.mu.Lock()
	defer keeper.mu.Unlock()
	session := keeper.session
	if session == nil {
		return setupRestoreAgentScreen{Ended: keeper.ended, PublicAddress: setupRestorePublicAddress(request)}
	}
	status := session.status()
	view := setupRestoreAgentScreen{Active: true, ExpiresAt: status.ExpiresAt, Place: status.Place, Found: status.Found,
		Typed: status.Typed, AgentSeen: session.agentSeen, Running: session.running, Failure: session.failure,
		Restart: session.restart, PublicAddress: setupRestorePublicAddress(request)}
	if session.running {
		view.Progress = runningBackupProgress(app, backupProgressStoreKey).snapshot()
	}
	if session.outside != "" {
		view.Outside = setupRestoreOutsideOf(app, session.outside)
	}
	return view
}

func readSetupRestoreAgentState(event *core.RequestEvent) error {
	if _, ok := admitSetupRestoreAgentScreen(event); !ok {
		return nil
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, setupRestoreAgentOf(event.App).screen(event.App, event.Request))
}

// typeSetupRestoreAgentSecrets keeps what the person typed for the agent's
// restore; an empty field leaves what was typed before.
func typeSetupRestoreAgentSecrets(event *core.RequestEvent) error {
	input, ok := admitSetupRestoreAgentScreen(event)
	if !ok {
		return nil
	}
	typed := input.setupRestoreAgentSecrets
	for _, value := range []string{typed.WebDAVPassword, typed.S3AccessKey, typed.S3SecretKey, typed.Secret} {
		if len(value) > maxScheduledBackupPasswordLength {
			return apiError(event, http.StatusBadRequest, errorCodeScheduledBackupInputInvalid, "That is longer than a password or key can be.")
		}
	}
	keeper := setupRestoreAgentOf(event.App)
	keeper.mu.Lock()
	if session := keeper.session; session != nil {
		keep := func(target *string, value string) {
			if value != "" {
				*target = value
			}
		}
		keep(&session.secrets.WebDAVPassword, typed.WebDAVPassword)
		keep(&session.secrets.S3AccessKey, strings.TrimSpace(typed.S3AccessKey))
		keep(&session.secrets.S3SecretKey, typed.S3SecretKey)
		keep(&session.secrets.Secret, typed.Secret)
	}
	keeper.mu.Unlock()
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, keeper.screen(event.App, event.Request))
}

func endSetupRestoreAgent(event *core.RequestEvent) error {
	if _, ok := admitSetupRestoreAgentScreen(event); !ok {
		return nil
	}
	keeper := setupRestoreAgentOf(event.App)
	keeper.mu.Lock()
	restoring := keeper.session != nil && (keeper.session.running || keeper.session.restart != "")
	keeper.mu.Unlock()
	if !restoring {
		keeper.end(event.App, setupRestoreAgentEndedClosed)
	}
	return event.JSON(http.StatusOK, keeper.screen(event.App, event.Request))
}

// setupRestorePublicAddress is the address agents anywhere reach the server
// at already: the one written for it when outside any network, or this
// request's own when it came from outside to an address of the internet.
func setupRestorePublicAddress(request *http.Request) string {
	if written := configuredServerAddress(); outsideOrigin(written) {
		return written
	}
	if request == nil || requestTunnelOrigin(request) != "" || !hostOutsideNetwork(request.Host) {
		return ""
	}
	if requestScheme(request) == "https" {
		return "https://" + request.Host
	}
	return ""
}

// The agent's JSON API, beside its MCP tools.

func admitSetupRestoreAgentCall(event *core.RequestEvent) (setupRestoreAgentCall, *setupRestoreAgentSession, bool) {
	call := setupRestoreAgentCall{}
	if err := event.BindBody(&call); err != nil {
		_ = apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Send JSON with the restore code as restoreCode.")
		return call, nil, false
	}
	session, err := setupRestoreAgentOf(event.App).admit(event.Request.Context(), event.App, call.RestoreCode, false)
	if err != nil {
		_ = answerSetupRestoreError(event, err)
		return call, nil, false
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return call, session, true
}

func callSetupRestoreAgentStatus(event *core.RequestEvent) error {
	call := setupRestoreAgentCall{}
	if err := event.BindBody(&call); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Send JSON with the restore code as restoreCode.")
	}
	status, err := setupRestoreAgentOf(event.App).statusFor(event.Request.Context(), event.App, call.RestoreCode)
	if err != nil {
		return answerSetupRestoreError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, status)
}

type setupRestoreAgentFindResult struct {
	setupRestorePlace
	Status setupRestoreAgentStatus `json:"status"`
}

func callSetupRestoreAgentFind(event *core.RequestEvent) error {
	call, session, ok := admitSetupRestoreAgentCall(event)
	if !ok {
		return nil
	}
	place, status, err := setupRestoreAgentOf(event.App).find(event.Request.Context(), event.App, session, call.setupRestoreAgentPlace)
	if err != nil {
		return answerSetupRestoreError(event, err)
	}
	return event.JSON(http.StatusOK, setupRestoreAgentFindResult{setupRestorePlace: place, Status: status})
}

func callSetupRestoreAgentRun(event *core.RequestEvent) error {
	call, session, ok := admitSetupRestoreAgentCall(event)
	if !ok {
		return nil
	}
	status, err := setupRestoreAgentOf(event.App).run(event.App, session, call.Choice)
	if err != nil {
		return answerSetupRestoreError(event, err)
	}
	return event.JSON(http.StatusAccepted, status)
}
