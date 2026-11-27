package app

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// The first run's name is confirmed in the agent's conversation (user
// decision 2026-11-13, which took back the window's "Start" press of
// 2026-11-09: Leafen can be set up without its screen ever being opened).
// `leafen setup start --name` leaves the name with the running server,
// which keeps it as a request waiting for its person (person_ask.go) and
// gives the words the agent asks them in; the agent passes their yes on with
// the request's id (`--confirmation`), and the server makes the first
// administrator with that name. No MCP connection exists before the first
// administrator does; the conversation asks, as every question of the one
// asking boundary does. A setup window open on the setup code
// meanwhile signs in as the new administrator once, with a one-time device
// code, and goes on as if it had set the server up itself; with none open,
// the agent gives the person a sign-in link (`leafen account device-code`).

const (
	// A name waits for its person's yes as long as the conversation's words say.
	setupStartLasting = 24 * time.Hour
	// How long a setup window open meanwhile may still sign in as the new administrator.
	setupHandoffLasting = 30 * time.Minute
	// How long the start waits for the first workspace to be listed, within
	// the command's own wait (setupStartWait).
	setupStartSettleWait = 20 * time.Second
	setupStartPending    = "pending"
)

var (
	setupStartQuestion = mcpWords{"리펜을 “%s” 이름으로 시작할까요?", "Start Leafen with the name “%s”?"}
	setupStartEffect   = mcpWords{"이 이름이 이 리펜의 첫 관리자가 됩니다 — 리펜을 설정하고 관리하는 사람입니다. 이름은 나중에 설정에서 바꿀 수 있습니다.",
		"This name becomes this Leafen’s first administrator — the person who sets it up and runs it. It can be changed later in Settings."}
)

// setupHandoff is the sign-in a setup window open on the setup code takes
// once the agent's name made the first administrator.
type setupHandoff struct {
	At    int64 `json:"at"`
	Taken bool  `json:"taken,omitempty"`
}

// setupStartPersonAsk is the question, in the language chosen for Leafen
// (English until one is).
func setupStartPersonAsk(name, language string) personAsk {
	return personAsk{Question: fmt.Sprintf(setupStartQuestion.in(language), name), Effect: setupStartEffect.in(language), Language: language}
}

// setupStartCommand is the command that passes the person's yes on. On
// Windows, where the program is typed for PowerShell (desktop/program_typed.go),
// the name is quoted as PowerShell reads it: the POSIX shell's way around
// an apostrophe ("O'Brien") leaves PowerShell's line unparsed.
func setupStartCommand(program, name, language, id string) string {
	quoted := shellQuoted
	if runtime.GOOS == "windows" {
		quoted = psQuoted
	}
	line := program + " setup start --name " + quoted(name)
	if language != "" {
		line += " --language " + language
	}
	if id != "" {
		line += " --confirmation " + id
	}
	return line
}

func shellQuoted(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// setupStartRequests keeps the one name waiting for its person, in the
// setup state the server alone writes, while it changes it.
type setupStartRequests struct {
	state *setupState
	name  string
}

func (requests setupStartRequests) live() *setupStart {
	start := requests.state.Start
	if start == nil || start.ID == "" || time.Since(time.UnixMilli(start.At)) > setupStartLasting {
		return nil
	}
	return start
}

func (requests setupStartRequests) waiting() (string, int64, error) {
	if start := requests.live(); start != nil && start.Status == setupStartPending && start.Name == requests.name {
		return start.ID, start.At + setupStartLasting.Milliseconds(), nil
	}
	now := time.Now()
	start := &setupStart{Name: requests.name, At: now.UnixMilli(), ID: "lss_" + security.RandomStringWithAlphabet(16, deviceCodeAlphabet), Status: setupStartPending}
	requests.state.Start = start
	return start.ID, now.Add(setupStartLasting).UnixMilli(), nil
}

// status answers for the name it was asked with only: a yes for one name
// never starts Leafen with another.
func (requests setupStartRequests) status(id string) string {
	start := requests.live()
	if start == nil || start.ID != id || start.Name != requests.name {
		return ""
	}
	return start.Status
}

func (requests setupStartRequests) decide(id string, yes, _, _ bool) error {
	if start := requests.live(); start != nil && start.ID == id {
		start.Status = map[bool]string{true: "approved", false: "declined"}[yes]
	}
	return nil
}

func (requests setupStartRequests) spend(id string) error {
	if start := requests.live(); start != nil && start.ID == id {
		start.Status = "used"
	}
	return nil
}

// askStart takes a command's name, inside the state's change: it waits for
// its person, or with their yes passed on (confirm) it is the name to start
// with, which the caller makes the first administrator of, after the change.
func askStart(state *setupState, name, confirm string) (startWith string) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxAuthNameLength {
		state.Reply = fmt.Sprintf("give the name the person chose, at most %d characters", maxAuthNameLength)
		return ""
	}
	requests := setupStartRequests{state: state, name: name}
	confirm = strings.TrimSpace(confirm)
	// An id that can't pass a yes on is said as the command it is, not as
	// the MCP call the same asking serves.
	if confirm != "" {
		switch requests.status(confirm) {
		case "used":
			state.Reply = "that --confirmation id was used already; run the command without --confirmation to ask the person again"
			return ""
		case "":
			state.Reply = fmt.Sprintf("no start with the name %q waits for that --confirmation id: it was given for another name, "+
				"or ran out after a day. Run the command without --confirmation, ask the person again and pass their yes on with the id "+
				"it gives", name)
			return ""
		}
	}
	outcome, err := askPerson(requests, personAskCall{PassedOn: confirm}, setupStartPersonAsk(name, state.Language))
	switch {
	case err != nil:
		state.Reply = strings.TrimPrefix(err.Error(), errMCPToolInvalid.Error()+": ")
	case outcome.Proceed:
		return name
	}
	return ""
}

