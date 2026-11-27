package smoke

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	apiPath     = "/api/leafen/v1"
	mcpPath     = "/api/leafen/mcp"
	serverStart = 90 * time.Second
	commandWait = 90 * time.Second
	requestWait = 30 * time.Second
)

// program is the server program the walk runs: LEAFEN_SMOKE_BINARY (a
// relative path from the repository's root, as dist/leafen), or one built
// from this checkout into the test's own folder.
func program(t *testing.T) string {
	t.Helper()
	// go test runs in the package's folder, two below the root.
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if named := strings.TrimSpace(os.Getenv("LEAFEN_SMOKE_BINARY")); named != "" {
		path := named
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("LEAFEN_SMOKE_BINARY: %v", err)
		}
		return path
	}
	if _, err := os.Stat(filepath.Join(root, "web", "dist", "index.html")); err != nil {
		t.Fatalf("no web build in %s: run npm --prefix web run build first, or name a built program with LEAFEN_SMOKE_BINARY",
			filepath.Join(root, "web", "dist"))
	}
	out := filepath.Join(t.TempDir(), "leafen")
	if runtime.GOOS == "windows" {
		out += ".exe"
	}
	build := exec.Command("go", "build", "-ldflags=-s -w", "-o", out, "./cmd/leafen")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the server program: %v\n%s", err, output)
	}
	return out
}

// ports are the server's port and its tunnel door's: LEAFEN_SMOKE_PORTS, or
// two free loopback ports.
func ports(t *testing.T) (int, int) {
	t.Helper()
	if named := strings.TrimSpace(os.Getenv("LEAFEN_SMOKE_PORTS")); named != "" {
		serving, door, found := strings.Cut(named, ",")
		first, err1 := strconv.Atoi(strings.TrimSpace(serving))
		second, err2 := strconv.Atoi(strings.TrimSpace(door))
		if !found || err1 != nil || err2 != nil {
			t.Fatalf("LEAFEN_SMOKE_PORTS is \"<http port>,<door port>\", not %q", named)
		}
		return first, second
	}
	var listeners [2]net.Listener
	for index := range listeners {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		listeners[index] = listener
	}
	return listeners[0].Addr().(*net.TCPAddr).Port, listeners[1].Addr().(*net.TCPAddr).Port
}

// environment is what the server and its commands run with: no browser to
// open and no update check, so the walk asks nothing of the network.
func environment(door int) []string {
	return append(os.Environ(),
		"LEAFEN_SETUP_OPEN_MODE=print",
		"LEAFEN_UPDATE_CHECK=off",
		"LEAFEN_TUNNEL_DOOR_PORT="+strconv.Itoa(door),
	)
}

// server is one run of `leafen serve` on a data folder.
type server struct {
	origin string
	log    string
	cmd    *exec.Cmd
	exited chan error
	once   sync.Once
}

// serve starts the program on dir and waits until its health answers ok.
// It runs until stopped or until the walk (owner) ends.
func serve(t, owner *testing.T, program, dir string, port, door int) *server {
	t.Helper()
	logFile, err := os.Create(dir + ".log")
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	address := "127.0.0.1:" + strconv.Itoa(port)
	s := &server{origin: "http://" + address, log: logFile.Name(), exited: make(chan error, 1)}
	s.cmd = exec.Command(program, "--dir", dir, "serve", "--http", address)
	s.cmd.Env = environment(door)
	s.cmd.Stdout, s.cmd.Stderr = logFile, logFile
	if err := s.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { s.exited <- s.cmd.Wait() }()
	owner.Cleanup(func() {
		s.stop()
		if owner.Failed() {
			s.showLog(owner)
		}
	})
	deadline := time.Now().Add(serverStart)
	for {
		var health struct {
			Status string `json:"status"`
		}
		if status, err := request(http.MethodGet, s.origin+apiPath+"/health", "", nil, &health); err == nil &&
			status == http.StatusOK && health.Status == "ok" {
			return s
		}
		select {
		case err := <-s.exited:
			s.exited <- err
			t.Fatalf("the server stopped before it answered: %v", err)
		case <-time.After(250 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatalf("the server's health didn't answer ok within %s", serverStart)
		}
	}
}

// stop ends the server as a person's Ctrl-C does, and waits for it.
func (s *server) stop() {
	s.once.Do(func() {
		_ = s.cmd.Process.Signal(os.Interrupt)
		select {
		case <-s.exited:
		case <-time.After(30 * time.Second):
			_ = s.cmd.Process.Kill()
			<-s.exited
		}
	})
}

// showLog puts the end of the server's log beside a failure.
func (s *server) showLog(t *testing.T) {
	raw, err := os.ReadFile(s.log)
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) > 60 {
		lines = lines[len(lines)-60:]
	}
	t.Logf("the end of the server's log (%s):\n%s", s.log, strings.Join(lines, "\n"))
}

