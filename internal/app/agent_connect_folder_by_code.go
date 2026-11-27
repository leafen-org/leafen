package app

import (
	"encoding/json"
	"fmt"

	"github.com/pocketbase/pocketbase/core"
)

// An agent connects a folder, or another program, to Leafen itself, from any
// computer (2026-11-23, the user: an agent on another computer asked to
// connect a folder saw only Claude Code's own sign-in, /mcp and a press of
// its button, and handed both to the person — "whatever a person does, the
// agent does too"). It is `leafen connect --this-folder`'s road where
// Leafen's program isn't: with its own connection's key it makes a one-time
// code (POST /mcp/connection-codes, as the screen's text makes one) for an
// agent connection, its person's in all their workspaces — the folder's own
// rules say which workspace it works in (user 2026-11-26) —, and the code's answer says the one command that,
// run in that folder, trades the code and hands what it gives straight to
// Claude Code's settings for that folder alone — the key going from Leafen
// into the settings and showing in no output, as the screen's connection
// text does (agent_connect_by_code.go). Claude Code's own sign-in (OAuth) is
// the road only for a person already on that screen.

// agentFolderConnectNext is what a code made with an agent's key answers:
// the command that connects the folder it runs in, for a POSIX shell and
// for Windows PowerShell.
func agentFolderConnectNext(app core.App, origin, code, exchangeURL, clientName, ownerID string) string {
	// The name the connection is listed under: the code's, then the computer
	// and the folder the command runs in (agentConnectionListName).
	named, _ := json.Marshal(clientName)
	opening := `{"answer": "` + agentAddToClaudeCode + `", "code": "` + code + `", "name": ` + string(named[:len(named)-1])
	kept, _ := json.Marshal(map[string]string{"code": code})
	names := agentEntryNamesHere(app, ownerID)
	reach := agentEntryReachFor([]string{origin + nativeMCPPath}, ownerID)
	keptPath := apiRoot + mcpOwnCodePath
	shell := claudeCodeAddLine(claudeCodeThisFolder, names, reach,
		agentListNameComputer+agentListNameFolder+"curl -sSf "+shellQuoted(exchangeURL)+" -H 'Content-Type: application/json' -d "+
			shellQuoted(opening)+`"`+agentListNameWords+`"`+shellQuoted(`"}`),
		`printf '%s\n' "$K" | curl -sS -X POST "${A%`+nativeMCPPath+`}`+keptPath+`" -H @- -H 'Content-Type: application/json' -d `+
			shellQuoted(string(kept))+`; echo; `)
	// $W goes as $($W) before a colon in PowerShell's double-quoted words,
	// which reads "$W:" as a variable with a scope or drive ($env:PATH) and
	// left the whole line unparsed (2026-11-25; agentConnectKept.ts the same).
	powerShell := claudeCodeAddLinePowerShell(claudeCodeThisFolder, names, reach,
		"(Invoke-RestMethod -Method Post -Uri "+psQuoted(exchangeURL)+" "+psJSONBody+" -Body ("+
			psQuoted(`{"answer": "`+agentAddToClaudeCode+`", "code": "`+code+`", "name": "`)+" + "+
			agentListNamePowerShell(string(named[1:len(named)-1]), true)+" + "+psQuoted(`"}`)+"))",
		"Run this same line again: within the code's minutes Leafen trades it once more while the key it gave is unused.",
		"try { (Invoke-RestMethod -Method Post -Uri (($A -replace "+psQuoted(nativeMCPPath+"$")+", '') + "+psQuoted(keptPath)+
			") -Headers @{ Authorization = $K } "+psJSONBody+" -Body "+psQuoted(string(kept))+").message } catch { "+
			`"Leafen didn't take the code for $($W): " + `+psReason+" }; ")
	return fmt.Sprintf("Left to do, in the folder to connect, run as it is — the key goes from Leafen straight into Claude Code's "+
		"settings for that folder alone and shows in no output: %s — in Windows PowerShell: %s. A working entry at this Leafen in that "+
		"folder's settings, as your person, is kept instead; otherwise Leafen goes in under a name the settings don't hold, beside "+
		"another person's, which stays. A new Claude Code session in "+
		"that folder then reaches all its person's workspaces; the folder's own rules (its CLAUDE.md) say which one it works in. For another program, "+
		"the same exchange with \"answer\": \"codex\" gives Codex's table, and with no answer the key, kept out of the conversation by "+
		"`leafen mcp secret`. The code lasts ten minutes.", shell, powerShell)
}

// The operations list says what the road does (native_operation_does.go).
func init() {
	nativeOperationDoesNames["POST /mcp/connection-codes"] = mcpWords{
		"이 폴더나 다른 프로그램을 리펜에 연결하기(다른 컴퓨터에서도): 폴더 연결 코드 만들기",
		"Connect this folder or another program to Leafen, from another computer too: make a folder connection’s code"}
	nativeOperationNotes[nativeOperationShape("POST /mcp/connection-codes")] = "Sent with your own connection’s key, it makes a " +
		"one-time code (clientName: your program, such as Claude Code; Leafen lists it with the computer and folder the command runs in) for an agent connection — its person’s in all their workspaces —, and its next gives the one " +
		"command that, run in the folder to connect, puts that folder’s Claude Code on Leafen, the key never showing — `leafen connect " +
		"--this-folder` where Leafen’s program isn’t; the folder’s own rules say which workspace it works in. Claude Code’s own login " +
		"(/mcp) is for a person already on that screen."
}
