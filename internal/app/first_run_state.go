package app

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
)

// The first run's steps after the name and the language — the outside
// address, then how outside agents connect — are the server's, for whoever
// set it up (user decision 2026-11-09: the screen and an agent's commands go
// through the same steps, and whichever does one, the other sees it done;
// 2026-11-13: an agent's conversation alone can go through all of them, the
// name included, first_run_person_ask.go).
// The running server alone writes them, in the data folder's setup state
// file; `leafen setup` (setup_command.go) asks it things through the request
// file beside it, which the server follows within seconds, as `leafen
// tailscale` asks through the database. Neither file holds a secret but the
// one-time sign-in code an agent hands the desktop app (setup_sign_in.go),
// which the app's window takes within seconds and then removes, and the
// administrator's one-time code while none has the server open, which ends
// in ten minutes (administrator_sign_in_code.go).

const (
	setupStateFile      = "setup-state.json"
	setupRequestFile    = "setup-request.json"
	setupFollowEvery    = 2 * time.Second
	setupStoreKey       = "leafen.setupState"
	setupRequestLasting = 30 * time.Minute
)

// setupStepNames are the steps that can wait, in order, as the window names them.
var setupStepNames = []string{"address", "agents"}

// setupState is the state file.
type setupState struct {
	// Who set the server up; the steps are theirs.
	SetupBy   string   `json:"setupBy,omitempty"`
	Skipped   []string `json:"skipped"`
	Completed []string `json:"completed"`
	Done      bool     `json:"done"`
	// A name a command asked to start with, waiting for its person's yes in
	// the agent's conversation (first_run_person_ask.go), until the server is
	// set up; then the sign-in a setup window open meanwhile takes.
	Start   *setupStart   `json:"start,omitempty"`
	Handoff *setupHandoff `json:"handoff,omitempty"`
	// What the server said of the last request it couldn't do, for the command to say.
	Reply string `json:"reply,omitempty"`
	// A language a command chose before the account was made, given it then.
	Language string `json:"language,omitempty"`
	// A screen a command asked the setup person's window to open.
	Open *setupOpen `json:"open,omitempty"`
	// The last request taken (its at), and what a command's status reads.
	Answered int64 `json:"answered,omitempty"`
	// The last start request (its at) the server is done with: the first
	// administrator made and their first workspace listed for them, or the
	// server set up already. `leafen setup start --confirmation` says Leafen
	// is set up only then (2026-11-17: it said so as the administrator was
	// made, and `leafen connect` found no workspace for a minute).
	Started int64      `json:"started,omitzero"`
	Facts   setupFacts `json:"facts"`
}

// setupStart is a name waiting for its person: the request's id the agent
// passes their yes on with, and how it stands (person_ask.go).
type setupStart struct {
	Name   string `json:"name"`
	At     int64  `json:"at"`
	ID     string `json:"id,omitempty"`
	Status string `json:"status,omitempty"`
}

// setupOpen is a screen the window opens once: "notion", the import screen
// where the person adds their Notion token themselves.
type setupOpen struct {
	What string `json:"what"`
	At   int64  `json:"at"`
}

// setupOpenScreens are the screens a command can ask the window to open.
var setupOpenScreens = []string{"notion"}

// setupFacts are what `leafen setup status` says, as the server saw them.
type setupFacts struct {
	At            int64    `json:"at"`
	Configured    bool     `json:"configured"`
	Administrator string   `json:"administrator,omitempty"`
	Language      string   `json:"language,omitempty"`
	Here          string   `json:"here,omitempty"`
	Outside       string   `json:"outside,omitempty"`
	Tunnel        string   `json:"tunnel,omitempty"`
	Agents        []string `json:"agents"`
	StepsLeft     []string `json:"stepsLeft"`
	// Where the setup person sees Leafen (person_window.go): "app", the
	// desktop app's window, or "server", a browser at its address.
	Window personWindowKind `json:"window,omitempty"`
	// The setup person's Notion connections in Leafen, and their latest
	// migration's state ("scanning", "scanned", "running", "done", "failed").
	NotionConnections int    `json:"notionConnections"`
	NotionMigration   string `json:"notionMigration,omitempty"`
	// Whether nothing has come into the setup person's workspaces yet, so
	// the agent asks how to start (agent_start_offer.go).
	StartOffered bool `json:"startOffered"`
	// At the agents step, the question an agent asks before connecting
	// itself (agent_connect_first_run.go).
	AgentAsk string `json:"agentAsk,omitempty"`
	// While no administrator has the server open, the one-time code its log
	// shows them and when it ends (administrator_sign_in_code.go).
	AdministratorCode      string `json:"administratorCode,omitempty"`
	AdministratorCodeUntil int64  `json:"administratorCodeUntil,omitzero"`
}

