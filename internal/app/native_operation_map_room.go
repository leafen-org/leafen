package app

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// The map leafen_operations answers with, asked for nothing in particular —
// every group a name, a line and its counts, the connection's grant and the
// line on what to do next — is what an agent reads first of what Leafen
// does, kept to about two thousand tokens, at most 2,600 (four characters a
// token; the operations' map decision of 2026-11-26). The server checks it
// as it makes its first MCP server, beside the instructions' room and the
// tools' descriptions (checkedMCPToolArguments), and doesn't start when a
// group's line has grown past it (2026-11-27, the gate's lap 13: the kinds of
// connection to suggest, put in the connections' group's line, took the map
// to 10,828 characters). It is measured as an MCP token of one workspace is
// answered, the longest map a connection gets: every group of the data with
// its line in the map (nativeOperationGroupAbout), as many it runs and as
// many more as the data puts in the group, and the longest grant a
// connection is given, as the answer writes it (compact JSON, its text
// unescaped, in UTF-16 units as an agent's client counts). What a group's
// own answer opens with isn't in it.

// nativeOperationsMapBudget is the map's budget, in characters.
const nativeOperationsMapBudget = 2_600 * 4

// nativeOperationsMapLongest is the map's answer at its longest.
func nativeOperationsMapLongest() (int, error) {
	data, err := nativeOperationGroupsParsed()
	if err != nil {
		return 0, err
	}
	held := map[string]int{}
	for _, entry := range data.Operations {
		held[entry.Group]++
	}
	for _, entry := range data.Tools {
		held[entry.Group]++
	}
	output := leafenMCPOperationsOutput{Next: nativeOperationsMapNext}
	for _, group := range data.Groups {
		output.Groups = append(output.Groups, leafenMCPOperationGroup{Name: group.Name, About: nativeOperationGroupAbout(group, false),
			Count: held[group.Name], More: held[group.Name]})
	}
	widen := ""
	for _, kind := range []connectionKind{connectionKindAgent, connectionKindToken, connectionKindApp} {
		if candidate := connectionWiden(kind); utf16Length(candidate) > utf16Length(widen) {
			widen = candidate
		}
	}
	output.Connection = &nativeOperationConnection{Kind: string(connectionKindAgent), Scope: userKeyScopeNames[userKeyScopeStructure],
		OrganizationsAllow: userKeyScopeNames[userKeyScopeStructure], Workspaces: 999, Administrator: true, Widen: widen}
	var written bytes.Buffer
	encoder := json.NewEncoder(&written)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(output); err != nil {
		return 0, err
	}
	return utf16Length(string(bytes.TrimSpace(written.Bytes()))), nil
}

// checkNativeOperationsMapRoom says whether the map runs past its budget.
func checkNativeOperationsMapRoom() error {
	length, err := nativeOperationsMapLongest()
	if err != nil {
		return err
	}
	if length > nativeOperationsMapBudget {
		return fmt.Errorf("leafen_operations' map runs past its budget (native_operation_map_room.go): %d of %d characters", length,
			nativeOperationsMapBudget)
	}
	return nil
}
