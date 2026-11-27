package app

import (
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// What a refused comment, reaction or edit suggestion says to fix
// (request_refusal.go): the field as the request names it and why, after
// "Check the comment request.".

// commentInvalid refuses a comment request for the reason given.
func commentInvalid(format string, args ...any) error {
	return refuse(errCommentInvalid, format, args...)
}

// commentBodyRefused refuses a body that doesn't read as the request.
func commentBodyRefused(request string, err error) error {
	return bodyRefusal(errCommentInvalid, request, err)
}

// commentIdentifierProblem is why value can't be the id field names, or "".
func commentIdentifierProblem(field, value string) string {
	switch count := utf8.RuneCountInString(value); {
	case value == "":
		return field + " is empty"
	case value != strings.TrimSpace(value):
		return field + " has spaces at its ends"
	case count > commentIdentifierRunes:
		return fmt.Sprintf("%s is %d characters, over %d", field, count, commentIdentifierRunes)
	}
	return ""
}

// commentNewIDsProblem is why a new comment's id and requestId can't be
// used, or "".
func commentNewIDsProblem(id, requestID string) string {
	if problem := commentIdentifierProblem("id", id); problem != "" {
		return problem + ": a new id of yours names the comment"
	}
	if problem := commentIdentifierProblem("requestId", requestID); problem != "" {
		return problem + ": a new id of yours, so the same request sent again isn't done twice"
	}
	return ""
}

// commentRevisionProblem is why expectedRevision can't name a comment's
// revision, or "".
func commentRevisionProblem(revision int) string {
	if revision < 1 {
		return "expectedRevision is the comment's revision as the comments list reads it, 1 or more"
	}
	return ""
}

// commentBodyRefusalProblem is why a comment's body can't be one, or "".
func commentBodyRefusalProblem(body []byte) string {
	if len(body) > commentBodyBytes {
		return fmt.Sprintf("body is %d bytes, over %d", len(body), commentBodyBytes)
	}
	if !validCommentBody(body) {
		return `body is {"rich": [{"text": "…"}]}`
	}
	return ""
}

// suggestionCreateProblem is why an edit suggestion can't be made as asked,
// or "": each kind's rule in the words leafen_operations gives it.
func suggestionCreateProblem(input suggestionCreateInput, units []uint16, validMarks bool) string {
	if problem := commentNewIDsProblem(input.ID, input.RequestID); problem != "" {
		return problem
	}
	start, end := input.Offset, input.Offset+input.Length
	switch {
	case commentIdentifierProblem("blockId", input.BlockID) != "":
		return commentIdentifierProblem("blockId", input.BlockID) + ": the text block the suggestion is in"
	case !utf8.ValidString(input.ExpectedText) || !utf8.ValidString(input.Text):
		return "expectedText and text are UTF-8 text"
	case input.Length < 0:
		return "length is 0 or more"
	case (!suggestionTextBoundary(units, start) || !suggestionTextBoundary(units, end)) && input.ExpectedText == "":
		return "expectedText is empty: it is required, the block's whole text as it is now, suggested insertions included (a 409 LEAFEN_COMMENT_SUGGESTION_TEXT_CHANGED answer gives it); offset and length count its UTF-16 units"
	case !suggestionTextBoundary(units, start) || !suggestionTextBoundary(units, end):
		return fmt.Sprintf("offset %d and length %d fall outside expectedText, %d UTF-16 units: expectedText is the block's whole text as it is now, suggested insertions included (a 409 LEAFEN_COMMENT_SUGGESTION_TEXT_CHANGED answer gives it), and offset and length count its UTF-16 units",
			input.Offset, input.Length, len(units))
	case !validMarks:
		return "marks are bold, italic, underline, strikethrough or code true or false, color, background or link a setting (\"\" clears it), and for an insert or a replace a mention"
	}
	_, suggestable := suggestionBlockTypes[input.BlockType]
	switch input.Kind {
	case textSuggestionInsert:
		switch {
		case input.Length != 0:
			return "an insert has length 0: it goes in at offset"
		case input.Text == "":
			return "an insert's text is what goes in, not empty"
		case utf8.RuneCountInString(input.Text) > maxGraphTextCharacters:
			return fmt.Sprintf("text is over %d characters", maxGraphTextCharacters)
		}
	case textSuggestionReplace:
		switch {
		case input.Length == 0:
			return "a replace covers length 1 or more units from offset: the words it suggests away, the text going in before them"
		case input.Text == "":
			return "a replace's text is what goes in, not empty: a delete suggests words away alone"
		case utf8.RuneCountInString(input.Text) > maxGraphTextCharacters:
			return fmt.Sprintf("text is over %d characters", maxGraphTextCharacters)
		}
	case textSuggestionDelete, textSuggestionFormat:
		switch {
		case input.Length == 0:
			return "a " + input.Kind + " covers length 1 or more units from offset"
		case input.Text != "":
			return "a " + input.Kind + " sends no text: it covers offset and length of expectedText"
		case input.Kind == textSuggestionDelete && len(input.Marks) != 0:
			return "a delete sends no marks"
		case input.Kind == textSuggestionFormat && len(input.Marks) == 0:
			return "a format sends the marks it suggests"
		}
	case textSuggestionBlock, textSuggestionSplit:
		switch {
		case input.Length != 0 || len(input.Marks) != 0:
			return "a " + input.Kind + " has length 0 and no marks"
		case input.Kind == textSuggestionBlock && start != len(units):
			return fmt.Sprintf("a block goes after the block's text: offset is its end, %d", len(units))
		case input.Kind == textSuggestionSplit && start >= len(units):
			return fmt.Sprintf("a split breaks the text before its end: offset is under %d", len(units))
		case input.Kind == textSuggestionSplit && (input.Text != "" || input.FirstChild):
			return "a split sends no text or firstChild"
		case input.Picked && (input.Kind != textSuggestionBlock || input.FirstChild):
			return "picked goes with a block after the block, not a split or a first child"
		case commentIdentifierProblem("newBlockId", input.NewBlockID) != "":
			return commentIdentifierProblem("newBlockId", input.NewBlockID) + ": a new id of yours for the suggested block"
		case !suggestable || input.BlockType == graphBlockTypeCode:
			return fmt.Sprintf("blockType %q isn't a text block a suggestion makes (not code)", input.BlockType)
		case utf8.RuneCountInString(input.Text) > maxGraphTextCharacters:
			return fmt.Sprintf("text is over %d characters", maxGraphTextCharacters)
		}
	case textSuggestionTurn:
		switch {
		case input.Length != 0 || len(input.Marks) != 0 || input.Text != "" || input.NewBlockID != "" || input.FirstChild:
			return "kind \"turn\" sends blockType alone: no length, marks, text, newBlockId or firstChild"
		case !suggestable || input.BlockType == graphBlockTypeCode:
			return fmt.Sprintf("blockType %q isn't a text block a suggested block turns into (not code)", input.BlockType)
		}
	case textSuggestionRemove:
		return suggestionRemovalProblem(input)
	default:
		return fmt.Sprintf("kind %q isn't insert, delete, replace, format, block, split, turn or remove", input.Kind)
	}
	if input.Kind != textSuggestionBlock && input.Kind != textSuggestionSplit && input.Kind != textSuggestionTurn &&
		(input.NewBlockID != "" || input.BlockType != "" || input.FirstChild) {
		return "newBlockId, blockType and firstChild go with a block or split suggestion, blockType with kind \"turn\" too"
	}
	if len(input.BlockIDs) != 0 {
		return "blockIds go with kind \"remove\": the blocks after blockId it suggests away"
	}
	if input.Picked && input.Kind != textSuggestionBlock {
		return "picked goes with kind \"block\": a block a menu picked, a suggestion of its own"
	}
	if input.Stay && input.Kind != textSuggestionTurn {
		return "stay goes with kind \"turn\": the block turns within the suggestion it is in"
	}
	return ""
}

// suggestionTextChanged is a suggestion refused because the block's text
// isn't expectedText, with the text it is, to send again.
type suggestionTextChanged struct {
	current string
}

func (changed *suggestionTextChanged) Error() string { return errSuggestionTextChanged.Error() }
func (changed *suggestionTextChanged) Unwrap() error { return errSuggestionTextChanged }

// suggestionTextChangedMessage is what the refusal says: the block's text as
// it is, which the request's expectedText sends next time.
func suggestionTextChangedMessage(err error) string {
	changed, ok := err.(*suggestionTextChanged)
	if !ok {
		return "The text changed. Try the suggestion again."
	}
	return refusalMessage("The text changed. Try the suggestion again.", fmt.Sprintf(
		"the block's text is now %q (%d UTF-16 units, other suggested insertions included); send it as expectedText with offset and length within it",
		changed.current, len(utf16.Encode([]rune(changed.current)))))
}
