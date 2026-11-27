package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// An agent's plan is only what changes (2026-11-16: a real move listed every
// property of 22 databases, about 250 entries, to keep them as they are, and
// each plan answer carried the as-is draft beside the plan, about 52KB, over
// the client's output limit). leafen_notion_migration_set_plan fills what a
// plan doesn't name from the scan's as-is draft — a database left out comes
// with every property as it is, a property left out of a database that comes
// comes as it is, an empty format, comments or files is the draft's — as
// pages a plan doesn't name already come as they are. The saved plan is the
// whole plan, so the check, the screen and a run read it as before. Each save
// is the whole set of changes: what it doesn't name comes as it is in Notion,
// even when an earlier save changed it. Answers leave the draft out unless
// asked for, set_plan's the plan itself, and a plan read can give only what
// differs from the draft.

type leafenMCPNotionMigrationPlanReadInput struct {
	WorkspaceID  string `json:"workspace_id,omitempty" jsonschema:"Workspace of the migration; left out, the one holding migration_id of those this connection reaches"`
	MigrationID  string `json:"migration_id" jsonschema:"The migration whose plan to read"`
	ChangesOnly  bool   `json:"changes_only,omitzero" jsonschema:"Give only what the plan changes from the as-is draft: the databases and properties it names otherwise than as they are"`
	IncludeDraft bool   `json:"include_draft,omitzero" jsonschema:"Also give the as-is draft: everything as it is in Notion, what a plan of {\"format\": 1} saves"`
}

type leafenMCPNotionMigrationPlanInput struct {
	WorkspaceID  string         `json:"workspace_id,omitempty" jsonschema:"Workspace of the migration; left out, the one holding migration_id of those this connection reaches"`
	MigrationID  string         `json:"migration_id" jsonschema:"The migration to plan"`
	BaseRevision int            `json:"base_revision" jsonschema:"The plan revision last read, 0 when there is none yet"`
	Plan         map[string]any `json:"plan" jsonschema:"The migration plan (format 1) as described: only what changes; what it doesn't name comes as it is"`
	IncludePlan  bool           `json:"include_plan,omitzero" jsonschema:"Also give the whole saved plan back, filled with what comes as it is"`
}

// A plan, its one confirmation and what to do next (notion_migration_confirm.go).
type leafenMCPNotionMigrationPlanOutput struct {
	notionMigrationPlanState
	Confirm *leafenMCPNotionMigrationConfirm `json:"confirm,omitempty"`
	Next    string                           `json:"next,omitempty"`
}

// mcpNotionMigrationPlanOutput says where a refused person comes from: a
// member's id as notion-get-users gives it, or the owner's to map at all.
func mcpNotionMigrationPlanOutput(state notionMigrationPlanState) leafenMCPNotionMigrationPlanOutput {
	output := leafenMCPNotionMigrationPlanOutput{notionMigrationPlanState: state}
	if state.Check == nil {
		return output
	}
	for _, problem := range state.Check.Problems {
		if problem.Code != notionMigrationProblemPersonInvalid {
			continue
		}
		if problem.Detail == "workspace-owner" {
			output.Next = "plan_person_invalid (workspace-owner): mapping people is the workspace owner's; leave people empty, or ask the owner to save it."
		} else {
			output.Next = "plan_person_invalid: a people memberId isn't a member of this workspace. Give each person's memberId as the id notion-get-users lists for that member (or their Leafen ID), then save again."
		}
		break
	}
	return output
}

// mcpNotionMigrationPlanAnswer is a plan state as an agent reads it: its
// confirmation made from the whole plan, then the draft left out once there
// is a plan unless asked for, and the plan whole, as changes only, or left
// out.
func mcpNotionMigrationPlanAnswer(
	app core.App, ownerID, workspaceID string, state notionMigrationPlanState, includeDraft, changesOnly, includePlan bool,
) leafenMCPNotionMigrationPlanOutput {
	output := withNotionMigrationConfirm(app, ownerID, workspaceID, mcpNotionMigrationPlanOutput(state))
	draft := output.Draft
	if !includeDraft {
		output.Draft = nil
	}
	switch {
	case output.Plan == nil:
	case !includePlan:
		output.Plan = nil
	case changesOnly && draft != nil:
		changes := notionMigrationPlanChanges(*output.Plan, *draft)
		output.Plan = &changes
	}
	return output
}

// notionMigrationPlanWithAsIsRest fills what a plan doesn't name from its
// migration's as-is draft; a migration not scanned keeps the plan as given,
// for the save to refuse.
func notionMigrationPlanWithAsIsRest(app core.App, ownerID, workspaceID, id string, plan notionMigrationPlan) notionMigrationPlan {
	stored, err := loadNotionMigration(app, ownerID, workspaceID, id)
	if err != nil || stored.Status != notionMigrationStatusScanned {
		return plan
	}
	summary, err := loadNotionMigrationSummary(app, stored)
	if err != nil {
		return plan
	}
	return fillNotionMigrationPlan(plan, notionMigrationAsIsPlan(summary))
}

