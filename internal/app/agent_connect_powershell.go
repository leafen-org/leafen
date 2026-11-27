package app

import (
	"strings"

	"leafen.org/leafen/internal/commandline"
)

// Leafen's program gives each line it has Claude Code run for Windows
// PowerShell too, beside the one for a POSIX shell, under the same rules: an
// entry Claude Code holds at this Leafen that works is kept; otherwise
// Leafen goes in under the first name free, beside what is there, and the
// key shows in no output (user 2026-11-21: on a Windows PC without bash,
// Claude Code rewrote the connection text's bash command into PowerShell
// itself, and the key's save broke on how Windows PowerShell 5.1 passes
// quotes to a program). So no line hands a program JSON in an argument:
// Claude Code takes Leafen's address and header as plain arguments (`claude
// mcp add --transport http … --header`, whose output, which repeats the
// header, is kept back), and requests go through PowerShell's own web
// commands. They run in Windows PowerShell 5.1 and PowerShell 7 alike. What
// fails says so in plain words, never as PowerShell's error record
// (psNativeText, psReason). The web's text says the same
// (web/src/lib/agentConnection.ts).

// psQuoted is PowerShell's single-quoted string, typographic single quotes
// doubled with the ASCII one (commandline.PowerShellQuoted).
func psQuoted(value string) string {
	return commandline.PowerShellQuoted(value)
}

const (
	psStart = "$ErrorActionPreference = 'Continue'; $ProgressPreference = 'SilentlyContinue'; "
	// psJSONBody is the type a request's JSON body goes with: Windows
	// PowerShell 5.1 sends a body whose type names no charset as ISO-8859-1,
	// and a name in it ("June’s 시험") reached Leafen as "June's ??"
	// (2026-11-25); with charset=utf-8 it goes as UTF-8, in PowerShell 7 too.
	psJSONBody = "-ContentType 'application/json; charset=utf-8'"
	// Claude Code's own program: the one running the line when it is Claude
	// Code's, else claude on the PATH — its .exe or .cmd, never a .ps1 an
	// execution policy may refuse.
	psClaudeCode = "$C = $env:CLAUDE_CODE_EXECPATH; if ($C -and (Split-Path -Leaf $C) -notlike 'claude*') { $C = '' }; " +
		"if (-not $C) { $C = (Get-Command claude -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1).Source }; "
	// psReason is a caught error in plain words: what the server answered,
	// and the innermost reason (a .NET call's error names the call, not why
	// it failed).
	psReason = `"$($_.ErrorDetails.Message) $($_.Exception.GetBaseException().Message)".Trim()`
)

// psNativeText is a program's output with what it says on its error output
// as plain lines, joined by line feeds. Under 2>&1 Windows PowerShell 5.1
// makes each such line an error record, which Out-String prints with
// PowerShell's own position and category lines (위치 줄:1 문자:… CategoryInfo
// … NativeCommandError) between the program's words and what the line says
// next (2026-11-21, a failed save on the real Claude Code 2.1.330), and
// Out-String wraps at the console's width, which could split the key the
// line then hides.
func psNativeText(command string) string {
	return "((" + command + ` 2>&1 | ForEach-Object { "$_" }) -join [char]10)`
}

// psAgentEntryCheck is agentEntryCheckLine for PowerShell: one entry at $A,
// its Authorization header's value in $K, named $M, kept in $W when Leafen
// takes its key for the reach and kept holds, its name added to $R when
// Leafen refuses it.
func psAgentEntryCheck(reach agentEntryReach, kept string) string {
	check := apiRoot + mcpOwnConnectionPath
	if reach.Query != "" {
		check += "?" + reach.Query
	}
	condition := "$H -eq 204"
	if kept != "" {
		condition += " -and " + kept
	}
	return "$H = 0; try { $H = [int](Invoke-WebRequest -UseBasicParsing -Uri (($A -replace " + psQuoted(nativeMCPPath+"$") + ", '') + " +
		psQuoted(check) + ") -Headers @{ Authorization = $K }).StatusCode } catch { if ($_.Exception.Response) " +
		"{ $H = [int]$_.Exception.Response.StatusCode } }; if (" + condition + ") { $W = $M; break }; if ($H -eq 401) { $R += \" $M\" }; "
}

