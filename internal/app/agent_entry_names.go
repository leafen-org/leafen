package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"
	"golang.org/x/text/unicode/norm"
)

// Leafen's entry in an agent's settings is named after the server, the same
// in every folder and on every computer, so a person's rules, skills and
// allow lists that name its tools (mcp__leafen-tulip-cxb__…) hold everywhere
// (user 2026-11-27): leafen-<server>-<code>, its name in plain letters and
// its code (server_code.go, from the server's own ID, kept through backups,
// restores and moves), so two servers of one name differ — leafen-<code>
// when the name has no plain letters. Only where the settings hold that name
// already — another person's connection on the same computer account, an
// earlier one — does it go in beside it as leafen-<server>-<code>-<person>,
// the person connecting named by the first word of their name in plain
// letters (else of their email, else their account). Nothing is numbered and
// nothing random goes in it. Each name keeps Claude's tool names,
// mcp__<entry>__<tool>, within 64 characters for Leafen's longest tool,
// cutting the server's name alone. An entry there already is never renamed.
// In Leafen, the connections list tells them apart by program and computer
// (and folder): the name a connection is listed under carries them
// (agentConnectionListName). The web's text takes the same names
// (web/src/lib/agentEntryNames.ts).

const (
	// agentToolNameLimit is what a tool's name may be in Claude, and
	// agentLongestToolName Leafen's longest tool's name, notion-query-multiple-data-sources.
	agentToolNameLimit   = 64
	agentLongestToolName = 34
	// agentEntryNameRoom is what that leaves an entry's name: mcp__ + name + __ + tool.
	agentEntryNameRoom = agentToolNameLimit - len("mcp__") - len("__") - agentLongestToolName
	// A person's part is at most this long, so with the code it always fits.
	agentEntryPersonRoom = 12
)

// agentEntryNames are the names Leafen's entry may take, in the order tried:
// leafen-<server>-<code> (server in plain letters, agentServerLatinName;
// code lowercase), then, with a person, leafen-<server>-<code>-<person>
// (agentPersonLatinName).
func agentEntryNames(server, code, person string) []string {
	server = strings.Trim(strings.Map(func(letter rune) rune {
		if letter >= 'a' && letter <= 'z' || letter >= '0' && letter <= '9' || letter == '-' {
			return letter
		}
		return -1
	}, strings.ToLower(server)), "-")
	code = strings.ToLower(code)
	named := func(tail string) string {
		room := agentEntryNameRoom - len(claudeCodeServerName) - len(tail)
		if part := agentEntryNameCut(server, room-1); part != "" {
			return claudeCodeServerName + "-" + part + tail
		}
		return claudeCodeServerName + tail
	}
	codeTail := ""
	if code != "" {
		codeTail = "-" + code
	}
	names := []string{named(codeTail)}
	if person = agentEntryNameCut(agentPersonLatinWord(person), agentEntryPersonRoom); person != "" {
		names = append(names, named(codeTail+"-"+person))
	}
	return names
}

// agentServerLatinName is the server's part of the names: its name in plain
// letters (serverLatinName), none when the name has none, and the alias it
// was made with when it has no name.
func agentServerLatinName(name, alias string) string {
	if strings.TrimSpace(name) == "" {
		return alias
	}
	return serverLatinName(name, "")
}

// agentEntryNameCut is name kept to room letters, with no dash at its end.
func agentEntryNameCut(name string, room int) string {
	if len(name) > room {
		name = name[:max(room, 0)]
	}
	return strings.TrimRight(name, "-")
}

// agentPersonLatinName is how an entry beside another names its person: the
// first word of their name in plain letters (accents left off), else of
// their email's name, else their account's ID.
func agentPersonLatinName(name, email, id string) string {
	local, _, _ := strings.Cut(email, "@")
	for _, text := range []string{name, local, id} {
		if word := agentPersonLatinWord(text); word != "" {
			return word
		}
	}
	return ""
}

// agentPersonLatinWord is the first run of plain letters and digits in text,
// lowercase, its accents left off.
func agentPersonLatinWord(text string) string {
	var word strings.Builder
	for _, letter := range norm.NFD.String(strings.ToLower(text)) {
		switch {
		case letter >= 'a' && letter <= 'z', letter >= '0' && letter <= '9':
			word.WriteRune(letter)
		case unicode.Is(unicode.Mn, letter):
		case word.Len() > 0:
			return word.String()
		}
	}
	return word.String()
}

