package app

import (
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Where and how often the server backs itself up: to a folder, a WebDAV
// server or S3-compatible storage. The settings live in one row an
// administrator changes; a server set up without its screen takes them from
// LEAFEN_BACKUP_FOLDER, or else LEAFEN_BACKUP_WEBDAV_URL with
// LEAFEN_BACKUP_WEBDAV_USERNAME and LEAFEN_BACKUP_WEBDAV_PASSWORD, or else
// LEAFEN_BACKUP_S3_BUCKET with LEAFEN_BACKUP_S3_ENDPOINT, _REGION, _PREFIX,
// _ACCESS_KEY, _SECRET_KEY and _PATH_STYLE (any of them turns backups on),
// LEAFEN_BACKUP_INTERVAL (hours, or "6h", "2d", "daily", "weekly") and
// LEAFEN_BACKUP_KEEP (snapshots kept), and then the screen only shows them.
const (
	scheduledBackupFolderEnv         = "LEAFEN_BACKUP_FOLDER"
	scheduledBackupWebDAVURLEnv      = "LEAFEN_BACKUP_WEBDAV_URL"
	scheduledBackupWebDAVUsernameEnv = "LEAFEN_BACKUP_WEBDAV_USERNAME"
	scheduledBackupWebDAVPasswordEnv = "LEAFEN_BACKUP_WEBDAV_PASSWORD"
	scheduledBackupS3EndpointEnv     = "LEAFEN_BACKUP_S3_ENDPOINT"
	scheduledBackupS3RegionEnv       = "LEAFEN_BACKUP_S3_REGION"
	scheduledBackupS3BucketEnv       = "LEAFEN_BACKUP_S3_BUCKET"
	scheduledBackupS3PrefixEnv       = "LEAFEN_BACKUP_S3_PREFIX"
	scheduledBackupS3AccessKeyEnv    = "LEAFEN_BACKUP_S3_ACCESS_KEY"
	scheduledBackupS3SecretKeyEnv    = "LEAFEN_BACKUP_S3_SECRET_KEY"
	scheduledBackupS3PathStyleEnv    = "LEAFEN_BACKUP_S3_PATH_STYLE"
	scheduledBackupIntervalEnv       = "LEAFEN_BACKUP_INTERVAL"
	scheduledBackupKeepEnv           = "LEAFEN_BACKUP_KEEP"
	scheduledBackupPasswordEnv       = "LEAFEN_BACKUP_PASSWORD"
	scheduledBackupRecoveryCodeEnv   = "LEAFEN_BACKUP_RECOVERY_CODE"
	scheduledBackupDefaultHours      = 24
	scheduledBackupDefaultKeep       = 7
	// How long saving a WebDAV server waits for it to answer the check.
	scheduledBackupWebDAVCheckTimeout = 30 * time.Second
)

var errScheduledBackupConflict = errors.New(errorCodeScheduledBackupConflict)

type storedScheduledBackup struct {
	Revision           int    `db:"revision"`
	Enabled            bool   `db:"enabled"`
	Target             string `db:"target"`
	Folder             string `db:"folder"`
	WebDAVURL          string `db:"webdav_url"`
	WebDAVUsername     string `db:"webdav_username"`
	WebDAVPassword     string `db:"webdav_password_cipher"`
	S3Endpoint         string `db:"s3_endpoint"`
	S3Region           string `db:"s3_region"`
	S3Bucket           string `db:"s3_bucket"`
	S3Prefix           string `db:"s3_prefix"`
	S3AccessKey        string `db:"s3_access_key"`
	S3Secret           string `db:"s3_secret_cipher"`
	S3PathStyle        bool   `db:"s3_path_style"`
	IntervalHours      int    `db:"interval_hours"`
	Keep               int    `db:"keep_count"`
	NextRunAt          int64  `db:"next_run_at"`
	RunRequested       bool   `db:"run_requested"`
	RunningSince       int64  `db:"running_since"`
	LastFinishedAt     int64  `db:"last_finished_at"`
	LastSuccessAt      int64  `db:"last_success_at"`
	LastErrorCode      string `db:"last_error_code"`
	LastSnapshot       string `db:"last_snapshot"`
	LastSnapshotSHA256 string `db:"last_snapshot_sha256"`
	LastAddedBytes     int64  `db:"last_added_bytes"`
	// The last run as it ended (scheduled_backup_last_run.go).
	LastRunJSON    string `db:"last_run_json"`
	SnapshotCount  int    `db:"snapshot_count"`
	FailureCount   int    `db:"failure_count"`
	PasswordCipher string `db:"password_cipher"`
	// When an administrator dismissed the band suggesting backups.
	SuggestionDismissedAt int64 `db:"suggestion_dismissed_at"`
	// Whether changes also go out continuously (replica_runner.go).
	ReplicationEnabled bool `db:"replication_enabled"`
	// Whether this server follows another's replica as its standby, takes
	// over by itself, and the primary's address (replica_standby.go).
	StandbyEnabled       bool   `db:"standby_enabled"`
	StandbyAutoTakeover  bool   `db:"standby_auto_takeover"`
	StandbyPrimaryOrigin string `db:"standby_primary_origin"`
	// The server's backup key, sealed, and the record of what wraps it
	// (scheduled_backup_protection_settings.go).
	MasterCipher     string `db:"backup_master_cipher"`
	KeyRecord        string `db:"backup_key_record"`
	SetByEnvironment bool   `db:"-"`
}

// loadScheduledBackup is the row as it applies now: the environment's
// place, interval and count over the saved ones when it names a place.
func loadScheduledBackup(app core.App) (storedScheduledBackup, error) {
	stored := storedScheduledBackup{}
	err := app.DB().NewQuery(`
		SELECT [[revision]], [[enabled]], [[target]], [[folder]], [[webdav_url]], [[webdav_username]],
			[[webdav_password_cipher]], [[s3_endpoint]], [[s3_region]], [[s3_bucket]], [[s3_prefix]], [[s3_access_key]],
			[[s3_secret_cipher]], [[s3_path_style]], [[interval_hours]], [[keep_count]], [[next_run_at]],
			[[run_requested]], [[running_since]], [[last_finished_at]], [[last_success_at]], [[last_error_code]],
			[[last_snapshot]], [[last_snapshot_sha256]], [[last_added_bytes]], [[last_run_json]], [[snapshot_count]], [[failure_count]], [[password_cipher]],
			[[suggestion_dismissed_at]], [[replication_enabled]], [[standby_enabled]], [[standby_auto_takeover]],
			[[standby_primary_origin]], [[backup_master_cipher]], [[backup_key_record]]
		FROM {{leafen_scheduled_backups}} WHERE [[id]] = 'default'
	`).One(&stored)
	if err != nil {
		return stored, err
	}
	folder := strings.TrimSpace(os.Getenv(scheduledBackupFolderEnv))
	address := strings.TrimSpace(os.Getenv(scheduledBackupWebDAVURLEnv))
	bucket := strings.TrimSpace(os.Getenv(scheduledBackupS3BucketEnv))
	if folder != "" || address != "" || bucket != "" {
		stored.SetByEnvironment, stored.Enabled = true, true
		switch {
		case folder != "":
			stored.Target, stored.Folder = scheduledBackupTargetFolder, folder
		case address != "":
			stored.Target, stored.WebDAVURL = scheduledBackupTargetWebDAV, address
			stored.WebDAVUsername = strings.TrimSpace(os.Getenv(scheduledBackupWebDAVUsernameEnv))
		default:
			stored.Target, stored.S3Bucket = scheduledBackupTargetS3, bucket
			stored.S3Endpoint = strings.TrimSpace(os.Getenv(scheduledBackupS3EndpointEnv))
			stored.S3Region = strings.TrimSpace(os.Getenv(scheduledBackupS3RegionEnv))
			stored.S3Prefix = strings.TrimSpace(os.Getenv(scheduledBackupS3PrefixEnv))
			stored.S3AccessKey = strings.TrimSpace(os.Getenv(scheduledBackupS3AccessKeyEnv))
			stored.S3PathStyle, _ = strconv.ParseBool(strings.TrimSpace(os.Getenv(scheduledBackupS3PathStyleEnv)))
		}
		stored.IntervalHours = scheduledBackupIntervalFrom(os.Getenv(scheduledBackupIntervalEnv))
		stored.Keep = scheduledBackupKeepFrom(os.Getenv(scheduledBackupKeepEnv))
	}
	return stored, nil
}

// scheduledBackupIntervalFrom reads an interval in hours: "12", "12h", "2d",
// "hourly", "daily" or "weekly"; anything else is a day.
func scheduledBackupIntervalFrom(raw string) int {
	if hours, ok := parseScheduledBackupInterval(raw); ok {
		return hours
	}
	return scheduledBackupDefaultHours
}

func parseScheduledBackupInterval(raw string) (int, bool) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "hourly":
		return 1, true
	case "daily", "":
		return scheduledBackupDefaultHours, true
	case "weekly":
		return 7 * 24, true
	}
	unit := 1
	if strings.HasSuffix(value, "d") {
		unit, value = 24, strings.TrimSuffix(value, "d")
	} else {
		value = strings.TrimSuffix(value, "h")
	}
	hours, err := strconv.Atoi(value)
	if err != nil || hours*unit < 1 || hours*unit > maxScheduledBackupIntervalHours {
		return 0, false
	}
	return hours * unit, true
}

