// Package leafen helps write a Leafen app worker in Go (Apache-2.0, see
// ../../LICENSE). A worker is a WebAssembly module Leafen runs with the
// Extism calling convention: each function the manifest's worker lists is
// exported with //go:wasmexport, reads its input, sets its output or its
// error, and returns 0 (or 1 after Fail). Build it as a reactor:
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o workers/rules.wasm .
//
// Inside, a worker reaches Leafen only through the host functions wrapped
// here: the native API as the app's account (or the person, for an action
// run as them), the approved outside addresses through Leafen's relay, the
// app's organizations and its temporary store, and the run's log.
package leafen

import (
	"encoding/json"
	"time"
)

// APIRequest calls Leafen's native API (paths below /api/leafen/v1).
type APIRequest struct {
	// The organization whose app workspace to act in; empty means the run's own.
	Tenancy string `json:"tenancy,omitempty"`
	Method  string `json:"method"`
	Path    string `json:"path"`
	Query   string `json:"query,omitempty"`
	Body    any    `json:"body,omitempty"`
}

// APIResponse is the native API's answer.
type APIResponse struct {
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body"`
	Text   string          `json:"text"`
}

// Tenancy is an organization the app is installed for and its app workspace.
type Tenancy struct {
	Tenancy     string `json:"tenancy"`
	WorkspaceID string `json:"workspaceId"`
}

// RelayRequest reaches an outside address the manifest's network.relay lists.
type RelayRequest struct {
	Method     string            `json:"method"`
	URL        string            `json:"url"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       string            `json:"body,omitempty"`
	BodyBase64 bool              `json:"bodyBase64,omitempty"`
}

// RelayResponse is the outside address's answer.
type RelayResponse struct {
	Status     int               `json:"status"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
	BodyBase64 bool              `json:"bodyBase64"`
}

// KVRequest is one command on the app's temporary store (GET, SET, INCR,
// DEL, EXPIRE, TTL, HGET, HGETALL, HSET, HDEL, LPUSH, RPUSH, LPOP, RPOP,
// LRANGE, ZADD, ZSCORE, ZREM, ZRANGE).
type KVRequest struct {
	Command string  `json:"command"`
	Key     string  `json:"key"`
	Field   string  `json:"field,omitempty"`
	Value   string  `json:"value,omitempty"`
	Member  string  `json:"member,omitempty"`
	Score   float64 `json:"score,omitempty"`
	Start   int     `json:"start,omitempty"`
	Stop    int     `json:"stop,omitempty"`
	TTL     int64   `json:"ttl,omitempty"`
	// The organization, when the run has none (a schedule for every organization).
	Tenancy string `json:"tenancy,omitempty"`
}

// Error is a refusal Leafen answered a host call with.
type Error struct{ Message string }

func (failure *Error) Error() string { return failure.Message }

// InputJSON decodes the call's input into target.
func InputJSON(target any) error { return json.Unmarshal(Input(), target) }

// OutputJSON sets the call's output to value as JSON.
func OutputJSON(value any) error {
	encoded, err := json.Marshal(value)
	if err == nil {
		Output(encoded)
	}
	return err
}

// API calls Leafen's native API.
func API(request APIRequest) (APIResponse, error) {
	response := APIResponse{}
	err := hostJSON(hostAPI, request, &response)
	return response, err
}

// Tenancies lists the organizations the app is installed for.
func Tenancies() ([]Tenancy, error) {
	tenancies := []Tenancy{}
	err := hostJSON(hostTenancies, struct{}{}, &tenancies)
	return tenancies, err
}

// Relay reaches an approved outside address through Leafen, which fills in
// the {{secret:NAME}} and {{oauth:KEY}} placeholders.
func Relay(request RelayRequest) (RelayResponse, error) {
	answer := struct {
		RelayResponse
		Error string `json:"error"`
	}{}
	if err := hostJSON(hostRelay, request, &answer); err != nil {
		return RelayResponse{}, err
	}
	if answer.Error != "" {
		return RelayResponse{}, &Error{Message: answer.Error}
	}
	return answer.RelayResponse, nil
}

// KV runs one command on the app's temporary store and returns its result.
func KV(request KVRequest) (json.RawMessage, error) {
	answer := struct {
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	}{}
	if err := hostJSON(hostKV, request, &answer); err != nil {
		return nil, err
	}
	if answer.Error != "" {
		return nil, &Error{Message: answer.Error}
	}
	return answer.Result, nil
}

