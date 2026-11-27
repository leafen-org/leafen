package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
	"github.com/spf13/cobra"
)

// `leafen connect` connects an agent on the server's own computer to Leafen
// with no browser and no Leafen page (decided 2026-11-13: an agent's own
// connection is allowed inside the conversation). Whoever runs the server's
// program on its data folder reaches everything in it already, as `leafen
// account` says; the person's yes is still asked in the conversation first.
// Run without --approved, it says the one question for the agent to put to
// its person in their words, the rest left as Leafen's defaults (2026-11-19:
// an agent asked about folders, the instruction file and narrowing with it,
// in its own developer's words, and its person had to ask what it meant),
// and what to run on their yes; only then does the agent run it again with
// --approved, which makes an MCP token as the token page makes one. For
// Claude Code, Leafen hands the key to it without the agent seeing it
// (agent_connect_claude_code.go); another program gets the key, shown once
// here and again to its person on the token's page. A program that connects
// by MCP's standard sign-in on its own (a chat app's connector) still asks in
// the browser, as that standard has it. The run without --approved takes
// nothing but the name, so it changes no settings: --add-to and --approved
// come only with the person's yes, and in the first run the person is asked
// before any connect runs at all (agent_connect_first_run.go).

// How long connect waits for the account's workspaces to be listed.
const agentConnectSettleWait = 10 * time.Second

// agentConnectPlan is what a connection would be: an agent connection for
// whom, under which name — its person's own rights in all their workspaces
// (connection_person_rights.go) — and the question its person answers.
type agentConnectPlan struct {
	OwnerID    string
	Name       string
	Workspaces []storedBootstrapWorkspace
	// The address the command's outputs name for the server, and the one
	// given with --url, which the command to run next names again
	// (agent_connect_container_address.go).
	Origin, URL string
	Ask         personAsk
	// Whether the command named the account, which the command to run next
	// names again (agentConnectArgs).
	NamedAccount bool
	// The names Leafen's entry may take in the agent's settings, beside any
	// there already (agentEntryNames), and what one held there must reach to
	// be kept instead (agent_connect_reuse.go).
	EntryNames []string
	Reach      agentEntryReach
}

func connectCommand(pb *pocketbase.PocketBase) *cobra.Command {
	var name, email, account, address string
	// Flags of a command run before a connection reached all its person's
	// workspaces with their rights, taken and left aside.
	var scope, defaultWorkspace string
	var workspaces []string
	var approved, asJSON bool
	way := agentConnectWay{}
	command := &cobra.Command{
		Use:   "connect",
		Short: "Connect an agent on this computer to Leafen, once its person says yes in the conversation",
		Long: "Connects the agent named with --name to Leafen's MCP for the server's first administrator (or the account with --email " +
			"or --account), in all of that account's workspaces, with everything the account may do. First " + agentConnectFirstRun + ". Only once they say yes, run it again with --approved: it " +
			"makes the connection. With --approved, Claude Code adds --add-to claude-code: Leafen adds itself to Claude Code's " +
			"settings, for every folder (--this-folder: the folder it runs in alone), and the key never shows. Where Claude Code " +
			"isn't, as in Docker, --claude-code-json prints Leafen's entry " +
			"alone for the shell to hand to it there, with the line the first run gives: claude mcp add-json --scope user <name> " +
			"\"$(docker compose exec -T leafen leafen connect --name 'Claude Code' --approved --claude-code-json)\"; in a container that " +
			"can't tell the port its host publishes it on, and with no public address, --url gives the address Leafen is opened at. Leafen's entry " +
			"is named leafen-<server name>-<server code>, or, beside an entry of that name there already (another person's, an earlier " +
			"one), leafen-<server name>-<server code>-<the account's first name in plain letters>: nothing here removes or overwrites one, and one at " +
			"this Leafen that works as the account already is kept instead. Leafen lists the connection by the agent's name and this computer. Another program gets the MCP " +
			"address and its key, once, with the lines to add them. Works while the " +
			"server runs.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			plan, err := planAgentConnection(pb, name, email, account)
			if err != nil {
				return err
			}
			if plan.Origin, err = agentConnectAddress(pb, address); err != nil {
				return err
			}
			if strings.TrimSpace(address) != "" {
				plan.URL = plan.Origin
			}
			plan.Reach = agentConnectReachAt(pb, plan)
			if err := way.check(); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			switch {
			case !approved:
				return writeAgentConnectAsk(out, pb, plan, way, asJSON)
			case way.ClaudeCodeJSON:
				return printClaudeCodeEntry(out, cmd.ErrOrStderr(), pb, plan, way)
			case way.AddTo == agentAddToClaudeCode:
				return connectClaudeCode(out, pb, plan, way, asJSON)
			}
			return connectAgent(out, pb, plan, asJSON)
		},
	}
	command.Flags().StringVar(&name, "name", "", "the agent's name, as its person will see it in Leafen (such as \"Claude Code\")")
	command.Flags().StringVar(&email, "email", "", "the account's email, when it isn't the server's first administrator")
	command.Flags().StringVar(&account, "account", "", "the account's ID, when it isn't the server's first administrator")
	command.Flags().StringVar(&scope, "scope", "", "")
	command.Flags().StringArrayVar(&workspaces, "workspace", nil, "")
	command.Flags().StringVar(&defaultWorkspace, "default-workspace", "", "")
	for _, kept := range []string{"scope", "workspace", "default-workspace"} {
		_ = command.Flags().MarkDeprecated(kept, "an agent connection reaches all of the account's workspaces with everything it may do")
	}
	command.Flags().StringVar(&address, "url", "",
		"the address Leafen is opened at, for the agent to reach it by (needed in a container that can't tell the port its host publishes)")
	command.Flags().BoolVar(&approved, "approved", false, "the person said yes in the conversation to the question this command asked")
	command.Flags().BoolVar(&asJSON, "json", false, "print the question, or the connection, as JSON")
	command.Flags().StringVar(&way.AddTo, "add-to", "", "claude-code: add Leafen to Claude Code's settings here, so the key never shows")
	command.Flags().BoolVar(&way.ThisFolder, "this-folder", false, "with --add-to claude-code, for the folder it runs in alone, not every folder")
	command.Flags().BoolVar(&way.ClaudeCodeJSON, "claude-code-json", false,
		"print only Leafen's entry, key inside, for claude mcp add-json leafen \"$(…)\" to take where Claude Code runs (as with Docker)")
	_ = command.MarkFlagRequired("name")
	command.MarkFlagsMutuallyExclusive("email", "account")
	command.MarkFlagsMutuallyExclusive("claude-code-json", "add-to")
	command.MarkFlagsMutuallyExclusive("claude-code-json", "json")
	return command
}

