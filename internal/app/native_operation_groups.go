package app

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
	"unicode"
)

// leafen_operations finds operations as an agent picks them, not by reading
// what it says (2026-11-26, the user: the hand-written rules that read a
// request's words grew a table for every language and still missed one in
// five of a fixed set — "처음부터 다시 생각해봐"). The common work goes
// with the big tools on the agent's list; everything else is in groups,
// each named for what a person sets out to do, that the agent reads and
// chooses from: called with no arguments it answers the groups, a line and
// a count each; given one it answers that group's operations and Leafen's
// tools in it, leaving out what this connection can't run behind one line;
// given English words it finds them in each one's name, summary and
// keywords (native_operation_find.go). Which group each operation and tool
// is in, its one-line summary and the few other names it goes by are data
// kept beside the operations (native_operation_groups.json), not rules.

//go:embed native_operation_groups.json
var nativeOperationGroupsJSON []byte

// nativeOperationGroupDefinition is one group as the data names it.
type nativeOperationGroupDefinition struct {
	Name  string `json:"name"`
	About string `json:"about"`
}

// nativeOperationGroupEntry is what the data keeps of one operation or tool:
// its group, its summary (an operation's) and the other names it goes by.
type nativeOperationGroupEntry struct {
	Group    string   `json:"group"`
	Summary  string   `json:"summary,omitempty"`
	Keywords []string `json:"keywords,omitempty"`
}

type nativeOperationGroupsData struct {
	Groups     []nativeOperationGroupDefinition     `json:"groups"`
	Operations map[string]nativeOperationGroupEntry `json:"operations"`
	Tools      map[string]nativeOperationGroupEntry `json:"tools"`
}

// What has no place in the data: an operation added since, listed in a group
// of its own so it is still found (and a check of the data names it), and an
// installed app's own tools, which are the apps group's.
const (
	nativeOperationOtherGroup = "other"
	nativeOperationAppsGroup  = "apps"
)

// nativeOperationGroupsParsed is the data with the operations by their shape
// (nativeOperationShape), as a route and the data may name a path's values
// differently.
var nativeOperationGroupsParsed = sync.OnceValues(func() (nativeOperationGroupsData, error) {
	data := nativeOperationGroupsData{}
	if err := json.Unmarshal(nativeOperationGroupsJSON, &data); err != nil {
		return data, err
	}
	shaped := make(map[string]nativeOperationGroupEntry, len(data.Operations))
	for id, entry := range data.Operations {
		shaped[nativeOperationShape(id)] = entry
	}
	data.Operations = shaped
	return data, nil
})

// nativeOperationGroupAbout is a group's line, in the map or as its own
// answer opens it: the connections' group's says there which kind to
// suggest, and the map only that it does (connectionKindsAbout).
func nativeOperationGroupAbout(group nativeOperationGroupDefinition, opened bool) string {
	if group.Name == connectionKindsGroup {
		return connectionKindsAbout(group.About, opened)
	}
	return group.About
}

// nativeOperationGroupOpened is the line a group's own answer opens with.
func nativeOperationGroupOpened(name string) string {
	data, _ := nativeOperationGroupsParsed()
	for _, group := range data.Groups {
		if group.Name == name {
			return nativeOperationGroupAbout(group, true)
		}
	}
	return ""
}

// nativeOperationGroupEntryOf is what the data keeps of an operation.
func nativeOperationGroupEntryOf(id string) nativeOperationGroupEntry {
	data, err := nativeOperationGroupsParsed()
	entry, kept := data.Operations[nativeOperationShape(id)]
	if err != nil || !kept || entry.Group == "" {
		entry.Group = nativeOperationOtherGroup
	}
	return entry
}

// mcpToolGroupEntryOf is what the data keeps of a tool.
func mcpToolGroupEntryOf(name string) nativeOperationGroupEntry {
	data, err := nativeOperationGroupsParsed()
	entry, kept := data.Tools[name]
	switch {
	case err == nil && kept && entry.Group != "":
		return entry
	case mcpAppOwnTool(name):
		entry.Group = nativeOperationAppsGroup
	default:
		entry.Group = nativeOperationOtherGroup
	}
	return entry
}

// nativeOperationSummary is the one line an operation is listed with, where
// its name doesn't say it all: the data's, else what it does where its name
// doesn't say it.
func nativeOperationSummary(operation nativeOperation) string {
	if summary := nativeOperationGroupEntryOf(operation.ID).Summary; summary != "" {
		return summary
	}
	return operation.Does
}

// nativeOperationHandlerWords are a handler's name's words in lowercase:
// createPersonPassword is create, person and password.
func nativeOperationHandlerWords(name string) []string {
	var words []string
	var word strings.Builder
	runes := []rune(name)
	for index, r := range runes {
		boundary := unicode.IsUpper(r) && index > 0 && (unicode.IsLower(runes[index-1]) ||
			index+1 < len(runes) && unicode.IsLower(runes[index+1]))
		if boundary || !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			if word.Len() > 0 {
				words = append(words, word.String())
				word.Reset()
			}
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			word.WriteRune(unicode.ToLower(r))
		}
	}
	if word.Len() > 0 {
		words = append(words, word.String())
	}
	return words
}