// psClaudeCodeHeldLine is claudeCodeHeldLine for PowerShell.
func psClaudeCodeHeldLine(reach agentEntryReach) string {
	if len(reach.Addresses) == 0 {
		return ""
	}
	quoted := []string{}
	for _, address := range reach.Addresses {
		quoted = append(quoted, psQuoted(address))
	}
	return "$P = @(" + strings.Join(quoted, ", ") + "); foreach ($L in @(& $C mcp list 2>$null)) { " +
		`if ("$L" -match '^\s*(.+?): (\S+) \(HTTP\)' -and $P -contains $Matches[2]) { $M = $Matches[1]; ` +
		"$G = (& $C mcp get $M 2>$null) -join [char]10; " +
		`if ($LASTEXITCODE -eq 0 -and $G -match '(?m)^\s*URL:\s*(\S+)' -and $P -contains $Matches[1]) { $A = $Matches[1]; ` +
		`if ($G -match '(?m)^\s*Authorization:\s*(.+?)\s*$') { $K = $Matches[1]; ` +
		psAgentEntryCheck(reach, `$G -match "(?mi)^\s*Scope:\s*$S "`) + "} } } }; "
}

// claudeCodeAddLinePowerShell is claudeCodeAddLine for PowerShell: source is
// an expression whose value is Leafen's entry as `claude mcp add-json` takes
// it, run only once no entry at this Leafen works and a name is free.
func claudeCodeAddLinePowerShell(scope string, names []string, reach agentEntryReach, source, again, kept string) string {
	quoted := []string{}
	for _, name := range names {
		quoted = append(quoted, psQuoted(name))
	}
	failed := `"Claude Code didn't add Leafen: $O"`
	if again != "" {
		failed += " + " + psQuoted(" "+again)
	}
	return psStart + psClaudeCode + "$S = " + psQuoted(scope) + "; $W = ''; $R = ''; " +
		"if (-not $C) { 'Claude Code''s program (claude) is not found here: nothing was made' } else { " + psClaudeCodeHeldLine(reach) +
		"if ($W) { " + kept + `"Claude Code is connected to this Leafen already as $W, which works: nothing was made" } ` +
		"else { $N = ''; foreach ($T in @(" + strings.Join(quoted, ", ") + ")) { & $C mcp get $T *> $null; if ($LASTEXITCODE -ne 0) { $N = $T; break } }; " +
		"if (-not $N) { " + psQuoted("Claude Code has "+strings.Join(names, ", ")+" already: nothing was made") + " } " +
		"else { $E = $null; try { $E = " + source + ` } catch { "Leafen gave no entry: " + ` + psReason + " }; " +
		"if ($E) { $V = $E.headers.Authorization; " +
		"$O = " + psNativeText(`& $C mcp add --transport http --scope $S $N $E.url --header "Authorization: $V"`) + ".Replace($V, '<key>'); " +
		`if ($LASTEXITCODE -eq 0) { "Added Leafen to Claude Code as $N"; if ($R) { "Leafen refuses the key of:$R" } } ` +
		"else { " + failed + " } } } } }"
}

// claudeCodeHTTPCallPowerShell is claudeCodeHTTPCall for PowerShell: the
// header from Claude Code's own `claude mcp get` goes straight into the
// request, and the request's body and the answer go as UTF-8; a call that
// fails says what Leafen answered.
func claudeCodeHTTPCallPowerShell(name, address string) string {
	return psStart + psClaudeCode + "if (-not $C) { 'Claude Code''s program (claude) is not found here: nothing was sent' } else { try { " +
		"$K = ((& $C mcp get " + psQuoted(name) + `) -match '^\s*Authorization:' | Select-Object -First 1) ` +
		`-replace '^\s*Authorization:\s*', ''; $B = [Text.Encoding]::UTF8.GetBytes(` +
		`'{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": {"name": "<tool>", "arguments": {…}}}'); ` +
		"$V = Invoke-WebRequest -UseBasicParsing -Method Post -Uri " + psQuoted(address) + " -Headers @{ Authorization = $K; " +
		"Accept = 'application/json, text/event-stream' } " + psJSONBody + " -Body $B; " +
		`[Text.Encoding]::UTF8.GetString($V.RawContentStream.ToArray()) } catch { "The call to Leafen's MCP failed: " + ` + psReason + " } }"
}
