package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// Claude Code connects to Leafen without its key passing through it
// (2026-11-19: told by `connect --approved --json` to add the key it
// printed, Claude Code wrote sixty lines of throwaway Python to keep the key
// out of its conversation, and its person was asked to allow a script they
// couldn't read). `leafen connect` gives one road for each place it runs:
//
//   - Where Claude Code runs the command itself — the desktop app's program,
//     Leafen from source — `--add-to claude-code` has Leafen run Claude Code's
//     own `claude mcp add-json` with the key, and prints none.
//   - Where it can't — Leafen in Docker, or on a server reached over ssh —
//     `--claude-code-json` prints only Leafen's entry as `claude mcp add-json`
//     takes it, for the shell to hand straight to Claude Code under a name it
//     doesn't hold yet (claudeCodeAddLine):
//     … mcp add-json --scope user "$N" "$(… connect … --approved --claude-code-json)"
//     and says the rest on standard error, which the shell shows.
//   - An agent on another computer polls its device connection with
//     "answer": "claude-code" in the same line (mcp_device_connections.go).
//
// Claude Code takes an HTTP server's header only on its command line (2.1.280
// has no standard input or environment road for it), so the key is in that
// one command's arguments for the moment it runs; it shows in no output, log
// or address, and Claude Code keeps it in its own settings as it keeps every
// server's. Leafen goes in every folder unless the person asks for one folder
// (user decision 2026-11-16: every folder by default, not asked, moved later
// by a word), and beside any entry Claude Code holds already, never in its
// place (agent_connect_alongside.go).

const (
	agentAddToClaudeCode  = "claude-code"
	claudeCodeServerName  = "leafen"
	claudeCodeEveryFolder = "user"
	claudeCodeThisFolder  = "local"
	claudeCodeCommandWait = time.Minute
)

// agentConnectWay is how the connection reaches its program: printed as a key
// (none set), added to Claude Code here, or handed to it by the shell.
type agentConnectWay struct {
	AddTo          string
	ThisFolder     bool
	ClaudeCodeJSON bool
}

func (way agentConnectWay) check() error {
	switch {
	case way.AddTo != "" && way.AddTo != agentAddToClaudeCode:
		return errors.New("--add-to takes claude-code")
	case way.ThisFolder && way.AddTo == "" && !way.ClaudeCodeJSON:
		return errors.New("--this-folder goes with --add-to claude-code (or --claude-code-json)")
	}
	return nil
}

func (way agentConnectWay) claudeCodeScope() string {
	if way.ThisFolder {
		return claudeCodeThisFolder
	}
	return claudeCodeEveryFolder
}

// runByClaudeCode says Claude Code runs this command: it sets CLAUDECODE in
// the commands it runs.
func runByClaudeCode() bool { return os.Getenv("CLAUDECODE") == "1" }

// claudeCodeProgram finds Claude Code's own program: the one running this
// command when Claude Code runs it (its desktop app's too, which puts none on
// the PATH), else the one on the PATH, else where its installer puts it.
func claudeCodeProgram() (string, bool) {
	file := func(path string) bool {
		info, err := os.Stat(path)
		return err == nil && !info.IsDir()
	}
	if running := strings.TrimSpace(os.Getenv("CLAUDE_CODE_EXECPATH")); running != "" && file(running) {
		return running, true
	}
	if found, err := exec.LookPath("claude"); err == nil {
		return found, true
	}
	if home, err := os.UserHomeDir(); err == nil {
		suffix := ""
		if runtime.GOOS == "windows" {
			suffix = ".exe"
		}
		for _, candidate := range []string{filepath.Join(home, ".local", "bin", "claude"+suffix), filepath.Join(home, ".claude", "local", "claude"+suffix)} {
			if file(candidate) {
				return candidate, true
			}
		}
	}
	return "", false
}

// claudeCodeFolder is the folder Claude Code counts as this one for its
// folder's own settings: the repository's top folder when this is in one,
// else this folder.
func claudeCodeFolder() string {
	here, err := os.Getwd()
	if err != nil {
		return ""
	}
	for folder := here; ; {
		if _, err := os.Stat(filepath.Join(folder, ".git")); err == nil {
			return folder
		}
		parent := filepath.Dir(folder)
		if parent == folder {
			return here
		}
		folder = parent
	}
}