// planAgentConnection reads what the command asks for.
// errAgentConnectNoWorkspace is an account with no workspace for an agent to reach.
var errAgentConnectNoWorkspace = errors.New("the account has no workspace yet; make one in Leafen first")

func planAgentConnection(app core.App, name, email, account string) (agentConnectPlan, error) {
	plan := agentConnectPlan{Name: strings.TrimSpace(name)}
	if plan.Name == "" || utf8.RuneCountInString(plan.Name) > 255 || strings.ContainsFunc(plan.Name, unicode.IsControl) {
		return plan, errors.New("give the agent's name with --name, on one line and at most 255 characters")
	}
	if strings.TrimSpace(email) != "" || strings.TrimSpace(account) != "" {
		user, err := accountForCommand(app, email, "", account)
		if err != nil {
			return plan, err
		}
		plan.OwnerID, plan.NamedAccount = user.Id, true
	} else if plan.OwnerID = setupOwner(app, readSetupState(app.DataDir())); plan.OwnerID == "" {
		return plan, errors.New("this server isn't set up yet: run `leafen setup status` for the first run, or name an account with --email")
	}
	// The list as the person's own start reads it, once the jobs touching
	// them are done (bootstrap_visibility_readiness.go) — a workspace just
	// made is in it. A command has no request that settles them first.
	settled, settleErr := settleBootstrapVisibilityProjectionForWithin(
		context.Background(), app, plan.OwnerID, agentConnectSettleWait,
	)
	// An app's workspace and one an import is staging in are the app's and
	// the import's, as the consent screen sets them apart: the rest are
	// what a key for all of them names (userKeyPersonWorkspaces).
	own, err := userKeyChoosableWorkspaces(app, plan.OwnerID)
	if err != nil {
		return plan, err
	}
	if len(own) == 0 && (settleErr != nil || !settled) {
		return plan, errors.New("the account's workspaces are still being listed; run this again in a minute")
	}
	plan.Workspaces = own
	if len(plan.Workspaces) == 0 {
		return plan, errAgentConnectNoWorkspace
	}
	plan.Ask = agentConnectAsk(app, plan)
	plan.EntryNames = agentEntryNamesHere(app, plan.OwnerID)
	plan.Reach = agentEntryReachHere(app, plan.OwnerID)
	return plan, nil
}

