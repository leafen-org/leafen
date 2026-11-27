package app

import (
	"encoding/json"
	"strconv"
	"strings"
)

// A Markdown change finds the text it changes as Notion's does (seen
// 2026-11-10, "v1" and "v2" alike). update_content takes its replacements in
// turn, each in the text the ones before it left: one whose old_str the text
// doesn't hold is left out and named in a "no_match" warning, and the change
// is refused, naming those, when none of the ones that match changes the text
// (seen 2026-11-22, Notion's MCP and API alike: a missing old_str beside one
// whose new_str is its old_str is refused); one whose old_str the text holds
// more than once, unless it replaces all of them, refuses the change. A
// selection — insert_content's after, replace_content_range's content_range —
// finds its stretch as notion_page_markdown_selection.go says. Refusals are a
// validation error in its tool's words, with the tool's template beside them.

// pageMarkdownMatchError is a Markdown change whose text the page doesn't
// hold, or holds more than once.
type pageMarkdownMatchError struct {
	message, template string
}

func (e pageMarkdownMatchError) Error() string { return e.message }

// data is what Notion's error carries beside its words.
func (e pageMarkdownMatchError) data() map[string]any {
	return notionToolErrorData("invalid_input", e.template)
}

const notionMarkdownFetchAgain = "\n\nRead the page again and take old_str from its text as it is now."

// applyNotionMarkdownUpdates makes update_content's replacements in turn.
func applyNotionMarkdownUpdates(current string, updates []notionMarkdownContentUpdate) (string, []notionMarkdownWarning, error) {
	if len(updates) > 100 {
		return "", nil, pageMarkdownRequestError{message: "update_content takes at most 100 replacements."}
	}
	updated, warnings, missed, changed := current, []notionMarkdownWarning{}, []int{}, false
	for index, replacement := range updates {
		if replacement.OldString == "" {
			return "", nil, pageMarkdownRequestError{message: "update_content.old_str needs some text."}
		}
		// A stored file's address is the page's own for its block and file
		// (notion_page_markdown_file_matches.go).
		oldString := notionMarkdownFileAddressesRebased(updated, replacement.OldString)
		matches := strings.Count(updated, oldString)
		if matches == 0 {
			missed = append(missed, index)
			warnings = append(warnings, notionMarkdownWarning{Code: "no_match", Message: "Nothing in the page matches " + replacement.OldString + "."})
			continue
		}
		if matches > 1 && !replacement.ReplaceAllMatches {
			return "", nil, pageMarkdownMatchError{
				message: `"` + replacement.OldString + `" matches ` + strconv.Itoa(matches) +
					" places. To replace them all, set replace_all_matches to true; to replace one, give more of its text " +
					"so that only it matches.",
				template: `"{normalizedOldStr}" matches {matchCount} places. To replace them all, set {replaceAllMatchesField} ` +
					"to true; to replace one, give more of its text " +
					"so that only it matches.",
			}
		}
		count := 1
		if replacement.ReplaceAllMatches {
			count = -1
		}
		updated = strings.Replace(updated, oldString, replacement.NewString, count)
		changed = changed || oldString != replacement.NewString
	}
	if len(missed) == 0 || changed {
		return updated, warnings, nil
	}
	if len(missed) == 1 {
		return "", nil, pageMarkdownMatchError{
			message:  "Nothing in the page matches " + updates[missed[0]].OldString + "." + notionMarkdownFetchAgain,
			template: "Nothing in the page matches {normalizedOldStrs}." + notionMarkdownFetchAgain,
		}
	}
	lines := make([]string, len(missed))
	for at, index := range missed {
		quoted, _ := json.Marshal(updates[index].OldString)
		lines[at] = "- index " + strconv.Itoa(index) + ": " + string(quoted)
	}
	return "", nil, pageMarkdownMatchError{
		message: "Nothing in the page matches " + strconv.Itoa(len(missed)) + " content updates:\n" + strings.Join(lines, "\n") +
			notionMarkdownFetchAgain,
		template: "Nothing in the page matches {noMatchCount} content updates:\n{noMatchContentUpdates}" + notionMarkdownFetchAgain,
	}
}
