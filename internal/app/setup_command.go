package app

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase"
	"github.com/spf13/cobra"
)

// `leafen setup` goes through the first run from a terminal (user decision
// 2026-11-09: an agent asked to install Leafen carries on through its setup,
// asking the person what the screens ask and doing it by commands; changed
// 2026-11-13: all of it in the conversation, no Leafen screen opened): what
// is done and what is next, starting with a name (the person says yes to it
// in the conversation and the agent passes the yes on), the language,
// leaving a step for later, opening the Notion import's own token field for
// someone who would rather paste it there, and in the app, signing in to a
// Leafen used already, with a one-time code the person got from a device
// signed in there (setup_sign_in.go). The public address is `leafen
// tailscale` or `leafen address`; the Notion migration itself is the agent's
// MCP tools, which also take a token pasted into the conversation. It opens
// no data folder: it leaves its request beside the running server's data,
// which the server follows within seconds (first_run_state.go), and reads
// what the server writes back. It prints no secret but a name's request id,
// which stands for the person's yes, and while no administrator has the
// server open, their one-time sign-in code (administrator_sign_in_code.go).

// SetupApp is what the desktop app tells `leafen setup` about its own first
// screen; unset where the program serves alone.
type SetupApp struct {
	// The first screen's choice: "" while it shows, "local" (this computer
	// serves) or "server" (the app signs in to another Leafen).
	Choice func() string
	// How the window's sign-in with a code went for the request made at at:
	// the account it signed in as, or why it was refused; not answered yet
	// while the window hasn't taken it.
	SignedIn func(at int64) (account, refused string, answered bool)
	// The app's program as a command it prints is typed to run it again.
	Program string
}

var setupApp *SetupApp

// UseSetupApp makes `leafen setup` the app's (desktop/first_run.go).
func UseSetupApp(app SetupApp) { setupApp = &app }

const (
	setupAnswerWait = 12 * time.Second
	// The app starts its own server when a name is asked to start with: a minute for it to come up.
	setupServerWait = time.Minute
	setupStartWait  = 30 * time.Second
)

func setupCommand(pb *pocketbase.PocketBase) *cobra.Command {
	command := &cobra.Command{
		Use:   "setup",
		Short: "Go through Leafen's first run from the terminal, as its screens do",
		Long: "Says what is done and what is next (status), starts Leafen with a name the person says yes to in the conversation (start), " +
			"chooses the language (language), leaves a step for later (skip), opens the Notion import's own token field for a person who " +
			"would rather paste it there (notion) and, in the desktop app, logs in to a Leafen used already with a one-time code the person " +
			"got from a device logged in there (sign-in). The public address is " +
			"`leafen tailscale` or `leafen address`. Works with the running server.",
		SilenceUsage: true,
	}
	var asJSON bool
	status := &cobra.Command{Use: "status", Short: "Say what is done and what is next", Args: cobra.NoArgs, SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSetupStatus(cmd.OutOrStdout(), pb.DataDir(), asJSON)
		}}
	status.Flags().BoolVar(&asJSON, "json", false, "print what the server says as JSON")
	var name, language, confirmation string
	start := &cobra.Command{Use: "start", Short: "Start Leafen with a name, once the person says yes to it in the conversation", Args: cobra.NoArgs,
		SilenceUsage: true, RunE: func(cmd *cobra.Command, _ []string) error {
			return runSetupStart(cmd, pb.DataDir(), name, language, confirmation)
		}}
	start.Flags().StringVar(&name, "name", "", "the name others see, as the person said it")
	start.Flags().StringVar(&language, "language", "", "the language Leafen uses, such as ko or en")
	start.Flags().StringVar(&confirmation, "confirmation", "", "the request id the first run gave, once the person said yes to the name in the conversation")
	_ = start.MarkFlagRequired("name")
	command.AddCommand(status, start,
		&cobra.Command{Use: "language <code>", Short: "Choose the language Leafen uses (ko, en, ja, zh-Hans, …)", Args: cobra.ExactArgs(1),
			SilenceUsage: true, RunE: func(cmd *cobra.Command, args []string) error {
				return runSetupLanguage(cmd.OutOrStdout(), pb.DataDir(), args[0])
			}},
		&cobra.Command{Use: "skip <address|agents>", Short: "Leave a step for later, as its \"Later\" does", Args: cobra.ExactArgs(1),
			SilenceUsage: true, RunE: func(cmd *cobra.Command, args []string) error {
				return runSetupSkip(cmd.OutOrStdout(), pb.DataDir(), args[0])
			}},
		&cobra.Command{Use: "notion", Short: "Open Leafen's Notion import, where the person adds their Notion token", Args: cobra.NoArgs,
			SilenceUsage: true, RunE: func(cmd *cobra.Command, _ []string) error {
				return runSetupNotion(cmd.OutOrStdout(), pb.DataDir())
			}},
		setupSignInCommand(pb),
	)
	return command
}

