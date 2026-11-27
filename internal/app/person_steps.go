package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// A person step (user decision 2026-11-22, everything an agent does through
// MCP): what only the person can give — a password, a key, a recovery code
// they keep — goes in through a field on their own screen, as the setup
// restore's "직접 입력할 것" does, so the agent leads the rest. The agent
// sends the operation as it would, leaving the secret fields out and saying
// "secretsFromPerson" (person_step_hold.go); Leafen keeps it here, tells the
// person in their inbox and shows the fields in the settings place the
// operation belongs to (person_step_operations.go). The person types them
// and saves, and Leafen runs the operation as that person with them
// (person_step_apply.go); the agent follows the step's state and never sees
// what was typed. Nothing secret is kept here: the body holds what the agent
// sent, and a code Leafen makes for the person to keep lives in memory only
// until they keep it.

const (
	personStepsTable = "leafen_person_steps"
	personStepsPath  = apiRoot + "/person-steps"
	// How long a step waits for its person.
	personStepLifetime = 7 * 24 * time.Hour

	personStepWaiting  = "waiting"
	personStepDone     = "done"
	personStepCanceled = "canceled"
	// Said of a waiting step past its time; not kept as a state.
	personStepExpired = "expired"

	errorCodePersonStepInvalid  = "LEAFEN_PERSON_STEP_INVALID"
	errorCodePersonStepNotFound = "LEAFEN_PERSON_STEP_NOT_FOUND"
	errorCodePersonStepEnded    = "LEAFEN_PERSON_STEP_ENDED"
)

type storedPersonStep struct {
	ID             string `db:"id"`
	OwnerID        string `db:"owner_id"`
	ConnectionID   string `db:"connection_id"`
	ConnectionName string `db:"connection_name"`
	Operation      string `db:"operation"`
	Path           string `db:"path"`
	Body           string `db:"body"`
	Fields         string `db:"fields"`
	Keep           string `db:"keep"`
	KeptAt         int64  `db:"kept_at"`
	State          string `db:"state"`
	FailureCode    string `db:"failure_code"`
	Failure        string `db:"failure"`
	CreatedAt      int64  `db:"created_at"`
	UpdatedAt      int64  `db:"updated_at"`
	ExpiresAt      int64  `db:"expires_at"`
}

func (step storedPersonStep) fields() []string {
	fields := []string{}
	if json.Unmarshal([]byte(step.Fields), &fields) != nil {
		return []string{}
	}
	return fields
}

// state is the step's state as said now: a waiting one past its time has
// expired.
func (step storedPersonStep) state(now time.Time) string {
	if step.State == personStepWaiting && now.UnixMilli() >= step.ExpiresAt {
		return personStepExpired
	}
	return step.State
}

const personStepColumns = `[[id]], [[owner_id]], [[connection_id]], [[connection_name]], [[operation]], [[path]], [[body]],
	[[fields]], [[keep]], [[kept_at]], [[state]], [[failure_code]], [[failure]], [[created_at]], [[updated_at]], [[expires_at]]`

var errPersonStepMissing = errors.New("person step not found")

