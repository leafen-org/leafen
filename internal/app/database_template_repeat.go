package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database template may repeat, as Notion's do: every N days, every N weeks
// on chosen weekdays, every N months or every N years, at a time in a time
// zone from a start date to an optional end date or count of pages, it makes a page from itself at
// the end of its database. The repeat is the person's who set it: the pages
// it makes are theirs, and it pauses when they can no longer edit the
// database. A deleted template stops repeating and a restored one repeats
// again from then; a copy of a template doesn't repeat. When the server
// misses runs, one page stands for them and the schedule goes on from then.

const (
	databaseTemplateRepeatJobKind          = "database-template-repeat"
	databaseTemplateRepeatAdmissionChunk   = 50
	databaseTemplateRepeatPausedAccess     = "access"
	databaseTemplateRepeatPausedInvalid    = "invalid"
	databaseTemplateRepeatRequestIDPrefix  = "database-template-repeat:"
	databaseTemplateRepeatRowRequestPrefix = "template-repeat:"
)

// databaseTemplateRepeatTrigger is the repeat as a scheduled automation's
// trigger, whose schedules it shares.
func databaseTemplateRepeatTrigger(repeat databaseToolbarTemplateRepeatWire) databaseAutomationTrigger {
	return databaseAutomationTrigger{
		Type: databaseAutomationTriggerSchedule, Frequency: repeat.Frequency, Interval: repeat.Interval,
		Time: repeat.Time, TimeZone: repeat.TimeZone, StartsOn: repeat.StartsOn, EndsOn: repeat.EndsOn,
		EndsAfter: repeat.EndsAfter, Weekdays: repeat.Weekdays, MonthDay: repeat.MonthDay, MonthWeek: repeat.MonthWeek,
	}
}

// normalizedDatabaseTemplateRepeat admits a repeat on a schedule a scheduled
// automation admits, in the form it stores.
func normalizedDatabaseTemplateRepeat(
	repeat databaseToolbarTemplateRepeatWire,
) (databaseToolbarTemplateRepeatWire, databaseAutomationTrigger, error) {
	trigger := databaseTemplateRepeatTrigger(repeat)
	normalizeDatabaseAutomationScheduleTrigger(&trigger)
	if err := validateDatabaseAutomationScheduleTrigger(trigger); err != nil {
		return databaseToolbarTemplateRepeatWire{}, trigger, databaseToolbarRefused("repeat%s", databaseTemplateRepeatReason(err))
	}
	return databaseToolbarTemplateRepeatWire{
		Frequency: trigger.Frequency, Interval: trigger.Interval, Time: trigger.Time,
		TimeZone: trigger.TimeZone, StartsOn: trigger.StartsOn, EndsOn: trigger.EndsOn, EndsAfter: trigger.EndsAfter,
		Weekdays: trigger.Weekdays, MonthDay: trigger.MonthDay, MonthWeek: trigger.MonthWeek,
	}, trigger, nil
}

// storedDatabaseTemplateRepeat reads a stored repeat, false when there is none
// or it no longer admits.
func storedDatabaseTemplateRepeat(raw string) (databaseToolbarTemplateRepeatWire, databaseAutomationTrigger, bool) {
	repeat := databaseToolbarTemplateRepeatWire{}
	if raw == "" || json.Unmarshal([]byte(raw), &repeat) != nil {
		return repeat, databaseAutomationTrigger{}, false
	}
	normalized, trigger, err := normalizedDatabaseTemplateRepeat(repeat)
	return normalized, trigger, err == nil
}

// databaseTemplateRepeatNextRunAt is the repeat's first run after an instant,
// in Unix milliseconds, or 0 when it has none: it ended, or made the pages
// it ends after.
func databaseTemplateRepeatNextRunAt(raw string, runs int, after time.Time) int64 {
	_, trigger, ok := storedDatabaseTemplateRepeat(raw)
	if !ok || !databaseTemplateRepeatRunsLeft(trigger, runs) {
		return 0
	}
	next, exists, err := nextDatabaseAutomationScheduleRun(trigger, after)
	if err != nil || !exists {
		return 0
	}
	return next.UnixMilli()
}

