package app

import (
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

const (
	notionMeetingNotesQueryPath          = notionAPIRoot + "/blocks/meeting_notes/query"
	notionMeetingNotesUnsupportedCode    = "unsupported_feature"
	notionMeetingNotesUnsupportedMessage = "Meeting note queries are not supported by Leafen."
	notionMeetingNotesUnsupportedJSON    = `{"code":"unsupported_feature","message":"Meeting note queries are not supported by Leafen."}`
	notionMeetingNotesMCPToolName        = "notion-query-meeting-notes"
)

func serveUnsupportedNotionMeetingNotesQuery(event *core.RequestEvent) error {
	if refused, err := refuseNotionBodyByType(event); refused {
		return err
	}
	if refused, err := refuseNotionUnreadableJSON(event); refused {
		return err
	}
	_, authorized, err := authorizeNotionUserDirectory(event)
	if err != nil || !authorized {
		return err
	}
	body := map[string]any{}
	if err := event.BindBody(&body); err != nil {
		return notionCompatibilityError(
			event,
			http.StatusBadRequest,
			notionErrorValidation,
			"A JSON request body is required.",
		)
	}
	return notionCompatibilityError(
		event,
		http.StatusNotImplemented,
		notionMeetingNotesUnsupportedCode,
		notionMeetingNotesUnsupportedMessage,
	)
}

// The official meeting-notes query, listed in one line as the other tools
// Leafen doesn't have (mcp_tool_unsupported.go), takes any arguments,
// Notion's query among them, and answers unsupported_feature.
func registerUnsupportedNotionMeetingNotesMCPTool(server *mcp.Server) {
	addUnsupportedNotionMCPTool(server, notionMeetingNotesMCPToolName,
		"Not in Leafen (no meeting notes service).", func() (string, error) {
			return notionMeetingNotesUnsupportedJSON, nil
		})
}
