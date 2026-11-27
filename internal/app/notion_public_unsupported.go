package app

import (
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

const (
	notionAgentUnsupportedMessage   = "Custom Agent execution and session operations are not supported by Leafen."
	notionSkillUnsupportedMessage   = "Notion Skill catalog and artifact operations are not supported by Leafen."
	notionMeetingUnsupportedMessage = "Meeting note creation is not supported by Leafen."
)

func serveUnsupportedNotionPublicCapability(event *core.RequestEvent) (bool, error) {
	_, message, handled := unsupportedNotionPublicCapability(
		event.Request.Method,
		event.Request.URL.Path,
	)
	if !handled {
		return false, nil
	}
	_, authorized, err := authorizeNotionUserDirectory(event)
	if err != nil || !authorized {
		return true, err
	}
	return true, notionCompatibilityError(
		event,
		http.StatusNotImplemented,
		notionMeetingNotesUnsupportedCode,
		message,
	)
}

func unsupportedNotionPublicCapability(method string, path string) (string, string, bool) {
	value, found := strings.CutPrefix(path, notionAPIRoot+"/")
	if !found || value == "" {
		return "", "", false
	}
	parts := strings.Split(value, "/")

	if len(parts) == 2 && parts[0] == "agents" {
		switch {
		case method == http.MethodPost && parts[1] == "batch":
			return "agent-batch", notionAgentUnsupportedMessage, true
		case method == http.MethodPost && parts[1] == "query":
			return "query-agents", notionAgentUnsupportedMessage, true
		case parts[1] != "" && method == http.MethodGet:
			return "get-agent", notionAgentUnsupportedMessage, true
		case parts[1] != "" && method == http.MethodDelete:
			return "delete-agent", notionAgentUnsupportedMessage, true
		}
	}
	if len(parts) == 3 && parts[0] == "agents" && parts[1] != "" {
		switch {
		case method == http.MethodPatch && parts[2] == "credit_limit":
			return "update-agent-credit-limit", notionAgentUnsupportedMessage, true
		case method == http.MethodGet && parts[2] == "insights":
			return "get-agent-insights", notionAgentUnsupportedMessage, true
		case method == http.MethodPatch && parts[2] == "status":
			return "update-agent-status", notionAgentUnsupportedMessage, true
		}
	}

	if method == http.MethodGet && len(parts) == 2 && parts[0] == "ai" && parts[1] == "plugins" {
		return "list-skills-plugins", notionSkillUnsupportedMessage, true
	}
	if method == http.MethodGet && len(parts) == 3 && parts[0] == "ai" && parts[2] != "" {
		switch parts[1] {
		case "plugins":
			return "get-plugin-directory", notionSkillUnsupportedMessage, true
		case "skills":
			return "get-skill-directory", notionSkillUnsupportedMessage, true
		}
	}

	if len(parts) == 1 && parts[0] == "sessions" && method == http.MethodPost {
		return "update-session", notionAgentUnsupportedMessage, true
	}
	if len(parts) == 2 && parts[0] == "sessions" {
		if parts[1] == "query" && method == http.MethodPost {
			return "query-sessions", notionAgentUnsupportedMessage, true
		}
		if parts[1] != "" && method == http.MethodGet {
			return "retrieve-session", notionAgentUnsupportedMessage, true
		}
	}
	if len(parts) == 3 && parts[0] == "sessions" && parts[1] != "" &&
		parts[2] == "cancel" && method == http.MethodPost {
		return "cancel-session", notionAgentUnsupportedMessage, true
	}
	if len(parts) == 4 && parts[0] == "sessions" && parts[1] != "" &&
		parts[2] == "events" && parts[3] == "query" && method == http.MethodPost {
		return "query-session-events", notionAgentUnsupportedMessage, true
	}

	if len(parts) == 2 && parts[0] == "blocks" && parts[1] == "meeting_notes" &&
		method == http.MethodPost {
		return "create-meeting-note", notionMeetingUnsupportedMessage, true
	}
	return "", "", false
}
