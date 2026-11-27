package app

import (
	"bytes"
	"cmp"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
)

// The native API as operations an agent can find and run through MCP: every
// route the product registers under apiRoot that a key may use, with the
// scope it needs, whether it asks for confirmation, and what its contract
// says of it. Running one sends the request through the same router,
// middleware and handler the API uses, as the connection's person.

//go:embed native_operation_catalog.generated.json
var nativeOperationCatalogJSON []byte

// What a route without a wire contract reads from its body, from its handler's
// source (scripts/native-operation-inputs).
//
//go:embed native_operation_inputs.generated.json
var nativeOperationInputsJSON []byte

type nativeOperationInputField struct {
	Name   string   `json:"name"`
	Type   string   `json:"type"`
	Values []string `json:"values"`
	// Required is a field its handler refuses the request without.
	Required bool `json:"required,omitempty"`
	// Keys are the keys a map takes, and RequiredKeys those it needs.
	Keys         []string `json:"keys,omitempty"`
	RequiredKeys []string `json:"requiredKeys,omitempty"`
	// Closed says Values are all it takes: its handler refuses any other.
	Closed bool `json:"closed,omitempty"`
}

type nativeOperationInput struct {
	Record  string                                 `json:"record"`
	Records map[string][]nativeOperationInputField `json:"records"`
	Actions map[string]string                      `json:"actions,omitempty"`
	// Query are the query names its handler reads.
	Query []nativeOperationInputField `json:"query,omitempty"`
	// Answer is the record it answers with (native_operation_answer.go).
	Answer string `json:"answer,omitempty"`
}

type nativeOperationContracts struct {
	Operations map[string]struct {
		Contract string `json:"contract"`
		Name     string `json:"name"`
	} `json:"operations"`
	Contracts map[string]struct {
		Records map[string][]struct {
			Name       string `json:"name"`
			Type       string `json:"type"`
			ZeroMeans  string `json:"zeroMeans"`
			Deprecated string `json:"deprecated"`
		} `json:"records"`
		Enums map[string][]string `json:"enums"`
	} `json:"contracts"`
}

type nativeOperation struct {
	ID string `json:"id"`
	// Method and Path are the id's, given in full (detail "full") and for
	// one operation; a list gives the id alone.
	Method string `json:"method,omitempty"`
	Path   string `json:"path,omitempty"`
	// Scope is left out where a group or a search lists it.
	Scope    string `json:"scope,omitempty"`
	Confirm  bool   `json:"confirm,omitzero"`
	Contract string `json:"contract,omitempty"`
	Name     string `json:"name"`
	// Summary is the line a group or a search lists it with, and Group the
	// group it is in (native_operation_groups.go).
	Summary string `json:"summary,omitempty"`
	Group   string `json:"group,omitempty"`
	// Does says what it does where its name doesn't (native_operation_does.go).
	Does string `json:"does,omitempty"`
	// Note says where to read what an operation gives in a form only the
	// app uses (native_operation_notes.go).
	Note string `json:"note,omitempty"`
	// ThisConnection is whether the connection asking can run it
	// (native_operation_grant.go).
	ThisConnection string `json:"this_connection,omitempty"`
	// Handler names what the generated inputs are kept under
	// (nativeOperationHandler), and rule is the key rule it is listed with.
	Handler string      `json:"-"`
	rule    userKeyRule `json:"-"`
}

type nativeRoute struct {
	Method  string
	Path    string
	Handler string
}

var nativeOperationsState = struct {
	sync.Mutex
	router    *router.Router[*core.RequestEvent]
	mux       http.Handler
	contracts nativeOperationContracts
	loaded    bool
}{}

const errorCodeNativeOperationsFailed = "LEAFEN_NATIVE_OPERATIONS_FAILED"

