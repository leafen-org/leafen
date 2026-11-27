package smoke

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// walk is what the core path's steps hand on to the next.
type walk struct {
	owner      *testing.T
	program    string
	root       string
	port, door int
	server     *server

	key, mcpURL, workspaceID string
	pageID                   string
	sourceID, titleID        string
	backupPath, backupSHA256 string
}

const (
	personName    = "Smoke Tester"
	agentName     = "Leafen smoke test"
	pageTitle     = "Smoke test page"
	paragraphText = "Written by the smoke test, kept through a backup."
	databaseTitle = "Smoke test tasks"
	rowTitle      = "First smoke task"
)

// TestCorePath walks the core path once, each step on what the one before
// it made; a step that fails stops the walk.
func TestCorePath(t *testing.T) {
	if testing.Short() {
		t.Skip("the smoke walk starts the server program; skipped with -short")
	}
	w := &walk{owner: t, program: program(t), root: t.TempDir()}
	w.port, w.door = ports(t)
	steps := []struct {
		name string
		run  func(*testing.T)
	}{
		{"ServeEmptyFolder", w.serveEmptyFolder},
		{"ServesScreensAndNotices", w.servesScreensAndNotices},
		{"RefusesWithoutKey", w.refusesWithoutKey},
		{"FirstSetupByCommand", w.firstSetupByCommand},
		{"AgentConnectsAfterYes", w.agentConnectsAfterYes},
		{"PageThroughAPI", w.pageThroughAPI},
		{"PageTextThroughAPI", w.pageTextThroughAPI},
		{"DatabaseThroughAPI", w.databaseThroughAPI},
		{"MCPToolList", w.mcpToolList},
		{"MCPReadsThePage", w.mcpReadsThePage},
		{"BackupWhileServing", w.backupWhileServing},
		{"RestoreIntoNewFolder", w.restoreIntoNewFolder},
		{"RestoredDataIsThere", w.restoredDataIsThere},
	}
	for _, step := range steps {
		if !t.Run(step.name, step.run) {
			t.Fatalf("the walk stopped at %s", step.name)
		}
	}
}

func (w *walk) dataDir() string     { return filepath.Join(w.root, "data") }
func (w *walk) restoredDir() string { return filepath.Join(w.root, "restored") }

// A server started on an empty folder makes its data there and answers its health.
func (w *walk) serveEmptyFolder(t *testing.T) {
	if err := os.Mkdir(w.dataDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	w.server = serve(t, w.owner, w.program, w.dataDir(), w.port, w.door)
	if _, err := os.Stat(filepath.Join(w.dataDir(), "data.db")); err != nil {
		t.Fatalf("the server made no database in the empty folder: %v", err)
	}
}

// The program carries its web screens and its edition's third-party notices.
func (w *walk) servesScreensAndNotices(t *testing.T) {
	for _, page := range []struct{ path, kind, holds string }{
		{"/", "text/html", "<html"},
		{"/THIRD-PARTY-NOTICES.txt", "text/plain", "Third-party software notices for Leafen"},
	} {
		response, err := http.Get(w.server.origin + page.path)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, 1<<16))
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), page.kind) ||
			!strings.Contains(string(body), page.holds) {
			t.Fatalf("GET %s answered %d %s, not %s holding %q", page.path, response.StatusCode,
				response.Header.Get("Content-Type"), page.kind, page.holds)
		}
	}
}

