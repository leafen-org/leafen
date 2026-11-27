package app

import (
	"bytes"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/reearth/ygo/crdt"
)

// A to-do keeps its task in its content beside "checked", as Notion's to-do
// does (seen 2026-11-09): "assignees", the people it is assigned to in the
// order they were added, and "due", its due date with the end, time zone and
// reminder a date mention has. Neither is in its text, and a Notion API read
// of the block shows neither. The due date's reminder reminds the people
// assigned, or whoever set it when no one is.

const (
	todoAssigneesMax      = 50
	todoDueTimeZoneMaxLen = 64
	todoDueReminderPrefix = "todo-due|"
)

var todoDueDateValue = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(?:[T ]\d{2}:\d{2}(?::\d{2}(?:\.\d+)?)?(?:Z|[+-]\d{2}:?\d{2})?)?$`)

type graphTodoDue struct {
	Date         string `json:"date"`
	DateEnd      string `json:"dateEnd,omitempty"`
	DateTimeZone string `json:"dateTimeZone,omitempty"`
	DateReminder string `json:"dateReminder,omitempty"`
}

// validateGraphTodoTask checks a to-do's assignees and due date.
func validateGraphTodoTask(block *graphBlock) error {
	if block.Type != graphBlockTypeToDo || len(block.Content) == 0 {
		return nil
	}
	fields := map[string]json.RawMessage{}
	if json.Unmarshal(block.Content, &fields) != nil {
		return nil
	}
	if raw, present := fields["assignees"]; present && !bytes.Equal(raw, []byte("null")) {
		if _, err := parseTodoAssignees(raw); err != nil {
			return err
		}
	}
	if raw, present := fields["due"]; present && !bytes.Equal(raw, []byte("null")) {
		if _, err := parseTodoDue(raw); err != nil {
			return err
		}
	}
	return nil
}

func parseTodoAssignees(raw json.RawMessage) ([]string, error) {
	ids := []string{}
	if json.Unmarshal(raw, &ids) != nil || len(ids) > todoAssigneesMax {
		return nil, graphInputError(errorCodeGraphInvalid, "Check the to-do's assignees.")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !validCommentIdentifier(id) || seen[id] {
			return nil, graphInputError(errorCodeGraphInvalid, "Check the to-do's assignees.")
		}
		seen[id] = true
	}
	return ids, nil
}

func parseTodoDue(raw json.RawMessage) (graphTodoDue, error) {
	due := graphTodoDue{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&due) != nil || !todoDueDateValue.MatchString(due.Date) ||
		(due.DateEnd != "" && !todoDueDateValue.MatchString(due.DateEnd)) ||
		utf8.RuneCountInString(due.DateTimeZone) > todoDueTimeZoneMaxLen ||
		!validDateMentionReminder(due.Date, due.DateReminder) {
		return graphTodoDue{}, graphInputError(errorCodeGraphInvalid, "Check the to-do's due date.")
	}
	return due, nil
}

// todoTaskOf reads a stored to-do's task; a block that holds none, or not
// readably, has none.
func todoTaskOf(content string) (assignees []string, due *graphTodoDue) {
	if content == "" || !strings.Contains(content, `"assignees"`) && !strings.Contains(content, `"due"`) {
		return nil, nil
	}
	fields := map[string]json.RawMessage{}
	if json.Unmarshal([]byte(content), &fields) != nil {
		return nil, nil
	}
	if raw, present := fields["assignees"]; present {
		assignees, _ = parseTodoAssignees(raw)
	}
	if raw, present := fields["due"]; present && !bytes.Equal(raw, []byte("null")) {
		if parsed, err := parseTodoDue(raw); err == nil {
			due = &parsed
		}
	}
	return assignees, due
}

// todoBlockAssignees is whom a stored block assigns: a to-do's assignees,
// none once it is another kind of block.
func todoBlockAssignees(block storedReminderBlock) []string {
	if block.Type != graphBlockTypeToDo {
		return nil
	}
	assignees, _ := todoTaskOf(block.Content)
	return assignees
}

// sortedUnion is the people of both lists once each, in order.
func sortedUnion(first, second []string) []string {
	if len(second) == 0 {
		return first
	}
	seen := map[string]struct{}{}
	people := []string{}
	for _, list := range [][]string{first, second} {
		for _, userID := range list {
			if _, found := seen[userID]; !found {
				seen[userID] = struct{}{}
				people = append(people, userID)
			}
		}
	}
	sort.Strings(people)
	return people
}

// todoDueReminders is the reminder a to-do's due date sets, if any: for the
// people assigned, or whoever set it.
func todoDueReminders(content string, text *crdt.YText, blockID, creatorID string, fallbackZone func() string) []projectedReminder {
	assignees, due := todoTaskOf(content)
	if due == nil || due.DateReminder == "" {
		return nil
	}
	zone := due.DateTimeZone
	if zone == "" {
		zone = fallbackZone()
	}
	at, ok := dateMentionReminderDue(due.Date, zone, due.DateReminder)
	recipients := append([]string(nil), assignees...)
	sort.Strings(recipients)
	if len(recipients) == 0 && creatorID != "" {
		recipients = []string{creatorID}
	}
	if !ok || len(recipients) == 0 {
		return nil
	}
	preview := ""
	if text != nil {
		preview = notificationEmailLine(textAsItStands(text), notificationEmailPreviewRunes)
	}
	return []projectedReminder{{
		Key:     todoDueReminderPrefix + strings.Join([]string{blockID, due.Date, zone, due.DateReminder}, "|"),
		BlockID: blockID, CreatorID: creatorID, Recipients: recipients, DateValue: due.Date, Preview: preview, DueAt: at,
	}}
}
