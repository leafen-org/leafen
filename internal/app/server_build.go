package app

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"embed"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// What a server says of its own build (2026-11-16: an agent could not tell
// whether fixes had reached a server, which said only "0.1.0-dev"): the
// commit it is built from, when that commit was made and the build was, when
// this server first started on it, its data's schema version, and what
// changed since the build it ran before. The commit and the change list come
// from build_stamp (scripts/stamp-build.mjs, or git archive's export-subst
// for the commit alone); a build without them says so. The builds a server
// started on are kept in its data folder, newest first.

//go:embed build_stamp
var serverBuildStampFiles embed.FS

const (
	serverBuildsFile = "builds.json"
	serverBuildsKept = 20
	// What one answer lists of the changes; the count says how many there are.
	serverBuildChangesListed = 200
)

var archiveStampPattern = regexp.MustCompile(`^([0-9a-f]{40}) ([0-9]+)$`)

// serverBuild is this build, as the settings and an agent read it.
type serverBuild struct {
	Release string `json:"release"`
	// Build names this build: the release and its commit, or where the
	// commit isn't known, the program file's time.
	Build      string `json:"build"`
	Commit     string `json:"commit,omitempty"`
	CommitTime string `json:"commitTime,omitempty"`
	// Modified: built from a checkout with changes beside its commit.
	Modified bool   `json:"modified,omitzero"`
	BuiltAt  string `json:"builtAt,omitempty"`
	// Where builtAt comes from: "stamp", or "program file" — the time the
	// program file was written, for a build without a stamp.
	BuiltAtFrom   string `json:"builtAtFrom,omitempty"`
	SchemaVersion int    `json:"schemaVersion"`
	// When this server first started on this build, and this process.
	DeployedAt string `json:"deployedAt,omitempty"`
	StartedAt  string `json:"startedAt"`
}

// serverBuildSeen is a build the server started on.
type serverBuildSeen struct {
	Build          string `json:"build"`
	Release        string `json:"release"`
	Commit         string `json:"commit,omitempty"`
	SchemaVersion  int    `json:"schemaVersion"`
	FirstStartedAt int64  `json:"firstStartedAt"`
	LastStartedAt  int64  `json:"lastStartedAt"`
}

// serverBuildChange is a commit since another build.
type serverBuildChange struct {
	Commit  string `json:"commit"`
	At      string `json:"at"`
	Subject string `json:"subject"`
}

type serverBuildStamp struct {
	Commit     string `json:"commit"`
	CommitTime int64  `json:"commitTime"`
	BuiltAt    int64  `json:"builtAt"`
	Modified   bool   `json:"modified"`
}

var serverBuildState = struct {
	once      sync.Once
	stamp     serverBuildStamp
	startedAt time.Time
	mu        sync.Mutex
	// Each kept commit's parents and subject, read once when first asked.
	changesRead bool
	order       []string
	parents     map[string][]string
	subjects    map[string]string
	times       map[string]int64
}{startedAt: time.Now()}

func serverBuildStampOf() serverBuildStamp {
	serverBuildState.once.Do(func() {
		if raw, err := serverBuildStampFiles.ReadFile("build_stamp/stamp.json"); err == nil {
			_ = json.Unmarshal(raw, &serverBuildState.stamp)
		}
		if serverBuildState.stamp.Commit != "" {
			return
		}
		// A source put out with git archive: its commit and the commit's time.
		if raw, err := serverBuildStampFiles.ReadFile("build_stamp/archive.txt"); err == nil {
			if match := archiveStampPattern.FindStringSubmatch(strings.TrimSpace(string(raw))); match != nil {
				serverBuildState.stamp.Commit = match[1]
				serverBuildState.stamp.CommitTime, _ = strconv.ParseInt(match[2], 10, 64)
			}
		}
	})
	return serverBuildState.stamp
}

// currentServerBuild is this build, with the data's schema version and when
// this server first started on it.
func currentServerBuild(app core.App) serverBuild {
	stamp := serverBuildStampOf()
	build := serverBuild{Release: ReleaseVersion(), Commit: stamp.Commit, Modified: stamp.Modified,
		StartedAt: serverBuildState.startedAt.UTC().Format(time.RFC3339)}
	build.Build = serverBuildName(stamp)
	if stamp.CommitTime > 0 {
		build.CommitTime = time.Unix(stamp.CommitTime, 0).UTC().Format(time.RFC3339)
	}
	if stamp.BuiltAt > 0 {
		build.BuiltAt, build.BuiltAtFrom = time.Unix(stamp.BuiltAt, 0).UTC().Format(time.RFC3339), "stamp"
	} else if written := serverProgramTime(); !written.IsZero() {
		build.BuiltAt, build.BuiltAtFrom = written.UTC().Format(time.RFC3339), "program file"
	}
	_ = app.DB().NewQuery(`SELECT COALESCE(MAX([[version]]), 0) FROM {{leafen_schema_migrations}}`).Row(&build.SchemaVersion)
	for _, seen := range readServerBuildsSeen(app) {
		if seen.Build == build.Build {
			build.DeployedAt = time.UnixMilli(seen.FirstStartedAt).UTC().Format(time.RFC3339)
			break
		}
	}
	return build
}

