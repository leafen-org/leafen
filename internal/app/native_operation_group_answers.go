package app

import (
	"fmt"
	"strings"
)

// The answers leafen_operations (and GET /operations) gives without one
// operation named: the groups, one group, or what English words find
// (native_operation_groups.go, native_operation_find.go). tools are Leafen's
// tools the connection's list holds, which MCP passes in
// (mcp_operations_found_tools.go) and REST, which runs none, doesn't.

// nativeOperationTool is one of Leafen's tools as leafen_operations lists,
// finds and describes it.
type nativeOperationTool struct {
	name, title, description string
	readOnly                 bool
	inputSchema              any
}

// group is the tool's group.
func (tool nativeOperationTool) group() string { return mcpToolGroupEntryOf(tool.name).Group }

// listed is the tool as a list gives it: its name, its title or its
// description's first sentence, and whether it only reads.
func (tool nativeOperationTool) listed() leafenMCPFoundTool {
	description := tool.title
	if description == "" {
		description = mcpFirstSentence(tool.description)
	}
	return leafenMCPFoundTool{Name: tool.name, Description: description, ReadOnly: tool.readOnly}
}

// findText is what the tool is found by: its name's and title's words, its
// keywords and the data's summary, where it gives one — not its description,
// whose many words found it for nearly anything (2026-11-26: the Notion
// move's verifier and the bug report's draft beside "find template").
func (tool nativeOperationTool) findText() nativeOperationFindText {
	entry := mcpToolGroupEntryOf(tool.name)
	return nativeOperationFindText{
		name:     nativeOperationFindWords(strings.Join(mcpToolNameWords(tool.name), " ") + " " + tool.title),
		keywords: nativeOperationFindWords(strings.Join(entry.Keywords, " ")),
		summary:  nativeOperationFindWords(entry.Summary),
		phrases:  nativeOperationFindPhrases(entry.Keywords),
	}
}

// mcpToolNameWords are a tool's name's words without the prefix of its
// family (notion-, leafen_), in lowercase.
func mcpToolNameWords(name string) []string {
	return strings.FieldsFunc(strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(name, "notion-"), "leafen_")), func(r rune) bool {
		return r == '-' || r == '_' || r == ' '
	})
}

// nativeOperationGroupMapWithTools is the map with the tools counted in, all
// of which the connection's list lets it call.
func nativeOperationGroupMapWithTools(operations []nativeOperation, tools []nativeOperationTool, grant *nativeOperationGrant) []leafenMCPOperationGroup {
	groups := nativeOperationGroupMap(operations, grant)
	for _, tool := range tools {
		name, counted := tool.group(), false
		for index := range groups {
			if groups[index].Name == name {
				groups[index].Count++
				counted = true
				break
			}
		}
		if !counted {
			groups = append(groups, leafenMCPOperationGroup{Name: name, About: "Operations no group holds yet.", Count: 1})
		}
	}
	return groups
}

// answerNativeOperationGroups answers with the groups and a line on what to
// do with them.
func answerNativeOperationGroups(output *leafenMCPOperationsOutput, operations []nativeOperation, tools []nativeOperationTool,
	grant *nativeOperationGrant, next string) {
	output.Groups = nativeOperationGroupMapWithTools(operations, tools, grant)
	output.Next = next
}