// setupRequest is the request file: what a command asks of the running server.
type setupRequest struct {
	At       int64  `json:"at"`
	Start    string `json:"start,omitempty"`
	Language string `json:"language,omitempty"`
	// The person's yes to Start, passed on with its request's id.
	Confirm string `json:"confirm,omitempty"`
	Skip    string `json:"skip,omitempty"`
	Open    string `json:"open,omitempty"`
	// The app's own (desktop/first_run.go reads them): start new on this
	// computer, or sign in to a Leafen at this address — with the one-time
	// code the person got from a device signed in there, which may end the
	// account's oldest sign-in for this one where it is at its limit.
	StartHere        bool   `json:"startHere,omitzero"`
	SignIn           string `json:"signIn,omitempty"`
	Code             string `json:"code,omitempty"`
	SignOutLeastUsed bool   `json:"signOutLeastUsed,omitempty"`
	// The status command's: show the administrator a code while none has the
	// server open (administrator_sign_in_code.go).
	AdministratorCode bool `json:"administratorCode,omitzero"`
}

type setupKeeper struct {
	mu   sync.Mutex
	dir  string
	here string
	seen time.Time
}

func setupKeeperOf(app core.App) *setupKeeper {
	return app.Store().GetOrSet(setupStoreKey, func() any { return &setupKeeper{dir: app.DataDir()} }).(*setupKeeper)
}

func readSetupState(dir string) setupState {
	state := setupState{}
	if data, err := os.ReadFile(filepath.Join(dir, setupStateFile)); err == nil {
		_ = json.Unmarshal(data, &state)
	}
	if state.Skipped == nil {
		state.Skipped = []string{}
	}
	if state.Completed == nil {
		state.Completed = []string{}
	}
	return state
}

func readSetupRequest(dir string) (setupRequest, time.Time) {
	request := setupRequest{}
	info, err := os.Stat(filepath.Join(dir, setupRequestFile))
	if err != nil {
		return request, time.Time{}
	}
	if data, err := os.ReadFile(filepath.Join(dir, setupRequestFile)); err == nil {
		_ = json.Unmarshal(data, &request)
	}
	return request, info.ModTime()
}

// writeSetupFile writes one of the two files whole, so a reader never sees half.
func writeSetupFile(dir, name string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	temporary := filepath.Join(dir, name+".tmp")
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, filepath.Join(dir, name))
}

// change reads the state, changes it and writes it with fresh facts.
func (keeper *setupKeeper) change(app core.App, edit func(*setupState)) setupState {
	keeper.mu.Lock()
	defer keeper.mu.Unlock()
	state := readSetupState(keeper.dir)
	edit(&state)
	state.Facts = keeper.facts(app, state)
	_ = writeSetupFile(keeper.dir, setupStateFile, state)
	return state
}

func (keeper *setupKeeper) read() setupState {
	keeper.mu.Lock()
	defer keeper.mu.Unlock()
	return readSetupState(keeper.dir)
}

// stepsLeft are the steps still to show, in order: none once over.
func (state setupState) stepsLeft() []string {
	if state.SetupBy == "" || state.Done {
		return []string{}
	}
	return slices.DeleteFunc(slices.Clone(setupStepNames), func(step string) bool {
		return slices.Contains(state.Skipped, step) || slices.Contains(state.Completed, step)
	})
}