const nativeOperationsRESTUsage = "Send one to /api/leafen/v1 with its values filled in, as Authorization: Bearer <key>. " +
	"With no parameter the answer gives the groups of what Leafen does, a line each; ?group= one group's operations, those this key can run (?all=true adds the rest, each with what it lacks); " +
	"?query= finds operations by English words in their names, summaries and keywords; ?detail=list lists them all. " +
	"?operation=\"METHOD /path\" describes one: its body's record and query names, the fields it needs marked required, and the record it answers with (answer); ?detail=full adds its contract's other records. " +
	"An operation whose input is bytes (a file's part, an archive, an image) is described with bytes: send them as the body, with its content type and the headers it names. " +
	"A connection works as its person: an agent connection with everything they may in all their workspaces, an MCP token in its one workspace with the tools its page leaves on (a Notion-compatible internal integration token is a bot of its own and isn't taken here or on Leafen's MCP). " +
	"The scope shown is what an operation needs of what a key's tools and its organizations' key policy leave it, and this_connection whether this key can run it or what it lacks (connection: its grant); a group or a search says it only where it can't. " +
	"An operation marked confirm (deleting for good, sharing, permissions, members, administration) first answers 428. With a Leafen-Confirmation header, " +
	"send the same request again with that header within five minutes to carry it out; with an approval, ask the person in your conversation " +
	"in the words it gives — once they say yes, send the same request again with the Leafen-Confirmation header set to its approvalId."

// registerNativeOperationDispatcher keeps the router so an MCP call can run
// a route through it once the server has registered them all, and lists the
// operations for a connector that doesn't speak MCP.
func registerNativeOperationDispatcher(event *core.ServeEvent) {
	nativeOperationsState.Lock()
	defer nativeOperationsState.Unlock()
	nativeOperationsState.router = event.Router
	nativeOperationsState.mux = nil
	event.Router.GET(apiRoot+"/operations", serveNativeOperations)
}

// serveNativeOperations is leafen_operations over REST: the groups, ?group=
// one (?all=true with what the key can't run), ?query= (or ?filter=, its
// earlier name) what English words find, ?operation="METHOD /path" one with
// what a call sends it, ?detail=full with its contract's other records.
func serveNativeOperations(event *core.RequestEvent) error {
	if _, ok := authenticatedUser(event); !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	query := event.Request.URL.Query()
	// A key's answer says what it can run (native_operation_grant.go).
	var grant *nativeOperationGrant
	var err error
	if key, keyed := userKeyOf(event); keyed {
		grant, err = nativeOperationGrantOf(event.App, key)
	}
	output := leafenMCPOperationsOutput{}
	if err == nil {
		output, err = describeNativeOperations(leafenMCPOperationsInput{Query: cmp.Or(query.Get("query"), query.Get("filter")),
			Operation: query.Get("operation"), Detail: query.Get("detail"), Group: query.Get("group"), All: query.Get("all") == "true"}, grant, nil)
	}
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeNativeOperationsFailed,
			"Couldn't list the operations.", "native_operations", "list", errorCodeNativeOperationsFailed, err)
	}
	// How to find and run operations, once: with the groups, a list or a
	// search, not with each operation's description.
	if strings.TrimSpace(query.Get("operation")) == "" {
		output.Usage = nativeOperationsRESTUsage
	}
	return event.JSON(http.StatusOK, output)
}

func nativeOperationRuntime() (http.Handler, nativeOperationContracts, error) {
	nativeOperationsState.Lock()
	defer nativeOperationsState.Unlock()
	if !nativeOperationsState.loaded {
		if err := json.Unmarshal(nativeOperationCatalogJSON, &nativeOperationsState.contracts); err != nil {
			return nil, nativeOperationContracts{}, err
		}
		shaped := map[string]struct {
			Contract string `json:"contract"`
			Name     string `json:"name"`
		}{}
		for id, operation := range nativeOperationsState.contracts.Operations {
			shaped[nativeOperationShape(id)] = operation
		}
		nativeOperationsState.contracts.Operations = shaped
		nativeOperationsState.loaded = true
	}
	if nativeOperationsState.router == nil {
		return nil, nativeOperationContracts{}, errors.New("the native API isn't serving")
	}
	if nativeOperationsState.mux == nil {
		mux, err := nativeOperationsState.router.BuildMux()
		if err != nil {
			return nil, nativeOperationContracts{}, err
		}
		nativeOperationsState.mux = mux
	}
	return nativeOperationsState.mux, nativeOperationsState.contracts, nil
}