var (
	agentConnectAskQuestion = mcpWords{"리펜에 에이전트 “%s”를 연결할까요? (계정: %s)", "Connect the agent “%s” to Leafen? (account: %s)"}
	// What an agent connection may do: everything its person may, in all
	// their workspaces (connection_person_rights.go).
	agentConnectAskDoes  = mcpWords{"내 이름으로, 내가 리펜에서 할 수 있는 모든 일을 내 모든 워크스페이스에서 할 수 있습니다.", "It works as you, and may do everything you can in Leafen, in all your workspaces."}
	agentConnectAskLasts = mcpWords{"리펜의 %s에서 끊을 때까지 이어집니다.", "It stays connected until you disconnect it in Leafen’s %s."}
	// The settings list where a person sees and disconnects their agents.
	agentConnectConnectedScreen = []string{"settings", "features", "agents", "agentsConnected"}
)

// agentConnectAsk is the question for the person, in their language: which
// agent, as whom, what it may do, and for how long.
func agentConnectAsk(app core.App, plan agentConnectPlan) personAsk {
	language, err := serverTextLanguage(app, plan.OwnerID, nil)
	if err != nil {
		language = accountLanguageEn
	}
	account := plan.OwnerID
	if user, err := app.FindRecordById("users", plan.OwnerID); err == nil {
		account = firstNonempty(strings.TrimSpace(user.GetString("name")), user.Email(), plan.OwnerID)
	}
	return personAsk{Question: fmt.Sprintf(agentConnectAskQuestion.in(language), plan.Name, appOneLine(account, 80)),
		Effect: agentConnectEffect(app, plan.OwnerID, language), Language: language}
}

// agentConnectEffect is what allowing an agent's connection gives, in the
// person's language: everything they may, in all their workspaces, until
// they disconnect it on their own screen, named in its words. Every way an
// agent asks to connect asks with it (2026-11-23: an agent's request from
// another computer said "Leafen's %s", its screen left out).
func agentConnectEffect(app core.App, ownerID, language string) string {
	disconnect := mcpScreen(mcpScreenLanguage(app, ownerID), agentConnectConnectedScreen...)
	return agentConnectAskDoes.in(language) + " " + fmt.Sprintf(agentConnectAskLasts.in(language), disconnect)
}

// writeAgentConnectAsk tells the agent to ask its person the one question,
// in their words, and what to run on their yes — or, for Claude Code here
// holding a connection to this Leafen that works already, that nothing is
// asked or made (agent_connect_reuse.go).
func writeAgentConnectAsk(out io.Writer, app core.App, plan agentConnectPlan, way agentConnectWay, asJSON bool) error {
	if kept := keptClaudeCodeEntry(plan, way); kept != "" {
		return writeAgentEntryKept(out, app, plan, kept, asJSON)
	}
	words := plan.Ask.Question + " " + plan.Ask.Effect
	message := "Left to do before connecting: one question to your person in this conversation, in their language, carrying all " +
		"that this says: “" + words + "” Nothing else goes with it: every folder is Leafen's default, not a choice to offer or explain " +
		"unless they ask (then --this-folder for Claude Code). The answer is theirs " +
		"alone. Only on their yes: " + agentConnectNext(plan, way) + " On a no, nothing more."
	if asJSON {
		return json.NewEncoder(out).Encode(map[string]any{"status": "ask_person", "ask": plan.Ask, "message": message})
	}
	_, err := fmt.Fprintln(out, message)
	return err
}

// agentConnectOrigin is the address an agent on this computer reaches the
// running server at, as the server last said it; else its public one.
func agentConnectOrigin(app core.App) string {
	if here := strings.TrimRight(readSetupState(app.DataDir()).Facts.Here, "/"); here != "" {
		return here
	}
	return configuredPublicOrigin()
}

// agentConnection is a connection made: its ID, the server's address, its
// MCP address and key, and its workspaces.
type agentConnection struct {
	ID, Origin, Address, Key string
	WorkspaceIDs             []string
}

// noteCommandTunnelChoice has the command's own process read the way out
// chosen in the settings, as the server does at its start, so the lines it
// gives name the server's own address (agent_facing_address.go).
func noteCommandTunnelChoice(app core.App) {
	if serverTunnelChosen.Load() == nil {
		if settings, err := loadServerTunnelSettings(app); err == nil {
			noteChosenTunnelSettings(settings)
		}
	}
}

