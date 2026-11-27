package app

import (
	"fmt"
	"strings"
)

// leafen_operations gives what it finds a window at a time, as every answer
// that gives a growing list does (mcp_answer_lists.go): 2026-11-21 the words
// "page" found 199 operations, 21 KB in one answer, and detail list gave every
// operation (about 170 KB). Past one window every operation's list also
// counts them by area. What words find comes best first,
// nativeOperationSearchWindow an answer; a group, which holds no more than
// a window of a list, comes whole.

const mcpOperationsList = "operations"

// nativeOperationSearchWindow is how many of what words find an answer gives.
const nativeOperationSearchWindow = 10

// mcpOperationsWindowed is the window of the operations an answer found that
// start_cursor asks for.
func mcpOperationsWindowed(output leafenMCPOperationsOutput, cursor string, searched bool) (leafenMCPOperationsOutput, error) {
	listing, err := mcpAnswerListingOf(mcpOperationsList, cursor, mcpOperationsList)
	if err != nil {
		return output, err
	}
	window := mcpAnswerListWindow
	if searched {
		window = nativeOperationSearchWindow
	}
	if len(output.Operations) > window && output.Areas == nil && !searched {
		output.Areas = map[string]int{}
		for _, operation := range output.Operations {
			_, path, _ := strings.Cut(operation.ID, " ")
			area, _, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
			output.Areas[area]++
		}
	}
	page := mcpAnswerWindowSized(listing, mcpOperationsList, output.Operations, window)
	// A search's own line (words nothing holds) comes before the window's.
	said := output.Next
	output.Operations, output.NextCursor = page.Items, page.NextCursor
	if to := page.From + len(page.Items); page.NextCursor != "" && searched {
		output.Next = fmt.Sprintf("These are %d to %d of the %d operations found, best first. A group's answer lists all of one group; "+
			"or call again with the same words and start_cursor set to next_cursor.", page.From+1, to, page.Total)
	} else if page.NextCursor != "" {
		output.Next = fmt.Sprintf("These are %d to %d of the %d operations found, best first; areas counts them by area. "+
			"Narrow the words to find fewer, or call again with the same words or detail and start_cursor set to next_cursor.", page.From+1, to, page.Total)
	} else if page.From > 0 {
		output.Next = fmt.Sprintf("These are %d to %d of the %d operations found.", min(page.From+1, to), to, page.Total)
	}
	if said != "" && output.Next != said {
		output.Next = said + " " + output.Next
	}
	return output, nil
}