// nativeRoutes reads the routes the router holds under apiRoot. The router
// keeps them unexported; their method, path and action are read by
// reflection only.
func nativeRoutes(group any) []nativeRoute {
	routes := []nativeRoute{}
	var walk func(value reflect.Value, prefix string)
	walk = func(value reflect.Value, prefix string) {
		for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
			if value.IsNil() {
				return
			}
			value = value.Elem()
		}
		if value.Kind() != reflect.Struct {
			return
		}
		if prefixField := value.FieldByName("Prefix"); prefixField.IsValid() && prefixField.Kind() == reflect.String {
			prefix += prefixField.String()
		}
		if method := value.FieldByName("Method"); method.IsValid() && method.Kind() == reflect.String {
			path := prefix + value.FieldByName("Path").String()
			if strings.HasPrefix(path, apiRoot+"/") && path != apiRoot+nativeMissingRoutePath {
				handler := ""
				if action := value.FieldByName("Action"); action.IsValid() && !action.IsNil() {
					if function := runtime.FuncForPC(action.Pointer()); function != nil {
						handler = function.Name()
					}
				}
				routes = append(routes, nativeRoute{Method: method.String(), Path: strings.TrimPrefix(path, apiRoot), Handler: handler})
			}
			return
		}
		children := value.FieldByName("children")
		if !children.IsValid() {
			if embedded := value.FieldByName("RouterGroup"); embedded.IsValid() {
				walk(embedded.Addr(), prefix)
			}
			return
		}
		for index := range children.Len() {
			walk(children.Index(index), prefix)
		}
	}
	walk(reflect.ValueOf(group), "")
	slices.SortFunc(routes, func(a, b nativeRoute) int {
		return strings.Compare(a.Path+" "+a.Method, b.Path+" "+b.Method)
	})
	return routes
}

// nativeMissingRoutePath catches an API path no route answers; it is no
// operation of its own.
const nativeMissingRoutePath = "/{path...}"

var nativePathValue = regexp.MustCompile(`\{[^}]*\}`)

// nativeOperationShape is an operation's method and path with its path
// values unnamed: a route and its contract may name them differently.
func nativeOperationShape(id string) string {
	return nativePathValue.ReplaceAllString(id, "{}")
}

// nativeOperationHandler is a route's handler as the generated inputs name
// it (scripts/native-operation-inputs keeps the same names): a function by
// its name, a method by its receiver's type and its name
// (workspaceMoveOut.start) — two kinds of move each have a start — and a
// closure, which has no name of its own (func1), by its route, as
// postAccountDeletion for POST /account/deletion.
func nativeOperationHandler(route nativeRoute) string {
	name := route.Handler[strings.LastIndex(route.Handler, "/")+1:]
	name = strings.TrimSuffix(name[strings.Index(name, ".")+1:], "-fm")
	name = strings.NewReplacer("(*", "", ")", "").Replace(name)
	if last := name[strings.LastIndex(name, ".")+1:]; name != "" && !anonymousHandlerName.MatchString(last) {
		return name
	}
	var words strings.Builder
	words.WriteString(strings.ToLower(route.Method))
	for _, segment := range strings.Split(route.Path, "/") {
		if segment == "" || strings.HasPrefix(segment, "{") {
			continue
		}
		for _, word := range strings.FieldsFunc(segment, func(r rune) bool { return r == '-' || r == '_' || r == '.' }) {
			words.WriteString(strings.ToUpper(word[:1]) + word[1:])
		}
	}
	return words.String()
}

var anonymousHandlerName = regexp.MustCompile(`^(?:func)?\d+$`)

// nativeOperationNames name each operation by its handler: a method by its
// own name, or, where other routes' handlers have that name too, with its
// receiver's type before it (workspaceMoveOutStart, serverMoveOutStart), so
// no two handlers share one.
func nativeOperationNames(routes []nativeRoute) []string {
	handlers := make([]string, len(routes))
	sharing := map[string]map[string]bool{}
	for index, route := range routes {
		handlers[index] = nativeOperationHandler(route)
		own := handlers[index][strings.LastIndex(handlers[index], ".")+1:]
		if sharing[own] == nil {
			sharing[own] = map[string]bool{}
		}
		sharing[own][handlers[index]] = true
	}
	names := make([]string, len(routes))
	for index, handler := range handlers {
		receiver, own, isMethod := strings.Cut(handler, ".")
		switch {
		case !isMethod:
			names[index] = handler
		case len(sharing[own]) > 1:
			names[index] = receiver + strings.ToUpper(own[:1]) + own[1:]
		default:
			names[index] = own
		}
	}
	return names
}

