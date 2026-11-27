package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"leafen.org/leafen/internal/managedsettings"
)

// FirstRun is the app's first start (the multi-server decision of
// 2026-10-19): it asks before anything — signing in to a Leafen used already
// or starting new on this computer, which makes this computer a server — and
// starts its own server only when chosen — on a phone, Leafen tried alone
// on the phone (the trial of 2026-10-20, back 2026-11-01). The steps after the
// name and the language are the server's (internal/app/first_run_state.go).
// An install that already has its own data (the app used before) keeps it
// and is never asked. An agent going through the first run from a terminal
// (`leafen-desktop setup`, user decision 2026-11-09) chooses as the person
// told it: its request, beside the app's own data, shows to the window,
// which goes on as if its button were pressed. Signing in with a one-time
// code the person got from a device signed in there (user decision
// 2026-11-13: the whole first run in the agent's conversation), the window
// signs in itself and answers how it went, which the command says; past the
// first screen, one of the app's windows adds that Leafen the same way, as
// its account menu's "Add an account" would (TakeAgentSignIn).
type FirstRun struct {
	mu      sync.Mutex
	local   *localServer
	state   firstRunState
	started bool
	// The sign-in request a window took past the first screen (its at), so
	// that one window alone signs in with its code.
	taken int64
}

type firstRunState struct {
	// Kept from when the steps were the app's: an install whose first run
	// ended shows no first screen.
	Done bool `json:"done"`
	// "local" (this computer serves), "server" (connected to one), or "".
	Choice string `json:"choice"`
	// How the window's sign-in with an agent's code went, for its command to say.
	AgentSignIn *agentSignIn `json:"agentSignIn,omitempty"`
}

// agentSignIn is the window's answer to an agent's sign-in request (its
// at): the account it signed in as, or why it was refused — "address" or
// "plain" for an address the app can't use, "keep" where the sign-in
// couldn't be kept, else the server's error code.
type agentSignIn struct {
	At      int64  `json:"at"`
	Account string `json:"account,omitempty"`
	Refused string `json:"refused,omitempty"`
}

// firstRunRequest is what an agent's `leafen-desktop setup` chose for the
// first screen (internal/app/setup_command.go writes it).
type firstRunRequest struct {
	At        int64  `json:"at"`
	StartHere bool   `json:"startHere,omitempty"`
	SignIn    string `json:"signIn,omitempty"`
	// The one-time code to sign in at SignIn with, and whether that may sign
	// out the account's least recently used sign-in where it is at its limit.
	Code             string `json:"code,omitempty"`
	SignOutLeastUsed bool   `json:"signOutLeastUsed,omitempty"`
}

const (
	// firstRunRequestLasting is how long a request waits for the window.
	firstRunRequestLasting = 30 * time.Minute
	// The request file `setup` leaves beside the app's data.
	setupRequestName = "setup-request.json"
)

// FirstRunState is what the window is told.
type FirstRunState struct {
	firstRunState
	// Whether this start shows the first screens.
	Pending bool `json:"pending"`
	// A phone asks fewer things (no agents, no address, no sleep).
	Phone bool `json:"phone"`
	// What the computer's organization set (managed_settings.go): its
	// server filled in and the only choice.
	Managed managedsettings.Settings `json:"managed"`
	// Whether this computer serves a Leafen of its own (it has data, or was
	// chosen to): another server's sign-in offers the way back to it.
	OwnServer bool `json:"ownServer"`
	// What an agent chose from a terminal, while the first screen shows.
	Request *firstRunRequest `json:"request,omitempty"`
}

func newFirstRun(local *localServer) *FirstRun {
	run := &FirstRun{local: local}
	if path, err := firstRunFile(); err == nil {
		if data, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(data, &run.state)
		}
	}
	return run
}

func firstRunFile() (string, error) {
	local, err := localDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(local), "first-run.json"), nil
}

// hasLocalData says whether the app's own server has data already.
func hasLocalData() bool {
	dir, err := localDataDir()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, "data.db"))
	return err == nil
}

// startsLocal says whether the app's own server starts now: it has data, or
// this computer was chosen to serve.
func (run *FirstRun) startsLocal() bool {
	run.mu.Lock()
	defer run.mu.Unlock()
	return hasLocalData() || run.state.Choice == "local"
}

// idleState is what the window's local profile says while the app's own
// server is off: the first screens, or nothing to start (connected to a
// server).
func (run *FirstRun) idleState() string {
	run.mu.Lock()
	defer run.mu.Unlock()
	if run.started {
		return ""
	}
	if run.state.Choice == "" && !run.state.Done {
		return "first-run"
	}
	return "off"
}

