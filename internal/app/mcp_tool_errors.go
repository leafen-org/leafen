package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// A tool that refuses says why, so the agent can change what it sent rather
// than guess (2026-10-22: a team trial's manager, told only "invalid" for a
// schema, tried dozens of low-level calls around it). A refusal names what to
// change where the tool knows it; a refusal that still comes bare gets the
// general reason of its kind here, at the end of the call.

// mcpInvalid is a tool refusing its input, saying what to change.
func mcpInvalid(format string, values ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{errMCPToolInvalid}, values...)...)
}

// mcpConfirmationNeeded is a tool that does nothing until its person
// confirms what it would do, saying what (2026-11-16: notion-delete-pages
// and notion-restore-version asked for it as LEAFEN_MCP_TOOL_INVALID, an
// agent couldn't tell it from a call it got wrong). It comes as an error
// result, as Notion's own tools give theirs, under its own code.
func mcpConfirmationNeeded(format string, values ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{errMCPConfirmationRequired}, values...)...)
}

const errorCodeMCPConfirmationRequired = "LEAFEN_MCP_CONFIRMATION_REQUIRED"

var errMCPConfirmationRequired = errors.New(errorCodeMCPConfirmationRequired)

// mcpDenied is a tool not reaching what it was given, saying what.
func mcpDenied(format string, values ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{errMCPScopeDenied}, values...)...)
}

// mcpGraphReason gives a content-graph refusal's own words to the tool error
// it comes as, with what to fix after them as the API gives it
// (refusalMessage): told "Check the column's options." alone, an agent
// couldn't tell which (2026-11-26).
func mcpGraphReason(sentinel, err error) error {
	var requestError *graphRequestError
	if errors.As(err, &requestError) && strings.TrimSpace(requestError.message) != "" {
		return fmt.Errorf("%w: %s", sentinel, refusalMessage(requestError.message, requestError.reason))
	}
	return sentinel
}

// mcpBareReasons are the general reasons of a refusal that names none.
var mcpBareReasons = map[string]string{
	errorCodeMCPToolInvalid: "%s didn't take this input. Check each field against the tool's description and input schema: " +
		"IDs as Leafen's tools give them, a value from the list a field names, every field it needs.",
	errorCodeMCPScopeDenied: "%s can't reach that: it's outside the workspaces this connection was given, or the account it works " +
		"as can't open or change it, or it's gone. notion-search and notion-fetch show what this connection reaches.",
	errorCodeMCPToolFailed: "Leafen couldn't finish %s (logged as %s). Try once more; if it fails again, do it another way and say " +
		"what didn't work.",
	errorCodePageNotFound: "%s found no such page: it is gone, in the trash, or not one this account can open. notion-search finds pages.",
	errorCodeCursorConflict: "%s: the page changed since it was read — after the base_cursor given, or by another change saved " +
		"while this one was. Fetch it again, then make the change on what it holds now.",
	errorCodeMutationIdentityReused: "%s: this mutation_id was used for a different change. Leave it out, or give a new one.",
	errorCodePageUpdateFailed:       "Leafen couldn't save the page with %s (logged as %s). Try once more.",
	errorCodeMCPClientNotApproved: "%s: this workspace's administrators haven't approved this kind of connection here. Ask them, " +
		"or work in a workspace that allows it.",
	errorCodeDLPPolicyBlocked: "%s: the organization's data loss prevention (DLP) policy doesn't allow this, for a sensitive term in " +
		"the content or for this kind of action. Take out the term, or ask an organization administrator; the same call fails again.",
	errorCodeDLPUnavailable: "%s: couldn't check the organization's data loss prevention (DLP) policy, so nothing changed. Try once more.",
	errorCodeMCPWorkspaceRequired: "%s needs workspace_id: nothing in it says which of this connection's workspaces it is for; " +
		mcpWorkspacesListed + ".",
	errorCodeMCPConfirmationRequired: "%s does nothing until the person confirms. Left to do: what it would do, to them, and only on " +
		"their yes the same call again with user_confirmed: true.",
	errorCodeMCPServerStopped: "The server stopped while %s ran, before it finished (logged as %s); some of what it changes may be " +
		"saved. Once the server answers again, read what this call was to change, then send it again only for what isn't there.",
	errorCodeMCPServerBusy: "%s couldn't finish: the server was busy with other work (logged as %s). Wait a few seconds and " +
		"call it again.",
	errorCodePageBusy: "%s: the page is held by a change under way — its blocks being moved, a copy or a wiki change. " +
		"Call it again once that's done.",
	errorCodeMCPOtherWorkspace: "%s works within one workspace, and this would take something into another: Leafen's API moves a page to " +
		"another workspace (POST /pages/{id}/workspace-move) or copies one there (POST /pages/{id}/duplicate with destinationWorkspaceId).",
}