func loadPersonStep(app core.App, id string) (storedPersonStep, error) {
	step := storedPersonStep{}
	err := app.DB().NewQuery(`SELECT ` + personStepColumns + ` FROM {{leafen_person_steps}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": id}).One(&step)
	if err != nil && errors.Is(err, sql.ErrNoRows) {
		return step, errPersonStepMissing
	}
	return step, err
}

// personStepCursor is where a page of waiting steps ends: the last one's
// time and id, newest first.
type personStepCursor struct {
	CreatedAt int64
	ID        string
}

// waitingPersonSteps are a page of the steps waiting for a person, newest
// first: of the operations named (every one when nil), after the cursor, at
// most limit; more says whether others wait after them. Read in the
// database, so a section's steps are never cut by another's.
func waitingPersonSteps(app core.App, ownerID string, now time.Time, operations []string, after *personStepCursor, limit int) ([]storedPersonStep, bool, error) {
	steps := []storedPersonStep{}
	if operations != nil && len(operations) == 0 {
		return steps, false, nil
	}
	where := `[[owner_id]] = {:owner} AND [[state]] = 'waiting' AND [[expires_at]] > {:now}`
	params := dbx.Params{"owner": ownerID, "now": now.UnixMilli(), "limit": limit + 1}
	if operations != nil {
		names := make([]string, len(operations))
		for index, operation := range operations {
			names[index] = fmt.Sprintf("{:operation%d}", index)
			params[fmt.Sprintf("operation%d", index)] = operation
		}
		where += ` AND [[operation]] IN (` + strings.Join(names, ", ") + `)`
	}
	if after != nil {
		where += ` AND ([[created_at]] < {:afterAt} OR [[created_at]] = {:afterAt} AND [[id]] < {:afterId})`
		params["afterAt"], params["afterId"] = after.CreatedAt, after.ID
	}
	err := app.DB().NewQuery(`SELECT ` + personStepColumns + ` FROM {{leafen_person_steps}} WHERE ` + where +
		` ORDER BY [[created_at]] DESC, [[id]] DESC LIMIT {:limit}`).Bind(params).All(&steps)
	if err != nil {
		return nil, false, err
	}
	more := len(steps) > limit
	if more {
		steps = steps[:limit]
	}
	return steps, more, nil
}

// savePersonStep keeps a new step, ending one the same person's agent left
// waiting for the same operation: the newer one is what it asks now.
func savePersonStep(app core.App, step storedPersonStep) error {
	return app.RunInTransaction(func(tx core.App) error {
		if _, err := tx.DB().NewQuery(`UPDATE {{leafen_person_steps}} SET [[state]] = 'canceled', [[updated_at]] = {:now}
			WHERE [[owner_id]] = {:owner} AND [[operation]] = {:operation} AND [[path]] = {:path} AND [[state]] = 'waiting'`).
			Bind(dbx.Params{"owner": step.OwnerID, "operation": step.Operation, "path": step.Path, "now": step.CreatedAt}).Execute(); err != nil {
			return err
		}
		_, err := tx.DB().Insert(personStepsTable, dbx.Params{
			"id": step.ID, "owner_id": step.OwnerID, "connection_id": step.ConnectionID, "connection_name": step.ConnectionName,
			"operation": step.Operation, "path": step.Path, "body": step.Body, "fields": step.Fields, "keep": step.Keep,
			"kept_at": 0, "state": personStepWaiting, "failure_code": "", "failure": "",
			"created_at": step.CreatedAt, "updated_at": step.CreatedAt, "expires_at": step.ExpiresAt,
		}).Execute()
		return err
	})
}

// endPersonStep sets a waiting step's state, clearing a failure for one done.
func endPersonStep(app core.App, id, state string, now time.Time) error {
	_, err := app.DB().NewQuery(`UPDATE {{leafen_person_steps}} SET [[state]] = {:state}, [[updated_at]] = {:now},
		[[failure_code]] = CASE WHEN {:state} = 'done' THEN '' ELSE [[failure_code]] END,
		[[failure]] = CASE WHEN {:state} = 'done' THEN '' ELSE [[failure]] END
		WHERE [[id]] = {:id} AND [[state]] = 'waiting'`).
		Bind(dbx.Params{"id": id, "state": state, "now": now.UnixMilli()}).Execute()
	forgetPersonStepShown(app, id)
	return err
}

// failPersonStep keeps why the person's try didn't go through, as the
// operation said it, for the person's screen and the agent alike.
func failPersonStep(app core.App, id, code, message string, now time.Time) error {
	_, err := app.DB().NewQuery(`UPDATE {{leafen_person_steps}} SET [[failure_code]] = {:code}, [[failure]] = {:message},
		[[updated_at]] = {:now} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": id, "code": code, "message": appOneLine(message, 500), "now": now.UnixMilli()}).Execute()
	return err
}

func markPersonStepKept(app core.App, id string, now time.Time) error {
	_, err := app.DB().NewQuery(`UPDATE {{leafen_person_steps}} SET [[kept_at]] = {:now}, [[updated_at]] = {:now} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": id, "now": now.UnixMilli()}).Execute()
	return err
}

func newPersonStepID() string {
	return "lps_" + security.RandomString(20)
}

// personStepFieldsJSON is a step's fields as kept, in a steady order.
func personStepFieldsJSON(fields []string) string {
	fields = slices.Clone(fields)
	slices.Sort(fields)
	encoded, _ := json.Marshal(slices.Compact(fields))
	return string(encoded)
}

// Migration 848 makes the table.

func ensureNativePersonStepsSchema(app core.App) error {
	for _, statement := range []string{
		`CREATE TABLE {{leafen_person_steps}} (
			[[id]] TEXT PRIMARY KEY NOT NULL,
			[[owner_id]] TEXT NOT NULL,
			[[connection_id]] TEXT NOT NULL DEFAULT '',
			[[connection_name]] TEXT NOT NULL DEFAULT '',
			[[operation]] TEXT NOT NULL,
			[[path]] TEXT NOT NULL,
			[[body]] TEXT NOT NULL DEFAULT '',
			[[fields]] TEXT NOT NULL DEFAULT '[]',
			[[keep]] TEXT NOT NULL DEFAULT '',
			[[kept_at]] INTEGER NOT NULL DEFAULT 0,
			[[state]] TEXT NOT NULL DEFAULT 'waiting' CHECK ([[state]] IN ('waiting', 'done', 'canceled')),
			[[failure_code]] TEXT NOT NULL DEFAULT '',
			[[failure]] TEXT NOT NULL DEFAULT '',
			[[created_at]] INTEGER NOT NULL,
			[[updated_at]] INTEGER NOT NULL,
			[[expires_at]] INTEGER NOT NULL
		)`,
		`CREATE INDEX [[idx_leafen_person_steps_owner]] ON {{leafen_person_steps}} ([[owner_id]], [[state]], [[created_at]])`,
	} {
		if _, err := app.DB().NewQuery(statement).Execute(); err != nil {
			return err
		}
	}
	return validateNativePersonStepsSchema(app)
}

func validateNativePersonStepsSchema(app core.App) error {
	var columns, indexes int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM pragma_table_info('leafen_person_steps') WHERE [[notnull]] = 1`).
		Row(&columns); err != nil {
		return err
	}
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM sqlite_master WHERE [[type]] = 'index'
		AND [[name]] = 'idx_leafen_person_steps_owner'`).Row(&indexes); err != nil {
		return err
	}
	if columns != 16 || indexes != 1 {
		return errors.New(errorCodeSchemaMigrationHistoryInvalid)
	}
	return nil
}
