package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const administratorInstanceReportPath = "/admin/reports"

type storedAdministratorInstanceReport struct {
	Owner             string `db:"owner"`
	FormatVersion     int    `db:"format_version"`
	Revision          int    `db:"revision"`
	RequestID         string `db:"request_id"`
	RequestHash       string `db:"request_hash"`
	AuthorityRevision int64  `db:"authority_revision"`
	Users             int64  `db:"users_count"`
	Workspaces        int64  `db:"workspaces_count"`
	Sessions          int64  `db:"sessions_count"`
	Files             int64  `db:"files_count"`
	StorageBytes      int64  `db:"storage_bytes"`
	AuthActivity      int64  `db:"auth_activity_count"`
	CreatedAt         int64  `db:"created_at"`
}

func registerAdministratorInstanceReportRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+administratorInstanceReportPath, refreshAdministratorInstanceReport)
	event.Router.GET(apiRoot+administratorInstanceReportPath+"/current", currentAdministratorInstanceReport)
}

func refreshAdministratorInstanceReport(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	user, _ := authenticatedUser(event)
	input := administratorInstanceReportInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the report request details.")
	}
	input.RequestID = strings.TrimSpace(input.RequestID)
	if input.RequestID == "" || len([]rune(input.RequestID)) > administratorBackupRequestIDRunes || input.ExpectedRevision < 0 {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the report request details.")
	}
	requestHash := administratorInstanceReportRequestHash(input)
	replayed, conflict, identityReused := false, false, false
	stored := storedAdministratorInstanceReport{}
	err := event.App.RunInTransaction(func(txApp core.App) error {
		if err := requireAdministrator(txApp, user.Id); err != nil {
			return err
		}
		current, loadErr := loadStoredAdministratorInstanceReport(txApp, user.Id)
		if loadErr == nil {
			if current.RequestID == input.RequestID {
				if current.RequestHash != requestHash {
					identityReused = true
					return nil
				}
				stored, replayed = current, true
				return nil
			}
			if current.Revision != input.ExpectedRevision {
				conflict = true
				return nil
			}
		} else if !errors.Is(loadErr, sql.ErrNoRows) {
			return loadErr
		} else if input.ExpectedRevision != 0 {
			conflict = true
			return nil
		}

		now := time.Now().UTC().UnixMilli()
		nextRevision := input.ExpectedRevision + 1
		var result sql.Result
		var err error
		if errors.Is(loadErr, sql.ErrNoRows) {
			result, err = txApp.DB().NewQuery(`
				INSERT INTO {{leafen_instance_administration_reports}} (
					[[owner]], [[format_version]], [[revision]], [[request_id]], [[request_hash]],
					[[authority_revision]], [[users_count]], [[workspaces_count]], [[sessions_count]],
					[[files_count]], [[storage_bytes]], [[auth_activity_count]], [[created_at]]
				)
				SELECT {:owner}, 1, {:revision}, {:requestId}, {:requestHash},
					[[revision]], [[users_count]], [[workspaces_count]], [[sessions_count]],
					[[files_count]], [[storage_bytes]], [[auth_activity_count]], {:createdAt}
				FROM {{leafen_instance_report_authority}} WHERE [[id]] = 'current'
			`).Bind(dbx.Params{
				"owner": user.Id, "revision": nextRevision, "requestId": input.RequestID,
				"requestHash": requestHash, "createdAt": now,
			}).Execute()
		} else {
			result, err = txApp.DB().NewQuery(`
				UPDATE {{leafen_instance_administration_reports}}
				SET ([[format_version]], [[revision]], [[request_id]], [[request_hash]],
					[[authority_revision]], [[users_count]], [[workspaces_count]], [[sessions_count]],
					[[files_count]], [[storage_bytes]], [[auth_activity_count]], [[created_at]]) =
					(SELECT 1, {:revision}, {:requestId}, {:requestHash},
						[[revision]], [[users_count]], [[workspaces_count]], [[sessions_count]],
						[[files_count]], [[storage_bytes]], [[auth_activity_count]], {:createdAt}
					 FROM {{leafen_instance_report_authority}} WHERE [[id]] = 'current')
				WHERE [[owner]] = {:owner} AND [[revision]] = {:expectedRevision}
			`).Bind(dbx.Params{
				"owner": user.Id, "revision": nextRevision, "expectedRevision": input.ExpectedRevision,
				"requestId": input.RequestID, "requestHash": requestHash, "createdAt": now,
			}).Execute()
		}
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed != 1 {
			conflict = true
			return nil
		}
		stored, err = loadStoredAdministratorInstanceReport(txApp, user.Id)
		return err
	})
	if identityReused {
		return apiError(event, http.StatusConflict, errorCodeAdministratorReportIdentityReused, "The report request ID was used for a different request.")
	}
	if conflict {
		return apiError(event, http.StatusConflict, errorCodeAdministratorReportConflict, "The report changed. Check its current state again.")
	}
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeAdministratorReportReadFailed, "Couldn't create the report.", "administrator-report.refresh", "snapshot", errorCodeAdministratorReportReadFailed, err)
	}
	return event.JSON(http.StatusOK, administratorInstanceReportResult{
		Replayed: replayed,
		Report:   administratorInstanceReportResponse(stored),
	})
}

func currentAdministratorInstanceReport(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	user, _ := authenticatedUser(event)
	stored, err := loadStoredAdministratorInstanceReport(event.App, user.Id)
	if errors.Is(err, sql.ErrNoRows) {
		return event.JSON(http.StatusOK, administratorInstanceReportCurrentResult{})
	}
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeAdministratorReportReadFailed, "Couldn't read the report.", "administrator-report.current", "read", errorCodeAdministratorReportReadFailed, err)
	}
	report := administratorInstanceReportResponse(stored)
	return event.JSON(http.StatusOK, administratorInstanceReportCurrentResult{Report: &report})
}

func loadStoredAdministratorInstanceReport(app core.App, owner string) (storedAdministratorInstanceReport, error) {
	stored := storedAdministratorInstanceReport{}
	err := app.DB().NewQuery(`
		SELECT [[owner]], [[format_version]], [[revision]], [[request_id]], [[request_hash]],
			[[authority_revision]], [[users_count]], [[workspaces_count]], [[sessions_count]],
			[[files_count]], [[storage_bytes]], [[auth_activity_count]], [[created_at]]
		FROM {{leafen_instance_administration_reports}} WHERE [[owner]] = {:owner}
	`).Bind(dbx.Params{"owner": owner}).One(&stored)
	return stored, err
}

func administratorInstanceReportResponse(stored storedAdministratorInstanceReport) administratorInstanceReport {
	return administratorInstanceReport{
		RequestID: stored.RequestID, Revision: stored.Revision,
		AuthorityRevision: stored.AuthorityRevision,
		Counts: administratorInstanceReportCounts{
			Users: stored.Users, Workspaces: stored.Workspaces, Sessions: stored.Sessions,
			Files: stored.Files, StorageBytes: stored.StorageBytes, AuthActivity: stored.AuthActivity,
		},
		CreatedAt: time.UnixMilli(stored.CreatedAt).UTC().Format(time.RFC3339Nano),
	}
}

func administratorInstanceReportRequestHash(input administratorInstanceReportInput) string {
	digest := sha256.Sum256([]byte("administrator-instance-report\x00" + input.RequestID + "\x00" + strconv.Itoa(input.ExpectedRevision)))
	return hex.EncodeToString(digest[:])
}
