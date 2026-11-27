package app

import "strings"

// The words a refused Notion-compatible request is told what was wrong with
// it in, in Leafen's own words and kept here so every check says them alike:
// where it failed ("body", "query", "path", "Notion-Version header"), then a
// line for each value, the path and the values as sent kept as written.

// notionFaultBodyHead starts every refusal of a request's body.
const notionFaultBodyHead = "body isn't valid"

// notionFaultLine says what the value at path takes, from expect, and what
// came (shown, as JSON writes it); a value that should be there is missing,
// and one that has no place there is said to have none.
func notionFaultLine(path string, expect []string, shown string) string {
	switch {
	case len(expect) == 1 && expect[0] == "defined":
		return notionFaultMissing(path)
	case len(expect) == 1 && expect[0] == "not present":
		return path + " has no place here, and came as `" + shown + "`."
	}
	words := make([]string, len(expect))
	for index, one := range expect {
		words[index] = notionFaultExpectWords(one)
	}
	return path + " takes " + notionOrList(words) + ", not `" + shown + "`."
}

// notionFaultMissing says a value that should be at path isn't.
func notionFaultMissing(path string) string {
	return path + " is missing."
}

// notionFaultExpectWords is how one thing a value may be is said after
// "takes".
func notionFaultExpectWords(expect string) string {
	switch {
	case expect == "populated":
		return "something that isn't empty"
	case expect == "defined":
		return "a value"
	case expect == "not present":
		return "nothing"
	case strings.HasPrefix(expect, "≤ "):
		return "at most " + strings.TrimPrefix(expect, "≤ ")
	case strings.HasPrefix(expect, "≥ "):
		return "at least " + strings.TrimPrefix(expect, "≥ ")
	}
	return expect
}

// notionFaultSaid is a refusal at where of one line, or of several any one
// of which fixes it.
func notionFaultSaid(where string, lines ...string) string {
	if len(lines) == 1 {
		return where + " isn't valid: " + lines[0]
	}
	return notionFaultChoicesSaid(where, lines...)
}

// notionFaultChoicesSaid is a refusal at where that any one of lines fixes,
// however many there are.
func notionFaultChoicesSaid(where string, lines ...string) string {
	return where + " isn't valid; any one of these fixes it:\n" + strings.Join(lines, "\n")
}

// notionBodyFault is a refusal of one value of a request's body.
func notionBodyFault(path, expect, shown string) string {
	return notionFaultSaid("body", notionFaultLine(path, []string{expect}, shown))
}

// notionFaultOnlyOne refuses a request that gives two values of which it
// takes one, the API's and the MCP tools' alike.
func notionFaultOnlyOne(one, other string) string {
	return "Give " + one + " or " + other + ", not both."
}