// tryLeafen runs one of the program's commands on dir and answers what it
// printed; a command that fails answers what it said as the error.
func tryLeafen(program, dir string, door int, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandWait)
	defer cancel()
	command := exec.CommandContext(ctx, program, append([]string{"--dir", dir}, args...)...)
	command.Env = environment(door)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		return stdout.String(), fmt.Errorf("leafen %s: %w\n%s%s", strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String(), nil
}

// leafen is tryLeafen failing the step when the command fails.
func leafen(t *testing.T, program, dir string, door int, args ...string) string {
	t.Helper()
	out, err := tryLeafen(program, dir, door, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// leafenJSON runs a command that prints JSON and reads it into into.
func leafenJSON(t *testing.T, program, dir string, door int, into any, args ...string) {
	t.Helper()
	readJSON(t, leafen(t, program, dir, door, args...), into, args...)
}

// readJSON reads what a command printed as JSON into into.
func readJSON(t *testing.T, out string, into any, args ...string) {
	t.Helper()
	if err := json.Unmarshal([]byte(out), into); err != nil {
		t.Fatalf("leafen %s printed no JSON: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// request sends one HTTP request, with the key as its bearer when there is
// one, and reads a JSON answer into into.
func request(method, url, key string, body, into any) (int, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		reader = bytes.NewReader(raw)
	}
	ctx, cancel := context.WithTimeout(context.Background(), requestWait)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return response.StatusCode, err
	}
	if into != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, into); err != nil {
			return response.StatusCode, fmt.Errorf("%s %s answered %d with no JSON: %v: %.300s", method, url, response.StatusCode, err, raw)
		}
	}
	return response.StatusCode, nil
}

// api calls the native API with the agent's key and fails the step unless
// it answers one of the statuses wanted.
func api(t *testing.T, origin, key, method, path string, body, into any, wanted ...int) {
	t.Helper()
	status, err := request(method, origin+apiPath+path, key, body, into)
	if err != nil {
		t.Fatal(err)
	}
	for _, each := range wanted {
		if status == each {
			return
		}
	}
	t.Fatalf("%s %s answered %d, not %v", method, path, status, wanted)
}

// bearer adds the agent's key to every MCP request, as an agent's MCP
// settings do.
type bearer struct {
	key  string
	next http.RoundTripper
}

func (b bearer) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+b.key)
	return b.next.RoundTrip(req)
}

// connectMCP connects to the server's MCP as an agent with its key.
func connectMCP(t *testing.T, address, key string) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "leafen-smoke", Version: "1"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), requestWait)
	defer cancel()
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             address,
		HTTPClient:           &http.Client{Transport: bearer{key: key, next: http.DefaultTransport}, Timeout: requestWait},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("connecting to %s: %v", address, err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// fetchPage reads a page as an agent does, with leafen_fetch called by its
// name (the core list leaves it out), answering its title and its content
// as text.
func fetchPage(t *testing.T, session *mcp.ClientSession, pageID string) (string, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), requestWait)
	defer cancel()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "leafen_fetch", Arguments: map[string]any{"id": pageID}})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("leafen_fetch refused: %s", toolText(result))
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatalf("leafen_fetch answered %s: %v", raw, err)
	}
	return page.Title, page.Content
}

func toolText(result *mcp.CallToolResult) string {
	var words []string
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			words = append(words, text.Text)
		}
	}
	return strings.Join(words, "\n")
}