// agentEntryNamesHere are the names on this server for the person ownerID
// ("" names no one).
func agentEntryNamesHere(app core.App, ownerID string) []string {
	person := ""
	if user, err := app.FindRecordById("users", ownerID); ownerID != "" && err == nil {
		person = agentPersonLatinName(user.GetString("name"), user.Email(), user.Id)
	}
	code, _ := loadServerCode(app)
	settings, err := loadAdministratorSettings(app)
	if err != nil {
		return agentEntryNames("", code, person)
	}
	return agentEntryNames(agentServerLatinName(settings.ServerName, settings.ServerAlias), code, person)
}

// agentConnectionListName is the name a connection made on this computer is
// listed under in Leafen: its program, then this computer and the folder it
// is kept to ("" for every folder), so its person tells apart connections
// whose entries share a name. A container's own name, which is no
// computer its person knows, is left out.
func agentConnectionListName(program, folder string) string {
	parts := []string{program}
	if computer, err := os.Hostname(); err == nil && !agentConnectInContainer() {
		if computer, _, _ = strings.Cut(strings.TrimSpace(computer), "."); computer != "" {
			parts = append(parts, computer)
		}
	}
	if folder != "" {
		parts = append(parts, filepath.Base(folder))
	}
	name := strings.Join(parts, " · ")
	for utf8.RuneCountInString(name) > 255 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return name
}

// A connection made by a command an agent runs is listed the same way: the
// command names it by its program, the computer it runs on (uname -n, its
// first part; Environment.MachineName in PowerShell) and, kept to one
// folder, that folder, each in plain letters, after " · " — written in the
// trade's JSON as ·, so no shell or PowerShell changes it. The web's
// text names them the same (agentTradeName in web/src/lib/agentEntryNames.ts).
const (
	// agentListNameComputer sets U, in a POSIX shell, to this computer's name;
	// agentListNameFolder sets D to this folder's.
	agentListNameComputer = `U="$(uname -n 2>/dev/null | sed 's/[.].*//' | tr -cd 'A-Za-z0-9_-')"; `
	agentListNameFolder   = `D="$(basename "$PWD" | tr -cd 'A-Za-z0-9._-')"; `
	// agentListNameWords add them to a JSON string inside double quotes.
	agentListNameWords = `${U:+ · $U}${D:+ · $D}`
)

// agentListNamePowerShell is the same name in PowerShell, as the words of a
// JSON string: program (already a JSON string's words), this computer and,
// with folder, this folder.
func agentListNamePowerShell(program string, folder bool) string {
	parts := psQuoted(program) + ", ([Environment]::MachineName -replace '[^A-Za-z0-9_-]', '')"
	if folder {
		parts += ", ((Split-Path -Leaf (Get-Location).Path) -replace '[^A-Za-z0-9._-]', '')"
	}
	return "((@(" + parts + `) | Where-Object { $_ }) -join ' · ')`
}

// claudeCodeLeafenEntryName is the one entry Claude Code here holds at a
// Leafen's MCP address, for a call that names none: each is named after its
// server and person, so no one name is known ahead. Several says so; where
// it lists none, the name entries took before 2026-11-27, leafen.
func claudeCodeLeafenEntryName(ctx context.Context, program string) (string, error) {
	list := exec.CommandContext(ctx, program, "mcp", "list")
	list.Dir = claudeCodeFolder()
	output, _ := list.Output()
	names := []string{}
	for _, line := range strings.Split(string(output), "\n") {
		at, colon := strings.Index(line, nativeMCPPath+" (HTTP)"), strings.Index(line, ": ")
		if at > 0 && colon > 0 && colon < at {
			names = append(names, strings.TrimSpace(line[:colon]))
		}
	}
	switch len(names) {
	case 1:
		return names[0], nil
	case 0:
		return claudeCodeServerName, nil
	}
	return "", fmt.Errorf("Claude Code holds several Leafen entries here (%s): name the one to use with --entry", strings.Join(names, ", "))
}
