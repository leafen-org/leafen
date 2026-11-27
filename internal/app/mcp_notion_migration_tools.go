package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An agent's tools for a design-first Notion migration (B5): find the
// person's Notion connections in the workspace and what each reaches, start
// a migration of chosen roots, and scan it until its structure summary is
// done (notion_migrations.go). The Notion token is kept in Leafen, sealed —
// an agent with a browser gets it in Notion, else the person pastes it into
// the conversation, and the agent hands it over (mcp_notion_migration_connect
// .go, user decisions 2026-11-13, -11-19, -11-20; notion_import_handoff.go)
// — and bodies are counted, not read out.

const (
	mcpNotionConnectionsShown = 50
	// The longest sources waits for a connection the person is making: under
	// the minute an agent's call is let run (Codex's default).
	mcpNotionConnectionWaitMost  = 45 * time.Second
	mcpNotionConnectionWaitEvery = 2 * time.Second
)

type leafenMCPNotionMigrationSourcesInput struct {
	WorkspaceID  string `json:"workspace_id,omitempty" jsonschema:"Workspace to migrate into, when the connection reaches more than one"`
	ConnectionID string `json:"connection_id,omitempty" jsonschema:"A Notion connection from the list: its pages and databases to choose roots from"`
	Cursor       string `json:"start_cursor,omitempty" jsonschema:"next_cursor from the last call, for the next connections or roots"`
	Query        string `json:"query,omitempty" jsonschema:"Words in the title of a Notion page or database to find at any depth; without them the roots are what the connection reaches at the top"`
	WaitSeconds  int    `json:"wait_seconds,omitzero" jsonschema:"Without connection_id while there is no connection yet, only when the person puts the token in Leafen's import screen instead of pasting it here: wait up to this many seconds (at most 45), answering as soon as one is there; in the Leafen app, its window comes to the front on that screen"`
}

type leafenMCPNotionConnection struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Notion no longer takes its token (notionConnectionTokensRefused): a new
	// one is made as at first (notion_import_handoff.go).
	NeedsReconnect bool `json:"needs_reconnect,omitzero" db:"-"`
}

type leafenMCPNotionMigrationSourcesOutput struct {
	Connections []leafenMCPNotionConnection `json:"connections,omitempty"`
	Roots       []notionImportRoot          `json:"roots,omitempty"`
	NextCursor  string                      `json:"next_cursor,omitempty"`
	// With no connection yet (notion_import_handoff.go): Notion's page, the
	// person's steps there one at a time in their language, and the import
	// screen the token goes in.
	notionImportHandoffFields
	// How a move goes, for the person first (notion_migration_first_words.go).
	FirstWords string `json:"first_words,omitempty"`
	Next       string `json:"next"`
}

type leafenMCPNotionMigrationStartInput struct {
	WorkspaceID  string                         `json:"workspace_id,omitempty" jsonschema:"Workspace to migrate into, when the connection reaches more than one"`
	ConnectionID string                         `json:"connection_id" jsonschema:"The Notion connection to read from"`
	Roots        []leafenMCPNotionMigrationRoot `json:"roots" jsonschema:"The Notion pages and databases to move, each with id and kind (page or database); the title comes from Notion"`
	SecondCopy   bool                           `json:"second_copy,omitzero" jsonschema:"Only when the person chose a second, separate copy of what a move of theirs already brought here; otherwise that move is run again"`
}

type leafenMCPNotionMigrationScanInput struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"Workspace of the migration; left out, the one holding migration_id of those this connection reaches"`
	MigrationID string `json:"migration_id" jsonschema:"The migration leafen_notion_migration_start made"`
}

type leafenMCPNotionMigrationOutput struct {
	Migration notionMigration `json:"migration"`
	// Where the person watches it live (notion_migration_wait_next.go), and
	// the scan's line for them with its address (notion_migration_stage_lines.go).
	Link      string `json:"link,omitempty"`
	StageLine string `json:"stage_line,omitempty"`
	Next      string `json:"next"`
}

type leafenMCPNotionMigrationItemsInput struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"Workspace of the migration; left out, the one holding migration_id of those this connection reaches"`
	MigrationID string `json:"migration_id" jsonschema:"The migration whose scan to read"`
	Kind        string `json:"kind" jsonschema:"pages, databases, issues or people"`
	Cursor      string `json:"start_cursor,omitempty" jsonschema:"next_cursor from the last call of the same kind, for the next ones"`
	PageSize    int    `json:"page_size,omitzero" jsonschema:"How many to read at most, up to the kind's window (200, databases 50); left out, the whole window"`
}