// setDatabaseToolbarTemplateRepeat sets a template's repeat, the actor's, or
// clears it; its next page is the schedule's first run from now.
func setDatabaseToolbarTemplateRepeat(
	app core.App,
	authority databaseToolbarConfigAuthority,
	operation databaseToolbarConfigMutationOperation,
	state *databaseToolbarMutationState,
) error {
	template, err := databaseToolbarTemplateForMutation(app, authority, *operation.TemplateID)
	if err != nil {
		return err
	}
	// A template in the trash takes a repeat too, as Notion's (seen
	// 2026-10-30); it runs once the template is restored (the runner skips
	// deleted templates).
	if template.AuthorityRevision != *operation.ExpectedRevision {
		return errDatabaseToolbarConfigConflict
	}
	if err := ensureDatabaseToolbarTemplateState(app, authority, template.ID); err != nil {
		return err
	}
	repeatJSON, actorID, nextRunAt := "", "", int64(0)
	var stored struct {
		Runs       int    `db:"repeat_runs"`
		RepeatJSON string `db:"repeat_json"`
	}
	if err := app.DB().NewQuery(`SELECT [[repeat_runs]], [[repeat_json]] FROM {{leafen_database_toolbar_template_state}}
		WHERE [[template_id]]={:id}`).Bind(dbx.Params{"id": template.ID}).One(&stored); err != nil {
		return err
	}
	// A repeat started anew — on a template with none, after "반복 취소" or
	// after its last page, when it reads "사용 안 함" again — counts its pages
	// from none, as Notion's (seen 2026-11-03: a repeat ending after one page
	// made it though an earlier one had made a page).
	runs := stored.Runs
	if stored.RepeatJSON == "" || operation.Repeat == nil ||
		databaseTemplateRepeatNextRunAt(stored.RepeatJSON, stored.Runs, time.Now().UTC()) == 0 {
		runs = 0
	}
	if operation.Repeat != nil {
		repeat, _, err := normalizedDatabaseTemplateRepeat(*operation.Repeat)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(repeat)
		if err != nil {
			return err
		}
		// A schedule that ended makes no page; one that made the pages it ends
		// after stays stopped, as a recurring automation does.
		if databaseTemplateRepeatNextRunAt(string(encoded), 0, time.Now().UTC()) == 0 {
			return databaseToolbarRefused("repeat ends before its next run, so it would make no page")
		}
		nextRunAt = databaseTemplateRepeatNextRunAt(string(encoded), runs, time.Now().UTC())
		repeatJSON, actorID = string(encoded), state.ActorID
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := app.DB().NewQuery(`UPDATE {{leafen_database_templates}} SET [[updated_at]]={:now}
		WHERE [[id]]={:id}`).Bind(dbx.Params{"now": now, "id": template.ID}).Execute(); err != nil {
		return err
	}
	next := template.AuthorityRevision + 1
	result, err := app.DB().NewQuery(`UPDATE {{leafen_database_toolbar_template_state}}
		SET [[authority_revision]]={:next}, [[repeat_json]]={:repeat}, [[repeat_actor_id]]={:actor},
			[[repeat_next_run_at]]={:nextRunAt}, [[repeat_paused_reason]]='', [[repeat_runs]]={:runs}
		WHERE [[template_id]]={:id} AND [[authority_revision]]={:current}`).Bind(dbx.Params{
		"next": next, "repeat": repeatJSON, "actor": actorID, "nextRunAt": nextRunAt, "runs": runs,
		"id": template.ID, "current": template.AuthorityRevision,
	}).Execute()
	if err := requireDatabaseToolbarMutationApplied(result, err); err != nil {
		return err
	}
	state.TemplateAuthorityRevision = next
	return nil
}

// settleDatabaseTemplateRepeat stops a deleted template's repeat and starts a
// restored one's again from now.
func settleDatabaseTemplateRepeat(app core.App, templateID string, deleted bool) error {
	var stored struct {
		RepeatJSON string `db:"repeat_json"`
		Runs       int    `db:"repeat_runs"`
	}
	if err := app.DB().NewQuery(`SELECT [[repeat_json]], [[repeat_runs]] FROM {{leafen_database_toolbar_template_state}}
		WHERE [[template_id]]={:id}`).Bind(dbx.Params{"id": templateID}).One(&stored); err != nil {
		return err
	}
	repeatJSON := stored.RepeatJSON
	if repeatJSON == "" {
		return nil
	}
	nextRunAt := int64(0)
	if !deleted {
		nextRunAt = databaseTemplateRepeatNextRunAt(repeatJSON, stored.Runs, time.Now().UTC())
	}
	_, err := app.DB().NewQuery(`UPDATE {{leafen_database_toolbar_template_state}}
		SET [[repeat_next_run_at]]={:nextRunAt} WHERE [[template_id]]={:id}`).
		Bind(dbx.Params{"nextRunAt": nextRunAt, "id": templateID}).Execute()
	return err
}

// databaseTemplateRepeatMetadata is a template's repeat as its metadata shows
// it: the schedule, the next page's instant and why it paused.
func databaseTemplateRepeatMetadata(
	template storedDatabaseToolbarTemplate,
) (*databaseToolbarTemplateRepeatWire, string, string) {
	repeat, _, ok := storedDatabaseTemplateRepeat(template.RepeatJSON)
	if !ok {
		return nil, "", ""
	}
	nextRunAt := ""
	if template.RepeatNextRunAt > 0 {
		nextRunAt = time.UnixMilli(template.RepeatNextRunAt).UTC().Format(time.RFC3339)
	}
	return &repeat, nextRunAt, template.RepeatPausedReason
}

type databaseTemplateRepeatDue struct {
	TemplateID   string `db:"template_id"`
	WorkspaceID  string `db:"workspace_id"`
	OwnerID      string `db:"owner"`
	PageID       string `db:"page_id"`
	DataSourceID string `db:"data_source_id"`
	RepeatJSON   string `db:"repeat_json"`
	ActorID      string `db:"repeat_actor_id"`
	NextRunAt    int64  `db:"repeat_next_run_at"`
	Runs         int    `db:"repeat_runs"`
	Revision     int    `db:"authority_revision"`
}

type databaseTemplateRepeatJobPayload struct {
	TemplateID   string `json:"templateId"`
	PageID       string `json:"pageId"`
	DataSourceID string `json:"dataSourceId"`
	ActorID      string `json:"actorId"`
	TimeZone     string `json:"timeZone"`
	Position     string `json:"position"`
	ScheduledFor int64  `json:"scheduledFor"`
}

// databaseTemplateRepeatDueSQL selects the repeats due by an instant whose
// template and database are in place.
const databaseTemplateRepeatDueSQL = `
	FROM {{leafen_database_toolbar_template_state}} [[state]]
		INDEXED BY [[idx_leafen_database_toolbar_template_state_repeat_due]]
	JOIN {{leafen_database_templates}} [[template]] ON [[template]].[[id]] = [[state]].[[template_id]]
	WHERE [[state]].[[repeat_next_run_at]] > 0 AND [[state]].[[repeat_next_run_at]] <= {:dueAt}
		AND [[state]].[[deleted_at]] = '' AND [[state]].[[repeat_paused_reason]] = ''`

// admitDueDatabaseTemplateRepeats queues a page for each repeat that is due
// and moves the repeat to its next run.
func admitDueDatabaseTemplateRepeats(
	app core.App,
	dueAt time.Time,
) (databaseAutomationScheduleAdmissionResult, error) {
	result := databaseAutomationScheduleAdmissionResult{}
	dueMillis := dueAt.UTC().UnixMilli()
	placed := databaseSourceNotTrashedSQL("[[template]].[[data_source_id]]") + ` AND ` +
		blockMovePageUnfencedSQL(`[[template]].[[page_id]]`)
	err := app.RunInTransaction(func(txApp core.App) error {
		due := []databaseTemplateRepeatDue{}
		if err := txApp.DB().NewQuery(`
			SELECT [[state]].[[template_id]], [[template]].[[workspace_id]], [[template]].[[owner]],
				[[template]].[[page_id]], [[template]].[[data_source_id]], [[state]].[[repeat_json]],
				[[state]].[[repeat_actor_id]], [[state]].[[repeat_next_run_at]], [[state]].[[repeat_runs]],
				[[state]].[[authority_revision]]
			` + databaseTemplateRepeatDueSQL + ` AND ` + placed + `
			ORDER BY [[state]].[[repeat_next_run_at]], [[state]].[[template_id]] LIMIT {:limit}
		`).Bind(dbx.Params{"dueAt": dueMillis, "limit": databaseTemplateRepeatAdmissionChunk + 1}).All(&due); err != nil {
			return err
		}
		if len(due) > databaseTemplateRepeatAdmissionChunk {
			due = due[:databaseTemplateRepeatAdmissionChunk]
		}
		tails := map[string]string{}
		for _, repeat := range due {
			stored, trigger, ok := storedDatabaseTemplateRepeat(repeat.RepeatJSON)
			nextRunAt := int64(0)
			paused := ""
			if !ok {
				paused = databaseTemplateRepeatPausedInvalid
			} else {
				next, exists, err := nextDatabaseAutomationScheduleRun(trigger, time.UnixMilli(max(repeat.NextRunAt, dueMillis)).UTC())
				if err != nil {
					return err
				}
				// This page counts toward the pages it ends after.
				if exists && databaseTemplateRepeatRunsLeft(trigger, repeat.Runs+1) {
					nextRunAt = next.UnixMilli()
				}
			}
			made := 0
			if ok {
				made = 1
			}
			updated, err := txApp.DB().NewQuery(`UPDATE {{leafen_database_toolbar_template_state}}
				SET [[repeat_next_run_at]] = {:nextRunAt}, [[repeat_paused_reason]] = {:paused},
					[[repeat_runs]] = [[repeat_runs]] + {:made}
				WHERE [[template_id]] = {:id} AND [[repeat_next_run_at]] = {:scheduledFor}`).Bind(dbx.Params{
				"nextRunAt": nextRunAt, "paused": paused, "made": made, "id": repeat.TemplateID, "scheduledFor": repeat.NextRunAt,
			}).Execute()
			if err != nil {
				return err
			}
			if changed, _ := updated.RowsAffected(); changed != 1 || !ok {
				continue
			}

			// The page goes after the database's last page.
			tailKey := repeat.OwnerID + "\x00" + repeat.PageID + "\x00" + repeat.DataSourceID
			tail, known := tails[tailKey]
			if !known {
				if err := txApp.DB().NewQuery(`SELECT COALESCE(MAX([[position]]), '') FROM {{leafen_database_rows}}
					WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:dataSourceId}`).
					Bind(dbx.Params{"owner": repeat.OwnerID, "pageId": repeat.PageID, "dataSourceId": repeat.DataSourceID}).
					Row(&tail); err != nil {
					return err
				}
			}
			position, err := databasePositionAfter(tail)
			if err != nil {
				return err
			}
			tails[tailKey] = position

			jobID := automationStableID("htr", repeat.TemplateID, strconv.Itoa(repeat.Revision),
				strconv.FormatInt(repeat.NextRunAt, 10))
			payload, err := json.Marshal(databaseTemplateRepeatJobPayload{
				TemplateID: repeat.TemplateID, PageID: repeat.PageID, DataSourceID: repeat.DataSourceID,
				ActorID: repeat.ActorID, TimeZone: stored.TimeZone, Position: position, ScheduledFor: repeat.NextRunAt,
			})
			if err != nil {
				return err
			}
			requestHash := sha256.Sum256(payload)
			if _, err := txApp.DB().NewQuery(`
				INSERT OR IGNORE INTO {{leafen_jobs}}
					([[id]], [[owner]], [[workspace_id]], [[request_id]], [[request_hash]], [[kind]],
					 [[page_id]], [[status]], [[error_code]], [[not_before]], [[created]], [[updated]],
					 [[payload_json]], [[cursor]], [[processed_items]], [[total_items]], [[attempt]],
					 [[max_attempts]], [[chunk_count]])
				VALUES ({:id}, {:owner}, {:workspaceId}, {:requestId}, {:requestHash}, {:kind},
					{:pageId}, 'queued', '', {:notBefore}, {:now}, {:now}, {:payload}, '', 0, 1, 0,
					{:maxAttempts}, 0)
			`).Bind(dbx.Params{
				"id": jobID, "owner": repeat.OwnerID, "workspaceId": repeat.WorkspaceID,
				"requestId": databaseTemplateRepeatRequestIDPrefix + jobID, "requestHash": hex.EncodeToString(requestHash[:]),
				"kind": databaseTemplateRepeatJobKind, "pageId": repeat.PageID,
				"notBefore": dueMillis, "now": dueMillis, "payload": string(payload),
				"maxAttempts": defaultDurableJobMaxAttempts,
			}).Execute(); err != nil {
				return err
			}
			result.Admitted++
		}
		return txApp.DB().NewQuery(`SELECT EXISTS (SELECT 1 ` + databaseTemplateRepeatDueSQL + ` AND ` + placed + ` LIMIT 1)`).
			Bind(dbx.Params{"dueAt": dueMillis}).Row(&result.HasMore)
	})
	if err != nil {
		return databaseAutomationScheduleAdmissionResult{}, err
	}
	return result, nil
}

// applyDatabaseTemplateRepeatJob makes a repeat's page from its template as
// the template is now, as the person whose repeat it is, in the repeat's
// time zone. A template deleted, or a repeat cleared or set by someone else
// since, makes none; when its person can no longer edit the database, the
// repeat pauses.
func (r *durableJobRunner) applyDatabaseTemplateRepeatJob(ctx context.Context, job storedJob) error {
	if r.ygo == nil {
		return errors.New(errorCodeJobRunnerFailed)
	}
	payload := databaseTemplateRepeatJobPayload{}
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return err
	}
	current := struct {
		Revision   int    `db:"authority_revision"`
		DeletedAt  string `db:"deleted_at"`
		RepeatJSON string `db:"repeat_json"`
		ActorID    string `db:"repeat_actor_id"`
	}{}
	err := r.app.DB().NewQuery(`SELECT [[authority_revision]], [[deleted_at]], [[repeat_json]], [[repeat_actor_id]]
		FROM {{leafen_database_toolbar_template_state}} WHERE [[template_id]] = {:id}`).
		Bind(dbx.Params{"id": payload.TemplateID}).One(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && current.DeletedAt == "" && current.RepeatJSON != "" && current.ActorID == payload.ActorID {
		var created databaseToolbarTemplateRowCreateResponse
		created, err = r.ygo.admitDatabaseToolbarTemplateRow(ctx, payload.ActorID, payload.PageID, payload.DataSourceID,
			databaseToolbarTemplateRowCreateInput{
				RequestID: databaseTemplateRepeatRowRequestPrefix + job.ID, TemplateID: payload.TemplateID,
				ExpectedTemplateRevision: current.Revision, Position: payload.Position, TimeZone: payload.TimeZone,
			})
		if errors.Is(err, errDatabaseToolbarConfigNotFound) {
			if _, pauseErr := r.app.DB().NewQuery(`UPDATE {{leafen_database_toolbar_template_state}}
				SET [[repeat_paused_reason]] = {:reason}, [[repeat_next_run_at]] = 0
				WHERE [[template_id]] = {:id} AND [[repeat_actor_id]] = {:actor}`).Bind(dbx.Params{
				"reason": databaseTemplateRepeatPausedAccess, "id": payload.TemplateID, "actor": payload.ActorID,
			}).Execute(); pauseErr != nil {
				return pauseErr
			}
			err = nil
		}
		if err != nil {
			return err
		}
		if err := markLeafenSystemAuthored(r.app, created.RowID); err != nil {
			return err
		}
	}
	now := time.Now().UTC().UnixMilli()
	_, err = r.app.DB().NewQuery(`UPDATE {{leafen_jobs}}
		SET [[status]] = {:status}, [[processed_items]] = [[total_items]], [[chunk_count]] = [[chunk_count]] + 1,
			[[updated]] = {:updated}
		WHERE [[id]] = {:id} AND [[status]] = 'queued'`).Bind(dbx.Params{
		"status": durableJobStatusCompleted, "updated": now, "id": job.ID,
	}).Execute()
	return err
}

// leafenSystemAuthorID is who a page Leafen makes on its own reads as made
// and last edited by — a repeating template's, a scheduled automation's — as
// Notion writes those as its system user "Notion" (seen 2026-11-02). The
// person whose repeat or automation it is still runs it.
const leafenSystemAuthorID = "leafen-system"

// markLeafenSystemEdited says a page's last change, made under a person's
// authority, is Leafen's: its last editor and its history's latest version.
func markLeafenSystemEdited(app core.App, pageID, principalID string) error {
	if _, err := app.DB().NewQuery(`UPDATE {{leafen_pages}} SET [[last_edited_by]] = {:author} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"author": leafenSystemAuthorID, "id": pageID}).Execute(); err != nil {
		return err
	}
	return markLeafenSystemVersion(app, pageID, principalID)
}

// markLeafenSystemVersion says the latest version of a page's history, made
// under a person's authority, is Leafen's, leaving who last edited the page.
func markLeafenSystemVersion(app core.App, pageID, principalID string) error {
	_, err := app.DB().NewQuery(`UPDATE {{leafen_page_history_versions}} SET [[actor_id]] = {:author}
		WHERE [[page_id]] = {:id} AND [[actor_id]] = {:principal} AND [[version_cursor]] = (
			SELECT MAX([[version_cursor]]) FROM {{leafen_page_history_versions}} WHERE [[page_id]] = {:id}
		)`).Bind(dbx.Params{"author": leafenSystemAuthorID, "id": pageID, "principal": principalID}).Execute()
	return err
}

func markLeafenSystemAuthored(app core.App, pageIDs ...string) error {
	return markPagesAuthoredBy(app, leafenSystemAuthorID, pageIDs...)
}

// leafenAnonymousAuthorID is who an anonymous form response reads as made by:
// "익명", as Notion's (seen 2026-11-05). No one signs in as it.
const leafenAnonymousAuthorID = "leafen-anonymous"

func markPagesAuthoredBy(app core.App, author string, pageIDs ...string) error {
	for _, id := range pageIDs {
		if id == "" {
			continue
		}
		if _, err := app.DB().NewQuery(`UPDATE {{leafen_pages}} SET [[created_by]] = {:author}, [[last_edited_by]] = {:author}
			WHERE [[id]] = {:id}`).Bind(dbx.Params{"author": author, "id": id}).Execute(); err != nil {
			return err
		}
		if _, err := app.DB().NewQuery(`UPDATE {{leafen_page_history_versions}} SET [[actor_id]] = {:author}
			WHERE [[page_id]] = {:id}`).Bind(dbx.Params{"author": author, "id": id}).Execute(); err != nil {
			return err
		}
	}
	return nil
}