func scheduledBackupKeepFrom(raw string) int {
	if keep, ok := parseScheduledBackupKeep(raw); ok {
		return keep
	}
	return scheduledBackupDefaultKeep
}

func parseScheduledBackupKeep(raw string) (int, bool) {
	keep, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || keep < 1 || keep > maxScheduledBackupKeep {
		return 0, false
	}
	return keep, true
}

func registerScheduledBackupRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/admin/backups/schedule", getScheduledBackup)
	event.Router.PUT(apiRoot+"/admin/backups/schedule", updateScheduledBackup)
	event.Router.POST(apiRoot+"/admin/backups/schedule/run", runScheduledBackupNow)
	event.Router.POST(apiRoot+"/admin/backups/schedule/suggestion/dismiss", dismissScheduledBackupSuggestion)
	registerScheduledBackupRestoreRoutes(event)
	registerBackupProtectionRoutes(event)
	registerServerMoveRoutes(event)
}

func getScheduledBackup(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	stored, err := loadScheduledBackup(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the backup schedule.", err)
	}
	return event.JSON(http.StatusOK, administratorScheduledBackupResult{Schedule: projectScheduledBackupWithRestore(event.App, stored)})
}

func updateScheduledBackup(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	input := administratorScheduledBackupInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeScheduledBackupInputInvalid, "The backup schedule is not valid.")
	}
	input.Folder = strings.TrimSpace(input.Folder)
	input.WebDAVURL, input.WebDAVUsername = strings.TrimSpace(input.WebDAVURL), strings.TrimSpace(input.WebDAVUsername)
	input.S3Endpoint, input.S3Region = strings.TrimSpace(input.S3Endpoint), strings.TrimSpace(input.S3Region)
	input.S3Bucket, input.S3AccessKey = strings.TrimSpace(input.S3Bucket), strings.TrimSpace(input.S3AccessKey)
	input.S3Prefix = strings.Trim(strings.TrimSpace(input.S3Prefix), "/")
	if input.Target == "" {
		input.Target = scheduledBackupTargetFolder
	}
	if input.S3Region == "" && input.S3Bucket != "" {
		input.S3Region = "us-east-1"
	}
	wanted := storedScheduledBackup{Target: input.Target, Folder: input.Folder, WebDAVURL: input.WebDAVURL,
		S3Endpoint: input.S3Endpoint, S3Bucket: input.S3Bucket}
	if input.IntervalHours < 1 || input.IntervalHours > maxScheduledBackupIntervalHours ||
		input.Keep < 1 || input.Keep > maxScheduledBackupKeep || len(input.Folder) > maxScheduledBackupFolderLength ||
		(input.Target != scheduledBackupTargetFolder && input.Target != scheduledBackupTargetWebDAV &&
			input.Target != scheduledBackupTargetS3) ||
		len(input.S3Endpoint) > maxScheduledBackupWebDAVURLLength || len(input.S3Region) > maxScheduledBackupS3FieldLength ||
		len(input.S3Bucket) > maxScheduledBackupS3FieldLength || len(input.S3Prefix) > maxScheduledBackupS3FieldLength ||
		len(input.S3AccessKey) > maxScheduledBackupS3FieldLength || len(input.S3SecretKey) > maxScheduledBackupPasswordLength ||
		(input.S3SecretKey != "" && input.ClearS3Secret) ||
		len(input.WebDAVURL) > maxScheduledBackupWebDAVURLLength || len(input.WebDAVUsername) > maxScheduledBackupWebDAVUsernameLength ||
		len(input.WebDAVPassword) > maxScheduledBackupPasswordLength || (input.WebDAVPassword != "" && input.ClearWebDAVPassword) ||
		(input.Enabled && !wanted.targetSet()) || (input.Password != "" && input.ClearPassword) ||
		len(input.Password) > maxScheduledBackupPasswordLength {
		return apiError(event, http.StatusBadRequest, errorCodeScheduledBackupInputInvalid, "The backup schedule is not valid.")
	}
	// A password given here wraps the backups' key as the protection's own
	// route does (scheduled_backup_protection_settings.go).
	if input.Password != "" {
		if err := checkBackupPassword(input.Password); err != nil {
			return backupProtectionError(event, err)
		}
	}
	current, err := loadScheduledBackup(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the backup schedule.", err)
	}
	if current.SetByEnvironment {
		return apiError(event, http.StatusConflict, errorCodeScheduledBackupSetByEnvironment, "The server's environment sets the backup schedule.")
	}
	if input.Revision != current.Revision {
		return apiError(event, http.StatusConflict, errorCodeScheduledBackupConflict, "The backup schedule changed elsewhere.")
	}
	// A place off this machine takes no backup until a recovery code or a
	// password opens what it holds; the last of the two stays.
	hasCode, hasPassword := backupProtectionWays(current)
	if input.ClearPassword && hasPassword && !hasCode {
		return backupProtectionError(event, errBackupProtectionLast)
	}
	if input.Enabled && backupProtectionRequired(wanted) && !hasCode && input.Password == "" && (!hasPassword || input.ClearPassword) {
		return backupProtectionError(event, errBackupProtectionRequired)
	}
	folderTarget := input.Target == scheduledBackupTargetFolder
	s3Target := input.Target == scheduledBackupTargetS3
	if folderTarget && input.Folder != "" && (input.Enabled || input.Folder != current.Folder) {
		if err := checkScheduledBackupFolder(event.App, input.Folder); err != nil {
			return apiError(event, http.StatusBadRequest, errorCodeScheduledBackupFolderInvalid, scheduledBackupFolderRefusalFor(input.Folder))
		}
	}
	// The password that sealed older backups stays to open them; a new
	// password wraps the backup key instead.
	passwordCipher := current.PasswordCipher
	webdavCipher, err := replacedScheduledBackupSecret(event.App, current.WebDAVPassword, input.WebDAVPassword, input.ClearWebDAVPassword)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorSettingsSaveFailed, "Couldn't save the backup schedule.", err)
	}
	webdavChanged := input.WebDAVURL != current.WebDAVURL || input.WebDAVUsername != current.WebDAVUsername ||
		webdavCipher != current.WebDAVPassword
	s3Cipher, err := replacedScheduledBackupSecret(event.App, current.S3Secret, input.S3SecretKey, input.ClearS3Secret)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorSettingsSaveFailed, "Couldn't save the backup schedule.", err)
	}
	s3Changed := input.S3Endpoint != current.S3Endpoint || input.S3Region != current.S3Region ||
		input.S3Bucket != current.S3Bucket || input.S3Prefix != current.S3Prefix ||
		input.S3AccessKey != current.S3AccessKey || s3Cipher != current.S3Secret || input.S3PathStyle != current.S3PathStyle
	// A place off this machine is written to once as the save turns backups
	// on; one saved while they're off is kept as given and tried as they
	// turn on (scheduled_backup_place_check.go).
	if input.Enabled {
		if answered, err := refuseScheduledBackupPlace(event, input, current); answered {
			return err
		}
	}
	// A schedule turned on, sent elsewhere, or sealed another way backs up
	// right away.
	moved := input.Target != current.Target || (folderTarget && input.Folder != current.Folder) ||
		(input.Target == scheduledBackupTargetWebDAV && webdavChanged) || (s3Target && s3Changed)
	startNow := input.Enabled && (!current.Enabled || moved || passwordCipher != current.PasswordCipher)
	result, err := event.App.DB().NewQuery(`
		UPDATE {{leafen_scheduled_backups}} SET [[revision]] = [[revision]] + 1, [[enabled]] = {:enabled},
			[[target]] = {:target}, [[folder]] = {:folder}, [[webdav_url]] = {:webdavURL},
			[[webdav_username]] = {:webdavUsername}, [[webdav_password_cipher]] = {:webdavCipher},
			[[s3_endpoint]] = {:s3Endpoint}, [[s3_region]] = {:s3Region}, [[s3_bucket]] = {:s3Bucket},
			[[s3_prefix]] = {:s3Prefix}, [[s3_access_key]] = {:s3AccessKey}, [[s3_secret_cipher]] = {:s3Cipher},
			[[s3_path_style]] = {:s3PathStyle},
			[[interval_hours]] = {:interval}, [[keep_count]] = {:keep}, [[updated_at]] = {:now},
			[[password_cipher]] = {:passwordCipher},
			[[next_run_at]] = CASE WHEN {:startNow} THEN 0 ELSE [[next_run_at]] END
		WHERE [[id]] = 'default' AND [[revision]] = {:revision}
	`).Bind(dbx.Params{
		"enabled": input.Enabled, "target": input.Target, "folder": input.Folder, "webdavURL": input.WebDAVURL,
		"webdavUsername": input.WebDAVUsername, "webdavCipher": webdavCipher, "interval": input.IntervalHours,
		"keep": input.Keep, "now": time.Now().UnixMilli(), "startNow": startNow, "revision": input.Revision,
		"passwordCipher": passwordCipher, "s3Endpoint": input.S3Endpoint, "s3Region": input.S3Region,
		"s3Bucket": input.S3Bucket, "s3Prefix": input.S3Prefix, "s3AccessKey": input.S3AccessKey, "s3Cipher": s3Cipher,
		"s3PathStyle": input.S3PathStyle,
	}).Execute()
	if err == nil {
		var changed int64
		if changed, err = result.RowsAffected(); err == nil && changed != 1 {
			err = errScheduledBackupConflict
		}
	}
	if errors.Is(err, errScheduledBackupConflict) {
		return apiError(event, http.StatusConflict, errorCodeScheduledBackupConflict, "The backup schedule changed elsewhere.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorSettingsSaveFailed, "Couldn't save the backup schedule.", err)
	}
	if input.Password != "" {
		err = rewrapScheduledBackupKey(event.App, scheduledBackupWrapPassword, input.Password)
	} else if input.ClearPassword && hasPassword {
		err = rewrapScheduledBackupKey(event.App, scheduledBackupWrapPassword, "")
	}
	if err != nil {
		return backupProtectionError(event, err)
	}
	if input.Password != "" || input.ClearPassword {
		publishBackupKeyRecord(event.Request.Context(), event.App)
	}
	notifyScheduledBackupRunner(event.App)
	saved, err := loadScheduledBackup(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the backup schedule.", err)
	}
	// Turned on, sent elsewhere or sealed another way, it backs up at once:
	// the answer says so (user 2026-11-22).
	return event.JSON(http.StatusOK, administratorScheduledBackupResult{Schedule: projectScheduledBackupWithRestore(event.App, saved),
		BackupStarted: startNow})
}

