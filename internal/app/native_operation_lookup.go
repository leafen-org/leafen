package app

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// An operation is named to leafen_operations (operation) and to leafen_call
// (tool) the ways the lists show it, and each is found by them (2026-11-26
// scorecard: an agent asked for setStrict and writePageVerification, the
// names a group lists beside their ids, and for a path with its values
// filled in, and was answered with an empty list and nothing else, eleven
// times in nine runs; given to leafen_call as a tool, setStrict was "unknown
// tool"). Its id, or the same method and path with the path's values named
// otherwise; the method and a path with its values filled in, as leafen_call
// sends it; or its name, which several may share. What none of these finds
// is answered with what its words find, and how an operation is named.

// nativeOperationLookupMax is how many operations of one name an answer lists.
const nativeOperationLookupMax = 10

// nativeOperationsSameNameLine says how many operations share a name, and
// how many of them the answer lists where it lists fewer.
func nativeOperationsSameNameLine(count int, name string, listed int) string {
	if listed < count {
		return fmt.Sprintf(nativeOperationsSameMany, count, name, listed)
	}
	return fmt.Sprintf(nativeOperationsSameName, count, name)
}

// nativeOperationsLookedUp are the operations a name given for one names.
func nativeOperationsLookedUp(operations []nativeOperation, wanted string) []nativeOperation {
	wanted = strings.TrimSpace(wanted)
	for _, operation := range operations {
		if operation.ID == wanted || nativeOperationShape(operation.ID) == nativeOperationShape(wanted) {
			return []nativeOperation{operation}
		}
	}
	if method, path, ok := strings.Cut(wanted, " "); ok {
		return nativeOperationsOfFilledPath(operations, strings.ToUpper(method), strings.TrimSpace(path))
	}
	named := []nativeOperation{}
	for _, operation := range operations {
		if strings.EqualFold(operation.Name, wanted) {
			named = append(named, operation)
		}
	}
	return named
}

// nativeOperationsOfFilledPath are the operations of a method whose path a
// filled-in one fits — each of its fixed parts the same, a value in each of
// its named ones — those with the most fixed parts the same, as the router
// picks a route.
func nativeOperationsOfFilledPath(operations []nativeOperation, method, path string) []nativeOperation {
	path, _, _ = strings.Cut(path, "?")
	given := strings.Split(strings.Trim(path, "/"), "/")
	best, found := -1, []nativeOperation{}
	for _, operation := range operations {
		operationMethod, operationPath, _ := strings.Cut(operation.ID, " ")
		if operationMethod != method {
			continue
		}
		fixed, fits := nativeOperationPathFits(strings.Split(strings.Trim(operationPath, "/"), "/"), given)
		switch {
		case !fits || fixed < best:
		case fixed > best:
			best, found = fixed, []nativeOperation{operation}
		default:
			found = append(found, operation)
		}
	}
	return found
}

// nativeOperationPathFits is whether a filled-in path fits an operation's,
// and how many of its fixed parts it matched; a last {name...} takes the
// rest.
func nativeOperationPathFits(pattern, given []string) (int, bool) {
	fixed := 0
	for index, part := range pattern {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "...}") {
			return fixed, len(given) > index
		}
		if index >= len(given) || given[index] == "" {
			return 0, false
		}
		if !strings.HasPrefix(part, "{") {
			if part != given[index] {
				return 0, false
			}
			fixed++
		}
	}
	return fixed, len(given) == len(pattern)
}

// nativeOperationLookupWords are the words of a name or a path an operation
// was asked for by, for finding what they hold: a name's parts
// (writePageVerification: write page verification), a path's fixed parts.
func nativeOperationLookupWords(wanted string) string {
	words := []string{}
	for _, part := range strings.FieldsFunc(wanted, func(r rune) bool { return r == '/' || r == ' ' || r == '-' || r == '_' || r == '.' }) {
		if strings.HasPrefix(part, "{") || strings.ToUpper(part) == part || strings.ContainsFunc(part, unicode.IsDigit) {
			continue // a value, a method or an ID
		}
		start := 0
		for index, r := range part {
			if index > 0 && unicode.IsUpper(r) {
				words = append(words, strings.ToLower(part[start:index]))
				start = index
			}
		}
		words = append(words, strings.ToLower(part[start:]))
	}
	return strings.Join(words, " ")
}

// answerNativeOperationLookupNone answers a name nothing is named by: what
// its words find, and how an operation and a tool are named.
func answerNativeOperationLookupNone(output *leafenMCPOperationsOutput, operations []nativeOperation, tools []nativeOperationTool,
	grant *nativeOperationGrant, wanted string) {
	if words := nativeOperationLookupWords(wanted); words != "" {
		answerNativeOperationSearch(output, operations, tools, grant, words, false)
	}
	if len(output.Operations) > nativeOperationLookupMax/2 {
		output.Operations = output.Operations[:nativeOperationLookupMax/2]
	}
	output.Hidden = ""
	output.Next = fmt.Sprintf(nativeOperationsNotNamed, wanted)
	// A tool of the server's administration this connection doesn't reach is
	// named, with why and the way that does (connection_kind_ways.go).
	if grant != nil {
		if refusal := serverAdministrationToolRefusal(grant.kind, grant.reachesAll, wanted); refusal != "" {
			output.Next = wanted + " isn't on this connection's list: " + refusal
		}
	}
}

// nativeOperationNotATool is what leafen_call says when its tool is no tool:
// the operation of that name or id with how to run it, those that share the
// name, or how tools and operations are named.
func nativeOperationNotATool(tool string) string {
	operations, err := listNativeOperations()
	if err != nil {
		return fmt.Sprintf(nativeOperationsNoTool, tool)
	}
	found := nativeOperationsLookedUp(operations, tool)
	switch len(found) {
	case 0:
		return fmt.Sprintf(nativeOperationsNoTool, tool)
	case 1:
		method, path, _ := strings.Cut(found[0].ID, " ")
		return fmt.Sprintf(nativeOperationsIsOperation, tool, method, path, found[0].ID)
	}
	ids := []string{}
	for _, operation := range found[:min(len(found), nativeOperationLookupMax)] {
		ids = append(ids, operation.ID)
	}
	slices.Sort(ids)
	return fmt.Sprintf(nativeOperationsNamedSo, tool, strings.Join(ids, ", "))
}

// The lines a name given for an operation or a tool is answered with.
const (
	nativeOperationsSameName = "%d operations are named %s. Give one's id as operation for what a call sends it."
	nativeOperationsSameMany = "%d operations are named %s; these are the first %d. Give one's id as operation for what a call sends it."
	nativeOperationsNotNamed = "No operation or tool is named %q. operation takes an operation's id as METHOD /path, the way a group or a search " +
		"lists it, its method and a path with the values filled in, its name, or a tool's name; these are what its words find."
	nativeOperationsNoTool = "No tool is named %q. A group's or a search's answer from leafen_operations lists the tools (tools) beside the operations; " +
		"an operation runs with leafen_call's method and path."
	nativeOperationsIsOperation = "%s is an operation, not a tool: run it with leafen_call's method %s and path %s, its values filled in " +
		"(leafen_operations with operation %q says what its body takes)."
	nativeOperationsNamedSo = "%s is the name of operations, not of a tool: %s. Run one with leafen_call's method and path, after " +
		"leafen_operations with its id as operation says what its body takes."
)