// answerNativeOperationGroup answers with one group's operations and tools:
// those this connection can run and those only its person does (with where),
// and one line counting the rest, unless all asks for them too.
func answerNativeOperationGroup(output *leafenMCPOperationsOutput, operations []nativeOperation, tools []nativeOperationTool,
	grant *nativeOperationGrant, name string, all bool) {
	group, known := nativeOperationGroupNamed(nativeOperationGroupMapWithTools(operations, tools, grant), name)
	if !known {
		answerNativeOperationGroups(output, operations, tools, grant, fmt.Sprintf(nativeOperationsNoGroup, strings.TrimSpace(name)))
		return
	}
	output.Group, output.About = group.Name, group.About
	if opened := nativeOperationGroupOpened(group.Name); opened != "" {
		output.About = opened
	}
	hidden, notCalls := 0, 0
	for _, operation := range operations {
		switch {
		case nativeOperationGroupEntryOf(operation.ID).Group != group.Name:
		case all || nativeOperationShown(grant, operation):
			output.Operations = append(output.Operations, nativeOperationListed(operation, grant, false))
		case nativeOperationIsNotCall(grant, operation):
			notCalls++
		default:
			hidden++
		}
	}
	for _, tool := range tools {
		if tool.group() == group.Name {
			output.Tools = append(output.Tools, tool.listed())
		}
	}
	output.Hidden = nativeOperationsHiddenLine(nativeOperationsHiddenGroup, hidden, notCalls)
}

// nativeOperationsHiddenLine is the line counting what a group or a search
// left out: what needs more permission, then what no call runs.
func nativeOperationsHiddenLine(more string, hidden, notCalls int) string {
	said := []string{}
	if hidden > 0 {
		said = append(said, fmt.Sprintf(more, hidden))
	}
	if notCalls > 0 {
		said = append(said, fmt.Sprintf(nativeOperationsNotCallsHidden, notCalls))
	}
	return strings.Join(said, " ")
}

// answerNativeOperationSearch answers with what English words find, best
// first: the operations this connection can run or its person does (all:
// every one) with their
// groups, a line counting those it can't, and up to five tools; words in
// another script, or words nothing holds, are answered with the groups.
func answerNativeOperationSearch(output *leafenMCPOperationsOutput, operations []nativeOperation, tools []nativeOperationTool,
	grant *nativeOperationGrant, query string, all bool) {
	if !nativeOperationFindEnglish(query) {
		answerNativeOperationGroups(output, operations, tools, grant, nativeOperationsNotEnglish)
		return
	}
	texts := make([]nativeOperationFindText, 0, len(operations)+len(tools))
	for _, operation := range operations {
		texts = append(texts, nativeOperationFindTextOf(operation))
	}
	for _, tool := range tools {
		texts = append(texts, tool.findText())
	}
	finder := newNativeOperationFinder(query, texts)
	if unheld := finder.unheld(query); len(unheld) > 0 {
		output.Next = fmt.Sprintf(nativeOperationsUnheld, strings.Join(unheld, ", "))
	}
	hidden, notCalls := 0, 0
	for _, found := range nativeOperationFindRanked(finder, texts) {
		switch {
		case found.index >= len(operations):
			if len(output.Tools) < nativeOperationFindTools {
				output.Tools = append(output.Tools, tools[found.index-len(operations)].listed())
			}
		case all || nativeOperationShown(grant, operations[found.index]):
			output.Operations = append(output.Operations, nativeOperationListed(operations[found.index], grant, true))
		case nativeOperationIsNotCall(grant, operations[found.index]):
			notCalls++
		default:
			hidden++
		}
	}
	output.Hidden = nativeOperationsHiddenLine(nativeOperationsHiddenFound, hidden, notCalls)
	if len(output.Operations) == 0 && len(output.Tools) == 0 {
		answerNativeOperationGroups(output, operations, tools, grant, nativeOperationsFoundNone)
	}
}

// nativeOperationToolsOfOperation are the tools in an operation's group whose
// names hold every word of the operation's (createPage: notion-create-pages).
func nativeOperationToolsOfOperation(operation nativeOperation, tools []nativeOperationTool) []leafenMCPFoundTool {
	words := nativeOperationFindWords(operation.Name)
	group := nativeOperationGroupEntryOf(operation.ID).Group
	found := []leafenMCPFoundTool{}
	for _, tool := range tools {
		if len(words) == 0 || tool.group() != group {
			continue
		}
		text := tool.findText()
		holds := true
		for _, word := range words {
			holds = holds && text.weight(word) >= nativeOperationFindNameWeight
		}
		if holds {
			found = append(found, tool.listed())
		}
	}
	return found
}
