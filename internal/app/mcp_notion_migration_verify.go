package app

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// leafen_notion_migration_verify checks a migration's run against what it
// read from Notion again (notion_migration_verify.go): the run does it at its
// end by itself, and an agent calls it after fixing what that check named,
// which writes the report page again with what is left. Its answer names
// one by one only what its next asks to fix, and says the rest by group, as
// the report page does (mcp_notion_migration_status_lists.go).

type leafenMCPNotionMigrationVerifyOutput struct {
	Verify notionMigrationVerify `json:"verify"`
	// What still differs, by what differs and why, as the report page says it.
	CheckGroups []leafenMCPNotionReportGroup `json:"check_groups,omitempty"`
	Next        string                       `json:"next"`
}

func registerMCPNotionMigrationVerifyTool(server *mcp.Server, app core.App) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_notion_migration_verify", Title: "Check a Notion migration against Notion",
		Description: "Compares what the migration's run made with what it read from Notion: each database's rows and templates, the cells Notion holds empty, two-way relations held one way, the columns a split's targets have, formula and rollup values, and each view — a database's own and every linked one, in pages and row pages — " +
			"with its shown columns and their order, widths, sorts, filter conditions and groups. Says what differs by group with why (check_groups; verify.remaining names only those to fix, all of them with leafen_notion_migration_status list differences), and what it can't compare because Notion doesn't give it. " +
			"The run does it by itself at its end; call it again after fixing what it named: it keeps the new result and writes the report page again.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input leafenMCPNotionMigrationScanInput) (*mcp.CallToolResult, leafenMCPNotionMigrationVerifyOutput, error) {
		authority, workspace, err := mcpNotionMigrationAuthority(ctx, app, input.WorkspaceID, input.MigrationID)
		if err != nil {
			return nil, leafenMCPNotionMigrationVerifyOutput{}, err
		}
		stored, err := loadNotionMigration(app, authority.owner(), workspace.WorkspaceID, input.MigrationID)
		if err != nil {
			return nil, leafenMCPNotionMigrationVerifyOutput{}, mcpInvalid(mcpNotionMigrationIDAsked)
		}
		switch {
		case stored.ImportJobID == "" || stored.RunState == "":
			return nil, leafenMCPNotionMigrationVerifyOutput{}, errors.New("the migration hasn't run yet: there is nothing to check")
		case stored.RunState == notionMigrationRunRunning:
			return nil, leafenMCPNotionMigrationVerifyOutput{}, errors.New("the run is still going and checks itself at its end: follow it with leafen_notion_migration_status")
		}
		verify, err := verifyNotionMigrationRun(app, stored.ImportJobID, stored.ID)
		if err != nil {
			return nil, leafenMCPNotionMigrationVerifyOutput{}, failedBecause(errMCPToolFailed, err)
		}
		// The report page says what is left now.
		if runner, ok := app.Store().Get(durableJobRunnerStoreKey).(*durableJobRunner); ok {
			if job, err := loadStoredJob(app, stored.ImportJobID, authority.owner()); err == nil {
				payload := notionImportJobPayload{}
				if json.Unmarshal([]byte(job.PayloadJSON), &payload) == nil && payload.MigrationID == stored.ID {
					if err := runner.writeNotionMigrationReport(ctx, app, job, payload); err != nil {
						return nil, leafenMCPNotionMigrationVerifyOutput{}, failedBecause(errMCPToolFailed, err)
					}
				}
			}
		}
		language, _ := serverTextLanguage(app, authority.owner(), nil)
		groups := notionMigrationVerifyGroups(&verify, language, notionImportNotWholeText(language, stored.SourceLabel))
		return nil, leafenMCPNotionMigrationVerifyOutput{Verify: notionMigrationVerifyAnswered(verify),
			CheckGroups: notionMigrationReportGroupsSaid(groups), Next: notionMigrationVerifyNext(verify)}, nil
	})
}

// notionMigrationVerifyNext is what follows a check: fixing what it names
// that a tool can set, or the one report.
func notionMigrationVerifyNext(verify notionMigrationVerify) string {
	fixable := 0
	for _, difference := range verify.Remaining {
		if notionMigrationVerifyFixable(difference) {
			fixable++
		}
	}
	if fixable > 0 {
		return "Some of what differs has no reason Leafen knows (reason unexplained): each one's fix sets it as Notion has it (the view, or a relation's row, is id; notion-fetch reads it), " +
			"and leafen_notion_migration_verify then checks again. The others stay for their reason, which the report says."
	}
	return notionMigrationVerifiedNext
}

const notionMigrationVerifiedNext = "Checked: what still differs stays for its reason, which the report page says. Left: the one report to the person."
