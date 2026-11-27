package app

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// leafen_notion_migration_status reads a migration by its ID or, without
// one, the person's latest: Leafen's import window tells them they may close
// it and their agent, and tell the agent “It’s done” when the inbox says so —
// an agent in a new conversation has no migration_id then (2026-11-19). The
// latest is the one whose run started last, so a move that ran comes before
// a later one only scanned; with none run, the one changed last. The others
// come after it in brief, newest first, for a person who meant another.

type leafenMCPNotionMigrationStatusInput struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"Workspace of the migration; left out, the one holding migration_id, or with no migration_id every workspace this connection reaches"`
	MigrationID string `json:"migration_id,omitempty" jsonschema:"The migration to read; left out, the person's latest: the one whose run started last, else the one changed last"`
	List        string `json:"list,omitempty" jsonschema:"Only to go over one list item by item, a window at a time: formats_left (what stays at Leafen's defaults), differences (what the check still finds different) or reported_values (the reported properties' values); left out, the answer says them by their groups and counts"`
	Cursor      string `json:"start_cursor,omitempty" jsonschema:"next_cursor from the last call with the same list, for its next window"`
	Detail      bool   `json:"detail,omitzero" jsonschema:"true for the migration and its import as stored — the estimate's reckoning, the scan's and the import's counters; left out, both come short, with what to act on and what the person hears"`
	// WaitSeconds waits on the run (mcp_call_wait.go).
	WaitSeconds int `json:"wait_seconds,omitzero" jsonschema:"While the run goes (reading, creating, comments, verifying): wait up to this many seconds (at most 45) for it to end, answering as soon as it has"`
}

// leafenMCPNotionMigrationBrief is another of the person's migrations, as a
// status read without migration_id names it.
type leafenMCPNotionMigrationBrief struct {
	MigrationID string             `json:"migration_id"`
	WorkspaceID string             `json:"workspace_id"`
	Stage       string             `json:"stage"`
	Roots       []notionImportRoot `json:"roots"`
	UpdatedAt   int64              `json:"updated_at"`
}

// leafenMCPNotionMigrationOthers is how many other migrations a status read
// without migration_id names.
const leafenMCPNotionMigrationOthers = 5

func readMCPNotionMigrationStatus(
	ctx context.Context, app core.App, input leafenMCPNotionMigrationStatusInput,
) (*mcp.CallToolResult, leafenMCPNotionMigrationStatusOutput, error) {
	listing, err := notionMigrationStatusListingOf(input.List, input.Cursor)
	if err != nil {
		return nil, leafenMCPNotionMigrationStatusOutput{}, err
	}
	migrationID := strings.TrimSpace(input.MigrationID)
	if migrationID != "" {
		authority, workspace, err := mcpNotionMigrationAuthority(ctx, app, input.WorkspaceID, migrationID)
		if err != nil {
			return nil, leafenMCPNotionMigrationStatusOutput{}, err
		}
		stored, err := loadNotionMigration(app, authority.owner(), workspace.WorkspaceID, migrationID)
		if err != nil {
			return nil, leafenMCPNotionMigrationStatusOutput{}, mcpInvalid(mcpNotionMigrationIDAsked)
		}
		return mcpNotionMigrationStatusOf(app, authority, stored, listing, input.Detail)
	}
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, leafenMCPNotionMigrationStatusOutput{}, err
	}
	workspaceIDs := authority.WorkspaceIDs
	if workspaceID := strings.TrimSpace(input.WorkspaceID); workspaceID != "" {
		workspaceIDs = []string{workspaceID}
	}
	latest, err := latestMCPNotionMigrations(app, authority.owner(), workspaceIDs, leafenMCPNotionMigrationOthers+1)
	if err != nil {
		return nil, leafenMCPNotionMigrationStatusOutput{}, failedBecause(errMCPToolFailed, err)
	}
	if len(latest) == 0 {
		return nil, leafenMCPNotionMigrationStatusOutput{}, mcpInvalid("the person has no Notion migration here yet: start one as leafen_notion_migration_guide says")
	}
	// The latest's workspace, checked as any migration tool checks it.
	workspace, err := mcpNotionMigrationWorkspace(app, authority, latest[0].WorkspaceID)
	if err != nil {
		return nil, leafenMCPNotionMigrationStatusOutput{}, err
	}
	stored, err := loadNotionMigration(app, authority.owner(), workspace.WorkspaceID, latest[0].ID)
	if err != nil {
		return nil, leafenMCPNotionMigrationStatusOutput{}, failedBecause(errMCPToolFailed, err)
	}
	result, output, err := mcpNotionMigrationStatusOf(app, authority, stored, listing, input.Detail)
	if err != nil || len(latest) == 1 {
		return result, output, err
	}
	for _, row := range latest[1:] {
		other, err := loadNotionMigration(app, authority.owner(), row.WorkspaceID, row.ID)
		if err != nil {
			continue
		}
		wire, err := notionMigrationWire(other)
		if err != nil {
			continue
		}
		var job *notionImportJob
		if wire.RunState == notionMigrationRunRunning && wire.ImportJobID != "" {
			if read, err := notionImportResponse(app, wire.ImportJobID, authority.owner(), wire.WorkspaceID); err == nil {
				job = &read
			}
		}
		output.OtherMigrations = append(output.OtherMigrations, leafenMCPNotionMigrationBrief{
			MigrationID: wire.ID, WorkspaceID: wire.WorkspaceID, Stage: notionMigrationStatusStage(wire, job),
			Roots: wire.Roots, UpdatedAt: wire.UpdatedAt,
		})
	}
	if len(output.OtherMigrations) > 0 {
		output.Next = "This is the person's latest migration (read without migration_id); other_migrations lists their others, newest first: " +
			"if they meant one of those, read it with its migration_id. " + output.Next
	}
	return result, output, nil
}

// mcpNotionMigrationStatusOf is a stored migration's status, with the page
// its run made as the person's screen has it and the list the call asked for.
func mcpNotionMigrationStatusOf(
	app core.App, authority mcpAuthority, stored storedNotionMigration, listing mcpAnswerListing, detail bool,
) (*mcp.CallToolResult, leafenMCPNotionMigrationStatusOutput, error) {
	migration, err := notionMigrationWireWithTop(app, stored)
	if err != nil {
		return nil, leafenMCPNotionMigrationStatusOutput{}, failedBecause(errMCPToolFailed, err)
	}
	return nil, leafenMCPNotionMigrationStatusListed(app, authority, migration, listing, detail), nil
}

type latestNotionMigration struct {
	ID          string `db:"id"`
	WorkspaceID string `db:"workspace_id"`
}

// latestMCPNotionMigrations are the person's migrations in the workspaces,
// latest first: run ones by when their run started, then the rest by when
// they changed.
func latestMCPNotionMigrations(app core.App, ownerID string, workspaceIDs []string, limit int) ([]latestNotionMigration, error) {
	rows := []latestNotionMigration{}
	if len(workspaceIDs) == 0 {
		return rows, nil
	}
	reached := make([]any, 0, len(workspaceIDs))
	for _, id := range workspaceIDs {
		reached = append(reached, id)
	}
	err := app.DB().Select("id", "workspace_id").From("leafen_notion_migrations").
		Where(dbx.HashExp{"owner": ownerID}).AndWhere(dbx.In("workspace_id", reached...)).
		OrderBy("run_started_at DESC", "updated_at DESC", "id DESC").Limit(int64(limit)).All(&rows)
	return rows, err
}