// A call that failed because the database stayed busy — a write that waited
// out its time for the write connection or SQLite's lock — says so under a
// code of its own, telling its agent to wait and call again, as the app's
// requests are told busy (api_error_busy.go): told "couldn't finish… try once
// more, or do it another way", an agent gave up on what a moment's wait
// would have done (2026-11-18).
const errorCodeMCPServerBusy = "LEAFEN_MCP_SERVER_BUSY"

var errMCPServerBusy = errors.New(errorCodeMCPServerBusy)

// mcpBusyFailure is a call's failure as the database staying busy, when
// what failed it (failure, its tool's error) is that and its words are a
// failure's — Leafen's failure codes, bare or in words of their own, or words
// not Leafen's — never a refusal's; any other is as it was.
func mcpBusyFailure(failure error, text string) string {
	code, _, _ := strings.Cut(strings.TrimSpace(text), ":")
	if _, leafen := mcpBareReasons[code]; !isSQLiteBusyError(failure) || leafen && !mcpStopFailures[code] {
		return text
	}
	return errorCodeMCPServerBusy
}

// A call that fails while the server stops failed with the stop, its write
// under way stopped (request_write_stop.go). Its failure says so, under a code
// of its own, telling its agent to read what the call was to change once the
// server is back and send again only what is missing: told "try once more",
// an agent sent it again to a closing server, or, once it was back, made
// twice what the call had kept — a page made before its sub-page's write was
// stopped (2026-11-18, over HTTP and over server-sent events). A call over
// HTTP whose request kept nothing is answered busy in its place
// (mcp_request_app.go); over server-sent events its answer comes on the
// stream as this.
const errorCodeMCPServerStopped = "LEAFEN_MCP_SERVER_STOPPED"

// mcpStopFailures are the failures a stopping server words as its stop.
var mcpStopFailures = map[string]bool{errorCodeMCPToolFailed: true, errorCodePageUpdateFailed: true}

// mcpStoppedFailure is a call's failure as the server's stop, when it is
// one of mcpStopFailures, bare or in words of its own, and the server is
// stopping; any other refusal is as it was.
func mcpStoppedFailure(app core.App, text string) string {
	code, _, _ := strings.Cut(strings.TrimSpace(text), ":")
	if !mcpStopFailures[code] || !serverStopping(app) {
		return text
	}
	return errorCodeMCPServerStopped
}