// runScheduledBackupNow asks for a backup now, as the schedule makes one.
// One already running starts nothing more: the answer says so, with how far
// it has got (2026-11-22: asked while the first backup ran, a second one
// followed it at once).
func runScheduledBackupNow(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	current, err := loadScheduledBackup(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the backup schedule.", err)
	}
	if !current.Enabled || !current.targetSet() {
		return apiError(event, http.StatusConflict, errorCodeScheduledBackupInputInvalid, "Turn on backups first.")
	}
	if running := runningBackupProgress(event.App, backupProgressStoreKey).snapshot(); running != nil && running.Kind == backupProgressKindBackup {
		return event.JSON(http.StatusOK, administratorScheduledBackupResult{Schedule: projectScheduledBackupWithRestore(event.App, current),
			AlreadyRunning: true})
	}
	if _, err := event.App.DB().NewQuery(`UPDATE {{leafen_scheduled_backups}} SET [[run_requested]] = 1 WHERE [[id]] = 'default'`).
		Execute(); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorSettingsSaveFailed, "Couldn't ask for a backup.", err)
	}
	notifyScheduledBackupRunner(event.App)
	current.RunRequested = true
	return event.JSON(http.StatusAccepted, administratorScheduledBackupResult{Schedule: projectScheduledBackupWithRestore(event.App, current)})
}