// listNativeOperations lists what a key may run: every route, with the
// scope it needs — a route no rule names follows the person's own
// permission with the whole scope and a key for all their workspaces; a key
// below that scope sees it as needing more — and the routes no key runs,
// with the reason.
func listNativeOperations() ([]nativeOperation, error) {
	if _, contracts, err := nativeOperationRuntime(); err != nil {
		return nil, err
	} else {
		nativeOperationsState.Lock()
		group := nativeOperationsState.router
		nativeOperationsState.Unlock()
		operations := []nativeOperation{}
		routes := nativeRoutes(group)
		names := nativeOperationNames(routes)
		for index, route := range routes {
			rule, _ := userKeyRuleOfRoute(route.Method, route.Path)
			// A route closed with its reason is listed with it, so a key
			// learns what it can't do and why; SCIM's protocol and what needs
			// no sign-in aren't.
			if rule.scope == userKeyScopeClosed && rule.reason == "" || rule.place == userKeyPlacePublic {
				continue
			}
			operation := nativeOperation{
				ID: route.Method + " " + route.Path, Method: route.Method, Path: route.Path,
				Scope: userKeyScopeNames[rule.scope], Confirm: rule.confirm, Name: names[index],
				Handler: nativeOperationHandler(route), rule: rule,
			}
			if rule.scope == userKeyScopeClosed {
				operation.Scope = "closed: " + rule.reason
			}
			if rule.byOperations {
				operation.Scope = userKeyScopeNames[userKeyScopeContent] + " (structure for structural operations)"
			}
			if rule.actionScopes != nil {
				operation.Scope += " (by its action: " + userKeyActionScopesText(rule.actionScopes) + ")"
			}
			if rule.across {
				operation.Scope += " (a key for all your workspaces)"
			}
			operation.Scope += nativeOperationAppOnly(operation.ID)
			operation.Note = leafenCallWaitNoteFor(operation.ID, nativeOperationNotes[nativeOperationShape(operation.ID)])
			if contract, ok := contracts.Operations[nativeOperationShape(operation.ID)]; ok {
				operation.Contract, operation.Name = contract.Contract, contract.Name
			}
			operation.Does = nativeOperationDoesNames[operation.ID][1]
			operations = append(operations, operation)
		}
		return operations, nil
	}
}

type userKeyInternalContextKey struct{}

type nativeOperationResult struct {
	Status       int    `json:"status"`
	Confirmation string `json:"confirmation,omitempty"`
	Body         any    `json:"body,omitempty"`
	Text         string `json:"text,omitempty"`
	// RetryAfter is a refusal's wait, for an app's endpoint to pass on.
	RetryAfter string `json:"-"`
	// UnreadQuery are the query names the operation didn't read
	// (api_list_limit.go).
	UnreadQuery string `json:"unread_query,omitempty"`
	// BodyBase64, ContentType, Bytes, Filename and Note are an answer of
	// bytes, not words, or one larger than comes back
	// (native_operation_byte_answers.go).
	BodyBase64  string `json:"body_base64,omitempty" jsonschema:"An answer of bytes (a file, an export, a picture) of at most 200 KiB, in standard base64"`
	ContentType string `json:"content_type,omitempty" jsonschema:"The content type of an answer of bytes, or of one larger than comes back"`
	Bytes       int    `json:"bytes,omitempty" jsonschema:"How many bytes an answer of bytes, or one larger than comes back, holds"`
	Filename    string `json:"filename,omitempty" jsonschema:"The file name an answer of bytes is given under"`
	Note        string `json:"note,omitempty" jsonschema:"For an answer larger than comes back (200 KiB: bytes, words or JSON), how to download it; beside a secret the answer holds, how to keep the next one out of the conversation; beside a backup place's failure, its common cause in words"`
	// DownloadURL and DownloadExpiresAt hand such an answer over
	// (native_operation_answer_downloads.go).
	DownloadURL       string `json:"download_url,omitempty" jsonschema:"For an answer larger than comes back: an address that downloads it without a key, until download_expires_at"`
	DownloadExpiresAt string `json:"download_expires_at,omitempty" jsonschema:"When download_url stops working"`
}

