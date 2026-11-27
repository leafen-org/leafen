package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const formSubmissionAdmissionCause = "FORM_SUBMISSION_RATE_LIMIT"

type storedFormSubmissionAdmissionWindow struct {
	KeyDigest string `db:"key_digest"`
	ResetAt   int64  `db:"reset_at"`
}

type storedFormSubmissionAdmissionSummary struct {
	Count     int `db:"logical_count"`
	Duplicate int `db:"duplicate"`
}

func admitFormSubmission(
	app core.App,
	address string,
	input formSubmitRequest,
) (bool, error) {
	if requestLimitProfileIsDevelopment(os.Getenv(requestLimitProfileEnvironment)) {
		return true, nil
	}
	keyDigest := formSubmissionAdmissionDigest(
		formActionSubmit, strings.TrimSpace(address), input.Token,
	)
	logicalDigest := formSubmissionAdmissionDigest(input.RequestID)
	now := time.Now().UTC().UnixMilli()
	resetAt := now + int64(formSubmissionAdmissionWindowSeconds)*1000
	admitted := false
	err := app.RunInTransaction(func(txApp core.App) error {
		if err := cleanupExpiredFormSubmissionAdmission(txApp, now); err != nil {
			return err
		}
		window := storedFormSubmissionAdmissionWindow{}
		err := txApp.DB().NewQuery(`
			SELECT [[key_digest]], [[reset_at]]
			FROM {{leafen_form_submission_admission_windows}}
			WHERE [[key_digest]] = {:keyDigest}
		`).Bind(dbx.Params{"keyDigest": keyDigest}).One(&window)
		if errors.Is(err, sql.ErrNoRows) {
			return createFormSubmissionAdmissionWindow(
				txApp, keyDigest, logicalDigest, resetAt, now, &admitted,
			)
		}
		if err != nil {
			return err
		}
		if window.ResetAt <= now {
			if _, err := txApp.DB().NewQuery(`
				DELETE FROM {{leafen_form_submission_admission_windows}}
				WHERE [[key_digest]] = {:keyDigest}
			`).Bind(dbx.Params{"keyDigest": keyDigest}).Execute(); err != nil {
				return err
			}
			return createFormSubmissionAdmissionWindow(
				txApp, keyDigest, logicalDigest, resetAt, now, &admitted,
			)
		}
		summary := storedFormSubmissionAdmissionSummary{}
		if err := txApp.DB().NewQuery(`
			SELECT COUNT(*) AS [[logical_count]],
				COALESCE(MAX([[logical_digest]] = {:logicalDigest}), 0) AS [[duplicate]]
			FROM {{leafen_form_submission_admission_logical_ids}}
			WHERE [[key_digest]] = {:keyDigest}
		`).Bind(dbx.Params{
			"keyDigest": keyDigest, "logicalDigest": logicalDigest,
		}).One(&summary); err != nil {
			return err
		}
		if summary.Duplicate == 1 {
			admitted = true
			return nil
		}
		if summary.Count >= maxFormSubmissionAdmission {
			return nil
		}
		if err := insertFormSubmissionAdmissionLogicalID(
			txApp, keyDigest, logicalDigest, now,
		); err != nil {
			return err
		}
		admitted = true
		return nil
	})
	return admitted, err
}

func createFormSubmissionAdmissionWindow(
	app core.App,
	keyDigest, logicalDigest string,
	resetAt, now int64,
	admitted *bool,
) error {
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_form_submission_admission_windows}}
			([[key_digest]], [[reset_at]], [[created_at]])
		VALUES ({:keyDigest}, {:resetAt}, {:now})
	`).Bind(dbx.Params{
		"keyDigest": keyDigest, "resetAt": resetAt, "now": now,
	}).Execute(); err != nil {
		return err
	}
	if err := insertFormSubmissionAdmissionLogicalID(
		app, keyDigest, logicalDigest, now,
	); err != nil {
		return err
	}
	*admitted = true
	return nil
}

func insertFormSubmissionAdmissionLogicalID(
	app core.App,
	keyDigest, logicalDigest string,
	now int64,
) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_form_submission_admission_logical_ids}}
			([[key_digest]], [[logical_digest]], [[created_at]])
		VALUES ({:keyDigest}, {:logicalDigest}, {:now})
	`).Bind(dbx.Params{
		"keyDigest": keyDigest, "logicalDigest": logicalDigest, "now": now,
	}).Execute()
	return err
}

func cleanupExpiredFormSubmissionAdmission(app core.App, now int64) error {
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_form_submission_admission_windows}}
		WHERE [[key_digest]] IN (
			SELECT [[key_digest]]
			FROM {{leafen_form_submission_admission_windows}}
			WHERE [[reset_at]] <= {:now}
			ORDER BY [[reset_at]], [[key_digest]]
			LIMIT {:limit}
		)
	`).Bind(dbx.Params{
		"now": now, "limit": maxFormSubmissionAdmissionCleanupWindow,
	}).Execute()
	return err
}

func formSubmissionAdmissionDigest(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}