func projectScheduledBackup(stored storedScheduledBackup) administratorScheduledBackup {
	at := func(milliseconds int64) string {
		if milliseconds <= 0 {
			return ""
		}
		return time.UnixMilli(milliseconds).UTC().Format(time.RFC3339)
	}
	next := ""
	if stored.Enabled && stored.targetSet() {
		next = at(max(stored.NextRunAt, 1))
		if stored.RunRequested || stored.NextRunAt == 0 {
			next = time.Now().UTC().Format(time.RFC3339)
		}
	}
	target := stored.Target
	if target == "" {
		target = scheduledBackupTargetFolder
	}
	webdavPasswordSet := stored.WebDAVPassword != "" ||
		(stored.SetByEnvironment && os.Getenv(scheduledBackupWebDAVPasswordEnv) != "")
	s3SecretSet := stored.S3Secret != "" || (stored.SetByEnvironment && os.Getenv(scheduledBackupS3SecretKeyEnv) != "")
	_, passwordWrapped := backupProtectionWays(stored)
	passwordWrapped = passwordWrapped || stored.PasswordCipher != ""
	return administratorScheduledBackup{
		Revision: stored.Revision, Enabled: stored.Enabled, Target: target, Folder: stored.Folder,
		WebDAVURL: stored.WebDAVURL, WebDAVUsername: stored.WebDAVUsername, WebDAVPasswordSet: webdavPasswordSet,
		S3Endpoint: stored.S3Endpoint, S3Region: stored.S3Region, S3Bucket: stored.S3Bucket, S3Prefix: stored.S3Prefix,
		S3AccessKey: stored.S3AccessKey, S3SecretSet: s3SecretSet, S3PathStyle: stored.S3PathStyle,
		IntervalHours: stored.IntervalHours, Keep: stored.Keep, SetByEnvironment: stored.SetByEnvironment,
		PasswordSet: passwordWrapped || (stored.SetByEnvironment && os.Getenv(scheduledBackupPasswordEnv) != ""),
		Protection:  projectBackupProtection(stored),
		Running:     stored.RunningSince > 0, NextRunAt: next, LastFinishedAt: at(stored.LastFinishedAt),
		LastSuccessAt: at(stored.LastSuccessAt), LastErrorCode: stored.LastErrorCode, FailureCount: stored.FailureCount,
		LastSnapshot: stored.LastSnapshot, LastSnapshotSHA256: stored.LastSnapshotSHA256,
		LastAddedBytes: stored.LastAddedBytes, LastRun: projectScheduledBackupLastRun(stored.LastRunJSON), Snapshots: stored.SnapshotCount,
		ContainerFolder: scheduledBackupContainerFolder(),
	}
}