type leafenMCPNotionMigrationItemsOutput struct {
	Pages      []notionMigrationPage     `json:"pages,omitempty"`
	Databases  []notionMigrationDatabase `json:"databases,omitempty"`
	Issues     []notionMigrationIssue    `json:"issues,omitempty"`
	People     []notionMigrationPerson   `json:"people,omitempty"`
	NextCursor string                    `json:"next_cursor,omitempty"`
	Next       string                    `json:"next"`
}

func registerMCPNotionMigrationTools(server *mcp.Server, app core.App, readOnly *mcp.ToolAnnotations) {
	registerMCPNotionMigrationConnectTool(server, app)
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_notion_migration_sources", Title: "List Notion connections and roots to migrate",
		Description: "Without connection_id, lists the person's Notion connections in the workspace (each a Notion internal integration token passed on with leafen_notion_migration_connect, or put in Leafen's import screen); " +
			"with none yet it gives Notion's page and its steps — to do yourself in your browser (theirs or your own), after their yes, else with them one at a time, the token pasted here — and with wait_seconds waits for one they put in the import screen instead. " +
			"With connection_id, lists the pages and databases that connection reaches at the top (with query, those whose title matches at any depth), to choose the roots of a migration from.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input leafenMCPNotionMigrationSourcesInput) (*mcp.CallToolResult, leafenMCPNotionMigrationSourcesOutput, error) {
		return listMCPNotionMigrationSources(ctx, app, input)
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_notion_migration_start", Title: "Start a Notion migration",
		Description: "Starts a design-first migration of the chosen Notion roots from one of the person's connections. Then call leafen_notion_migration_scan until its status is scanned. Roots a move of the person's already brought into the workspace are refused, naming that move: run it again for what changed in Notion since.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input leafenMCPNotionMigrationStartInput) (*mcp.CallToolResult, leafenMCPNotionMigrationOutput, error) {
		return notionMigrationStartNoted{app: app, ctx: ctx}.with(startMCPNotionMigration(ctx, app, input))
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_notion_migration_scan", Title: "Scan a Notion migration's structure",
		Description: "Reads the migration's Notion structure to the end however large: the page tree with block counts, each database's properties with the Leafen type each becomes, row counts (capped), three sample rows and whether Notion gives its views, and what cannot move. " +
			"Leafen reads on by itself once asked; each call answers within about 25 seconds, at once when the scan ends, with how far it is. Call again while status is scanning, then read what it found with leafen_notion_migration_items. Bodies are counted, not returned.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input leafenMCPNotionMigrationScanInput) (*mcp.CallToolResult, leafenMCPNotionMigrationOutput, error) {
		return scanMCPNotionMigration(ctx, app, input)
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_notion_migration_items", Title: "Read a Notion migration's scan",
		Description: "Reads what the scan found, one kind at a time and a window at a time: pages (title, parent, block count), databases (properties with their Leafen types and the actions and types a plan may give each, row counts, sample rows, views, for each two relations the rows that fill both and how many of them differ; a database of several data sources has its first's properties and each other's under dataSources), " +
			"issues (what cannot move, each with the page or database it is about) or people (the Notion people rows name). An untitled page or database reads New page or New database, as Notion shows it. With next_cursor, more follow: give it as start_cursor.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input leafenMCPNotionMigrationItemsInput) (*mcp.CallToolResult, leafenMCPNotionMigrationItemsOutput, error) {
		authority, workspace, err := mcpNotionMigrationAuthority(ctx, app, input.WorkspaceID, input.MigrationID)
		if err != nil {
			return nil, leafenMCPNotionMigrationItemsOutput{}, err
		}
		stored, err := loadNotionMigration(app, authority.owner(), workspace.WorkspaceID, input.MigrationID)
		if err != nil {
			return nil, leafenMCPNotionMigrationItemsOutput{}, mcpInvalid(mcpNotionMigrationIDAsked)
		}
		items, err := readNotionMigrationScanItems(app, stored, strings.TrimSpace(input.Kind), input.Cursor, input.PageSize)
		if errors.Is(err, errNotionMigrationInvalid) {
			return nil, leafenMCPNotionMigrationItemsOutput{}, errors.New(firstNonempty(mcpNotionMigrationRefusal(err),
				"give kind pages, databases, issues or people, and as start_cursor the next_cursor of the last call of that kind or none"))
		}
		if err != nil {
			return nil, leafenMCPNotionMigrationItemsOutput{}, failedBecause(errMCPToolFailed, err)
		}
		output := leafenMCPNotionMigrationItemsOutput{Pages: items.Pages, Databases: items.Databases, Issues: items.Issues,
			People: items.People, NextCursor: items.NextCursor, Next: "That is all of them."}
		output.showTitles()
		if items.NextCursor != "" {
			output.Next = "More follow: call again with the same kind and start_cursor set to next_cursor."
		}
		return nil, output, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_notion_migration_plan", Title: "Read a Notion migration's plan",
		Description: "Reads the migration's plan with its revision and its check: the problems that stop it from running, each with a reason code and source, the agenda of all the move won't bring as it is, item by item, to say to the person, and the counts it would import. " +
			"Once there is one, the plan itself (with changes_only, just what it changes from the as-is draft); the as-is draft, everything as it is in Notion, only with include_draft.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input leafenMCPNotionMigrationPlanReadInput) (*mcp.CallToolResult, leafenMCPNotionMigrationPlanOutput, error) {
		authority, workspace, err := mcpNotionMigrationAuthority(ctx, app, input.WorkspaceID, input.MigrationID)
		if err != nil {
			return nil, leafenMCPNotionMigrationPlanOutput{}, err
		}
		state, err := readNotionMigrationPlan(app, authority.owner(), workspace.WorkspaceID, input.MigrationID)
		if err != nil {
			return nil, leafenMCPNotionMigrationPlanOutput{}, mcpInvalid(mcpNotionMigrationIDAsked)
		}
		answer := mcpNotionMigrationPlanAnswer(app, authority.owner(), workspace.WorkspaceID, state, input.IncludeDraft, input.ChangesOnly, true)
		answer = withNotionMigrationAskedAgain(app, authority.owner(), workspace.WorkspaceID, answer)
		return nil, withNotionMigrationConfirmLink(app, authority, workspace.WorkspaceID, answer), nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_notion_migration_set_plan", Title: "Save a Notion migration's plan",
		Description: "Saves the migration plan agreed with the person, over the revision last read (0 for the first), and returns its check (the plan itself with include_plan). " +
			"Write only what changes: what the plan doesn't name comes as it is in Notion, even when an earlier save changed it. " +
			"The plan (format 1): scanned databases with action import, skip or merge (into another imported one), " +
			"its title, the page to go under and splits sending rows elsewhere; " +
			"their properties with action map (name, Leafen type, option values from→to, into gathering a relation into another), preserve (kept as text), report (listed in the migration report) " +
			"or object (its names linked to rows of a database), exactly one becoming the title, and each other data source's properties likewise under dataSources; " +
			"pages to skip, rename or move; destinationPageId (empty for the top of the workspace); people from notionUserId (as the scan's people list it) to a workspace memberId (the member's id as notion-get-users lists it, or their Leafen ID); " +
			"comments import or skip; files copy or skip; and optionally expect, the page, database and row counts agreed. The migration guide has every rule. Nothing runs while the check has problems.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input leafenMCPNotionMigrationPlanInput) (*mcp.CallToolResult, leafenMCPNotionMigrationPlanOutput, error) {
		authority, workspace, err := mcpNotionMigrationAuthority(ctx, app, input.WorkspaceID, input.MigrationID)
		if err != nil {
			return nil, leafenMCPNotionMigrationPlanOutput{}, err
		}
		plan := notionMigrationPlan{}
		encoded, err := json.Marshal(input.Plan)
		if err != nil || json.Unmarshal(encoded, &plan) != nil {
			return nil, leafenMCPNotionMigrationPlanOutput{}, mcpInvalid("plan didn't read: send it in the form leafen_notion_migration_plan returns")
		}
		plan = notionMigrationPlanWithAsIsRest(app, authority.owner(), workspace.WorkspaceID, input.MigrationID, plan)
		state, err := saveNotionMigrationPlan(context.Background(), app, authority.owner(), workspace.WorkspaceID, input.MigrationID,
			notionMigrationPlanInput{BaseRevision: input.BaseRevision, Plan: plan})
		switch {
		case errors.Is(err, errNotionMigrationPlanConflict):
			return nil, leafenMCPNotionMigrationPlanOutput{}, errors.New("the plan changed since base_revision: read it again")
		case errors.Is(err, errNotionMigrationNotScanned):
			return nil, leafenMCPNotionMigrationPlanOutput{}, errors.New("the scan is not done: call leafen_notion_migration_scan")
		case errors.Is(err, errNotionMigrationRunning):
			return nil, leafenMCPNotionMigrationPlanOutput{}, errors.New("the migration is running its plan: the plan can change after the run ends")
		case mcpNotionMigrationRefusal(err) != "":
			return nil, leafenMCPNotionMigrationPlanOutput{}, mcpInvalid("the plan wasn't saved: %s", mcpNotionMigrationRefusal(err))
		case err != nil:
			return nil, leafenMCPNotionMigrationPlanOutput{}, mcpInvalid("the plan wasn't saved: check migration_id and the plan's form, as leafen_notion_migration_plan returns it")
		}
		answer := mcpNotionMigrationPlanAnswer(app, authority.owner(), workspace.WorkspaceID, state, false, false, input.IncludePlan)
		return nil, withNotionMigrationConfirmLink(app, authority, workspace.WorkspaceID, answer), nil
	})
}