// serverInnerRequestKey marks a request this process sends its own router,
// which no request off the network can carry: an app's own server lets it
// past the guard that keeps other addresses out (LocalOwner.guard).
type serverInnerRequestKey struct{}

func withServerInnerRequest(ctx context.Context) context.Context {
	return context.WithValue(ctx, serverInnerRequestKey{}, true)
}

func isServerInnerRequest(request *http.Request) bool {
	inner, _ := request.Context().Value(serverInnerRequestKey{}).(bool)
	return inner
}

// callNativeOperation runs one native request as the key's person: path
// values already in the path, a JSON body, and the confirmation an earlier
// call returned.
func callNativeOperation(ctx context.Context, key userKeyAuthority, method, path, query string, body []byte, confirmation string) (nativeOperationResult, error) {
	body, header := nativeJSONRequestBody(body)
	return callNativeOperationWith(ctx, key, method, path, query, body, header, confirmation)
}

// nativeJSONRequestBody is the body a call this server makes sends as JSON,
// with its header. No body — nothing, a Go nil marshalled, or an app's
// "body": null — goes as none, as the screen sends none: a body sent as
// JSON must read as an object or list (api_error_unreadable_json.go), and
// Notion's API refuses "null" too (2026-11-17, GET, POST, PATCH and DELETE
// alike: 400 invalid_json). An update that trashed a page read it with no
// body, sent as "null" and refused, and every workspace stayed on the
// app's old data (2026-11-17).
func nativeJSONRequestBody(body []byte) ([]byte, http.Header) {
	header := http.Header{}
	if trimmed := bytes.TrimSpace(body); len(trimmed) == 0 || string(trimmed) == "null" {
		return nil, header
	}
	header.Set("Content-Type", "application/json")
	return body, header
}

// callNativeOperationWith runs one as callNativeOperation does, its body
// sent with these headers: an operation that takes bytes, with its content
// type and the headers it names (native_operation_byte_inputs.go).
func callNativeOperationWith(ctx context.Context, key userKeyAuthority, method, path, query string, body []byte, header http.Header,
	confirmation string) (nativeOperationResult, error) {
	mux, _, err := nativeOperationRuntime()
	if err != nil {
		return nativeOperationResult{}, err
	}
	target := apiRoot + path
	if query != "" {
		target += "?" + strings.TrimPrefix(query, "?")
	}
	// The request carries the key's authority, the app's run it is made for
	// and the caller's cancellation, not the MCP call's other values.
	base, cancel := context.WithCancel(withInnerRequestArrival(withAppRunID(context.Background(), appRunIDOf(ctx)), innerRequestArrivalOf(ctx)))
	defer cancel()
	defer context.AfterFunc(ctx, cancel)()
	// Whatever the path holds, the call answers (inner_request_target.go).
	request, err := newInnerRequest(withServerInnerRequest(context.WithValue(base, userKeyInternalContextKey{}, key)), method, target, body)
	if err != nil {
		return nativeOperationResult{}, err
	}
	request.RemoteAddr = "127.0.0.1:0"
	for name, values := range header {
		request.Header[name] = values
	}
	if confirmation != "" {
		request.Header.Set(userKeyConfirmationHeader, confirmation)
	}
	recorder := newNativeOperationRecorder()
	recorder.downloads = operationAnswerDownloadsOf(ctx)
	mux.ServeHTTP(recorder, request)
	result := nativeOperationResult{Status: recorder.Code, Confirmation: recorder.Header().Get(userKeyConfirmationHeader),
		RetryAfter: recorder.Header().Get("Retry-After"), UnreadQuery: recorder.Header().Get(apiUnreadQueryHeader)}
	// JSON as its body, words as they came and bytes in base64
	// (native_operation_byte_answers.go).
	result.readAnswer(recorder, method, path, query)
	if recorder.spool != nil {
		recorder.spool.offer(&result, key, recorder.total)
	}
	return result, nil
}