// ask leaves a request for the running server and waits for its answer: the
// state it wrote, and whether it answered at all.
func askSetup(dir string, request setupRequest) (setupState, bool, error) {
	return askSetupWaiting(dir, &request, setupAnswerWait)
}

// askSetupWaiting is askSetup waiting as long as wait, stamping the request
// with the time it was left.
func askSetupWaiting(dir string, request *setupRequest, wait time.Duration) (setupState, bool, error) {
	if err := leaveSetupRequest(dir, request); err != nil {
		return setupState{}, false, err
	}
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if state := readSetupState(dir); state.Answered >= request.At {
			return state, true, nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return readSetupState(dir), false, nil
}

// leaveSetupRequest writes the request file, stamped with its time. The
// program alone asks the server that keeps its data in dir, so a folder with
// none names no server: it says so at once and makes nothing there
// (2026-11-17: a wrong --dir made the folder and waited 12 s). The app's
// own folder may come with the server its first screen starts.
func leaveSetupRequest(dir string, request *setupRequest) error {
	if setupApp == nil && !holdsServerData(dir) {
		return noServerDataError(dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	request.At = time.Now().UnixMilli()
	if err := writeSetupFile(dir, setupRequestFile, *request); err != nil {
		return fmt.Errorf("the request couldn't be left for the server: %w", err)
	}
	return nil
}

// setupNoServer says that no server answered for the data in dir, and what to do.
func setupNoServer(dir string) string {
	if absolute, err := filepath.Abs(dir); err == nil {
		dir = absolute
	}
	words := "No running Leafen server answered for the data in " + dir + ". Start it — open the Leafen app, run `leafen serve`, " +
		"or start the container — and run this again."
	if setupApp == nil {
		words += " If the server keeps its data in another folder, name that one with --dir."
	}
	return words
}

// appFirstScreen answers for the app while its first screen shows, or signs in elsewhere.
func appFirstScreen(out io.Writer) bool {
	if setupApp == nil {
		return false
	}
	switch setupApp.Choice() {
	case "":
		fmt.Fprintln(out, "Leafen's first screen is open, and nothing is chosen yet.")
		fmt.Fprintln(out, "Ask the person first: start new on this computer, or log in to a Leafen they use already (a company or team server, or another device)?")
		fmt.Fprintln(out, "Then: leafen setup start --name \"<their name>\" --language <code>   or, for logging in, ask for the address and a "+
			"one-time code from a device logged in there (the workspace name in the sidebar › Add a device): "+
			"leafen setup sign-in <invite link or address> --code <code>")
		return true
	case "server":
		fmt.Fprintln(out, "This app logs in to another Leafen; that Leafen's administrator set it up. Nothing here is left to set up.")
		fmt.Fprintln(out, setupAddLeafenNext)
		return true
	}
	return false
}

func runSetupStatus(out io.Writer, dir string, asJSON bool) error {
	if !asJSON && appFirstScreen(out) {
		return nil
	}
	// It shows the administrator a code while none has the server open (administrator_sign_in_code.go).
	state, answered, err := askSetup(dir, setupRequest{AdministratorCode: true})
	if err != nil {
		return err
	}
	if asJSON {
		addresses := map[string]string{}
		if state.Facts.Here != "" {
			addresses["here"] = state.Facts.Here + nativeMCPPath
		}
		if state.Facts.Outside != "" {
			addresses["outside"] = state.Facts.Outside + nativeMCPPath
		}
		return json.NewEncoder(out).Encode(map[string]any{"answered": answered, "facts": state.Facts, "mcp": addresses,
			"nameToConfirm": setupNameWaiting(state)})
	}
	if !answered {
		fmt.Fprintln(out, setupNoServer(dir))
		return nil
	}
	writeSetupFacts(out, state)
	return nil
}

// setupNameWaiting is a name that waits for its person's yes.
func setupNameWaiting(state setupState) string {
	if state.Facts.Configured {
		return ""
	}
	if start := (setupStartRequests{state: &state}).live(); start != nil && start.Status == setupStartPending {
		return start.Name
	}
	return ""
}

// writeSetupFacts says what is done, then the one thing next, in the first run's order.
func writeSetupFacts(out io.Writer, state setupState) {
	facts := state.Facts
	none := func(values []string) string {
		if len(values) == 0 {
			return "none"
		}
		return strings.Join(values, ", ")
	}
	if facts.Configured {
		fmt.Fprintf(out, "Set up: yes — first administrator: %s\n", facts.Administrator)
		fmt.Fprintf(out, "Language: %s\n", map[bool]string{true: "not chosen yet", false: facts.Language}[facts.Language == ""])
		if facts.AdministratorCode != "" {
			fmt.Fprintf(out, "Logged in: no administrator has Leafen open on a device now. A one-time code logs one device in "+
				"as an administrator, once, until %s: %s — on the login page, choose \"Log in with a code\" and type it.\n",
				time.UnixMilli(facts.AdministratorCodeUntil).Format("15:04"), facts.AdministratorCode)
		}
	} else if waiting := setupNameWaiting(state); waiting != "" {
		fmt.Fprintf(out, "Set up: not yet — the name %q waits for the person's yes in the conversation\n", waiting)
	} else {
		fmt.Fprintln(out, "Set up: not yet")
	}
	if facts.Here != "" {
		fmt.Fprintf(out, "Address on this computer: %s  (MCP: %s%s)\n", facts.Here, facts.Here, nativeMCPPath)
	}
	outside := "none"
	if facts.Outside != "" {
		outside = facts.Outside
		if facts.Tunnel != "" {
			outside += " (" + facts.Tunnel + ")"
		}
	}
	fmt.Fprintf(out, "Public address: %s  (MCP: %s)\n", outside, map[bool]string{true: "-", false: facts.Outside + nativeMCPPath}[facts.Outside == ""])
	fmt.Fprintf(out, "Agents connected: %s\n", none(facts.Agents))
	fmt.Fprintf(out, "Steps left in the window: %s\n", none(facts.StepsLeft))
	if facts.Configured {
		notion := fmt.Sprintf("%d connection(s)", facts.NotionConnections)
		if facts.NotionMigration != "" {
			notion += ", latest migration " + facts.NotionMigration
		}
		fmt.Fprintf(out, "Notion: %s\n", notion)
	}
	fmt.Fprint(out, "Next: ")
	switch {
	case !facts.Configured && setupNameWaiting(state) != "":
		fmt.Fprintf(out, "run %s again: it gives the words to ask the person in, and how to pass their yes on.\n",
			setupStartCommand("leafen", setupNameWaiting(state), "", ""))
	case !facts.Configured:
		fmt.Fprintln(out, "ask which language Leafen should use and the person's name, then: leafen setup start --name \"<their name>\" --language <code>")
	case facts.Language == "":
		fmt.Fprintln(out, "ask which language Leafen should use, then: leafen setup language <code> (ko, en, ja, zh-Hans, …)")
	case slices.Contains(facts.StepsLeft, "address"):
		fmt.Fprintln(out, "ask whether Leafen should open from anywhere (phones, people they work with and AI chat apps on the internet "+
			"reach it by an address of its own; for this computer alone it can wait). Tailscale, recommended: leafen tailscale. "+
			"Their own domain: leafen address --address https://…  Later: leafen setup skip address")
	case slices.Contains(facts.StepsLeft, "agents") && len(facts.Agents) == 0:
		fmt.Fprintln(out, setupConnectNext(facts.AgentAsk))
	case facts.StartOffered:
		// Only where nothing has come into their workspace yet (agent_start_offer.go).
		fmt.Fprintln(out, "nothing is left of the first run. Left to do: one question, in their language, how they'd like to start — by "+
			"moving their Notion workspace here, carrying in the same question all that this says of how that goes: “"+
			notionImportWords(chosenLanguageOrEnglish(facts.Language)).FirstWords+"” — or from an empty workspace. For a move: "+setupNotionNext(facts))
	default:
		fmt.Fprintln(out, "nothing is left of the first run.")
	}
	if facts.Configured {
		fmt.Fprintln(out, setupSignInNext(facts))
	}
	if setupApp != nil {
		fmt.Fprintln(out, setupAddLeafenNext)
	}
}

// What the agent does next, where the person never opens a Leafen screen
// (user decision 2026-11-13).
const (
	setupAddLeafenNext = "To add another Leafen to this app (a company or team server), ask the person for its address and a one-time code " +
		"from a device logged in there (the workspace name in the sidebar › Add a device): leafen setup sign-in <address> --code <code>"
)

// runSetupStart asks the running server to start with a name: the first
// time, it gives the words to ask the person in; with their yes passed on
// (confirmation), the server makes the first administrator.
func runSetupStart(cmd *cobra.Command, dir, name, language, confirmation string) error {
	out := cmd.OutOrStdout()
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxAuthNameLength {
		return fmt.Errorf("give the name the person chose, at most %d characters", maxAuthNameLength)
	}
	if language != "" {
		if _, allowed := supportedAccountLanguagePreferences[language]; !allowed {
			return fmt.Errorf("Leafen has no language %q; one of: %s", language, strings.Join(selectableAccountLanguagePreferenceValues[1:], ", "))
		}
	}
	request := setupRequest{Start: name, Language: language, Confirm: strings.TrimSpace(confirmation)}
	wait := setupAnswerWait
	if setupApp != nil {
		switch setupApp.Choice() {
		case "":
			request.StartHere = true
			wait = setupServerWait
		case "server":
			return errors.New("this app logs in to another Leafen, whose administrator set it up")
		}
	}
	state, answered, err := askSetupWaiting(dir, &request, wait)
	if err != nil {
		return err
	}
	switch {
	case !answered:
		fmt.Fprintln(out, setupNoServer(dir))
		return nil
	case state.Facts.Configured && request.Confirm == "":
		fmt.Fprintf(out, "Leafen is set up already; its first administrator is %s.\n", state.Facts.Administrator)
		return nil
	case state.Reply != "":
		return errors.New(state.Reply)
	case request.Confirm == "":
		start := (setupStartRequests{state: &state, name: name}).live()
		if start == nil || start.Status != setupStartPending {
			return errors.New("the server didn't keep the name; run this again")
		}
		chosen := language
		if chosen == "" {
			chosen = state.Language
		}
		program := "leafen"
		if setupApp != nil {
			program = cmp.Or(setupApp.Program, "leafen-desktop")
		}
		fmt.Fprintln(out, personAskConversation(setupStartPersonAsk(name, chosen),
			"run this command: `"+setupStartCommand(program, name, language, start.ID)+"`."))
		return nil
	}
	// The yes was passed on: the server makes the first administrator and
	// their first workspace, and says this request done (Started) once
	// `leafen connect` finds it, as the next step does.
	for deadline := time.Now().Add(setupStartWait); time.Now().Before(deadline); {
		if state.Facts.Configured && state.Started >= request.At {
			fmt.Fprintf(out, "Leafen is set up: %s is its first administrator.\n", state.Facts.Administrator)
			writeSetupFacts(out, state)
			return nil
		}
		if state.Reply != "" {
			return errors.New(state.Reply)
		}
		select {
		case <-cmd.Context().Done():
			return cmd.Context().Err()
		case <-time.After(500 * time.Millisecond):
		}
		state = readSetupState(dir)
	}
	return errors.New("the server didn't finish starting Leafen; `leafen setup status` says where it stands")
}

func runSetupLanguage(out io.Writer, dir, language string) error {
	if _, allowed := supportedAccountLanguagePreferences[language]; !allowed {
		return fmt.Errorf("Leafen has no language %q; one of: %s", language, strings.Join(selectableAccountLanguagePreferenceValues[1:], ", "))
	}
	if appFirstScreen(out) {
		return nil
	}
	state, answered, err := askSetup(dir, setupRequest{Language: language})
	if err != nil {
		return err
	}
	switch {
	case !answered:
		fmt.Fprintln(out, setupNoServer(dir))
	case state.Facts.Configured:
		fmt.Fprintf(out, "Leafen's language is now %s; the window follows.\n", language)
	default:
		fmt.Fprintf(out, "Kept: the account the person starts with takes %s.\n", language)
	}
	return nil
}

func runSetupSkip(out io.Writer, dir, step string) error {
	if !slices.Contains(setupStepNames, step) {
		return fmt.Errorf("no step %q; one of: %s", step, strings.Join(setupStepNames, ", "))
	}
	if appFirstScreen(out) {
		return nil
	}
	state, answered, err := askSetup(dir, setupRequest{Skip: step})
	if err != nil {
		return err
	}
	switch {
	case !answered:
		fmt.Fprintln(out, setupNoServer(dir))
	case !state.Facts.Configured:
		fmt.Fprintln(out, "Leafen isn't set up yet: start it first (leafen setup start).")
	default:
		fmt.Fprintf(out, "Left for later: %s. %s brings it back.\n", step, mcpScreen("", "settings", "preferences", "install", "finishSkipped"))
	}
	return nil
}