type leafenMCPNotionMigrationRunInput struct {
	WorkspaceID  string `json:"workspace_id,omitempty" jsonschema:"Workspace of the migration; left out, the one holding migration_id of those this connection reaches"`
	MigrationID  string `json:"migration_id" jsonschema:"The migration to run"`
	PlanRevision int    `json:"plan_revision" jsonschema:"The plan revision the person agreed to, from leafen_notion_migration_plan"`
	Resume       bool   `json:"resume,omitzero" jsonschema:"After a run stopped: go on from the step it stopped at instead of reading all of Notion again"`
	SecondCopy   bool   `json:"second_copy,omitzero" jsonschema:"Only when the person chose a second, separate copy of what another move of theirs brought here"`
}

func registerMCPNotionMigrationRunTools(server *mcp.Server, app core.App, readOnly *mcp.ToolAnnotations) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_notion_migration_run", Title: "Run a Notion migration's plan",
		Description: "Carries out the plan revision the person agreed to with Leafen's import engine. The plan must pass its check. " +
			"Bodies go from Notion to Leafen directly; follow the run with leafen_notion_migration_status. " +
			"Run it again once it is done to bring what is new in Notion since, and the rows and blocks Notion changed; that reads all of Notion again. " +
			"After a run stopped, resume true goes on from the step it stopped at, reading nothing again.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input leafenMCPNotionMigrationRunInput) (*mcp.CallToolResult, leafenMCPNotionMigrationStatusOutput, error) {
		authority, workspace, err := mcpNotionMigrationAuthority(ctx, app, input.WorkspaceID, input.MigrationID)
		if err != nil {
			return nil, leafenMCPNotionMigrationStatusOutput{}, err
		}
		var migration notionMigration
		if input.Resume {
			migration, err = resumeNotionMigrationRun(ctx, app, authority.owner(), workspace.WorkspaceID, input.MigrationID)
		} else {
			migration, err = runNotionMigration(ctx, app, authority.owner(), workspace.WorkspaceID, input.MigrationID,
				notionMigrationRunInput{RequestID: mcpCallSeed(), PlanRevision: input.PlanRevision, SecondCopy: input.SecondCopy})
		}
		switch {
		case errors.Is(err, errNotionMigrationNothingToResume):
			return nil, leafenMCPNotionMigrationStatusOutput{}, errors.New("no stopped run to go on from: read leafen_notion_migration_status, or run without resume")
		case errors.Is(err, errNotionMigrationPlanConflict):
			return nil, leafenMCPNotionMigrationStatusOutput{}, errors.New("plan_revision is not the plan's current revision: read the plan and agree on it again")
		case errors.Is(err, errNotionMigrationPlanBlocked):
			return nil, leafenMCPNotionMigrationStatusOutput{}, errors.New("the plan has problems: read leafen_notion_migration_plan and fix them")
		case errors.Is(err, errNotionMigrationRunning):
			return nil, leafenMCPNotionMigrationStatusOutput{}, errors.New("the migration is already running: follow it with leafen_notion_migration_status")
		case errors.Is(err, errNotionMigrationNotScanned):
			return nil, leafenMCPNotionMigrationStatusOutput{}, errors.New("the scan is not done: call leafen_notion_migration_scan")
		case err != nil:
			return nil, leafenMCPNotionMigrationStatusOutput{}, mcpNotionMigrationRunNotStarted(err)
		}
		return nil, leafenMCPNotionMigrationStatus(app, authority, migration), nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_notion_migration_status", Title: "Follow a Notion migration",
		Description: "Reads where a migration is: its one stage (scanning, planning, reading, creating, comments, verifying, done or stopped), the link the person can open now, the import's progress and real counts, what hasn't come whole so far (not_whole: the counts Leafen's screen shows, each item in words; import_issues: each of them with the first pages it was on, linked, and what was there), once done what still differs from Notion and what stays at Leafen's defaults, and the next step. Lists that grow with the move come by their groups and counts only: list formats_left, differences or reported_values gives one of them item by item, a window at a time. " +
			"Without migration_id it reads the person's latest migration — the one whose run started last — in any workspace this connection reaches, with their others in brief: call it so when they say their move is done in a conversation that has no migration_id. " +
			"While the run goes, wait_seconds (at most 45) answers as soon as the run ends. " +
			"The migration and its import come short, with what to act on and what the person hears, and while the run goes not_whole by its counts alone; detail true gives all of it as stored, for a value the short answer leaves out. " +
			"import.progress.percentDone is the share of the run's time gone, in whole percent; detail's timeShare is the same in thousandths. Times named …At are milliseconds since 1970 (UTC).",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input leafenMCPNotionMigrationStatusInput) (*mcp.CallToolResult, leafenMCPNotionMigrationStatusOutput, error) {
		status, err := waitWhileJobGoes(ctx, input.WaitSeconds, func() (leafenMCPNotionMigrationStatusOutput, error) {
			_, status, err := readMCPNotionMigrationStatus(ctx, app, input)
			return status, err
		}, func(status leafenMCPNotionMigrationStatusOutput) bool {
			return input.List == "" && notionMigrationStageGoes(status.Stage)
		})
		return nil, status, err
	})
}