// startAdministrator makes the first administrator with the name its person
// said yes to, as the setup screen does with the name typed there, and their
// first workspace; their steps then begin, and a setup window open meanwhile
// may sign in as them. The start request made at at is done (Started) once
// that workspace is in the lists that read it.
func (keeper *setupKeeper) startAdministrator(app core.App, name string, at int64) {
	// Setup screens hear the server is set up once all of this is done (setup_claims.go).
	defer setupClaimsOf(app).agentStart()()
	input := administratorSetupInput{Name: name, Password: security.RandomString(48)}
	administrator, err := claimSetupAdministrator(context.Background(), app, input, false)
	if err != nil {
		reply := "Leafen couldn't make the first administrator; the server's log says why"
		if errors.Is(err, errSetupAlreadyClaimed) {
			reply = "Leafen is set up already"
		} else {
			app.Logger().Warn("leafen setup start failed", "error", err)
		}
		keeper.change(app, func(state *setupState) { state.Reply = reply })
		return
	}
	language := keeper.read().Language
	keeper.begin(app, administrator.Id)
	// The workspace the window's language step would make, so the agent can
	// connect and move Notion in before any window opens.
	if _, err := startFirstWorkspace(app, administrator, language); err != nil {
		app.Logger().Warn("leafen setup first workspace failed", "error", err)
	}
	// The workspace lists — the window's, `leafen connect`'s — read it once
	// its visibility is projected. A request that makes a workspace wakes the
	// projection; this start comes by the request file, so the jobs touching
	// the administrator are done here, before the start is said done
	// (2026-11-17: they waited up to the projection's idle poll, and
	// `leafen connect` right after found no workspace for about a minute).
	if _, err := settleBootstrapVisibilityProjectionForWithin(
		context.Background(), app, administrator.Id, setupStartSettleWait,
	); err != nil {
		app.Logger().Warn("leafen setup first workspace listing waits", "error", err)
	}
	keeper.change(app, func(state *setupState) {
		state.Handoff = &setupHandoff{At: time.Now().UnixMilli()}
		state.Started = at
	})
}

// takeHandoff gives the setup window that asks with the setup code a
// one-time code that signs it in as the first administrator the agent's name
// made, once, a short while after.
func (keeper *setupKeeper) takeHandoff(app core.App) string {
	code := ""
	keeper.change(app, func(state *setupState) {
		handoff := state.Handoff
		if handoff == nil || handoff.Taken || state.SetupBy == "" || time.Since(time.UnixMilli(handoff.At)) > setupHandoffLasting {
			return
		}
		// The claim's own audit line says who set the server up; the window's sign-in is recorded as it joins.
		issued, err := issueAccountDeviceCode(app, state.SetupBy, "", "", "")
		if err != nil {
			app.Logger().Warn("leafen setup window sign-in failed", "error", err)
			return
		}
		handoff.Taken = true
		code = issued.Code
	})
	return code
}