// leafenMCPOperationGroup is one group as the map gives it: how many of its
// operations and tools this connection can run, how many more it would with
// more permission, and how many only its person does.
type leafenMCPOperationGroup struct {
	Name  string `json:"name"`
	About string `json:"about"`
	Count int    `json:"count"`
	More  int    `json:"more,omitzero"`
	// PersonOnly are those no key runs, which the person does on Leafen's
	// screen: listed with where, not counted as needing more permission.
	PersonOnly int `json:"person_only,omitzero"`
}

// nativeOperationRuns is whether the connection asking runs an operation, in
// part at least; a person's own session (no grant) runs what it may.
func nativeOperationRuns(grant *nativeOperationGrant, operation nativeOperation) bool {
	if grant == nil {
		return true
	}
	can := grant.of(operation)
	return can == nativeOperationCan || strings.HasPrefix(can, nativeOperationPartlyPrefix)
}

// nativeOperationPersonOnly is an operation no key runs, its person doing it
// signed in on Leafen's screen (2026-11-26 scorecard: hidden among those that
// "need more permission", an agent told its person the switch didn't exist).
func nativeOperationPersonOnly(grant *nativeOperationGrant, operation nativeOperation) bool {
	return grant != nil && operation.rule.scope == userKeyScopeClosed
}

// nativeOperationIsNotCall is an operation no call runs, said so to a
// connection (native_operation_not_calls.go).
func nativeOperationIsNotCall(grant *nativeOperationGrant, operation nativeOperation) bool {
	return grant != nil && nativeOperationNotCall(operation.ID) != ""
}

// nativeOperationShown is whether a group or a search lists an operation
// without all: one the connection runs, or one its person does, with where.
func nativeOperationShown(grant *nativeOperationGrant, operation nativeOperation) bool {
	return nativeOperationRuns(grant, operation) || nativeOperationPersonOnly(grant, operation)
}

// nativeOperationGroupMap is every group in the data's order, with how many
// of its operations the connection runs, how many more need more permission
// and how many only its person does; what no call runs isn't counted. Tools
// are counted in by MCP (mcp_operations_found_tools.go).
func nativeOperationGroupMap(operations []nativeOperation, grant *nativeOperationGrant) []leafenMCPOperationGroup {
	data, _ := nativeOperationGroupsParsed()
	groups := make([]leafenMCPOperationGroup, 0, len(data.Groups)+1)
	at := map[string]int{}
	for _, group := range data.Groups {
		at[group.Name] = len(groups)
		groups = append(groups, leafenMCPOperationGroup{Name: group.Name, About: nativeOperationGroupAbout(group, false)})
	}
	for _, operation := range operations {
		name := nativeOperationGroupEntryOf(operation.ID).Group
		index, known := at[name]
		if !known {
			at[name], index = len(groups), len(groups)
			groups = append(groups, leafenMCPOperationGroup{Name: name, About: "Operations no group holds yet."})
		}
		switch {
		case nativeOperationRuns(grant, operation):
			groups[index].Count++
		case nativeOperationIsNotCall(grant, operation):
			// What no call runs isn't counted: the map says what an
			// agent picks from (native_operation_not_calls.go).
		case nativeOperationPersonOnly(grant, operation):
			groups[index].PersonOnly++
		default:
			groups[index].More++
		}
	}
	return groups
}

// nativeOperationGroupNamed is the group of that name, if there is one.
func nativeOperationGroupNamed(groups []leafenMCPOperationGroup, name string) (leafenMCPOperationGroup, bool) {
	wanted := strings.ToLower(strings.TrimSpace(name))
	for _, group := range groups {
		if group.Name == wanted {
			return group, true
		}
	}
	return leafenMCPOperationGroup{}, false
}

// nativeOperationListed is an operation as a group or a search lists it: its
// id, name, summary and whether it asks for confirmation, with what this
// connection lacks where it can't run all of it; its group where a search
// found it.
func nativeOperationListed(operation nativeOperation, grant *nativeOperationGrant, withGroup bool) nativeOperation {
	listed := nativeOperation{ID: operation.ID, Name: operation.Name, Confirm: operation.Confirm,
		Summary: nativeOperationSummary(operation), Handler: operation.Handler, rule: operation.rule}
	if withGroup {
		listed.Group = nativeOperationGroupEntryOf(operation.ID).Group
	}
	if grant != nil {
		if can := grant.of(operation); can != nativeOperationCan {
			listed.ThisConnection = can
		}
	}
	return listed
}

// The lines a group, a search or the map is answered with.
const (
	nativeOperationsMapNext = "Name a group as group for its operations and tools, then one as operation for what a call sends it; " +
		"run it with leafen_call. query finds them by English words."
	nativeOperationsNoGroup        = "No group is named %q; these are the groups."
	nativeOperationsNotEnglish     = "Search takes English words: give them in English, or name one of these groups as group."
	nativeOperationsFoundNone      = "Nothing holds these words; name one of these groups as group, or try other English words."
	nativeOperationsUnheld         = "No operation or tool holds %s: what you want may go by another name — the groups (call without arguments) list everything."
	nativeOperationsHiddenGroup    = "%d more in this group need more permission than this connection has: all true lists them, each with what it lacks."
	nativeOperationsHiddenFound    = "%d more found need more permission than this connection has: all true lists them, each with what it lacks."
	nativeOperationsNotCallsHidden = "%d more aren't calls — a socket, a page a browser opens, what another program sends: " +
		"all true lists them, each with why and the way that does it."
)
