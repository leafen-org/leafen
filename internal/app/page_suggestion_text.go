package app

import (
	"encoding/json"
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/reearth/ygo/crdt"
)

// Suggesting changes a block's text in place: suggested text goes in marked
// "insert", text suggested away stays marked "delete", and suggested
// formatting is applied with the formatting it replaces kept beside it.
// Typing over a selection is one suggestion, a replacement: its words
// suggested away and the new words suggested in before them, as Notion's
// (seen 2026-11-22 in Chrome, twice: "omega" over "echo" reads
// "omega" then struck "echo", one discussion, the caret after "omega").
// Someone's own open suggestion grows as they keep typing or deleting next
// to it, and their own suggested text is changed or removed as it is. Text
// in anyone else's suggestion is left alone. Accepting a suggestion settles
// its text as suggested, and rejecting it settles the text as it was.

// suggestionTextRun is a run of a block's text with one set of attributes,
// at its UTF-16 range.
type suggestionTextRun struct {
	Start, End int
	Text       string
	Attributes crdt.Attributes
}

func (run suggestionTextRun) suggestion() (string, string) {
	kind, _ := run.Attributes["suggestion"].(string)
	id, _ := run.Attributes["suggestionId"].(string)
	return kind, id
}

func suggestionTextRuns(text *crdt.YText) []suggestionTextRun {
	runs := []suggestionTextRun{}
	position := 0
	for _, delta := range text.ToDelta() {
		content, isText := delta.Insert.(string)
		length := 1
		if isText {
			length = len(utf16.Encode([]rune(content)))
		}
		if length == 0 {
			continue
		}
		runs = append(runs, suggestionTextRun{
			Start: position, End: position + length, Text: content, Attributes: delta.Attributes,
		})
		position += length
	}
	return runs
}

// suggestionTexts are the text each suggestion holds in a block.
func suggestionTexts(text *crdt.YText) map[string]string {
	texts := map[string]string{}
	for _, run := range suggestionTextRuns(text) {
		if _, id := run.suggestion(); id != "" {
			texts[id] += run.Text
		}
	}
	return texts
}

// suggestionWordsOfKind are the words each suggestion holds in a block in
// its runs of one kind.
func suggestionWordsOfKind(text *crdt.YText, kind string) map[string]string {
	words := map[string]string{}
	for _, run := range suggestionTextRuns(text) {
		if runKind, id := run.suggestion(); id != "" && runKind == kind {
			words[id] += run.Text
		}
	}
	return words
}

// suggestionTextBoundary reports whether a UTF-16 offset falls between two
// characters of a text.
func suggestionTextBoundary(units []uint16, offset int) bool {
	if offset < 0 || offset > len(units) {
		return false
	}
	return offset == 0 || offset == len(units) ||
		!utf16.IsSurrogate(rune(units[offset-1])) || units[offset-1] >= 0xdc00
}

// suggestionMarkAttributes reads a suggestion's formatting as text
// attributes, a mark turned off or cleared as nil. Only the formatting a
// suggestion can change is taken.
func suggestionMarkAttributes(marks map[string]any, inserting bool) (crdt.Attributes, bool) {
	attributes := crdt.Attributes{}
	if inserting {
		if !suggestedMentionAttributes(marks, attributes) {
			return nil, false
		}
	}
	for mark, value := range marks {
		if _, mention := attributes[mark]; mention {
			continue
		}
		switch mark {
		case "bold", "italic", "underline", "strikethrough", "code":
			on, ok := value.(bool)
			if !ok {
				return nil, false
			}
			attributes[mark] = nil
			if on {
				attributes[mark] = true
			}
		case "color", "background", "link":
			setting, ok := value.(string)
			if !ok || setting != "" && (mark == "color" && !validNotionBlockColor(setting) ||
				mark == "background" && !validNotionSpanBackground(setting) ||
				mark == "link" && !validGraphTextLink(setting)) {
				return nil, false
			}
			attributes[mark] = nil
			if setting != "" {
				attributes[mark] = setting
			}
		default:
			return nil, false
		}
	}
	return attributes, true
}