// begin starts the steps for whoever just set the server up, giving their
// account the language a command chose before it was made.
func (keeper *setupKeeper) begin(app core.App, userID string) {
	keeper.change(app, func(state *setupState) {
		if state.Language != "" {
			_ = saveSetupLanguage(app, userID, state.Language)
		}
		*state = setupState{SetupBy: userID, Skipped: []string{}, Completed: []string{}, Answered: state.Answered}
	})
}

// pass notes a step done, or left for later.
func (keeper *setupKeeper) pass(app core.App, step string, done bool) {
	if !slices.Contains(setupStepNames, step) {
		return
	}
	keeper.change(app, func(state *setupState) {
		if done {
			state.Completed = appendOnce(state.Completed, step)
			state.Skipped = slices.DeleteFunc(state.Skipped, func(kept string) bool { return kept == step })
		} else {
			state.Skipped = appendOnce(state.Skipped, step)
		}
	})
}

func appendOnce(steps []string, step string) []string {
	if slices.Contains(steps, step) {
		return steps
	}
	return append(steps, step)
}

// follow takes the requests commands leave, within seconds, while the server runs.
func (keeper *setupKeeper) follow(ctx context.Context, app core.App) {
	ticker := time.NewTicker(setupFollowEvery)
	defer ticker.Stop()
	for {
		keeper.takeRequest(app)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (keeper *setupKeeper) takeRequest(app core.App) {
	request, modified := readSetupRequest(keeper.dir)
	if modified.IsZero() || modified.Equal(keeper.seen) {
		return
	}
	keeper.seen = modified
	state := keeper.read()
	if request.At <= state.Answered || time.Since(time.UnixMilli(request.At)) > setupRequestLasting {
		return
	}
	configured, _ := administratorConfigured(app)
	if request.AdministratorCode {
		if _, _, err := administratorSignInCodeOf(app).current(app, true); err != nil {
			app.Logger().Warn("leafen administrator sign-in code not shown", "error", err)
		}
	}
	startWith := ""
	keeper.change(app, func(state *setupState) {
		state.Answered = request.At
		state.Reply = ""
		if request.Language != "" && !configured {
			state.Language = request.Language
		}
		if request.Start != "" && !configured {
			startWith = askStart(state, request.Start, request.Confirm)
		} else if request.Start != "" {
			state.Started = request.At
		}
		if request.Language != "" {
			if configured && state.SetupBy != "" {
				_ = saveSetupLanguage(app, state.SetupBy, request.Language)
			} else if !configured {
				state.Language = request.Language
			}
		}
		if request.Skip != "" && slices.Contains(setupStepNames, request.Skip) && state.SetupBy != "" {
			state.Skipped = appendOnce(state.Skipped, request.Skip)
		}
		if slices.Contains(setupOpenScreens, request.Open) && configured {
			askWindowOpen(app, state, request.Open, request.At)
		}
	})
	if slices.Contains(setupOpenScreens, request.Open) && configured {
		showInAppWindow(app, request.Open)
	}
	if startWith != "" {
		keeper.startAdministrator(app, startWith, request.At)
	}
}

// setupOwner is whoever set the server up: the steps' person, or on a server
// set up before the steps were the server's, its first administrator.
func setupOwner(app core.App, state setupState) string {
	owner := state.SetupBy
	if owner == "" {
		_ = app.DB().NewQuery(`SELECT [[setup_admin_id]] FROM {{leafen_server_settings}} WHERE [[id]] = 'default'`).Row(&owner)
	}
	return owner
}

// following says whether a command was taken a short while ago: an agent is
// going through the first run, and the setup person's window follows it.
func (state setupState) following() bool {
	return state.Answered > 0 && time.Since(time.UnixMilli(state.Answered)) < setupRequestLasting
}

// facts reads what a command's status says.
func (keeper *setupKeeper) facts(app core.App, state setupState) setupFacts {
	facts := setupFacts{At: time.Now().UnixMilli(), Here: keeper.here, Agents: []string{}, StepsLeft: state.stepsLeft()}
	// Only an https address the internet reaches is one from anywhere (outsideOrigin).
	if public := configuredPublicOrigin(); outsideOrigin(public) {
		facts.Outside = public
	}
	facts.Configured, _ = administratorConfigured(app)
	facts.AdministratorCode, facts.AdministratorCodeUntil, _ = administratorSignInCodeOf(app).current(app, false)
	if settings, err := loadServerTunnelSettings(app); err == nil {
		facts.Tunnel = settings.Mode
	}
	owner := setupOwner(app, state)
	if owner == "" {
		return facts
	}
	if user, err := app.FindRecordById("users", owner); err == nil {
		facts.Administrator = user.GetString("name")
	}
	if stored, err := storedAccountLanguage(app, owner); err == nil {
		facts.Language = stored.LanguagePreference
	}
	facts.Agents, facts.Window = setupAgentNames(app, owner), personWindowFor(app, owner, owner)
	if facts.Configured && len(facts.Agents) == 0 && slices.Contains(facts.StepsLeft, "agents") {
		facts.AgentAsk = setupAgentAsk(app, owner)
	}
	if workspaces, err := userKeyPersonWorkspaces(app, owner); err == nil {
		facts.StartOffered = agentStartOffered(app, workspaces)
	}
	_ = app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_notion_import_connections}} WHERE [[owner]] = {:owner} AND [[active]] = 1`).
		Bind(dbx.Params{"owner": owner}).Row(&facts.NotionConnections)
	migration := struct {
		Status   string `db:"status"`
		RunState string `db:"run_state"`
	}{}
	if app.DB().NewQuery(`
		SELECT [[status]], [[run_state]] FROM {{leafen_notion_migrations}}
		WHERE [[owner]] = {:owner} ORDER BY [[updated_at]] DESC LIMIT 1
	`).Bind(dbx.Params{"owner": owner}).One(&migration) == nil {
		facts.NotionMigration = migration.Status
		if migration.RunState != "" {
			facts.NotionMigration = migration.RunState
		}
	}
	return facts
}

// saveSetupLanguage gives an account its language, as its language step does.
func saveSetupLanguage(app core.App, userID, language string) error {
	if _, allowed := supportedAccountLanguagePreferences[language]; !allowed {
		return nil
	}
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_account_preferences}} ([[owner_id]], [[language_preference]], [[updated_at]])
		VALUES ({:owner}, {:language}, {:updated})
		ON CONFLICT ([[owner_id]]) DO UPDATE SET
			[[language_preference]] = excluded.[[language_preference]], [[updated_at]] = excluded.[[updated_at]]
	`).Bind(dbx.Params{"owner": userID, "language": language, "updated": time.Now().UnixMilli()}).Execute()
	forgetServerLanguage(app)
	return err
}

