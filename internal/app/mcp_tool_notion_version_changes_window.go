package app

import "fmt"

// get-version-changes stops where Notion's answer stops
// (mcpNotionAnswerLength): the latest edited changes that fit, then
// older_changes_end_time, when the first change left out was last edited,
// and a message to call again with it as end_time for the older ones
// (Notion's, seen 2026-11-22: 80 of a database's changes, the time the next
// one's last edit). The answer stops between two times, never among changes
// last edited at one time, so the next call, ending at that time, takes up
// the ones left out; those last edited at the latest time all go in even
// when they alone pass the length, as no call could read them apart.

// mcpVersionChangesWindow keeps the changes that fit in one answer, and
// reports whether it left some out.
func mcpVersionChangesWindow(output *notionMCPVersionChangesOutput) (bool, error) {
	changes := output.Changes
	around := *output
	// What the answer says when it stops, at its length: the times are all
	// written alike.
	around.Changes, around.OlderChangesEndTime = []notionMCPVersionChange{}, output.StartTime
	around.Message = mcpVersionChangesOlder(output.Message, output.StartTime, output.StartTime)
	kept, err := mcpNotionAnswerKept(around, changes)
	if err != nil || kept == len(changes) {
		return false, err
	}
	// Back to the first change last edited when the first one left out was.
	for kept > 0 && changes[kept-1].LastEditTime == changes[kept].LastEditTime {
		kept--
	}
	if kept == 0 {
		// Those last edited at the latest time alone pass the length.
		for kept < len(changes) && changes[kept].LastEditTime == changes[0].LastEditTime {
			kept++
		}
	}
	if kept == len(changes) {
		return false, nil
	}
	output.Changes, output.OlderChangesEndTime = changes[:kept], changes[kept].LastEditTime
	output.Message = mcpVersionChangesOlder(output.Message, output.StartTime, output.OlderChangesEndTime)
	return true, nil
}

// mcpVersionChangesOlder is the answer's message with how to read the
// changes it left out.
func mcpVersionChangesOlder(message, start, olderEnd string) string {
	older := fmt.Sprintf("Not every change in this range fits in one answer: for the older ones, call again with start_time %q and end_time %q.", start, olderEnd)
	if message == "" {
		return older
	}
	return message + " " + older
}
