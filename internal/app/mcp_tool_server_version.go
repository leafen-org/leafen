package app

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// leafen_server_version and GET /admin/server-version say what build a server
// runs and what changed since the one before (server_build.go), for an agent
// checking whether a fix reached it and for Settings → Server → Updates.

type leafenMCPServerVersionInput struct {
	Since string `json:"since,omitempty" jsonschema:"A commit (seven or more of its first characters) to list the changes since, instead of the build this server ran before."`
}

// serverVersionAnswer is the build, the one before it here, and the changes
// between them.
type serverVersionAnswer struct {
	serverBuild
	Previous *serverBuildSeenAnswer `json:"previous,omitempty"`
	Changes  serverVersionChanges   `json:"changes"`
}

type serverBuildSeenAnswer struct {
	Build         string `json:"build"`
	Release       string `json:"release"`
	Commit        string `json:"commit,omitempty"`
	SchemaVersion int    `json:"schemaVersion"`
	DeployedAt    string `json:"deployedAt"`
	LastStartedAt string `json:"lastStartedAt"`
}

type serverVersionChanges struct {
	// The commit they are counted from: the build before's, or the one asked.
	Since string `json:"since,omitempty"`
	// Known: the change list reaches since, so count is all of them.
	Known bool `json:"known"`
	// How many there are, merges left out, and the newest of them.
	Count  int                 `json:"count"`
	Listed []serverBuildChange `json:"listed"`
	// Why none are listed, or that only the newest are.
	Note string `json:"note,omitempty"`
}

func readServerVersion(app core.App, since string) serverVersionAnswer {
	answer := serverVersionAnswer{serverBuild: currentServerBuild(app), Changes: serverVersionChanges{Listed: []serverBuildChange{}}}
	if previous, ok := previousServerBuild(app, answer.Build); ok {
		answer.Previous = &serverBuildSeenAnswer{Build: previous.Build, Release: previous.Release, Commit: previous.Commit,
			SchemaVersion: previous.SchemaVersion, DeployedAt: time.UnixMilli(previous.FirstStartedAt).UTC().Format(time.RFC3339),
			LastStartedAt: time.UnixMilli(previous.LastStartedAt).UTC().Format(time.RFC3339)}
		if since == "" {
			since = previous.Commit
		}
	}
	since = strings.TrimSpace(since)
	answer.Changes.Since = since
	switch {
	case answer.Commit == "":
		answer.Changes.Note = "This build doesn't carry its commit: it was built without scripts/stamp-build.mjs, from a checkout rather than a git archive."
	case since == "" && answer.Previous == nil:
		answer.Changes.Note = "This server has run no other build since it began keeping them; name a commit in since to list the changes after it."
	case since == "":
		answer.Changes.Note = "The build this server ran before doesn't carry its commit, so what changed since can't be told; name a commit in since."
	default:
		changes, total, known := serverBuildChangesSince(since)
		switch {
		case !known:
			answer.Changes.Note = "This build's change list doesn't reach " + since + ": it lists the commits it was stamped with (scripts/stamp-build.mjs), and none if it wasn't."
		case total > len(changes):
			answer.Changes.Note = "The newest of them are listed."
		}
		answer.Changes.Known, answer.Changes.Count, answer.Changes.Listed = known, total, changes
	}
	return answer
}

func registerMCPServerVersionTool(server *mcp.Server, app core.App, readOnly *mcp.ToolAnnotations) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_server_version", Title: "See the server's build and what changed",
		Description: "The server's release, build and commit, when the commit was made, the build built and first started here, its data's schema version, " +
			"the build it ran before, and the commits (subjects) since that one — or since a commit you name — to tell whether a change has reached it.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input leafenMCPServerVersionInput) (*mcp.CallToolResult, serverVersionAnswer, error) {
		authority, err := mcpAuthorityFromContext(ctx)
		if err != nil {
			return nil, serverVersionAnswer{}, err
		}
		return nil, readServerVersion(app, input.Since).onClock(personLocation(app, authority.owner())), nil
	})
}

// onClock puts the answer's times on the person's clock, for the agent to tell
// them (person_clock.go); Settings › Server › Updates shows them in its own.
func (answer serverVersionAnswer) onClock(location *time.Location) serverVersionAnswer {
	for _, at := range []*string{&answer.CommitTime, &answer.BuiltAt, &answer.DeployedAt, &answer.StartedAt} {
		*at = personClockRestamp(*at, location)
	}
	if answer.Previous != nil {
		previous := *answer.Previous
		previous.DeployedAt, previous.LastStartedAt = personClockRestamp(previous.DeployedAt, location), personClockRestamp(previous.LastStartedAt, location)
		answer.Previous = &previous
	}
	listed := make([]serverBuildChange, len(answer.Changes.Listed))
	for index, change := range answer.Changes.Listed {
		change.At = personClockRestamp(change.At, location)
		listed[index] = change
	}
	answer.Changes.Listed = listed
	return answer
}

// GET /admin/server-version is Settings → Server → Updates' (an agent reads
// the tool, which any connection may).
func registerServerVersionRoute(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/admin/server-version", readServerReleaseBuild)
}

// readServerReleaseBuild is named so leafen_operations finds it by "server",
// "version", "release" or "build".
func readServerReleaseBuild(request *core.RequestEvent) error {
	if !authorizeAdministratorRequest(request) {
		return nil
	}
	return request.JSON(http.StatusOK, readServerVersion(request.App, request.Request.URL.Query().Get("since")))
}