func listMCPNotionMigrationSources(
	ctx context.Context, app core.App, input leafenMCPNotionMigrationSourcesInput,
) (*mcp.CallToolResult, leafenMCPNotionMigrationSourcesOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, leafenMCPNotionMigrationSourcesOutput{}, err
	}
	workspace, err := mcpNotionMigrationWorkspace(app, authority, input.WorkspaceID)
	if err != nil {
		return nil, leafenMCPNotionMigrationSourcesOutput{}, err
	}
	connectionID := strings.TrimSpace(input.ConnectionID)
	if connectionID == "" {
		// The connections, newest first, 50 an answer from the cursor on.
		keyset, params := "", dbx.Params{"workspaceId": workspace.WorkspaceID, "owner": authority.owner(), "limit": mcpNotionConnectionsShown + 1}
		if cursor := strings.TrimSpace(input.Cursor); cursor != "" {
			at, id, found := strings.Cut(cursor, ":")
			created, err := strconv.ParseInt(at, 10, 64)
			if !found || err != nil || id == "" {
				return nil, leafenMCPNotionMigrationSourcesOutput{}, mcpInvalid("start_cursor is the next_cursor this tool gave, as it gave it")
			}
			keyset, params["at"], params["id"] = `AND ([[created]] < {:at} OR ([[created]] = {:at} AND [[id]] < {:id}))`, created, id
		}
		rows := []struct {
			leafenMCPNotionConnection
			Created int64 `db:"created"`
		}{}
		// Asked to wait, a first page without a connection Notion takes is read
		// again until the person adds one in the import screen, or the wait
		// ends; each connection's token is asked about once.
		wait := min(time.Duration(max(input.WaitSeconds, 0))*time.Second, mcpNotionConnectionWaitMost)
		if keyset != "" {
			wait = 0
		}
		until := time.Now().Add(wait)
		refused, opened := map[string]bool{}, false
		usable := func() bool {
			for _, row := range rows {
				if !refused[row.ID] {
					return true
				}
			}
			return false
		}
		for {
			rows = rows[:0]
			if err := app.DB().NewQuery(`
				SELECT [[id]],[[label]],[[created]] FROM {{leafen_notion_import_connections}}
				WHERE [[workspace_id]]={:workspaceId} AND [[owner]]={:owner} AND [[active]]=1 ` + keyset + `
				ORDER BY [[created]] DESC, [[id]] DESC LIMIT {:limit}
			`).Bind(params).All(&rows); err != nil {
				return nil, leafenMCPNotionMigrationSourcesOutput{}, failedBecause(errMCPToolFailed, err)
			}
			ids := make([]string, 0, len(rows))
			for _, row := range rows {
				ids = append(ids, row.ID)
			}
			notionConnectionTokensRefused(ctx, app, authority.owner(), workspace.WorkspaceID, ids, refused)
			if usable() || !time.Now().Add(mcpNotionConnectionWaitEvery).Before(until) {
				break
			}
			// Waiting for the token: the person's window opens the import screen it goes in.
			opened = opened || notionImportWindowOpened(app, authority.owner())
			select {
			case <-ctx.Done():
				return nil, leafenMCPNotionMigrationSourcesOutput{}, ctx.Err()
			case <-time.After(mcpNotionConnectionWaitEvery):
			}
		}
		output := leafenMCPNotionMigrationSourcesOutput{Connections: []leafenMCPNotionConnection{},
			Next: notionConnectionsListedNext}
		if len(rows) > mcpNotionConnectionsShown {
			rows = rows[:mcpNotionConnectionsShown]
			last := rows[len(rows)-1]
			output.NextCursor = strconv.FormatInt(last.Created, 10) + ":" + last.ID
			output.Next = "More connections follow: call again with start_cursor set to next_cursor, or call with connection_id to list what one reaches."
		}
		for _, row := range rows {
			row.NeedsReconnect = refused[row.ID]
			output.Connections = append(output.Connections, row.leafenMCPNotionConnection)
		}
		if len(rows) > 0 && !usable() && input.Cursor == "" {
			// Only connections whose token Notion refuses: the person makes a new
			// one as at first, and the agent waits for it the same way.
			output.notionImportHandoffFields, output.Next = notionImportHandoff(app, authority.owner(), authority.Resource, workspace.WorkspaceID, true)
		} else if slices.ContainsFunc(output.Connections, func(connection leafenMCPNotionConnection) bool { return connection.NeedsReconnect }) {
			output.Next += " Notion no longer accepts the token of those marked needs_reconnect; use another, or make a new token for it as leafen_notion_migration_guide section 1 says."
		}
		if len(rows) == 0 && input.Cursor == "" {
			// The agent gets it: itself with a browser, else with the person one
			// step at a time, the token pasted into the conversation.
			output.notionImportHandoffFields, output.Next = notionImportHandoff(app, authority.owner(), authority.Resource, workspace.WorkspaceID, false)
		}
		if input.Cursor == "" && notionMigrationFirstWordsDue(authority.ConnectionID, workspace.WorkspaceID) {
			output.FirstWords, output.Next = notionMigrationFirstWords(app, authority.owner()), notionMigrationFirstWordsNext+output.Next
		}
		return nil, output, nil
	}
	connection, err := loadNotionConnection(app, connectionID, authority.owner(), workspace.WorkspaceID)
	if err != nil {
		return nil, leafenMCPNotionMigrationSourcesOutput{}, mcpInvalid("connection_id names a Notion connection of this workspace, as leafen_notion_migration_sources lists it")
	}
	token, err := decryptNotionConnection(app, connection)
	if err != nil {
		return nil, leafenMCPNotionMigrationSourcesOutput{}, failedBecause(errMCPToolFailed, err)
	}
	requestContext, cancel := context.WithTimeout(ctx, longJobCallMost)
	defer cancel()
	client, started := notionSourceClient(app, connection.ID, token), time.Now()
	scope, err := client.searchRoots(requestContext, strings.TrimSpace(input.Cursor), input.Query)
	if err != nil {
		reason := notionProviderFailureReason(err, client.admission.askedToWaitSince(connection.ID, started))
		return nil, leafenMCPNotionMigrationSourcesOutput{}, fmt.Errorf("%w: %s", errMCPToolFailed, reason)
	}
	next := "These are what the connection reaches at the top, the pages the person gave it in Notion: leafen_notion_migration_start takes all of them, and the one confirmation later names what moves; a query finds a page or database the person named deeper down."
	if strings.TrimSpace(input.Query) != "" || input.Cursor != "" {
		next = "These match the query at any depth. Start leafen_notion_migration_start with the ones the person named."
	}
	if scope.HasMore {
		next = "More matches follow: call again with the same query and start_cursor set to next_cursor, or choose from these."
	}
	// No stage of a move goes yet: the scan's line comes with start's answer,
	// as the scan starts (notionMigrationStageLineIn).
	return nil, leafenMCPNotionMigrationSourcesOutput{Roots: notionMigrationRootsShown(scope.Items), NextCursor: scope.NextCursor,
		Next: next + notionConnectionCommentsNext(requestContext, client, scope.Items, input)}, nil
}

