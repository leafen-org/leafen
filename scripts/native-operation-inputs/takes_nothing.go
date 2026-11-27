package main

import "go/ast"

// A handler that never reads its request's body takes none, and is written
// with nothing read (an empty entry), so the operation list says so rather
// than giving every record of its contract (2026-11-16: disconnecting an MCP
// connection, which takes nothing, was described by the connection
// contract's ten records). It is so only where nothing it or a function it
// hands the request to does might read the body; deeper than takesNothingDepth
// it may, and isn't written.

const takesNothingDepth = 3

// bodyReads are the calls that read a request's body, or may: binding or
// decoding it, a form, or JSON from anywhere.
var bodyReads = map[string]bool{
	"BindBody": true, "Decode": true, "Unmarshal": true, "bindNotionStrictJSON": true, "ReadAll": true,
	"ParseMultipartForm": true, "ParseForm": true, "FormFile": true, "FormValue": true, "PostFormValue": true,
	"MultipartReader": true,
}

// notInputs read a request's body for every route alike, not as what one
// takes: every refusal's writer (internal/app/diagnostic.go) reads back a
// body the handler read, to say where its JSON broke — apiErrorFrom reaches
// nothing else, so a handler failing through it deep down still takes
// nothing, nor do the rules every failure's answer goes by, whatever its
// shape (failureAnswer, answeredFailure: api_failure_answer.go) — and a
// key's confirmation (mcp_action_approvals.go) shows the person the request
// as sent.
var notInputs = map[string]bool{
	"writeAPIErrorWithReference": true, "apiErrorFrom": true, "failureAnswer": true, "answeredFailure": true,
	"requireMCPActionApproval": true,
}

// takesNothing tells a handler's body that reads no request body.
func (code source) takesNothing(body *ast.BlockStmt, seen map[string]bool, depth int) bool {
	nothing := true
	ast.Inspect(body, func(node ast.Node) bool {
		if !nothing {
			return false
		}
		switch node := node.(type) {
		case *ast.SelectorExpr:
			// event.Request.Body, request.Body
			if node.Sel.Name == "Body" && requestValue(node.X) {
				nothing = false
			}
		case *ast.CallExpr:
			name := ""
			switch fun := node.Fun.(type) {
			case *ast.SelectorExpr:
				name = fun.Sel.Name
			case *ast.Ident:
				name = fun.Name
			}
			// JSON read back from a stored value is no body (body_reads.go).
			if bodyReads[name] && (name != "Unmarshal" && name != "Decode" || code.readsBody(node, name, body)) {
				nothing = false
				return false
			}
			function := code.functions[name]
			if function == nil || function.Body == nil || seen[name] || notInputs[name] || !mentionsEvent(node) {
				return true
			}
			if depth >= takesNothingDepth {
				nothing = false
				return false
			}
			seen[name] = true
			nothing = code.takesNothing(function.Body, seen, depth+1)
		}
		return nothing
	})
	return nothing
}

// requestValue tells an expression naming the request: event.Request, or a
// variable request or r.
func requestValue(expression ast.Expr) bool {
	switch expression := expression.(type) {
	case *ast.SelectorExpr:
		return expression.Sel.Name == "Request"
	case *ast.Ident:
		return expression.Name == "request" || expression.Name == "r"
	}
	return false
}
