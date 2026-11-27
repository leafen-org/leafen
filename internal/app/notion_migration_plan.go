package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A migration's plan (decision "설계 우선 Notion 이사" 3절 원칙 2, 4절): one
// declarative JSON per migration, made from its finished scan by the person
// with their agent or from the as-is draft. Each save raises its revision
// and is made over the revision it was read at, so a screen and an agent
// don't overwrite each other. Reading or saving it checks it against the
// scan (notion_migration_plan_check.go); a plan with problems is kept so it
// can be fixed, but does not run.

// readNotionMigrationPlan gives a migration's plan, checked, or no plan at
// revision 0.
func readNotionMigrationPlan(app core.App, ownerID, workspaceID, id string) (notionMigrationPlanState, error) {
	return readNotionMigrationPlanFor(context.Background(), app, ownerID, workspaceID, id)
}

// readNotionMigrationPlanFor is readNotionMigrationPlan for the owner whose
// own request ctx carries, its estimate said in its language.
func readNotionMigrationPlanFor(ctx context.Context, app core.App, ownerID, workspaceID, id string) (notionMigrationPlanState, error) {
	stored, err := loadNotionMigration(app, ownerID, workspaceID, id)
	if err != nil {
		return notionMigrationPlanState{}, err
	}
	return notionMigrationPlanStateOf(ctx, app, stored)
}

func notionMigrationPlanStateOf(ctx context.Context, app core.App, stored storedNotionMigration) (notionMigrationPlanState, error) {
	state := notionMigrationPlanState{MigrationID: stored.ID, Revision: stored.PlanRevision}
	if stored.PlanJSON == "" && stored.Status != notionMigrationStatusScanned {
		return state, nil
	}
	summary, err := loadNotionMigrationSummary(app, stored)
	if err != nil {
		return notionMigrationPlanState{}, err
	}
	// The scan gives the "as is" draft: to start from until there is a plan,
	// and after, to tell what the plan changes.
	if stored.Status == notionMigrationStatusScanned {
		draft := notionMigrationAsIsPlan(summary)
		state.Draft = &draft
	}
	if stored.PlanJSON == "" {
		state.Estimate = notionMigrationPlanEstimate(ctx, app, stored, summary, state)
		return state, nil
	}
	plan := notionMigrationPlan{}
	if err := json.Unmarshal([]byte(stored.PlanJSON), &plan); err != nil {
		return notionMigrationPlanState{}, err
	}
	check, err := checkNotionMigrationPlan(app, stored, summary, plan)
	if err != nil {
		return notionMigrationPlanState{}, err
	}
	// A member reads back as notion-get-users lists them, the ID the plan
	// was given, however the plan keeps it, and each person with what the
	// plan does with them.
	for index := range plan.People {
		plan.People[index].MemberID = notionMigrationMemberAsListed(plan.People[index].MemberID)
		plan.People[index].Action = notionMigrationPersonChoice(plan.People[index])
	}
	state.Plan, state.Check = &plan, &check
	state.Estimate = notionMigrationPlanEstimate(ctx, app, stored, summary, state)
	return state, nil
}

// notionMigrationMemberAsListed is a member as the Notion-compatible tools
// name people: a Leafen account's ID as its Notion-compatible ID, anything
// else as it is.
func notionMigrationMemberAsListed(member string) string {
	if listed, err := notionCompatibleUserID(strings.TrimSpace(member)); err == nil {
		return listed
	}
	return member
}