func serverBuildName(stamp serverBuildStamp) string {
	switch {
	case len(stamp.Commit) >= 12 && stamp.Modified:
		return ReleaseVersion() + "+" + stamp.Commit[:12] + ".modified"
	case len(stamp.Commit) >= 12:
		return ReleaseVersion() + "+" + stamp.Commit[:12]
	}
	if written := serverProgramTime(); !written.IsZero() {
		return ReleaseVersion() + "+built." + written.UTC().Format("20060102T150405Z")
	}
	return ReleaseVersion()
}

// serverProgramTime is when the running program's file was written.
func serverProgramTime() time.Time {
	program, err := os.Executable()
	if err != nil {
		return time.Time{}
	}
	info, err := os.Stat(program)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

func readServerBuildsSeen(app core.App) []serverBuildSeen {
	seen := []serverBuildSeen{}
	if raw, err := os.ReadFile(filepath.Join(app.DataDir(), serverBuildsFile)); err == nil {
		_ = json.Unmarshal(raw, &seen)
	}
	return seen
}

// noteServerBuildStart keeps the build the server starts on: a new one
// first, the one it ran before next. It says so in the server's output too
// (server_build_start_line.go).
func noteServerBuildStart(app core.App) {
	build := currentServerBuild(app)
	now := serverBuildState.startedAt.UnixMilli()
	seen := readServerBuildsSeen(app)
	previous := ""
	if len(seen) > 0 && seen[0].Build != build.Build {
		previous = seen[0].Build
	}
	sayServerBuildStart(app, build, previous)
	if len(seen) > 0 && seen[0].Build == build.Build {
		seen[0].LastStartedAt, seen[0].SchemaVersion = now, build.SchemaVersion
	} else {
		seen = append([]serverBuildSeen{{Build: build.Build, Release: build.Release, Commit: build.Commit,
			SchemaVersion: build.SchemaVersion, FirstStartedAt: now, LastStartedAt: now}}, seen...)
	}
	if len(seen) > serverBuildsKept {
		seen = seen[:serverBuildsKept]
	}
	if err := writeSetupFile(app.DataDir(), serverBuildsFile, seen); err != nil {
		app.Logger().Warn("The builds this server started on couldn't be kept", "error", err)
	}
}

// previousServerBuild is the build the server ran before this one.
func previousServerBuild(app core.App, current string) (serverBuildSeen, bool) {
	for _, seen := range readServerBuildsSeen(app) {
		if seen.Build != current {
			return seen, true
		}
	}
	return serverBuildSeen{}, false
}

func readServerBuildChanges() bool {
	serverBuildState.mu.Lock()
	defer serverBuildState.mu.Unlock()
	if serverBuildState.changesRead {
		return len(serverBuildState.order) > 0
	}
	serverBuildState.changesRead = true
	raw, err := fs.ReadFile(serverBuildStampFiles, "build_stamp/changes.txt.gz")
	if err != nil {
		return false
	}
	reader, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return false
	}
	serverBuildState.parents, serverBuildState.subjects, serverBuildState.times = map[string][]string{}, map[string]string{}, map[string]int64{}
	lines := bufio.NewScanner(reader)
	lines.Buffer(make([]byte, 64<<10), 1<<20)
	for lines.Scan() {
		fields := strings.SplitN(lines.Text(), " ", 4)
		if len(fields) < 3 {
			continue
		}
		commit := fields[0]
		serverBuildState.order = append(serverBuildState.order, commit)
		if fields[1] != "-" {
			serverBuildState.parents[commit] = strings.Split(fields[1], ",")
		}
		serverBuildState.times[commit], _ = strconv.ParseInt(fields[2], 10, 64)
		if len(fields) == 4 {
			serverBuildState.subjects[commit] = fields[3]
		}
	}
	return len(serverBuildState.order) > 0
}

// serverBuildChangesSince lists the commits this build has that one before
// it hadn't, newest first, leaving merges out; known is false when this
// build's list doesn't reach that commit.
func serverBuildChangesSince(since string) (changes []serverBuildChange, total int, known bool) {
	stamp := serverBuildStampOf()
	if len(stamp.Commit) < 12 || len(since) < 7 || !readServerBuildChanges() {
		return nil, 0, false
	}
	serverBuildState.mu.Lock()
	defer serverBuildState.mu.Unlock()
	find := func(prefix string) string {
		prefix = strings.ToLower(prefix)
		for _, commit := range serverBuildState.order {
			if strings.HasPrefix(commit, prefix) || strings.HasPrefix(prefix, commit) {
				return commit
			}
		}
		return ""
	}
	head, base := find(stamp.Commit), find(since)
	if head == "" || base == "" {
		return nil, 0, false
	}
	reach := func(from string) map[string]bool {
		reached := map[string]bool{}
		next := []string{from}
		for len(next) > 0 {
			commit := next[len(next)-1]
			next = next[:len(next)-1]
			if reached[commit] {
				continue
			}
			reached[commit] = true
			next = append(next, serverBuildState.parents[commit]...)
		}
		return reached
	}
	had, has := reach(base), reach(head)
	changes = []serverBuildChange{}
	for _, commit := range serverBuildState.order {
		if !has[commit] || had[commit] || len(serverBuildState.parents[commit]) > 1 {
			continue
		}
		total++
		if len(changes) < serverBuildChangesListed {
			changes = append(changes, serverBuildChange{Commit: commit, Subject: serverBuildState.subjects[commit],
				At: time.Unix(serverBuildState.times[commit], 0).UTC().Format(time.RFC3339)})
		}
	}
	return changes, total, true
}
