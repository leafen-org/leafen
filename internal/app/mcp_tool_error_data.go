package app

import (
	"encoding/json"
	"errors"
	"net/http"
)

// A refusal Notion's MCP gives with its API's error data — what a Markdown
// change finds nothing for, or more than one place for: update_content's
// old_str, an insert's anchor — is answered over MCP as Notion's is (seen
// 2026-11-22: its text is the API's error, validation_error with status
// 400, its message and additional_data — tool_error_code, tool_error_class
// "validation", tool_error_retryable "false" and the message's template): the
// error object Leafen's public API gives for it (notion_page_markdown_matches.go),
// its message in Leafen's words. Other refusals keep their code and words.

// mcpNotionDataError is a refusal that carries Notion's error data.
type mcpNotionDataError interface {
	error
	mcpNotionData() map[string]any
}

func (e pageMarkdownMatchError) mcpNotionData() map[string]any { return e.data() }

// mcpNotionErrorAnswer is such a refusal as an agent reads it, its fields
// in the order of Notion's.
type mcpNotionErrorAnswer struct {
	Object         string             `json:"object"`
	Status         int                `json:"status"`
	Code           string             `json:"code"`
	Message        string             `json:"message"`
	AdditionalData mcpNotionErrorData `json:"additional_data"`
}

type mcpNotionErrorData struct {
	Code      any `json:"tool_error_code,omitempty"`
	Class     any `json:"tool_error_class,omitempty"`
	Retryable any `json:"tool_error_retryable,omitempty"`
	Template  any `json:"tool_error_message_template,omitempty"`
}

// mcpNotionErrorText is the text of a tool's refusal that carries Notion's
// error data; false for another.
func mcpNotionErrorText(failure error) (string, bool) {
	var refusal mcpNotionDataError
	if failure == nil || !errors.As(failure, &refusal) {
		return "", false
	}
	data := refusal.mcpNotionData()
	text, err := json.Marshal(mcpNotionErrorAnswer{
		Object: "error", Status: http.StatusBadRequest, Code: "validation_error", Message: refusal.Error(),
		AdditionalData: mcpNotionErrorData{Code: data["tool_error_code"], Class: data["tool_error_class"],
			Retryable: data["tool_error_retryable"], Template: data["tool_error_message_template"]},
	})
	if err != nil {
		return "", false
	}
	return string(text), true
}