// saveNotionMigrationPlan keeps a plan for a scanned migration over the
// revision it was read at.
func saveNotionMigrationPlan(
	ctx context.Context, app core.App, ownerID, workspaceID, id string, input notionMigrationPlanInput,
) (notionMigrationPlanState, error) {
	// A member is named as the Notion-compatible tools give people
	// (notion-get-users) or by their Leafen ID; the plan keeps the Leafen ID.
	for index := range input.Plan.People {
		input.Plan.People[index].MemberID = leafenPersonReference(input.Plan.People[index].MemberID)
	}
	if problem := notionMigrationPlanValueProblem(input.Plan); problem != "" {
		return notionMigrationPlanState{}, refuse(errNotionMigrationInvalid, "plan's %s", problem)
	}
	encoded, err := json.Marshal(input.Plan)
	if err != nil {
		return notionMigrationPlanState{}, err
	}
	if len(encoded) > notionMigrationPlanMaxBytes {
		return notionMigrationPlanState{}, refuse(errNotionMigrationInvalid, "plan is over %s", requestByteSize(notionMigrationPlanMaxBytes))
	}
	stored, err := loadNotionMigration(app, ownerID, workspaceID, id)
	if err != nil {
		return notionMigrationPlanState{}, err
	}
	release := acquireProductOperationLock(notionMigrationLockNamespace, stored.ID)
	defer release()
	if stored, err = loadNotionMigration(app, ownerID, workspaceID, id); err != nil {
		return notionMigrationPlanState{}, err
	}
	if stored.Status != notionMigrationStatusScanned {
		return notionMigrationPlanState{}, errNotionMigrationNotScanned
	}
	// A run reads the revision it started with until it ends.
	if stored.RunState == notionMigrationRunRunning {
		return notionMigrationPlanState{}, errNotionMigrationRunning
	}
	if input.BaseRevision != stored.PlanRevision {
		return notionMigrationPlanState{}, errNotionMigrationPlanConflict
	}
	stored.PlanJSON, stored.PlanRevision, stored.UpdatedAt = string(encoded), stored.PlanRevision+1, time.Now().UTC().UnixMilli()
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_notion_migrations}} SET [[plan_json]]={:plan},[[plan_revision]]={:revision},[[updated_at]]={:updatedAt}
		WHERE [[id]]={:id} AND [[plan_revision]]={:base}
	`).Bind(dbx.Params{
		"plan": stored.PlanJSON, "revision": stored.PlanRevision, "updatedAt": stored.UpdatedAt, "id": stored.ID,
		"base": input.BaseRevision,
	}).Execute()
	if err != nil {
		return notionMigrationPlanState{}, err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return notionMigrationPlanState{}, refusedUnlessStorage(errNotionMigrationPlanConflict, err)
	}
	return notionMigrationPlanStateOf(ctx, app, stored)
}

func readNotionMigrationPlanRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	state, err := readNotionMigrationPlanFor(event.Request.Context(), event.App, user.Id, strings.TrimSpace(event.Request.PathValue("workspaceId")),
		event.Request.PathValue("migrationId"))
	if err != nil {
		return notionMigrationRouteError(event, err, "plan-read")
	}
	return event.JSON(http.StatusOK, state)
}

func saveNotionMigrationPlanRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := notionMigrationPlanInput{}
	if err := event.BindBody(&input); err != nil {
		return notionMigrationRouteError(event, bodyRefusal(errNotionMigrationInvalid, "migration plan", err), "plan-input")
	}
	state, err := saveNotionMigrationPlan(event.Request.Context(), event.App, user.Id, strings.TrimSpace(event.Request.PathValue("workspaceId")),
		event.Request.PathValue("migrationId"), input)
	if err != nil {
		return notionMigrationRouteError(event, err, "plan-save")
	}
	return event.JSON(http.StatusOK, state)
}

// notionMigrationAsIsPlan is the plan that moves everything the scan found
// as it is (decision 3절 원칙 1): every database with every property in the
// type it becomes, one Leafen has no type for reported; comments and files
// brought; the import at the top of the person's private pages.
func notionMigrationAsIsPlan(summary notionMigrationSummary) notionMigrationPlan {
	plan := notionMigrationPlan{
		Format: notionMigrationPlanFormat, Comments: "import", Files: "copy",
		Pages: []notionMigrationPlanPage{}, People: []notionMigrationPlanPerson{},
		Databases: make([]notionMigrationPlanDatabase, 0, len(summary.Databases)),
	}
	for _, database := range summary.Databases {
		planned := notionMigrationPlanDatabase{
			SourceID: database.ID, Action: "import", Properties: notionMigrationAsIsProperties(database.Properties),
		}
		// Each data source after the first brings its own properties.
		for _, source := range database.DataSources {
			planned.DataSources = append(planned.DataSources, notionMigrationPlanDataSource{
				SourceID: source.ID, Properties: notionMigrationAsIsProperties(source.Properties),
			})
		}
		plan.Databases = append(plan.Databases, planned)
	}
	return plan
}

// notionMigrationAsIsProperties maps each property to the type it becomes,
// or reports one Leafen has no type for.
func notionMigrationAsIsProperties(properties []notionMigrationProperty) []notionMigrationPlanProperty {
	planned := make([]notionMigrationPlanProperty, 0, len(properties))
	for _, property := range properties {
		action := "map"
		if property.LeafenType == "" {
			action = "report"
		}
		planned = append(planned, notionMigrationPlanProperty{SourceID: property.ID, Action: action})
	}
	return planned
}