func startMCPNotionMigration(
	ctx context.Context, app core.App, input leafenMCPNotionMigrationStartInput,
) (*mcp.CallToolResult, leafenMCPNotionMigrationOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, leafenMCPNotionMigrationOutput{}, err
	}
	workspace, err := mcpNotionMigrationWorkspace(app, authority, input.WorkspaceID)
	if err != nil {
		return nil, leafenMCPNotionMigrationOutput{}, err
	}
	migration, err := createNotionMigration(app, authority.owner(), workspace.WorkspaceID,
		notionMigrationInput{ConnectionID: input.ConnectionID, Roots: notionMigrationRootsNamed(input.Roots), SecondCopy: input.SecondCopy}, true)
	if reason := mcpNotionMigrationRefusal(err); reason != "" {
		return nil, leafenMCPNotionMigrationOutput{}, mcpInvalid("%s", reason)
	}
	if err != nil {
		return nil, leafenMCPNotionMigrationOutput{}, mcpNotionMigrationStartNotMade(err)
	}
	link := notionMigrationLiveLink(app, authority, workspace.WorkspaceID)
	language, _ := serverTextLanguage(app, authority.owner(), nil)
	next := notionMigrationWithStageLine(notionMigrationStartedNext, language, notionMigrationStatusStage(migration, nil))
	return nil, leafenMCPNotionMigrationOutput{Migration: migration, Link: link, Next: notionMigrationNamesLink(next, link),
		StageLine: notionMigrationStageLineIn(language, migration, nil, link, 0)}, nil
}