// suggestedMentionKeys are what a suggested mention carries.
var suggestedMentionKeys = []string{"mention", "userId", "pageId", "date", "dateEnd", "dateTimeZone", "dateReminder", "dateFormat", "dateTimeFormat"}

// suggestedMentionAttributes takes a suggested insert's mention — a person,
// a page or a date, as "@" picks one — into attributes, checked as a text
// write checks one; whether there is none or it is whole (2026-10-19: "@" in
// suggesting mode suggested the letter alone, the mention couldn't be
// suggested). A date is a day or a time in its zone, as the editor writes
// it, and keeps its reminder, as Notion's "리마인더 받기" does while
// suggesting (seen 2026-11-18: the suggested "@내일 오전 9:00" has its alarm,
// and keeps it once accepted); the reminder reminds once the text stands
// (blockTextReminders).
func suggestedMentionAttributes(marks map[string]any, attributes crdt.Attributes) bool {
	values := map[string]string{}
	for _, key := range suggestedMentionKeys {
		raw, present := marks[key]
		if !present {
			continue
		}
		value, ok := raw.(string)
		if !ok || value == "" || value != strings.TrimSpace(value) || len([]rune(value)) > commentIdentifierRunes*2 {
			return false
		}
		values[key] = value
	}
	if len(values) == 0 {
		return true
	}
	switch values["mention"] {
	case "person":
		if !validCommentIdentifier(values["userId"]) || len(values) != 2 {
			return false
		}
	case "page":
		if !validCommentIdentifier(values["pageId"]) || len(values) != 2 {
			return false
		}
	case "date":
		if values["date"] == "" || values["userId"] != "" || values["pageId"] != "" ||
			!validNotionMentionDateRange(values["date"], values["dateEnd"], values["dateTimeZone"]) ||
			!validDateMentionReminder(values["date"], values["dateReminder"]) ||
			values["dateFormat"] != "" && !validDateMentionFormat(values["dateFormat"]) ||
			values["dateTimeFormat"] != "" && !validDateMentionTimeFormat(values["dateTimeFormat"]) {
			return false
		}
	default:
		return false
	}
	for key, value := range values {
		attributes[key] = value
	}
	return true
}

// suggestedInsertAttributes are suggested text's attributes: its own
// formatting, its mention and its suggestion, and nothing from the text
// around it.
func suggestedInsertAttributes(marks crdt.Attributes, suggestionID string) crdt.Attributes {
	span := graphTextSpan{Suggestion: textSuggestionInsert, SuggestionID: suggestionID}
	span.Mention, _ = marks["mention"].(string)
	span.UserID, _ = marks["userId"].(string)
	span.PageID, _ = marks["pageId"].(string)
	span.Date, _ = marks["date"].(string)
	span.DateEnd, _ = marks["dateEnd"].(string)
	span.DateTimeZone, _ = marks["dateTimeZone"].(string)
	span.DateReminder, _ = marks["dateReminder"].(string)
	span.DateFormat, _ = marks["dateFormat"].(string)
	span.DateTimeFormat, _ = marks["dateTimeFormat"].(string)
	span.Bold, span.Italic = marks["bold"] == true, marks["italic"] == true
	span.Underline, span.Strikethrough = marks["underline"] == true, marks["strikethrough"] == true
	span.Code = marks["code"] == true
	span.Color, _ = marks["color"].(string)
	span.Background, _ = marks["background"].(string)
	span.Link, _ = marks["link"].(string)
	return graphTextAttributes(span)
}