func fillNotionMigrationPlan(plan, draft notionMigrationPlan) notionMigrationPlan {
	if plan.Format == 0 {
		plan.Format = draft.Format
	}
	plan.Comments = firstNonempty(strings.TrimSpace(plan.Comments), draft.Comments)
	plan.Files = firstNonempty(strings.TrimSpace(plan.Files), draft.Files)
	if plan.Pages == nil {
		plan.Pages = []notionMigrationPlanPage{}
	}
	if plan.People == nil {
		plan.People = []notionMigrationPlanPerson{}
	}
	named := map[string]int{}
	for index, database := range plan.Databases {
		named[notionMigrationSourceKey(database.SourceID)] = index
	}
	for _, drafted := range draft.Databases {
		index, ok := named[notionMigrationSourceKey(drafted.SourceID)]
		if !ok {
			plan.Databases = append(plan.Databases, drafted)
			continue
		}
		database := &plan.Databases[index]
		database.Action = firstNonempty(strings.TrimSpace(database.Action), drafted.Action)
		if database.Action == "skip" {
			continue
		}
		database.Properties = fillNotionMigrationProperties(database.Properties, drafted.Properties)
		sources := map[string]int{}
		for at, source := range database.DataSources {
			sources[notionMigrationSourceKey(source.SourceID)] = at
		}
		for _, source := range drafted.DataSources {
			if at, ok := sources[notionMigrationSourceKey(source.SourceID)]; ok {
				database.DataSources[at].Properties = fillNotionMigrationProperties(database.DataSources[at].Properties, source.Properties)
			} else {
				database.DataSources = append(database.DataSources, source)
			}
		}
	}
	return plan
}

// fillNotionMigrationProperties adds each drafted property a plan's database
// doesn't name, and gives one it names without an action the draft's.
func fillNotionMigrationProperties(planned, drafted []notionMigrationPlanProperty) []notionMigrationPlanProperty {
	named := map[string]int{}
	for index, property := range planned {
		named[notionMigrationPropertyKey(property.SourceID)] = index
	}
	for _, property := range drafted {
		if index, ok := named[notionMigrationPropertyKey(property.SourceID)]; ok {
			planned[index].Action = firstNonempty(strings.TrimSpace(planned[index].Action), property.Action)
			continue
		}
		planned = append(planned, property)
	}
	return planned
}

// notionMigrationPlanChanges is a plan without what comes as the draft has
// it: a database the same as drafted is left out, and of one that differs,
// the properties and data sources the same as drafted.
func notionMigrationPlanChanges(plan, draft notionMigrationPlan) notionMigrationPlan {
	drafted := map[string]notionMigrationPlanDatabase{}
	for _, database := range draft.Databases {
		drafted[notionMigrationSourceKey(database.SourceID)] = database
	}
	changes := plan
	changes.Databases = []notionMigrationPlanDatabase{}
	for _, database := range plan.Databases {
		original, ok := drafted[notionMigrationSourceKey(database.SourceID)]
		if !ok {
			changes.Databases = append(changes.Databases, database)
			continue
		}
		database.Properties = notionMigrationPlanPropertyDiff(database.Properties, original.Properties)
		sources := map[string][]notionMigrationPlanProperty{}
		for _, source := range original.DataSources {
			sources[notionMigrationSourceKey(source.SourceID)] = source.Properties
		}
		kept := []notionMigrationPlanDataSource{}
		for _, source := range database.DataSources {
			source.Properties = notionMigrationPlanPropertyDiff(source.Properties, sources[notionMigrationSourceKey(source.SourceID)])
			if len(source.Properties) > 0 {
				kept = append(kept, source)
			}
		}
		database.DataSources = kept
		// The rest of the entry as drafted, with nothing changed under it.
		bare, originalBare := database, original
		bare.SourceID, bare.Properties, bare.DataSources = original.SourceID, nil, nil
		originalBare.Properties, originalBare.DataSources = nil, nil
		if len(database.Properties) == 0 && len(kept) == 0 && notionMigrationSamePlanEntry(bare, originalBare) {
			continue
		}
		changes.Databases = append(changes.Databases, database)
	}
	return changes
}

func notionMigrationPlanPropertyDiff(planned, drafted []notionMigrationPlanProperty) []notionMigrationPlanProperty {
	original := map[string]notionMigrationPlanProperty{}
	for _, property := range drafted {
		original[notionMigrationPropertyKey(property.SourceID)] = property
	}
	changed := []notionMigrationPlanProperty{}
	for _, property := range planned {
		same, ok := original[notionMigrationPropertyKey(property.SourceID)]
		written := property
		written.SourceID = same.SourceID
		if ok && notionMigrationSamePlanEntry(written, same) {
			continue
		}
		changed = append(changed, property)
	}
	return changed
}

// notionMigrationSamePlanEntry compares two plan entries as they're written.
func notionMigrationSamePlanEntry(a, b any) bool {
	left, leftErr := json.Marshal(a)
	right, rightErr := json.Marshal(b)
	return leftErr == nil && rightErr == nil && string(left) == string(right)
}