func (run *FirstRun) save() error {
	path, err := firstRunFile()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(run.state, "", "  ")
	if err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

// startLocal starts the app's own server once.
func (run *FirstRun) startLocal() {
	run.mu.Lock()
	if run.started {
		run.mu.Unlock()
		return
	}
	run.started = true
	run.mu.Unlock()
	run.local.launch()
}

// State says whether the first screens show, what was chosen, and what an
// agent chose for the person while they show.
func (run *FirstRun) State() FirstRunState {
	run.mu.Lock()
	defer run.mu.Unlock()
	pending := !run.state.Done && run.state.Choice == "" && !hasLocalData()
	state := FirstRunState{firstRunState: run.state, Pending: pending, Phone: runtime.GOOS == "ios" || runtime.GOOS == "android",
		Managed: managedSettings(), OwnServer: hasLocalData() || run.state.Choice == "local"}
	if pending {
		state.Request = agentFirstRunRequest()
		// One the window answered already isn't taken again.
		if answer := run.state.AgentSignIn; state.Request != nil && answer != nil && answer.At >= state.Request.At {
			state.Request = nil
		}
	}
	return state
}

// TakeAgentSignIn hands an agent's sign-in with a code, once, to the window
// that asks first, past the first screen (while it shows, State hands it to
// the first screen): that window adds the Leafen and answers how it went.
func (run *FirstRun) TakeAgentSignIn() *firstRunRequest {
	run.mu.Lock()
	defer run.mu.Unlock()
	if !run.state.Done && run.state.Choice == "" && !hasLocalData() {
		return nil
	}
	request := agentFirstRunRequest()
	if request == nil || request.SignIn == "" || request.Code == "" || request.At <= run.taken ||
		run.state.AgentSignIn != nil && run.state.AgentSignIn.At >= request.At {
		return nil
	}
	run.taken = request.At
	return request
}

// AnswerAgentSignIn is the window's answer to an agent's sign-in with a
// code, the request's at: signed in as account — on the first screen that
// makes this computer a server's terminal, as ConnectedToServer does — or
// refused and why. Either way the code leaves the request file.
func (run *FirstRun) AnswerAgentSignIn(at int64, account, refused string) error {
	forgetAgentCode(at)
	run.mu.Lock()
	defer run.mu.Unlock()
	run.state.AgentSignIn = &agentSignIn{At: at, Account: account, Refused: refused}
	if account != "" && run.state.Choice == "" {
		run.state.Choice = "server"
	}
	return run.save()
}

// forgetAgentCode takes the code out of the request it came in, once answered.
func forgetAgentCode(at int64) {
	dir, err := localDataDir()
	if err != nil {
		return
	}
	path := filepath.Join(dir, setupRequestName)
	data, err := os.ReadFile(path)
	request := map[string]json.RawMessage{}
	if err != nil || json.Unmarshal(data, &request) != nil || string(request["at"]) != strconv.FormatInt(at, 10) {
		return
	}
	if _, held := request["code"]; !held {
		return
	}
	delete(request, "code")
	delete(request, "signOutLeastUsed")
	if data, err = json.MarshalIndent(request, "", "  "); err != nil {
		return
	}
	if os.WriteFile(path+".tmp", data, 0o600) == nil {
		_ = os.Rename(path+".tmp", path)
	}
}

// setupSignedIn is the window's answer to the sign-in request made at at,
// as a terminal's `setup` reads it.
func setupSignedIn(at int64) (account, refused string, answered bool) {
	answer := newFirstRun(nil).state.AgentSignIn
	if answer == nil || answer.At != at {
		return "", "", false
	}
	return answer.Account, answer.Refused, true
}

// agentFirstRunRequest reads what an agent chose from a terminal, if it is recent.
func agentFirstRunRequest() *firstRunRequest {
	dir, err := localDataDir()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(dir, setupRequestName))
	if err != nil {
		return nil
	}
	request := firstRunRequest{}
	if json.Unmarshal(data, &request) != nil || (!request.StartHere && request.SignIn == "") ||
		time.Since(time.UnixMilli(request.At)) > firstRunRequestLasting {
		return nil
	}
	return &request
}

// setupChoice is the first screen's choice as a terminal's `setup` reads it:
// an install with its own data serves.
func setupChoice() string {
	if hasLocalData() {
		return "local"
	}
	return newFirstRun(nil).state.Choice
}

// StartHere makes this computer a server: its own server starts.
func (run *FirstRun) StartHere() error {
	// A computer whose organization names its server doesn't serve itself.
	if managedSettings().ServerURL != "" {
		return errManagedSetting
	}
	run.mu.Lock()
	run.state.Choice = "local"
	err := run.save()
	run.mu.Unlock()
	if err != nil {
		return err
	}
	run.startLocal()
	return nil
}

// ConnectedToServer notes that this computer is a server's terminal: its own
// server stays off until the person adds a workspace of their own here.
func (run *FirstRun) ConnectedToServer() error {
	run.mu.Lock()
	defer run.mu.Unlock()
	if run.state.Choice == "" {
		run.state.Choice = "server"
	}
	return run.save()
}