// suggestionMarksBefore is a run's formatting before a suggestion changes
// it, and whether the suggestion changes it at all.
func suggestionMarksBefore(attributes, marks crdt.Attributes) (map[string]any, bool) {
	before := make(map[string]any, len(marks))
	changes := false
	for mark, next := range marks {
		switch mark {
		case "color", "background", "link":
			current, _ := attributes[mark].(string)
			wanted, _ := next.(string)
			before[mark] = current
			changes = changes || current != wanted
		default:
			current := attributes[mark] == true
			before[mark] = current
			changes = changes || current != (next == true)
		}
	}
	return before, changes
}

// suggestionEdit is what a suggested edit did to a block's text: the
// suggestion it made or grew and the kind of one it made, whether it
// suggested anything, and the author's own suggestions whose text it
// changed on the way.
type suggestionEdit struct {
	SuggestionID string
	Kind         string
	Grew         bool
	Changed      bool
	Revised      []string
}

// suggestionOwner reports whether a suggestion is the author's own open one
// of a kind.
type suggestionOwner func(id, kind string) bool

// growSuggestion is the author's own suggestion of a kind that a range
// touches, which the edit grows, or the new one.
func growSuggestion(runs []suggestionTextRun, start, end int, kind, newID string, own suggestionOwner) suggestionEdit {
	for _, run := range runs {
		if run.End < start || run.Start > end {
			continue
		}
		if runKind, id := run.suggestion(); runKind == kind && own(id, kind) {
			return suggestionEdit{SuggestionID: id, Grew: true}
		}
	}
	return suggestionEdit{SuggestionID: newID}
}

// suggestInsert suggests text at an offset; in a block suggested by the
// person's own open suggestion, the text grows that suggestion.
func suggestInsert(
	transaction *crdt.Transaction, text *crdt.YText, runs []suggestionTextRun,
	offset int, content string, marks crdt.Attributes, newID, blockSuggestion string, own suggestionOwner,
) suggestionEdit {
	edit := growSuggestion(runs, offset, offset, textSuggestionInsert, newID, own)
	if !edit.Grew && blockSuggestion != "" && own(blockSuggestion, textSuggestionInsert) {
		edit = suggestionEdit{SuggestionID: blockSuggestion, Grew: true}
	}
	edit.Kind, edit.Changed = textSuggestionInsert, true
	text.Insert(transaction, offset, content, suggestedInsertAttributes(marks, edit.SuggestionID))
	return edit
}

// suggestReplace suggests text in place of a range: the range's text
// suggested away and the text suggested in before it, one suggestion; the
// author's own suggested text in the range is removed instead and anyone
// else's left alone. A range with nothing left to suggest away is an insert
// where it was (suggestInsert), as typing over one's own suggested text is.
func suggestReplace(
	transaction *crdt.Transaction, text *crdt.YText, runs []suggestionTextRun,
	start, end int, content string, marks crdt.Attributes, newID, blockSuggestion string, own suggestionOwner,
) suggestionEdit {
	standing := slices.ContainsFunc(runs, func(run suggestionTextRun) bool {
		kind, id := run.suggestion()
		return kind == "" && id == "" && run.Start < end && start < run.End
	})
	revised := []string{}
	// From the end, so the runs before keep their offsets.
	for index := len(runs) - 1; index >= 0; index-- {
		run := runs[index]
		from, to := max(run.Start, start), min(run.End, end)
		if from >= to {
			continue
		}
		switch kind, id := run.suggestion(); {
		case kind == "" && id == "":
			text.Format(transaction, from, to-from, crdt.Attributes{"suggestion": textSuggestionDelete, "suggestionId": newID})
		case kind == textSuggestionInsert && own(id, kind):
			text.Delete(transaction, from, to-from)
			if !slices.Contains(revised, id) {
				revised = append(revised, id)
			}
		}
	}
	if !standing {
		edit := suggestInsert(transaction, text, runs, start, content, marks, newID, blockSuggestion, own)
		for _, id := range revised {
			if !slices.Contains(edit.Revised, id) {
				edit.Revised = append(edit.Revised, id)
			}
		}
		return edit
	}
	text.Insert(transaction, start, content, suggestedInsertAttributes(marks, newID))
	return suggestionEdit{SuggestionID: newID, Kind: textSuggestionReplace, Changed: true, Revised: revised}
}

