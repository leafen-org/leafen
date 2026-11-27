package app

import (
	"context"
	"errors"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// leafen_standby lets an agent of the server's administrator set up a
// standby server with the person (the agent decision of 2026-10-19 09:33):
// how backups, the replica and a pair stand on this server, what is left to
// do, and the way through — candidates, the place, the connection code the
// person pastes themselves, a rehearsed takeover and back, and a page on
// what happens in an accident. It reads only and never carries a key, a
// password, an access key or a code: the person enters those in the screens.

type leafenMCPStandbyInput struct{}

type leafenMCPStandbyPlace struct {
	Kind    string `json:"kind"`
	Where   string `json:"where,omitempty"`
	Sealed  bool   `json:"sealed"`
	Slow    bool   `json:"slow,omitzero"`
	Enabled bool   `json:"enabled"`
	// The last scheduled backup.
	LastSuccessAt string `json:"lastSuccessAt,omitempty"`
	LastErrorCode string `json:"lastErrorCode,omitempty"`
}

type leafenMCPStandbyOutput struct {
	Role          string                        `json:"role"`
	Release       string                        `json:"release"`
	SchemaVersion int                           `json:"schemaVersion"`
	Holds         administratorServerMoveCounts `json:"holds"`
	DiskFreeMB    uint64                        `json:"diskFreeMb,omitzero"`
	Backups       leafenMCPStandbyPlace         `json:"backups"`
	Replication   administratorReplication      `json:"replication"`
	Standby       administratorStandby          `json:"standby"`
	ToDo          []string                      `json:"toDo"`
	Instructions  string                        `json:"instructions"`
}

// leafenMCPStandbyInstructions guide a standby pair, the screens and buttons
// named in the person's words (mcp_screen_names.go). Only a connection the
// server's administration takes reads them (mcpServerAdministrationTools),
// and it runs their leafen_call steps itself.
func leafenMCPStandbyInstructions(language string) string {
	screen := func(names ...string) string { return mcpScreen(language, names...) }
	return "A standby is a second Leafen server of the person's own that follows this one through its replica and takes over by itself " +
		"if this one stops answering. Choose the candidate with the person: one of their computers that stays on " +
		"or a NAS or hosting, with this version or newer. Agree on the place both servers reach at once — S3, or a drive both mount; a cloud sync folder is too late. " +
		"Set it all up yourself with leafen_call: the place (PUT /admin/backups/schedule), the replica (PUT /admin/backups/replication), " +
		"the connection code here (POST /admin/backups/standby/code) and, on the standby, joining (PUT /admin/backups/standby/join). " +
		"Where a key, password, recovery code or the connection code goes, send \"secretsFromPerson\": true instead: Leafen shows the person " +
		"just that — in their inbox and in " + screen("settings", "server", "serverBackups") + " or " + screen("serverStandby") +
		", the connection code to them alone, which they paste into the join's field on the standby — and GET /person-steps/{id} tells you " +
		"when it went through. Never ask for them in the chat and never write them down. Call leafen_standby on either server to follow. " +
		"If the person wants a rehearsal, do it with them: on the standby, POST /admin/backups/standby/takeover (" + screen("standbyTakeOver") +
		"; this server stops taking changes for a few seconds and nothing is lost), check people and pages and that the address answers there, " +
		"then switch back — here, POST /admin/backups/replication/apart (" + screen("standbyKeepApart") + "); on the new primary, a new " +
		"connection code; here, joining with it, then taking over; and make the other server the standby again the same way. " +
		"Last, write a Leafen page for the person on what happens " +
		"in an accident: who takes over, after how long, what may be lost, how to switch back — with no key, password or code in it."
}

func registerMCPStandbyTool(server *mcp.Server, app core.App) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_standby", Title: "Set up a standby server",
		Description: "For the server's administrator, on an agent connection as all the server's administration is (not an MCP token): how " +
			"backups, the continuous replica and a standby pair stand on this server, what is left " +
			"to do, and how to set up a standby server with the person, rehearse a takeover and switch back.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ leafenMCPStandbyInput) (*mcp.CallToolResult, leafenMCPStandbyOutput, error) {
		authority, err := mcpAuthorityFromContext(ctx)
		if err != nil {
			return nil, leafenMCPStandbyOutput{}, err
		}
		administrator, err := isAdministrator(app, authority.owner())
		if err != nil || !administrator || authority.Scope < userKeyScopeAdmin {
			return nil, leafenMCPStandbyOutput{}, errors.New("a standby server is for the server's administrator, on a connection whose tools and organizations leave it everything")
		}
		stored, err := loadScheduledBackup(app)
		if err != nil {
			return nil, leafenMCPStandbyOutput{}, err
		}
		projected := projectScheduledBackupWithRestore(app, stored)
		output := leafenMCPStandbyOutput{Role: projected.Standby.Role, Release: ReleaseVersion(), Holds: countServerContents(app),
			Replication: projected.Replication, Standby: projected.Standby, Instructions: leafenMCPStandbyInstructions(mcpScreenLanguage(app, authority.owner())), ToDo: []string{}}
		_ = app.DB().NewQuery(`SELECT COALESCE(MAX([[version]]), 0) FROM {{leafen_schema_migrations}}`).Row(&output.SchemaVersion)
		if free, _, ok := dataDiskSpace(app.DataDir()); ok {
			output.DiskFreeMB = free >> 20
		}
		protection := projectBackupProtection(stored)
		place := leafenMCPStandbyPlace{Kind: stored.Target, Enabled: stored.Enabled, Sealed: protection.Code || protection.Password,
			LastSuccessAt: personClockMillisOf(projected.LastSuccessAt, authority.clock()), LastErrorCode: projected.LastErrorCode}
		// Its times on the person's clock (person_clock.go).
		output.Standby.FollowedAt, output.Standby.TookOverAt = personClockMillisOf(output.Standby.FollowedAt, authority.clock()), personClockMillisOf(output.Standby.TookOverAt, authority.clock())
		output.Replication.LastSyncAt = personClockMillisOf(output.Replication.LastSyncAt, authority.clock())
		switch stored.Target {
		case scheduledBackupTargetFolder:
			place.Where, place.Slow = stored.Folder, syncFolderReplicaPlace(stored.Folder)
		case scheduledBackupTargetWebDAV:
			if parsed, err := url.Parse(stored.WebDAVURL); err == nil {
				place.Where = parsed.Host
			}
		case scheduledBackupTargetS3:
			place.Where = stored.S3Bucket
			if parsed, err := url.Parse(stored.S3Endpoint); err == nil && parsed.Host != "" {
				place.Where += " at " + parsed.Host
			}
		}
		output.Backups = place
		output.ToDo = standbyToDo(stored, place, projected)
		return nil, output, nil
	})
}

// standbyToDo is what is left before this server has a standby, in order.
func standbyToDo(stored storedScheduledBackup, place leafenMCPStandbyPlace, projected administratorScheduledBackup) []string {
	todo := []string{}
	switch {
	case projected.Standby.Role == standbyRoleStandby:
		return todo
	case !stored.targetSet() || !stored.Enabled:
		todo = append(todo, "Turn on scheduled backups to a place both servers reach at once: S3, or a drive both mount.")
	case place.Slow:
		todo = append(todo, "The backup place is a cloud sync folder, which shows changes too late: choose S3 or a drive both servers mount.")
	}
	if projected.Protection.Required && !place.Sealed {
		todo = append(todo, "Encrypt the backups with a recovery code or a password first: a location off this computer takes no unencrypted backup.")
	}
	if !projected.Replication.Enabled {
		todo = append(todo, "Turn on continuous replication.")
	}
	if projected.Standby.Role == standbyRoleNone {
		todo = append(todo, "Add a standby server: the code shows once on this screen, and the person pastes it on the standby under Become a standby server.")
	}
	return todo
}