func scanMCPNotionMigration(
	ctx context.Context, app core.App, input leafenMCPNotionMigrationScanInput,
) (*mcp.CallToolResult, leafenMCPNotionMigrationOutput, error) {
	authority, workspace, err := mcpNotionMigrationAuthority(ctx, app, input.WorkspaceID, input.MigrationID)
	if err != nil {
		return nil, leafenMCPNotionMigrationOutput{}, err
	}
	// It reads on on the server and answers within longJobCallMost
	// (notion_migration_scan_on.go).
	migration, err := scanNotionMigrationOn(ctx, app, authority.owner(), workspace.WorkspaceID, input.MigrationID,
		func(connectionID, token string) notionProviderClient {
			return notionSourceClient(app, connectionID, token)
		})
	if err != nil {
		return nil, leafenMCPNotionMigrationOutput{}, mcpInvalid("the scan didn't go on: check migration_id, and that its Notion connection still works (leafen_notion_migration_sources)")
	}
	output := leafenMCPNotionMigrationOutput{Migration: notionMigrationAnswered(migration), Link: notionMigrationLiveLink(app, authority, workspace.WorkspaceID)}
	switch migration.Status {
	case notionMigrationStatusScanning:
		// The counts the person's import screen shows while it scans.
		output.Next = notionMigrationScanningNext(
			migration.Counts.Pages, migration.Counts.Databases, migration.Counts.Rows)
	case notionMigrationStatusScanned:
		output.Next = "Read what the scan found with leafen_notion_migration_items (pages, databases, issues, people), then save the plan with leafen_notion_migration_set_plan, only what changes (a plan of {\"format\": 1} alone is the as-is draft): " +
			"its answer's confirm is the one question to ask the person (where it goes, people, what moves), with what cannot move (issues) and its estimate.say, how long the move takes, as Leafen reckons it." + notionMigrationFormatsScanNext(migration.Source)
	default:
		output.Next = "The scan stopped: " + notionImportFailureReason(migration.ErrorCode, migration.Source) + " (" + migration.ErrorCode + ")."
	}
	return nil, output, nil
}

// mcpNotionMigrationRefusal is why a migration refused the call, in the
// tools' own field names, or "".
func mcpNotionMigrationRefusal(err error) string {
	if !errors.Is(err, errNotionMigrationInvalid) {
		return ""
	}
	return strings.NewReplacer("connectionId", "connection_id", "cursor is the nextCursor", "start_cursor is the next_cursor").
		Replace(refusalReason(err))
}