// makeAgentConnection makes the connection the person said yes to, listed
// in Leafen under listName (agentConnectionListName; plan.Name when "").
func makeAgentConnection(app core.App, plan agentConnectPlan, listName string) (agentConnection, error) {
	noteCommandTunnelChoice(app)
	origin := plan.Origin
	if origin == "" {
		origin = agentConnectOrigin(app)
	}
	if origin == "" {
		return agentConnection{}, errors.New(setupNoServer(app.DataDir()))
	}
	secret, err := mcpOAuthSecret(app)
	if err != nil {
		return agentConnection{}, err
	}
	input := mcpConnectionInput{RequestID: "command:" + security.RandomString(15), ClientName: firstNonempty(listName, plan.Name)}
	requestHash, err := mcpManualCompletionRequestHash(input)
	if err != nil {
		return agentConnection{}, err
	}
	result := completeManualMCPConnection(app, plan.OwnerID, input, requestHash, origin+mcpRoutePath, secret)
	var policyDenial userKeyPolicyError
	switch {
	case errors.As(result.err, &policyDenial):
		return agentConnection{}, fmt.Errorf("the organization doesn't allow this connection: %s", policyDenial.reason)
	case errors.Is(result.err, errMCPClientNotApproved):
		return agentConnection{}, errors.New("an administrator of the workspaces' organization has to approve agents connected by hand first")
	case errors.Is(result.err, errMCPConnectionCapacity):
		return agentConnection{}, errors.New("the account has as many agent connections as it may; its person disconnects one they no " +
			"longer use in Leafen's " + mcpScreen("", agentConnectConnectedScreen...) + " first")
	case errors.Is(result.err, errMCPPublicDestinationHTTPSRequired):
		return agentConnection{}, errors.New("agents connect to a public server address only over HTTPS; open Leafen on this computer " +
			"and run this again")
	case result.err != nil:
		return agentConnection{}, result.err
	}
	_, key, err := parseMCPConnectionURI(result.response.ConnectionURI, true)
	if err != nil {
		return agentConnection{}, err
	}
	return agentConnection{ID: result.response.ID, Origin: origin, Address: origin + nativeMCPPath, Key: key,
		WorkspaceIDs: result.response.WorkspaceIDs}, nil
}

// connectAgent makes the connection the person said yes to and says how
// to add it, with its key.
func connectAgent(out io.Writer, app core.App, plan agentConnectPlan, asJSON bool) error {
	made, err := makeAgentConnection(app, plan, agentConnectionListName(plan.Name, ""))
	if err != nil {
		return err
	}
	start := agentStartNext(app, plan.OwnerID, made.WorkspaceIDs)
	if asJSON {
		return json.NewEncoder(out).Encode(map[string]any{"status": "connected", "connectionId": made.ID, "name": plan.Name,
			"workspaceIds": made.WorkspaceIDs, "mcpUrl": made.Address, "key": made.Key,
			"instructionLines": agentInstructionLines(agentFacingOrigin(made.Origin)), "next": strings.Join(start, "\n")})
	}
	lines := append([]string{
		fmt.Sprintf("Connected “%s” to Leafen. Add Leafen's MCP to your program with this key; it is shown once here (its person sees it "+
			"again on the token's page in Leafen's settings), so don't repeat it in the conversation.", plan.Name),
	}, mcpAddLines(plan.EntryNames, made.Address, made.Key)...)
	lines = append(lines, "Add these lines, which hold no key, to your instruction file for every folder (~/.claude/CLAUDE.md for "+
		"Claude Code, ~/.codex/AGENTS.md for Codex, or your own), "+agentInstructionLinesNote+":", agentInstructionLines(agentFacingOrigin(made.Origin)))
	lines = append(lines, start...)
	_, err = fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

// mcpAddLines say how a program adds Leafen's MCP at address with key, under
// the first of names it holds no server under (agent_connect_alongside.go).
func mcpAddLines(names []string, address, key string) []string {
	return []string{
		"MCP address: " + address,
		"Key: " + key,
		fmt.Sprintf("Claude Code: claude mcp add --transport http --scope user %s %s --header \"Authorization: Bearer %s\"", names[0], address, key),
		fmt.Sprintf("Cursor (~/.cursor/mcp.json, under mcpServers): \"%s\": {\"url\": \"%s\", \"headers\": {\"Authorization\": \"Bearer %s\"}}",
			names[0], address, key),
		"Another program: an HTTP MCP server at that address with the header Authorization: Bearer <the key>.",
		agentKeyOneComputer,
		agentEntryBesideLine(names),
	}
}

// agentKeyOneComputer says where a printed key goes: an agent connection
// belongs to one computer, and a folder several computers open keeps only a
// narrowed MCP token (connection_kind_ways.go; MCP.md says the same).
const agentKeyOneComputer = "This key belongs to this computer: keep it in this computer's own settings or keychain, never in a " +
	"folder several computers open (a NAS's shared folder, a synced one), which keeps in its own settings only Leafen's address " +
	"and, if it needs a key, a narrowed MCP token for one workspace and the tools it uses: a folder copied or passed on whole " +
	"takes the keys inside it along."