// suggestDelete suggests a range's text away; the author's own suggested
// text in it is removed instead.
func suggestDelete(
	transaction *crdt.Transaction, text *crdt.YText, runs []suggestionTextRun,
	start, end int, newID string, own suggestionOwner,
) suggestionEdit {
	edit := growSuggestion(runs, start, end, textSuggestionDelete, newID, own)
	edit.Kind = textSuggestionDelete
	// From the end, so the runs before keep their offsets.
	for index := len(runs) - 1; index >= 0; index-- {
		run := runs[index]
		from, to := max(run.Start, start), min(run.End, end)
		if from >= to {
			continue
		}
		switch kind, id := run.suggestion(); {
		case kind == "" && id == "":
			text.Format(transaction, from, to-from, crdt.Attributes{
				"suggestion": textSuggestionDelete, "suggestionId": edit.SuggestionID,
			})
			edit.Changed = true
		case kind == textSuggestionInsert && own(id, kind):
			text.Delete(transaction, from, to-from)
			if !slices.Contains(edit.Revised, id) {
				edit.Revised = append(edit.Revised, id)
			}
		}
	}
	return edit
}

// suggestFormat suggests formatting over a range; the author's own
// suggested text in it takes the formatting as it is.
func suggestFormat(
	transaction *crdt.Transaction, text *crdt.YText, runs []suggestionTextRun,
	start, end int, marks crdt.Attributes, newID string, own suggestionOwner,
) suggestionEdit {
	edit := suggestionEdit{SuggestionID: newID, Kind: textSuggestionFormat}
	for index := len(runs) - 1; index >= 0; index-- {
		run := runs[index]
		from, to := max(run.Start, start), min(run.End, end)
		if from >= to {
			continue
		}
		switch kind, id := run.suggestion(); {
		case kind == "" && id == "":
			before, changes := suggestionMarksBefore(run.Attributes, marks)
			if !changes {
				continue
			}
			encoded, _ := json.Marshal(before)
			attributes := crdt.Attributes{
				"suggestion": textSuggestionFormat, "suggestionId": newID, "suggestionBefore": string(encoded),
			}
			for mark, value := range marks {
				attributes[mark] = value
			}
			text.Format(transaction, from, to-from, attributes)
			edit.Changed = true
		case kind == textSuggestionInsert && own(id, kind):
			text.Format(transaction, from, to-from, marks)
			if !slices.Contains(edit.Revised, id) {
				edit.Revised = append(edit.Revised, id)
			}
		}
	}
	return edit
}

// settleSuggestion accepts or rejects a suggestion's text in a block.
func settleSuggestion(
	transaction *crdt.Transaction, text *crdt.YText, runs []suggestionTextRun, suggestionID string, accept bool,
) {
	for index := len(runs) - 1; index >= 0; index-- {
		run := runs[index]
		kind, id := run.suggestion()
		if id != suggestionID {
			continue
		}
		length := run.End - run.Start
		if kind == textSuggestionInsert && !accept || kind == textSuggestionDelete && accept {
			text.Delete(transaction, run.Start, length)
			continue
		}
		attributes := crdt.Attributes{"suggestion": nil, "suggestionId": nil, "suggestionBefore": nil}
		if kind == textSuggestionFormat && !accept {
			before, _ := run.Attributes["suggestionBefore"].(string)
			for mark, value := range textSuggestionBefore(before) {
				if !textSuggestionMark(mark) {
					continue
				}
				attributes[mark] = nil
				if value != nil && value != false && value != "" {
					attributes[mark] = value
				}
			}
		}
		text.Format(transaction, run.Start, length, attributes)
	}
}
