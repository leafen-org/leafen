package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// refuseScheduledBackupPlace writes once to the place a save turns backups
// on for, off this machine: S3-compatible storage or a WebDAV server. A
// place that can't take a backup answers with the fields that are wrong;
// what is missing is asked for by name, not blamed on the address (user
// 2026-11-22: storage saved without its keys was refused as "the endpoint,
// region or bucket"). It answers whether it refused.
func refuseScheduledBackupPlace(event *core.RequestEvent, input administratorScheduledBackupInput, current storedScheduledBackup) (bool, error) {
	switch {
	case input.Target == scheduledBackupTargetS3 && input.S3Bucket != "":
		secret, err := givenOrSavedSecret(event.App, input.S3SecretKey, input.ClearS3Secret, current.S3Secret)
		if err != nil {
			return true, apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the backup schedule.", err)
		}
		if missing := s3BackupKeysMissing(input.S3AccessKey, secret); missing != "" {
			return true, apiError(event, http.StatusBadRequest, errorCodeScheduledBackupS3KeysMissing,
				"Enter the storage's "+missing+" to back up there.")
		}
		checked := storedScheduledBackup{S3Endpoint: input.S3Endpoint, S3Region: input.S3Region, S3Bucket: input.S3Bucket,
			S3Prefix: input.S3Prefix, S3AccessKey: input.S3AccessKey, S3PathStyle: input.S3PathStyle}
		if err := checkScheduledBackupS3(event.Request.Context(), checked, secret); errors.Is(err, errS3BackupUnauthorized) {
			return true, refusedByPlace(event, errorCodeScheduledBackupS3Unauthorized, "The storage refused the access key or secret key", err, secret)
		} else if err != nil {
			return true, refusedByPlace(event, errorCodeScheduledBackupS3Invalid, "The storage's endpoint, region or bucket can't be used to write backups", err, secret)
		}
	case input.Target == scheduledBackupTargetWebDAV && input.WebDAVURL != "":
		password, err := givenOrSavedSecret(event.App, input.WebDAVPassword, input.ClearWebDAVPassword, current.WebDAVPassword)
		if err != nil {
			return true, apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the backup schedule.", err)
		}
		err = checkScheduledBackupWebDAV(event.Request.Context(), event.App, input.WebDAVURL, input.WebDAVUsername, password)
		// A server that asks for what wasn't given asks for it; one given
		// both refused them. A server may take neither, so only its answer
		// tells that they were missing.
		if errors.Is(err, errWebDAVBackupUnauthorized) && (input.WebDAVUsername == "" || password == "") {
			missing := "a password: enter it"
			if input.WebDAVUsername == "" && password == "" {
				missing = "a user name and password: enter them"
			} else if input.WebDAVUsername == "" {
				missing = "a user name: enter it"
			}
			return true, apiError(event, http.StatusBadRequest, errorCodeScheduledBackupWebDAVCredentialsMissing,
				"The WebDAV server asks for "+missing+" to back up there.")
		}
		if errors.Is(err, errWebDAVBackupUnauthorized) {
			return true, refusedByPlace(event, errorCodeScheduledBackupWebDAVUnauthorized, "The WebDAV server refused the user or its access", err, password)
		}
		if err != nil {
			return true, refusedByPlace(event, errorCodeScheduledBackupWebDAVInvalid,
				"The WebDAV address must be an http or https address of a folder the server can reach and write to", err, password)
		}
	}
	return false, nil
}

// refusedByPlace answers a save the place refused, with what it answered
// and that answer's common cause beside the code (backup_failure_reason.go).
func refusedByPlace(event *core.RequestEvent, code, message string, err error, secret string) error {
	reason := backupFailureReason(err, secret)
	apiErrorBecause(event, reason, backupFailureCause(err))
	return apiError(event, http.StatusBadRequest, code, message+": "+reason+".")
}

// givenOrSavedSecret is the secret a save goes on with: the one typed, none
// when it is cleared, or the one saved before.
func givenOrSavedSecret(app core.App, given string, clear bool, saved string) (string, error) {
	if given != "" || clear {
		return given, nil
	}
	return decryptAdministratorSetting(app, saved)
}

// s3BackupKeysMissing names the keys storage needs that aren't there, or
// nothing when both are.
func s3BackupKeysMissing(accessKey, secretKey string) string {
	missing := []string{}
	if strings.TrimSpace(accessKey) == "" {
		missing = append(missing, "access key")
	}
	if secretKey == "" {
		missing = append(missing, "secret key")
	}
	return strings.Join(missing, " and ")
}