// claudeCodeEntry is Leafen's MCP as `claude mcp add-json` takes it.
func claudeCodeEntry(address, key string) (string, error) {
	entry, err := json.Marshal(struct {
		Type    string            `json:"type"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
	}{"http", address, map[string]string{"Authorization": "Bearer " + key}})
	return string(entry), err
}

// addToClaudeCode adds the entry to Claude Code's settings in scope under
// name, one it holds no server under (freeClaudeCodeEntryName), run in
// folder; it removes and overwrites nothing. What Claude Code says goes back
// without the key.
func addToClaudeCode(program, scope, folder, name, entry, key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), claudeCodeCommandWait)
	defer cancel()
	command := exec.CommandContext(ctx, program, "mcp", "add-json", "--scope", scope, name, entry)
	command.Dir = folder
	output, err := command.CombinedOutput()
	if err == nil {
		return nil
	}
	said := appOneLine(strings.ReplaceAll(string(output), key, "<key>"), 300)
	return fmt.Errorf("Claude Code didn't add Leafen (%s): %s", err, firstNonempty(said, "it said nothing"))
}

var (
	claudeCodeEveryFolderTell = mcpWords{"이제 어느 폴더에서 Claude Code를 열어도 리펜을 쓸 수 있어요. 한 폴더에서만 쓰고 싶으면 말씀만 하세요.",
		"Claude Code can now use Leafen in any folder you open it in. If you’d rather keep it to one folder, just say so."}
	claudeCodeThisFolderTell = mcpWords{"이제 이 폴더%s에서 Claude Code를 열 때 리펜을 쓸 수 있어요. 어느 폴더에서나 쓰고 싶으면 말씀만 하세요.",
		"Claude Code can now use Leafen when you open it in this folder%s. If you’d like it in every folder, just say so."}
)

// claudeCodeAfter is what the agent does once Leafen is in Claude Code's
// settings in scope under entry ("" when the shell chose it: first, the
// server's own name for it, or the name beside an earlier one; folder ""
// when it can't be named): tell its
// person where, in their words, go on in this conversation with Leafen's
// tools (agent_connect_same_conversation.go), add the instruction lines, and
// how it moves later (againOne, againEvery: connecting once more, for one
// folder or for every one, beside the entry it has, which stays) — one text
// for every road. here says Leafen's program runs beside Claude Code, which
// ran this command, whose settings folder then names its file.
func claudeCodeAfter(language, scope, folder, origin, entry, first, againOne, againEvery string, here bool) []string {
	tell := claudeCodeEveryFolderTell.in(language)
	file := claudeCodeEveryFolderFile(here)
	// The entry left beside is asked about once, never removed unasked (agentEntryTidyAsk).
	tidy := " connection beside it then no longer serves: ask them once whether to remove it, and only on their yes disconnect it with its " +
		"own key (DELETE " + origin + apiRoot + mcpOwnConnectionPath + ", its header read from your settings and never printed) and take it out " +
		"of your settings (claude mcp remove <its name> with its scope)."
	move := "If they want it in one folder later: " + againOne + " — Leafen goes in there beside the every-folder entry, " +
		"which you leave as it is — and move the lines to that folder's own CLAUDE.md; the every-folder" + tidy
	if scope == claudeCodeThisFolder {
		named := ""
		if folder != "" {
			named = "(" + filepath.Base(folder) + ")"
			if language != accountLanguageKo {
				named = " " + named
			}
		}
		tell = fmt.Sprintf(claudeCodeThisFolderTell.in(language), named)
		file = "CLAUDE.md in the folder Claude Code runs in"
		if folder != "" {
			file = filepath.Join(folder, "CLAUDE.md")
		}
		move = "If they want it in every folder later: " + againEvery + " — Leafen goes in beside this folder's entry, which " +
			"you leave as it is — and move the lines to " + claudeCodeEveryFolderFile(here) + "; this folder's" + tidy
	}
	return append(claudeCodeSameConversation(language, tell, origin+nativeMCPPath, entry, first, here),
		"Add these lines, which hold no key, to "+file+" without asking (they come with the connection the person said yes to), "+
			agentInstructionLinesNote+":",
		agentInstructionLines(agentFacingOrigin(origin)),
		move,
	)
}

// claudeCodeEveryFolderFile is Claude Code's instruction file for every
// folder: in the settings folder it runs with, when it runs this command.
func claudeCodeEveryFolderFile(here bool) string {
	if own := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); here && own != "" {
		return filepath.Join(own, "CLAUDE.md")
	}
	return "~/.claude/CLAUDE.md"
}

// How `leafen connect` connects Claude Code again for one folder, or for
// every one.
const (
	agentConnectAgainOne   = "this command again in that folder with --this-folder"
	agentConnectAgainEvery = "this command again without --this-folder"
)

// agentInstructionLines are the lines an agent keeps in its instruction file
// for Leafen, as Leafen's settings give them (web/src/lib/agentConnection.ts):
// the person's own words, approved as they connect, which keep their address
// as Leafen says it is (agent_facing_address.go).
func agentInstructionLines(origin string) string {
	return "<!-- leafen:begin -->\nLeafen (" + origin + ") is my shared workspace and memory: search and read it before answering about my " +
		"work, and save lasting knowledge there. " + agentInstructionLinesAddress + "\n<!-- leafen:end -->"
}

// agentInstructionLinesAddress is the lines' sentence that keeps their
// address up to date.
const agentInstructionLinesAddress = "When Leafen says its address changed, update the address in these lines."

// claudeCodePipe is the line that hands the key from Leafen's program, run
// where Claude Code isn't (in its Docker container, -T keeping the key apart
// from what it says next), straight to Claude Code, under a name it doesn't
// hold yet — for a POSIX shell, and then for Windows PowerShell
// (agent_connect_powershell.go).
func claudeCodePipe(plan agentConnectPlan, way agentConnectWay) string {
	program := "<the program you ran this with>"
	if agentConnectInContainer() {
		program = "docker compose exec -T leafen leafen"
	}
	flags := " --approved --claude-code-json"
	if way.ThisFolder {
		flags += " --this-folder"
	}
	return claudeCodeAddLine(way.claudeCodeScope(), plan.EntryNames, plan.Reach, program+" "+agentConnectArgs(plan, shellQuoted)+flags, "") +
		" — in Windows PowerShell: " + claudeCodeAddLinePowerShell(way.claudeCodeScope(), plan.EntryNames, plan.Reach,
		"("+program+" "+agentConnectArgs(plan, psQuoted)+flags+" | Out-String | ConvertFrom-Json)", "", "")
}

// agentConnectNext is what the agent runs on its person's yes: for Claude
// Code, the road that keeps the key from passing through it, by where this
// command runs.
func agentConnectNext(plan agentConnectPlan, way agentConnectWay) string {
	place := "for every folder"
	if way.ThisFolder {
		place = "for the folder you run in alone"
	}
	claudeCode := way.AddTo == agentAddToClaudeCode || way.ClaudeCodeJSON || runByClaudeCode()
	_, here := claudeCodeProgram()
	switch {
	case claudeCode && here && !way.ClaudeCodeJSON:
		flags := " --approved --add-to claude-code"
		if way.ThisFolder {
			flags += " --this-folder"
		}
		return "run the same command again with" + flags + ": Leafen adds itself to Claude Code " + place +
			" and the key never passes through you. It then says what is left to do."
	case agentConnectInContainer():
		return "if you are Claude Code, run this where you run (outside this container): the key goes from Leafen to Claude Code " +
			"without showing: " + claudeCodePipe(plan, way) + ". Another program: the same command with --approved alone prints its " +
			"key, once."
	case claudeCode:
		lead := "run this where Claude Code runs"
		if !here {
			lead = "Claude Code's own program isn't where this command runs, so run this where Claude Code runs"
		}
		return lead + ": the key goes from Leafen to Claude Code without showing: " + claudeCodePipe(plan, way) + "."
	}
	return "run the same command again with --approved: it prints Leafen's MCP address and your key, once, with the lines that add them. " +
		"If you are Claude Code, add --add-to claude-code to it instead: Leafen then adds itself to Claude Code and the key never passes " +
		"through you. " + agentEntryCheckWords(plan.Reach)
}

// agentConnectArgs is the connect command for the plan, as a shell reads
// it, its words quoted so (shellQuoted, psQuoted).
func agentConnectArgs(plan agentConnectPlan, quoted func(string) string) string {
	args := "connect --name " + quoted(plan.Name)
	if plan.NamedAccount {
		args += " --account " + quoted(plan.OwnerID)
	}
	if plan.URL != "" {
		args += " --url " + quoted(plan.URL)
	}
	return args
}

// connectClaudeCode makes the connection the person said yes to and adds it
// to Claude Code here, saying how it went and never the key. A connection
// Claude Code didn't take is disconnected again, so no key is left that no
// one has.
func connectClaudeCode(out io.Writer, app core.App, plan agentConnectPlan, way agentConnectWay, asJSON bool) error {
	program, found := claudeCodeProgram()
	if !found {
		return errors.New("Claude Code's own program isn't where this command runs, and nothing was connected: run this where Claude " +
			"Code runs, so the key goes from Leafen to it without showing: " + claudeCodePipe(plan, way))
	}
	folder := claudeCodeFolder()
	working, refused := claudeCodeEntriesHere(program, folder, way.claudeCodeScope(), plan.Reach)
	if working != "" {
		return writeAgentEntryKept(out, app, plan, working, asJSON)
	}
	name, taken, err := freeClaudeCodeEntryName(program, folder, plan.EntryNames)
	if err != nil {
		return err
	}
	// Listed in Leafen by program, this computer and the folder it is kept to.
	kept := ""
	if way.ThisFolder {
		kept = folder
	}
	made, err := makeAgentConnection(app, plan, agentConnectionListName(plan.Name, kept))
	if err != nil {
		return err
	}
	entry, err := claudeCodeEntry(made.Address, made.Key)
	if err == nil {
		err = addToClaudeCode(program, way.claudeCodeScope(), folder, name, entry, made.Key)
	}
	if err != nil {
		if undo := disconnectAgentConnection(app, plan.OwnerID, made.ID); undo != nil {
			return fmt.Errorf("%w; the connection it made stays — its person disconnects “%s” in Leafen's %s", err, plan.Name,
				mcpScreen("", agentConnectConnectedScreen...))
		}
		return fmt.Errorf("%w. Nothing stays connected; run this again once that is fixed", err)
	}
	scope := way.claudeCodeScope()
	where := "for every folder (Claude Code's user settings)"
	if scope == claudeCodeThisFolder {
		where = "for this folder alone (" + folder + ", Claude Code's local settings)"
	}
	lines := []string{fmt.Sprintf("Connected “%s” to Leafen and added it to Claude Code as %s %s: the key went from Leafen straight "+
		"to Claude Code and isn't shown.", plan.Name, name, where)}
	// Beside the names taken and the entries at this Leafen it refuses, whatever their names.
	held := slices.Clone(taken)
	for _, other := range refused {
		if !slices.Contains(held, other) {
			held = append(held, other)
		}
	}
	if len(held) > 0 {
		lines = append(lines, agentEntryBesideTell(held, refused, name))
	}
	lines = append(lines, claudeCodeAfter(plan.Ask.Language, scope, folder, made.Origin, name, name, agentConnectAgainOne,
		agentConnectAgainEvery, true)...)
	lines = append(lines, agentStartNext(app, plan.OwnerID, made.WorkspaceIDs)...)
	if !asJSON {
		_, err = fmt.Fprintln(out, strings.Join(lines, "\n"))
		return err
	}
	result := map[string]any{"status": "connected", "connectionId": made.ID, "name": plan.Name,
		"workspaceIds": made.WorkspaceIDs, "mcpUrl": made.Address, "addedTo": agentAddToClaudeCode, "claudeCodeScope": scope,
		"entryName": name, "instructionLines": agentInstructionLines(agentFacingOrigin(made.Origin)), "message": strings.Join(lines, "\n")}
	if scope == claudeCodeThisFolder {
		result["folder"] = folder
	}
	return json.NewEncoder(out).Encode(result)
}

// printClaudeCodeEntry makes the connection and prints Leafen's entry alone
// for the shell to hand to Claude Code where it runs, and what comes next on
// standard error, which the shell shows apart from it.
func printClaudeCodeEntry(out, said io.Writer, app core.App, plan agentConnectPlan, way agentConnectWay) error {
	// Listed under the agent's name alone: this program runs where Claude Code
	// isn't (a container), whose computer isn't the agent's.
	made, err := makeAgentConnection(app, plan, "")
	if err != nil {
		return err
	}
	entry, err := claudeCodeEntry(made.Address, made.Key)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, entry); err != nil {
		return err
	}
	lines := append([]string{fmt.Sprintf("Connected “%s” to Leafen; Claude Code takes the key from here without showing it. "+
		"Once it says it added Leafen — as %s, or beside an entry of that name it holds already under the name it says, which "+
		"then names Leafen's entry in what follows; never remove an earlier one:", plan.Name, plan.EntryNames[0])},
		claudeCodeAfter(plan.Ask.Language, way.claudeCodeScope(), "", made.Origin, "", plan.EntryNames[0],
			"the same line again in that folder, with S=local for Claude Code and --this-folder for Leafen",
			"the same line again with S=user and without --this-folder", false)...)
	lines = append(lines, agentStartNext(app, plan.OwnerID, made.WorkspaceIDs)...)
	_, err = fmt.Fprintln(said, strings.Join(lines, "\n"))
	return err
}

// disconnectAgentConnection disconnects a connection its person made.
func disconnectAgentConnection(app core.App, ownerID, connectionID string) error {
	return app.RunInTransaction(func(txApp core.App) error {
		_, _, err := revokeMCPConnectionsInTransaction(txApp,
			mcpConnectionRevocationFilter{ConnectionID: connectionID, OwnerID: ownerID}, time.Now().UnixMilli())
		return err
	})
}

// mcpDeviceConnectionAllowedAnswer answers a check once the person allowed
// the request: nothing is made yet, and it says how Claude Code takes the
// key without it passing through the agent, and what comes after.
func mcpDeviceConnectionAllowedAnswer(event *core.RequestEvent, stored mcpDeviceConnection) error {
	origin, err := publicServerOrigin(event.Request)
	if err != nil {
		return oauthProtocolErrorFrom(event, http.StatusInternalServerError, "server_error", errorCodeMCPDeviceConnectionFailed,
			"Couldn't read the request to connect.", err)
	}
	language, err := serverTextLanguage(event.App, stored.OwnerID, nil)
	if err != nil {
		language = accountLanguageEn
	}
	workspaceIDs := []string{}
	_ = json.Unmarshal([]byte(stored.WorkspaceID), &workspaceIDs)
	reach := agentEntryReachHere(event.App, stored.OwnerID, origin)
	names := agentEntryNamesHere(event.App, stored.OwnerID)
	pipe := claudeCodeAddLine(claudeCodeEveryFolder, names, reach, fmt.Sprintf(`curl -sSf %s -H `+
		`'Content-Type: application/json' -d '{"device_code": "<device_code>", "answer": "claude-code"}'`,
		shellQuoted(origin+apiRoot+mcpDeviceConnectionTokenPath)), "") + " — in Windows PowerShell: " +
		claudeCodeAddLinePowerShell(claudeCodeEveryFolder, names, reach, "(Invoke-RestMethod -Method Post -Uri "+
			psQuoted(origin+apiRoot+mcpDeviceConnectionTokenPath)+" "+psJSONBody+` -Body '{"device_code": "<device_code>", "answer": "claude-code"}')`,
			"Run this same line again: within the request's minutes Leafen gives the key once more while the one it gave is unused.", "")
	lines := append([]string{fmt.Sprintf("Your person allowed “%s”; nothing is made until you take it. Claude Code: run this with "+
		"your device_code, so the key goes from Leafen to Claude Code without passing through you, under a name it doesn't hold "+
		"yet, unless it holds a connection to this Leafen that works already as your person, which it keeps: %s — and once it "+
		"says it added Leafen, or that it was connected already (as %s, or under the name it says, which then names Leafen's "+
		"entry in what follows; never remove an earlier one):", stored.ClientName, pipe, names[0])},
		claudeCodeAfter(language, claudeCodeEveryFolder, "", origin, "", names[0],
			"ask to connect again from that folder, with S=local in Claude Code's line", "ask to connect again", false)...)
	lines = append(lines, agentStartNext(event.App, stored.OwnerID, workspaceIDs)...)
	lines = append(lines, "Another program: poll once more without check for its key. "+agentEntryCheckWords(reach))
	event.Response.Header().Set("Cache-Control", "no-store")
	event.Response.Header().Set("Pragma", "no-cache")
	return event.JSON(http.StatusOK, map[string]any{"status": "allowed", "message": strings.Join(lines, "\n")})
}
