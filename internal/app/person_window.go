package app

import (
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Where the person sees Leafen while a first-run command or an agent's
// answer names a screen for them, decided here alone for all of them (user
// 2026-11-19, the first ten minutes: in the s251 round the Mac app's person
// was sent to Leafen's import screen in their browser, which wasn't signed
// in, while the app's window was; `leafen setup status` said the window and
// the MCP tool a browser address):
//
//   - personWindowApp: the desktop app's own server (LocalOwner), for the
//     person who set it up — the one its window is signed in as. A screen
//     opens in that window, which comes to the front; there is no address to
//     give, as a browser isn't signed in there.
//   - personWindowServer: a server installed on its own (Docker, `leafen
//     serve`), for the person who set it up: their browser at the server's
//     address, signing in first if it asks; a Leafen window of theirs open
//     there opens the screen by itself.
//   - personWindowBrowser: anyone else — a member of the server, or a
//     server not set up yet — in a browser at the server's address.
//
// The setup state's facts carry the setup person's (setupFacts.Window), so
// `leafen setup` says what the MCP tools say.

type personWindowKind string

const (
	personWindowApp     personWindowKind = "app"
	personWindowServer  personWindowKind = "server"
	personWindowBrowser personWindowKind = "browser"
)

// personWindowFor is the person's, where setupPerson set the server up.
func personWindowFor(app core.App, person, setupPerson string) personWindowKind {
	switch {
	case person == "" || person != setupPerson:
		return personWindowBrowser
	case localOwnerOf(app) != nil:
		return personWindowApp
	}
	return personWindowServer
}

// personWindowOf is the person's, as the server's setup state has it.
func personWindowOf(app core.App, person string) personWindowKind {
	return personWindowFor(app, person, setupOwner(app, setupKeeperOf(app).read()))
}

// setupOpenPaths are the screens a command or an agent asks the setup
// person's window to open (setupOpenScreens), at their address.
var setupOpenPaths = map[string]string{"notion": notionImportScreenPath}

// An agent waiting for what the person puts in a screen asks for it once in
// this long: its next waits don't bring the window up again over the page
// the person is on.
const setupOpenAgainAfter = 10 * time.Minute

// askWindowOpen notes that the setup person's window opens what: a window of
// theirs open on the server follows it (setup_routes.go), the app's coming
// to the front on it (showInAppWindow). The first run's agents step, its
// agent connected, is done with then: the window stayed on its "connected:
// Claude Code — continue" while `leafen setup notion` said the import opened
// (2026-11-19, the s251 round).
func askWindowOpen(app core.App, state *setupState, what string, at int64) {
	state.Open = &setupOpen{What: what, At: at}
	if slices.Contains(state.stepsLeft(), "agents") && len(setupAgentNames(app, setupOwner(app, *state))) > 0 {
		state.Completed = appendOnce(state.Completed, "agents")
		state.Skipped = slices.DeleteFunc(state.Skipped, func(kept string) bool { return kept == "agents" })
	}
}

// showInAppWindow brings the desktop app's window to the front on what,
// where the shell lets the server do so (LocalOwner.ShowsScreens).
func showInAppWindow(app core.App, what string) {
	if owner := localOwnerOf(app); owner != nil && setupOpenPaths[what] != "" {
		if show := owner.screens.Load(); show != nil {
			(*show)(setupOpenPaths[what])
		}
	}
}

// openForAgent is an agent's ask, waiting for what the person puts in the
// screen what, that the person's window open it — once in
// setupOpenAgainAfter. Only the setup person's window follows; anyone
// else's browser opens the address the agent gives them.
func openForAgent(app core.App, person, what string) personWindowKind {
	kind := personWindowOf(app, person)
	if kind == personWindowBrowser {
		return kind
	}
	asked := false
	setupKeeperOf(app).change(app, func(state *setupState) {
		now := time.Now().UnixMilli()
		if state.Open != nil && state.Open.What == what && time.Duration(now-state.Open.At)*time.Millisecond < setupOpenAgainAfter {
			return
		}
		askWindowOpen(app, state, what, now)
		asked = true
	})
	if asked && kind == personWindowApp {
		showInAppWindow(app, what)
	}
	return kind
}

// ShowsScreens lets the app's shell open a screen of the server's in its
// window and bring the window to the front: path is the screen's on this
// server.
func (owner *LocalOwner) ShowsScreens(show func(path string)) {
	owner.screens.Store(&show)
}

// setupAgentNames are the agents connected as the person, by name.
func setupAgentNames(app core.App, person string) []string {
	names := []string{}
	_ = app.DB().NewQuery(`
		SELECT DISTINCT [[client_name]] FROM {{leafen_mcp_connections}}
		WHERE [[owner_id]] = {:owner} AND [[revoked_at]] = 0 AND [[app_install_id]] = ''
		ORDER BY [[client_name]] LIMIT 32
	`).Bind(dbx.Params{"owner": person}).Column(&names)
	return names
}

// A screen's address in a person's words, given in brackets after the
// screen's name ({{url}}), as every language has it.
var personWordsAddress = regexp.MustCompile(`\s?[(（]\{\{url\}\}[)）]|\{\{url\}\}`)

// withScreenAddress puts the screen's address in the person's words, or,
// where the screen opens in the app's window and has none (address ""),
// leaves it out with its brackets.
func withScreenAddress(words, address string) string {
	if address == "" {
		return personWordsAddress.ReplaceAllString(words, "")
	}
	return strings.ReplaceAll(words, "{{url}}", address)
}

// personScreenWay is how an agent takes the person to one of Leafen's
// screens, named as their screens name it, at path on this server: in the
// desktop app's window, which is signed in, by a leafen: link that opens it
// there (person_links.go), never a browser, which isn't; else at its address
// in their browser — in a tab of the agent's own where it has a browser tool
// there, doing the screen's steps itself, else opened for them
// (agent_outside_steps.go).
func personScreenWay(app core.App, authority mcpAuthority, path, name string) string {
	link := personLinkAt(app, authority, path)
	if strings.HasPrefix(link, "leafen:") {
		return "open " + name + " for them in the Leafen app's window on their computer — " + link +
			" opens it there (run open with it on their computer, or give them the link; not a browser, which isn't logged in to Leafen)"
	}
	return "open " + name + " for them (" + link + ") " + agentInTheirBrowser
}