// Log adds a line to the run's log, which Leafen keeps with the run.
func Log(message string) { _, _ = hostCall(hostLog, []byte(message)) }

// NoticeRequest is a notice for a person who may use the app: one line of
// text in their inbox (and mail and push, as they chose), opening Path in
// the app. The manifest declares "notifications": true.
type NoticeRequest struct {
	Person string `json:"person"`
	Text   string `json:"text"`
	Path   string `json:"path,omitempty"`
	// The organization, when the run has none (a schedule for every organization).
	Tenancy string `json:"tenancy,omitempty"`
}

// SocketOpen connects to a TCP or TLS address the manifest's network.sockets
// lists, through Leafen; a run keeps at most four open, closed when it ends.
func SocketOpen(host string, port int, tls bool) (int, error) {
	answer := struct {
		Socket int    `json:"socket"`
		Error  string `json:"error"`
	}{}
	if err := hostJSON(hostSocketOpen, map[string]any{"host": host, "port": port, "tls": tls}, &answer); err != nil {
		return 0, err
	}
	if answer.Error != "" {
		return 0, &Error{Message: answer.Error}
	}
	return answer.Socket, nil
}

// SocketWrite sends up to 64 KB.
func SocketWrite(socket int, data []byte) error {
	return socketCall(hostSocketWrite, map[string]any{"socket": socket, "data": data}, nil)
}

// SocketRead waits up to wait (30 seconds at most) for up to max bytes (64 KB
// at most); closed says the connection ended.
func SocketRead(socket, max int, wait time.Duration) (data []byte, closed bool, err error) {
	answer := struct {
		Data   []byte `json:"data"`
		Closed bool   `json:"closed"`
	}{}
	err = socketCall(hostSocketRead, map[string]any{"socket": socket, "max": max, "waitMs": wait.Milliseconds()}, &answer)
	return answer.Data, answer.Closed, err
}

// SocketClose ends a connection.
func SocketClose(socket int) error {
	return socketCall(hostSocketClose, map[string]any{"socket": socket}, nil)
}

func socketCall(function hostFunction, request map[string]any, answer any) error {
	raw, err := hostCall(function, mustJSON(request))
	if err != nil {
		return err
	}
	refusal := struct {
		Error string `json:"error"`
	}{}
	if json.Unmarshal(raw, &refusal) == nil && refusal.Error != "" {
		return &Error{Message: refusal.Error}
	}
	if answer != nil {
		return json.Unmarshal(raw, answer)
	}
	return nil
}

func mustJSON(value any) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}

// Wait lets a long-running worker rest up to a minute; it returns true when
// the worker should end (the app stopped, or its hour is nearly up).
func Wait(pause time.Duration) (stop bool, err error) {
	answer := struct {
		Stop bool `json:"stop"`
	}{}
	err = hostJSON(hostWait, map[string]int64{"ms": pause.Milliseconds()}, &answer)
	return answer.Stop, err
}

// RoomNext gives a room loop the messages its room sent the server — up to
// 64, waiting up to wait (30 seconds at most) for the first — whether to
// stop (the room emptied, the app stopped) and how many are in the room.
func RoomNext(wait time.Duration) (messages []string, stop bool, peers int, err error) {
	answer := struct {
		Messages []string `json:"messages"`
		Stop     bool     `json:"stop"`
		Peers    int      `json:"peers"`
	}{}
	err = hostJSON(hostRoomNext, map[string]int64{"waitMs": wait.Milliseconds()}, &answer)
	return answer.Messages, answer.Stop, answer.Peers, err
}

// RoomSender is who sent a room's message, as Leafen knows them: a person
// (their ID and name) or a guest (their session's ID; a guest's name, if
// the app asks for one, is the guest's own word).
type RoomSender struct {
	Person string `json:"person,omitempty"`
	Guest  string `json:"guest,omitempty"`
	Name   string `json:"name"`
}

// RoomMessage is a message a room sent the server and who sent it.
type RoomMessage struct {
	Text string
	From RoomSender
}

// RoomNextFrom is RoomNext with each message's sender.
func RoomNextFrom(wait time.Duration) (messages []RoomMessage, stop bool, peers int, err error) {
	answer := struct {
		Messages []string     `json:"messages"`
		Senders  []RoomSender `json:"senders"`
		Stop     bool         `json:"stop"`
		Peers    int          `json:"peers"`
	}{}
	err = hostJSON(hostRoomNext, map[string]int64{"waitMs": wait.Milliseconds()}, &answer)
	for index, text := range answer.Messages {
		message := RoomMessage{Text: text}
		if index < len(answer.Senders) {
			message.From = answer.Senders[index]
		}
		messages = append(messages, message)
	}
	return messages, answer.Stop, answer.Peers, err
}