// registerSetupState follows the commands' requests while the server runs
// and answers the window's steps (setup_routes.go). As it starts, the server
// writes its own addresses into the folder's facts, so the links commands
// print from them (a device code's, an agent's connection) open the server
// that serves this folder now — not the one the facts came from in a backup
// restored here, nor this folder's server at the port of its last start
// (2026-11-19: a restored copy served at 8096 printed the live app's 60449).
func registerSetupState(pb *pocketbase.PocketBase) {
	pb.OnServe().BindFunc(func(event *core.ServeEvent) error {
		keeper := setupKeeperOf(event.App)
		keeper.here = setupServedAddress(event)
		innerRequestHome.Store(keeper.here)
		registerSetupRoutes(event)
		// Until the server stops (server_background.go).
		serverBackgroundOf(event.App).run("setup requests", func(ctx context.Context) { keeper.follow(ctx, event.App) })
		if err := event.Next(); err != nil {
			return err
		}
		// After the other start hooks, which note the address chosen in the
		// settings (server_tunnel_chosen.go), and before the server answers.
		keeper.change(event.App, func(*setupState) {})
		return nil
	})
}

// setupServedAddress is the address this computer opens the server at: the
// one its operator wrote where it is a local one, else where it listens.
func setupServedAddress(event *core.ServeEvent) string {
	if written, err := url.Parse(configuredServerAddress()); err == nil && written.Host != "" && isPrivateMCPDestination(written.Hostname()) {
		return written.String()
	}
	if event.Server == nil || event.Server.Addr == "" {
		return ""
	}
	return thisMachineURL("http://" + event.Server.Addr)
}
