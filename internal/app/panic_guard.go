package app

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// A panic — a defect in one tool, app or job — ends what panicked, not the
// server. Where Leafen runs work outside an HTTP request's own recovery (the
// MCP SDK runs each call where none reaches; the app platform runs hooks,
// rooms and workers on goroutines of their own), it recovers here, logs the
// panic with its stack under a diagnostic reference, and that work fails
// (2026-10-14: a structure tool's nil map ended the process for everyone).

// logRecoveredPanic logs a recovered panic and gives its reference.
func logRecoveredPanic(app core.App, where string, recovered any, attributes ...any) string {
	reference := newDiagnosticReference()
	attributes = append([]any{
		"diagnosticReference", reference, "where", where,
		"panic", fmt.Sprint(recovered), "stack", string(debug.Stack()),
	}, attributes...)
	if app != nil {
		app.Logger().Error("leafen work panicked", attributes...)
	}
	if app == nil || !app.IsDev() {
		productionDiagnosticLogger.Error("leafen work panicked", attributes...)
	}
	return reference
}

// runGuarded runs work, a panic in it logged rather than ending the server.
func runGuarded(app core.App, where string, work func()) {
	defer func() {
		if recovered := recover(); recovered != nil {
			logRecoveredPanic(app, where, recovered)
		}
	}()
	work()
}

// guardedError runs work, a panic in it logged and given as its error.
func guardedError(app core.App, where string, work func() error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%s stopped on a defect, logged as %s", where, logRecoveredPanic(app, where, recovered))
		}
	}()
	return work()
}

// mcpPanicGuard ends an MCP call that panics with an error — a tool's call
// as the tool's error — and logs the panic.
func mcpPanicGuard(app core.App) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (result mcp.Result, err error) {
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}
				tool := ""
				if call, ok := request.(*mcp.CallToolRequest); ok && call.Params != nil {
					tool = call.Params.Name
				}
				reference := logRecoveredPanic(app, "mcp "+method, recovered, "tool", tool)
				message := "Leafen couldn't finish this call because of a defect, logged as " + reference + "."
				if method == "tools/call" {
					result, err = &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}}}, nil
					return
				}
				result, err = nil, errors.New(message)
			}()
			return next(ctx, method, request)
		}
	}
}