// RoomSend sends a message (text up to 64 KB) to everyone in a room loop's room.
func RoomSend(message string) error {
	return socketCall(hostRoomSend, map[string]any{"message": message}, nil)
}

// Notify sends a notice; at most ten an hour reach one person.
func Notify(request NoticeRequest) error {
	answer := struct {
		Error string `json:"error"`
	}{}
	if err := hostJSON(hostNotify, request, &answer); err != nil {
		return err
	}
	if answer.Error != "" {
		return &Error{Message: answer.Error}
	}
	return nil
}

// EndpointRequest is what an endpoint's function gets: the method, the path
// below the endpoint (starting with /), the query, the headers (lowercased,
// without cookies, and without the sign-in except on an opened public
// endpoint, which leaves it to the app), the body as base64 — or, past a megabyte,
// BodyFile, the body kept as a file in the app's workspace — the person
// and organization it acts for, and the endpoint's address as people are
// shown it.
type EndpointRequest struct {
	Endpoint string            `json:"endpoint"`
	Address  string            `json:"address"`
	Method   string            `json:"method"`
	Path     string            `json:"path"`
	Query    string            `json:"query"`
	Headers  map[string]string `json:"headers"`
	Body     string            `json:"body"`
	BodyFile *StoredFile       `json:"bodyFile,omitempty"`
	Person   string            `json:"person"`
	Tenancy  string            `json:"tenancy"`
}

// EndpointAnswer is what an endpoint's function returns: a status, headers,
// and a body as text, as base64, or as a stored file Leafen sends with
// ranges.
type EndpointAnswer struct {
	Status     int               `json:"status,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       string            `json:"body,omitempty"`
	BodyBase64 string            `json:"bodyBase64,omitempty"`
	File       string            `json:"file,omitempty"`
	// PackageFile names one of the package's site/ files, below site/, for
	// an endpoint that serves pages: Leafen sends it as a page.
	PackageFile string `json:"packageFile,omitempty"`
}

// StoredFile is a file in Leafen's storage.
type StoredFile struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	ContentType string `json:"contentType"`
	SHA256      string `json:"sha256"`
}

// JoinFiles writes files the run may read one after another into a new file
// in the app's workspace, as a resumable or multipart upload ends.
func JoinFiles(files []string, name, contentType string) (StoredFile, error) {
	return JoinFilesInto("", files, name, contentType)
}

// JoinFilesInto is JoinFiles into another workspace the run may write —
// where a page that takes the file as a block lives. Joining one file
// copies it there.
func JoinFilesInto(workspaceID string, files []string, name, contentType string) (StoredFile, error) {
	answer := struct {
		File  StoredFile `json:"file"`
		Error string     `json:"error"`
	}{}
	request := map[string]any{"files": files, "name": name, "contentType": contentType}
	if workspaceID != "" {
		request["workspaceId"] = workspaceID
	}
	if err := hostJSON(hostFilesJoin, request, &answer); err != nil {
		return StoredFile{}, err
	}
	if answer.Error != "" {
		return StoredFile{}, &Error{Message: answer.Error}
	}
	return answer.File, nil
}

// hostJSON calls a host function with request as JSON and decodes its
// answer into answer; Leafen's refusal ({"error": "…"}) is an *Error.
func hostJSON(function hostFunction, request, answer any) error {
	encoded, err := json.Marshal(request)
	if err != nil {
		return err
	}
	raw, err := hostCall(function, encoded)
	if err != nil {
		return err
	}
	refusal := struct {
		Error string `json:"error"`
	}{}
	if len(raw) > 0 && raw[0] == '{' && json.Unmarshal(raw, &refusal) == nil && refusal.Error != "" {
		return &Error{Message: refusal.Error}
	}
	return json.Unmarshal(raw, answer)
}

type hostFunction int

const (
	hostLog hostFunction = iota
	hostAPI
	hostRelay
	hostTenancies
	hostKV
	hostNotify
	hostSocketOpen
	hostSocketWrite
	hostSocketRead
	hostSocketClose
	hostWait
	hostRoomNext
	hostRoomSend
	hostFilesJoin
)