// The API and MCP answer nothing without a key, nor with a made-up one.
func (w *walk) refusesWithoutKey(t *testing.T) {
	for _, key := range []string{"", "made-up-key"} {
		var refusal struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		status, err := request(http.MethodGet, w.server.origin+apiPath+"/operations", key, nil, &refusal)
		if err != nil {
			t.Fatal(err)
		}
		if status != http.StatusUnauthorized || refusal.Error.Code != "LEAFEN_UNAUTHORIZED" {
			t.Fatalf("the API answered %d %q to the key %q", status, refusal.Error.Code, key)
		}
		status, err = request(http.MethodPost, w.server.origin+mcpPath, key,
			map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if status != http.StatusUnauthorized {
			t.Fatalf("MCP answered %d to the key %q", status, key)
		}
	}
}

type setupStatus struct {
	Answered bool `json:"answered"`
	Facts    struct {
		Configured    bool   `json:"configured"`
		Administrator string `json:"administrator"`
		Language      string `json:"language"`
	} `json:"facts"`
}

var confirmationPattern = regexp.MustCompile(`--confirmation ([A-Za-z0-9_-]+)`)

// The first run goes by commands: the name waits for the person's yes,
// which the agent passes on, and the server makes its first administrator.
func (w *walk) firstSetupByCommand(t *testing.T) {
	var before setupStatus
	leafenJSON(t, w.program, w.dataDir(), w.door, &before, "setup", "status", "--json")
	if !before.Answered || before.Facts.Configured {
		t.Fatalf("before the first run, setup status answered %+v", before)
	}
	asked := leafen(t, w.program, w.dataDir(), w.door, "setup", "start", "--name", personName, "--language", "en")
	confirmation := confirmationPattern.FindStringSubmatch(asked)
	if confirmation == nil {
		t.Fatalf("setup start gave no confirmation to pass the person's yes on with:\n%s", asked)
	}
	started := leafen(t, w.program, w.dataDir(), w.door,
		"setup", "start", "--name", personName, "--language", "en", "--confirmation", confirmation[1])
	if !strings.Contains(started, "Leafen is set up") {
		t.Fatalf("setup start with the yes answered:\n%s", started)
	}
	var after setupStatus
	leafenJSON(t, w.program, w.dataDir(), w.door, &after, "setup", "status", "--json")
	if !after.Facts.Configured || after.Facts.Administrator != personName || after.Facts.Language != "en" {
		t.Fatalf("after the first run, setup status answered %+v", after)
	}
}

// An agent on this computer connects once its person says yes: asked first,
// with no key, and given its key and MCP address only with --approved.
func (w *walk) agentConnectsAfterYes(t *testing.T) {
	var ask struct {
		Status string `json:"status"`
		Key    string `json:"key"`
	}
	// The first administrator's workspace may still be being listed just
	// after the first run, which the command says rather than waiting long.
	askArgs := []string{"connect", "--name", agentName, "--json"}
	for attempt := 1; ; attempt++ {
		out, err := tryLeafen(w.program, w.dataDir(), w.door, askArgs...)
		if err != nil && strings.Contains(err.Error(), "still being listed") && attempt < 10 {
			time.Sleep(time.Second)
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		readJSON(t, out, &ask, askArgs...)
		break
	}
	if ask.Status != "ask_person" || ask.Key != "" {
		t.Fatalf("connect without the yes answered status %q (a key given: %t)", ask.Status, ask.Key != "")
	}
	var connected struct {
		Status       string   `json:"status"`
		Key          string   `json:"key"`
		MCPURL       string   `json:"mcpUrl"`
		WorkspaceIDs []string `json:"workspaceIds"`
	}
	leafenJSON(t, w.program, w.dataDir(), w.door, &connected, "connect", "--name", agentName, "--approved", "--json")
	if connected.Status != "connected" || connected.Key == "" || len(connected.WorkspaceIDs) == 0 ||
		connected.MCPURL != w.server.origin+mcpPath {
		t.Fatalf("connect --approved answered status %q, MCP address %q, %d workspaces (a key given: %t)",
			connected.Status, connected.MCPURL, len(connected.WorkspaceIDs), connected.Key != "")
	}
	w.key, w.mcpURL, w.workspaceID = connected.Key, connected.MCPURL, connected.WorkspaceIDs[0]
}

type pageAnswer struct {
	Page struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"page"`
	Duplicate bool `json:"duplicate"`
}

// A page made through the API with a request id is made once: the same
// request again answers the same page.
func (w *walk) pageThroughAPI(t *testing.T) {
	body := map[string]any{"requestId": "smoke-page-" + newID(t), "workspaceId": w.workspaceID, "title": pageTitle}
	var made, again pageAnswer
	api(t, w.server.origin, w.key, http.MethodPost, "/pages", body, &made, http.StatusCreated, http.StatusOK)
	if made.Page.ID == "" || made.Page.Title != pageTitle || made.Duplicate {
		t.Fatalf("POST /pages answered %+v", made)
	}
	api(t, w.server.origin, w.key, http.MethodPost, "/pages", body, &again, http.StatusCreated, http.StatusOK)
	if again.Page.ID != made.Page.ID || !again.Duplicate {
		t.Fatalf("the same request again answered %+v, not the page made first (%s)", again, made.Page.ID)
	}
	w.pageID = made.Page.ID
	var read struct {
		Title string `json:"title"`
	}
	api(t, w.server.origin, w.key, http.MethodGet, "/pages/"+w.pageID, nil, &read, http.StatusOK)
	if read.Title != pageTitle {
		t.Fatalf("GET /pages/{id} answered the title %q", read.Title)
	}
}

type mutationReceipt struct {
	Counts struct {
		Blocks      int `json:"blocks"`
		DataSources int `json:"dataSources"`
		Views       int `json:"views"`
		Properties  int `json:"properties"`
		Rows        int `json:"rows"`
	} `json:"counts"`
}

// A paragraph goes into the page through one content-graph mutation.
func (w *walk) pageTextThroughAPI(t *testing.T) {
	block := newID(t)
	var receipt mutationReceipt
	api(t, w.server.origin, w.key, http.MethodPost, "/pages/"+w.pageID+"/content-graph/mutations", map[string]any{
		"mutationId": "smoke-text-" + block,
		"operations": []any{map[string]any{"kind": "upsertBlock", "block": map[string]any{
			"id": block, "parentId": nil, "afterId": nil, "position": "a0", "type": "paragraph", "content": map[string]any{},
		}}},
		"textUpdates": []any{map[string]any{"blockId": block, "spans": []any{map[string]any{"text": paragraphText}}}},
	}, &receipt, http.StatusCreated, http.StatusOK)
	if receipt.Counts.Blocks != 1 {
		t.Fatalf("the mutation wrote %d blocks", receipt.Counts.Blocks)
	}
	if !slices.Contains(graphBlockTypes(t, w.server.origin, w.key, w.pageID), "paragraph") {
		t.Fatal("the page's content graph has no paragraph")
	}
}

// graphBlockTypes reads a page's content graph and answers its blocks' types.
func graphBlockTypes(t *testing.T, origin, key, pageID string) []string {
	t.Helper()
	var graph struct {
		Items []struct {
			Kind  string `json:"kind"`
			Value struct {
				Type string `json:"type"`
			} `json:"value"`
		} `json:"items"`
	}
	api(t, origin, key, http.MethodGet, "/pages/"+pageID+"/content-graph", nil, &graph, http.StatusOK)
	types := []string{}
	for _, item := range graph.Items {
		if item.Kind == "block" {
			types = append(types, item.Value.Type)
		}
	}
	return types
}

// A database goes into the page through one mutation — its block, its data
// source, a table view, a title property and a row — and its query answers
// the row.
func (w *walk) databaseThroughAPI(t *testing.T) {
	block, view, row := newID(t), newID(t), newID(t)
	w.sourceID, w.titleID = newID(t), newID(t)
	var receipt mutationReceipt
	api(t, w.server.origin, w.key, http.MethodPost, "/pages/"+w.pageID+"/content-graph/mutations", map[string]any{
		"mutationId": "smoke-database-" + block,
		"operations": []any{
			map[string]any{"kind": "upsertBlock", "block": map[string]any{
				"id": block, "parentId": nil, "afterId": nil, "position": "a1", "type": "inline_database",
				"content": map[string]any{"dataSourceId": w.sourceID, "viewId": view},
			}},
			map[string]any{"kind": "upsertDataSource", "dataSource": map[string]any{"id": w.sourceID, "title": databaseTitle}},
			map[string]any{"kind": "upsertView", "view": map[string]any{
				"id": view, "dataSourceId": w.sourceID, "type": "table", "name": "Table", "position": "a0",
				"config": map[string]any{"rowOrder": []any{}},
			}},
			map[string]any{"kind": "upsertProperty", "property": map[string]any{
				"id": w.titleID, "dataSourceId": w.sourceID, "type": "title", "name": "Name", "position": "a0", "config": map[string]any{},
			}},
			map[string]any{"kind": "upsertRow", "row": map[string]any{
				"id": row, "dataSourceId": w.sourceID, "subitemParentId": nil, "position": "a0",
				"values": map[string]any{w.titleID: map[string]any{"type": "title", "value": rowTitle}},
			}},
		},
	}, &receipt, http.StatusCreated, http.StatusOK)
	if c := receipt.Counts; c.Blocks != 1 || c.DataSources != 1 || c.Views != 1 || c.Properties != 1 || c.Rows != 1 {
		t.Fatalf("the database's mutation wrote %+v", c)
	}
	if titles := w.rowTitles(t, w.server.origin); !slices.Equal(titles, []string{rowTitle}) {
		t.Fatalf("the database's query answered the rows %q", titles)
	}
}

// rowTitles queries the walk's database and answers its rows' titles.
func (w *walk) rowTitles(t *testing.T, origin string) []string {
	t.Helper()
	var query struct {
		State string `json:"state"`
		Rows  []struct {
			Values map[string]struct {
				Value any `json:"value"`
			} `json:"values"`
		} `json:"rows"`
	}
	api(t, origin, w.key, http.MethodPost, "/pages/"+w.pageID+"/database-query", map[string]any{
		"dataSourceId": w.sourceID, "sorts": []any{}, "subitemScope": "all", "archiveScope": "active",
		"timeZone": "UTC", "limit": 50, "countOnly": false,
	}, &query, http.StatusOK)
	titles := []string{}
	for _, row := range query.Rows {
		titles = append(titles, fmt.Sprint(row.Values[w.titleID].Value))
	}
	return titles
}

// The agent's MCP connection lists Leafen's core tools, among them the ones
// that find, read and change pages and reach every other operation by name;
// a tool left off the list is still called by its name (MCPReadsThePage).
func (w *walk) mcpToolList(t *testing.T) {
	session := connectMCP(t, w.mcpURL, w.key)
	ctx, cancel := context.WithTimeout(context.Background(), requestWait)
	defer cancel()
	names := []string{}
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, tool.Name)
	}
	for _, wanted := range []string{"leafen_search", "notion-fetch", "notion-update-page", "leafen_operations", "leafen_call"} {
		if !slices.Contains(names, wanted) {
			t.Fatalf("the MCP tool list (%d tools) has no %s", len(names), wanted)
		}
	}
}

// The agent reads the page it wrote by MCP, with its text.
func (w *walk) mcpReadsThePage(t *testing.T) {
	title, content := fetchPage(t, connectMCP(t, w.mcpURL, w.key), w.pageID)
	if title != pageTitle || !strings.Contains(content, paragraphText) {
		t.Fatalf("leafen_fetch answered the title %q and the content %q", title, content)
	}
}

var sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// A complete backup is made while the server runs.
func (w *walk) backupWhileServing(t *testing.T) {
	var receipt struct {
		Status string `json:"status"`
		Path   string `json:"path"`
		Bytes  int64  `json:"bytes"`
		SHA256 string `json:"sha256"`
	}
	output := filepath.Join(w.root, "backup.zip")
	leafenJSON(t, w.program, w.dataDir(), w.door, &receipt, "backup", "--output", output)
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Status != "created" || !sha256Pattern.MatchString(receipt.SHA256) || receipt.Bytes != info.Size() {
		t.Fatalf("backup answered %+v for a file of %d bytes", receipt, info.Size())
	}
	w.backupPath, w.backupSHA256 = output, receipt.SHA256
}

// The backup goes into a new folder, which the doctor finds sound.
func (w *walk) restoreIntoNewFolder(t *testing.T) {
	w.server.stop()
	var receipt struct {
		Status string `json:"status"`
		SHA256 string `json:"sha256"`
	}
	leafenJSON(t, w.program, w.restoredDir(), w.door, &receipt,
		"restore", "--input", w.backupPath, "--sha256", w.backupSHA256)
	if receipt.Status != "restored" || receipt.SHA256 != w.backupSHA256 {
		t.Fatalf("restore answered %+v", receipt)
	}
	var doctor struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	leafenJSON(t, w.program, w.restoredDir(), w.door, &doctor, "doctor")
	if doctor.Status != "ok" {
		t.Fatalf("the doctor found the restored folder %q: %s", doctor.Status, doctor.Reason)
	}
}

// A server on the restored folder has it all: the first administrator, the
// agent's connection, the page with its text and the database with its row.
func (w *walk) restoredDataIsThere(t *testing.T) {
	w.server = serve(t, w.owner, w.program, w.restoredDir(), w.port, w.door)
	var status setupStatus
	leafenJSON(t, w.program, w.restoredDir(), w.door, &status, "setup", "status", "--json")
	if !status.Facts.Configured || status.Facts.Administrator != personName {
		t.Fatalf("on the restored folder, setup status answered %+v", status)
	}
	var page struct {
		Title string `json:"title"`
	}
	api(t, w.server.origin, w.key, http.MethodGet, "/pages/"+w.pageID, nil, &page, http.StatusOK)
	if page.Title != pageTitle {
		t.Fatalf("the restored page's title is %q", page.Title)
	}
	if _, content := fetchPage(t, connectMCP(t, w.mcpURL, w.key), w.pageID); !strings.Contains(content, paragraphText) {
		t.Fatalf("the restored page's content is %q", content)
	}
	if titles := w.rowTitles(t, w.server.origin); !slices.Equal(titles, []string{rowTitle}) {
		t.Fatalf("the restored database's query answered the rows %q", titles)
	}
}

// newID is a new random UUID, as a program names the blocks and rows it writes.
func newID(t *testing.T) string {
	t.Helper()
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatal(err)
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16])
}