// mcpToolReasons gives a bare refusal its kind's general reason, and logs a
// failure for whoever runs the server.
func mcpToolReasons(app core.App) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			result, err := next(ctx, method, request)
			call, ok := request.(*mcp.CallToolRequest)
			if method != "tools/call" || !ok || call.Params == nil {
				return result, err
			}
			done, ok := result.(*mcp.CallToolResult)
			if err != nil || !ok || !done.IsError || len(done.Content) != 1 {
				return result, err
			}
			text, ok := done.Content[0].(*mcp.TextContent)
			if !ok {
				return result, err
			}
			// Inner IDs it holds come out as the agent's (mcp_error_outward_ids.go).
			defer func() { text.Text = mcpOutwardIDs(app, text.Text) }()
			// One with Notion's error data reads as Notion's (mcp_tool_error_data.go).
			if answer, notion := mcpNotionErrorText(done.GetError()); notion {
				text.Text = answer
				return result, err
			}
			text.Text = mcpOwnWords(text.Text)
			text.Text = mcpStoppedFailure(app, text.Text)
			if text.Text != errorCodeMCPServerStopped {
				text.Text = mcpBusyFailure(done.GetError(), text.Text)
				text.Text = mcpHeldPageFailure(done.GetError(), text.Text)
				text.Text = mcpRefusedFailure(done.GetError(), text.Text)
			}
			code := strings.TrimSpace(text.Text)
			reason, bare := mcpBareReasons[code]
			if !bare {
				return result, err
			}
			if strings.Count(reason, "%s") == 2 {
				reference := newDiagnosticReference()
				logged := "leafen mcp tool failed"
				switch code {
				case errorCodeMCPServerStopped:
					logged = "leafen mcp tool stopped with the server"
				case errorCodeMCPServerBusy:
					logged = "leafen mcp tool told busy"
				}
				attributes := []any{"tool", call.Params.Name, "diagnosticReference", reference}
				if failure := done.GetError(); failure != nil {
					attributes = append(attributes, "causeClass", diagnosticCauseClass(failure))
				}
				app.Logger().Warn(logged, attributes...)
				text.Text = code + ": " + fmt.Sprintf(reason, call.Params.Name, reference)
			} else {
				text.Text = code + ": " + fmt.Sprintf(reason, call.Params.Name)
			}
			return result, err
		}
	}
}

// mcpOwnWords is a refusal from its own code on: a refusal made while a
// page is written comes back in the write's own code (LEAFEN_YGO_PERSISTENCE_FAILED:
// LEAFEN_MCP_TOOL_INVALID: …), which isn't why the tool refused. Codes before
// a refusal's own go; text that holds none is as it was.
func mcpOwnWords(text string) string {
	rest := text
	for {
		for code := range mcpBareReasons {
			if strings.HasPrefix(rest, code) {
				return rest
			}
		}
		head, tail, cut := strings.Cut(rest, ": ")
		if !cut || head == "" || strings.TrimLeft(head, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_") != "" {
			return text
		}
		rest = tail
	}
}

// mcpInWorkspace says a workspace a tool was given holds what it reached.
func mcpInWorkspace(app core.App, given, actual string) bool {
	return given == "" || given == actual
}

// The refusals many tools give alike.
var (
	errMCPChangedMeanwhile  = mcpDenied("it moved or changed while this call ran: fetch it again and retry")
	errMCPOutsideConnection = mcpDenied("it is outside the workspaces this connection was given")
	errMCPCantChange        = mcpDenied("this account can't change it, or the page it is on")
	errMCPCantRead          = mcpDenied("this account can't open it, or the page it is on")
	errMCPGoneOrUnreached   = mcpDenied("it is gone, or isn't one this connection reaches")
)

// mcpDLPError is a DLP decision as the bare refusal mcpToolReasons words.
func mcpDLPError(err error) error {
	if errors.Is(err, errDLPPolicyUnavailable) {
		return errDLPPolicyUnavailable
	}
	return errDLPPolicyBlocked
}

// mcpReasoned passes on a refusal that says why; anything else, a failure.
func mcpReasoned(err error) error {
	if errors.Is(err, errMCPToolInvalid) || errors.Is(err, errMCPScopeDenied) || errors.Is(err, errMCPToolFailed) ||
		errors.Is(err, errMCPConfirmationRequired) || errors.Is(err, errMCPPageMissing) || errors.Is(err, errMCPPageBusy) {
		return err
	}
	if isDLPPolicyDecisionError(err) {
		return mcpDLPError(err)
	}
	return failedBecause(errMCPToolFailed, err)
}
