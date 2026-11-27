package app

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"
)

// The backup protection's settings (scheduled_backup_protection.go): the
// server keeps its backup key sealed with its administrator settings key
// and the record of what wraps it (migration 548); an administrator makes a
// recovery code, sets a password, or drops one of the two while the other
// remains; a place that leaves the machine — S3, WebDAV, a cloud sync
// folder — takes no backup until one exists.

var (
	errBackupProtectionRequired = errors.New("a backup place off this machine needs a recovery code or a password")
	errBackupProtectionLast     = errors.New("the last way to open the backups can't be dropped")
	errBackupOpenerWrong        = errors.New("that recovery code or password opens no backup key at the place")
	backupProtectionMu          sync.Mutex
)

// scheduledBackupSecretOf is what seals the server's backups: the
// environment's password where it sets the schedule, else the server's
// backup key with its record, and the password older backups were sealed
// by; none leaves them unsealed.
func scheduledBackupSecretOf(app core.App, stored storedScheduledBackup) (scheduledBackupSecret, error) {
	return scheduledBackupSecretWith(stored, administratorDecrypter(app))
}

// scheduledBackupSecretWith is that secret with the saved ones opened by
// decrypt.
func scheduledBackupSecretWith(stored storedScheduledBackup, decrypt func(string) (string, error)) (scheduledBackupSecret, error) {
	if stored.SetByEnvironment {
		return scheduledBackupSecret{Password: os.Getenv(scheduledBackupPasswordEnv), Code: os.Getenv(scheduledBackupRecoveryCodeEnv)}, nil
	}
	secret := scheduledBackupSecret{}
	password, err := decrypt(stored.PasswordCipher)
	if err != nil {
		return secret, err
	}
	secret.Password = password
	if stored.MasterCipher == "" {
		return secret, nil
	}
	encoded, err := decrypt(stored.MasterCipher)
	if err != nil {
		return secret, err
	}
	if secret.Master, err = hex.DecodeString(encoded); err != nil || len(secret.Master) != 32 {
		return secret, errors.New("the server's backup key is not valid")
	}
	record := storedBackupKeyRecord{}
	if err := json.Unmarshal([]byte(stored.KeyRecord), &record); err != nil || record.ID == "" || record.Wraps == nil {
		return secret, errors.New("the server's backup key record is not valid")
	}
	secret.Record = &record.scheduledBackupKeyRecord
	secret.Opened = record.Opened
	return secret, nil
}

// storedBackupKeyRecord is the key record as the server keeps it: a key
// opened at another server's place says so, and the place never sees the
// mark (it reads the record alone).
type storedBackupKeyRecord struct {
	scheduledBackupKeyRecord
	Opened bool `json:"opened,omitempty"`
}

// backupKeyOpened is whether the server's key was opened at another server's
// place and not yet chosen to write there.
func backupKeyOpened(stored storedScheduledBackup) bool {
	record := storedBackupKeyRecord{}
	return stored.KeyRecord != "" && json.Unmarshal([]byte(stored.KeyRecord), &record) == nil && record.Opened
}

// sealingBackupSecretOf is what seals what the server writes to its backup
// place: nothing goes to a place off this machine unsealed, whoever set the
// schedule.
func sealingBackupSecretOf(app core.App, stored storedScheduledBackup) (scheduledBackupSecret, error) {
	secret, err := scheduledBackupSecretOf(app, stored)
	if err == nil && secret.empty() && backupProtectionRequired(stored) {
		return secret, errBackupProtectionRequired
	}
	return secret, err
}

// backupProtectionRequired is a place that leaves the machine: a phone's
// picked folder counts as one, as its picker offers the phone's cloud
// drives beside its own storage.
func backupProtectionRequired(stored storedScheduledBackup) bool {
	switch stored.Target {
	case scheduledBackupTargetS3, scheduledBackupTargetWebDAV:
		return true
	case scheduledBackupTargetFolder:
		return syncFolderReplicaPlace(stored.Folder) || deviceBackupFolderNamed(stored.Folder)
	}
	return false
}

// backupProtectionWays says which ways open the server's backups.
func backupProtectionWays(stored storedScheduledBackup) (code, password bool) {
	if stored.SetByEnvironment {
		return os.Getenv(scheduledBackupRecoveryCodeEnv) != "", os.Getenv(scheduledBackupPasswordEnv) != ""
	}
	record := scheduledBackupKeyRecord{}
	if stored.KeyRecord == "" || json.Unmarshal([]byte(stored.KeyRecord), &record) != nil {
		return false, false
	}
	for _, wrap := range record.Wraps {
		code = code || wrap.By == scheduledBackupWrapCode
		password = password || wrap.By == scheduledBackupWrapPassword
	}
	return code, password
}

func projectBackupProtection(stored storedScheduledBackup) administratorBackupProtection {
	code, password := backupProtectionWays(stored)
	return administratorBackupProtection{
		Code: code, Password: password, Required: backupProtectionRequired(stored), Opened: backupKeyOpened(stored),
	}
}

// rewrapScheduledBackupKey sets the server's backup key's wrap by one way,
// making the key the first time; secret "" drops that way while another
// remains. Nothing already sealed is sealed again.
func rewrapScheduledBackupKey(app core.App, by, given string) error {
	backupProtectionMu.Lock()
	defer backupProtectionMu.Unlock()
	stored, err := loadScheduledBackup(app)
	if err != nil {
		return err
	}
	secret, err := scheduledBackupSecretOf(app, stored)
	if err != nil {
		return err
	}
	master, record := secret.Master, scheduledBackupKeyRecord{}
	if secret.Record != nil {
		record = *secret.Record
	}
	if len(master) == 0 {
		if given == "" {
			return errBackupProtectionLast
		}
		if master, record, err = newScheduledBackupMaster(); err != nil {
			return err
		}
	}
	wraps := []scheduledBackupKeyWrap{}
	for _, wrap := range record.Wraps {
		if wrap.By != by {
			wraps = append(wraps, wrap)
		}
	}
	if given != "" {
		wrap, err := wrapScheduledBackupKey(master, record.ID, by, given)
		if err != nil {
			return err
		}
		wraps = append(wraps, wrap)
	}
	if len(wraps) == 0 {
		return errBackupProtectionLast
	}
	record.Wraps = wraps
	masterCipher, err := encryptAdministratorSetting(app, cipherScheduledBackup, hex.EncodeToString(master))
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_scheduled_backups}} SET [[backup_master_cipher]] = {:master}, [[backup_key_record]] = {:record},
			[[revision]] = [[revision]] + 1
		WHERE [[id]] = 'default'
	`).Bind(dbx.Params{"master": masterCipher, "record": string(encoded)}).Execute()
	return err
}

// publishBackupKeyRecord writes the server's key record to its backup place
// at once, so a new recovery code or password opens the backups there, and
// one dropped or replaced stops, without waiting for the next backup; a
// place out of reach takes it with its next backup.
func publishBackupKeyRecord(ctx context.Context, app core.App) {
	stored, err := loadScheduledBackup(app)
	if err != nil || stored.SetByEnvironment || !stored.targetSet() {
		return
	}
	secret, err := scheduledBackupSecretOf(app, stored)
	if err != nil || len(secret.Master) == 0 {
		return
	}
	store, err := openScheduledBackupStore(ctx, app, stored)
	if err == nil {
		defer closeScheduledBackupStore(store)
		if err = store.MakeDir(scheduledBackupRootName); err == nil {
			_, err = scheduledBackupKeyIn(store, scheduledBackupRootName, secret, true)
		}
	}
	if err != nil {
		app.Logger().Warn("backup protection: the backup place takes the new key record with its next backup", "error", err)
	}
}

// convertPasswordBackups gives a server whose backups a password alone
// sealed a backup key the same password opens, once: what it seals next
// takes a recovery code too, and its older backups still open with the
// password.
func convertPasswordBackups(app core.App) error {
	stored, err := loadScheduledBackup(app)
	if err != nil || stored.SetByEnvironment || stored.PasswordCipher == "" || stored.MasterCipher != "" {
		return err
	}
	password, err := decryptAdministratorSetting(app, stored.PasswordCipher)
	if err != nil || password == "" {
		return err
	}
	return rewrapScheduledBackupKey(app, scheduledBackupWrapPassword, password)
}

func backupProtectionError(event *core.RequestEvent, err error) error {
	switch {
	case errors.Is(err, errScheduledBackupPasswordWeak):
		return apiError(event, http.StatusBadRequest, errorCodeBackupPasswordWeak,
			"Choose a longer backup password: at least 12 characters, not a common one. A short sentence works well.")
	case errors.Is(err, errBackupProtectionLast):
		return apiError(event, http.StatusConflict, errorCodeBackupProtectionLast,
			"The backups need a recovery code or a password; add the other before dropping this one.")
	case errors.Is(err, errBackupOpenerWrong):
		return apiError(event, http.StatusBadRequest, errorCodeBackupOpenerWrong,
			"That recovery code or password doesn't open the backups here.")
	case errors.Is(err, errBackupProtectionRequired):
		return apiError(event, http.StatusConflict, errorCodeBackupProtectionRequired,
			"A backup place off this computer needs a recovery code or a password first.")
	}
	return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorSettingsSaveFailed, "Couldn't save the backup protection.", err)
}

func backupProtectionRoutesAllowed(event *core.RequestEvent) (storedScheduledBackup, bool, error) {
	if !authorizeAdministratorRequest(event) {
		return storedScheduledBackup{}, false, nil
	}
	stored, err := loadScheduledBackup(event.App)
	if err != nil {
		return stored, false, apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the backup schedule.", err)
	}
	if stored.SetByEnvironment {
		return stored, false, apiError(event, http.StatusConflict, errorCodeScheduledBackupSetByEnvironment, "The server's environment sets the backup schedule.")
	}
	return stored, true, nil
}

// createBackupRecoveryCode makes a recovery code, shown this once; one made
// before stops opening the backups.
func createBackupRecoveryCode(event *core.RequestEvent) error {
	if _, ok, err := backupProtectionRoutesAllowed(event); !ok {
		return err
	}
	code, _, err := newScheduledBackupCode()
	if err == nil {
		err = rewrapScheduledBackupKey(event.App, scheduledBackupWrapCode, code)
	}
	if err != nil {
		return backupProtectionError(event, err)
	}
	publishBackupKeyRecord(event.Request.Context(), event.App)
	saved, err := loadScheduledBackup(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the backup schedule.", err)
	}
	notifyScheduledBackupRunner(event.App)
	return event.JSON(http.StatusOK, administratorBackupRecoveryCode{Code: code, Schedule: projectScheduledBackupWithRestore(event.App, saved)})
}

// setBackupPassword sets the password that opens the backups.
func setBackupPassword(event *core.RequestEvent) error {
	if _, ok, err := backupProtectionRoutesAllowed(event); !ok {
		return err
	}
	input := administratorBackupPasswordInput{}
	if err := event.BindBody(&input); err != nil || len(input.Password) > maxScheduledBackupPasswordLength {
		return apiError(event, http.StatusBadRequest, errorCodeScheduledBackupInputInvalid, "The backup password is not valid.")
	}
	if input.Password == "" {
		return refuseMissingInput(event, errorCodeScheduledBackupInputInvalid, "password", "the backup password, at least 12 characters.")
	}
	if err := checkBackupPassword(input.Password); err != nil {
		return backupProtectionError(event, err)
	}
	if err := rewrapScheduledBackupKey(event.App, scheduledBackupWrapPassword, input.Password); err != nil {
		return backupProtectionError(event, err)
	}
	publishBackupKeyRecord(event.Request.Context(), event.App)
	notifyScheduledBackupRunner(event.App)
	return answerSchedule(event)
}

// dropBackupProtection drops the recovery code or the password while the
// other opens the backups.
func dropBackupProtection(event *core.RequestEvent) error {
	if _, ok, err := backupProtectionRoutesAllowed(event); !ok {
		return err
	}
	by := event.Request.PathValue("way")
	if by != scheduledBackupWrapCode && by != scheduledBackupWrapPassword {
		return apiError(event, http.StatusBadRequest, errorCodeScheduledBackupInputInvalid, "The backup protection is not valid.")
	}
	// A key's call asks its person only for a way it would take away
	// (user_key_acting_ask.go): not the last.
	if userKeyAsksWhereItActs(event) {
		if err := backupProtectionDropRefused(event.App, by); err != nil {
			return backupProtectionError(event, err)
		}
		if asked, err := askBeforeActing(event); asked {
			return err
		}
	}
	if err := rewrapScheduledBackupKey(event.App, by, ""); err != nil {
		return backupProtectionError(event, err)
	}
	publishBackupKeyRecord(event.Request.Context(), event.App)
	return answerSchedule(event)
}

// backupProtectionDropRefused is what dropping a way backups are protected
// would refuse, read without changing anything: the last way there is, as
// rewrapScheduledBackupKey refuses it.
func backupProtectionDropRefused(app core.App, by string) error {
	stored, err := loadScheduledBackup(app)
	if err != nil {
		return err
	}
	secret, err := scheduledBackupSecretOf(app, stored)
	if err != nil {
		return err
	}
	if len(secret.Master) > 0 && secret.Record != nil {
		for _, wrap := range secret.Record.Wraps {
			if wrap.By != by {
				return nil
			}
		}
	}
	return errBackupProtectionLast
}

// backupCodeCommand prints a fresh recovery code, for a schedule the
// environment sets (LEAFEN_BACKUP_RECOVERY_CODE).
func backupCodeCommand() *cobra.Command {
	return &cobra.Command{
		Use:          "backup-code",
		Short:        "Print a new recovery code for " + scheduledBackupRecoveryCodeEnv,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, _ []string) error {
			code, _, err := newScheduledBackupCode()
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(command.OutOrStdout(), code)
			return err
		},
	}
}

// adoptBackupKey makes the backup key a recovery code or a password opens at
// the place this server's own, as a server restoring another's backups
// needs: the newest key it opens; a key a password alone made keeps that
// password instead. A key opened at another server's place (opened) is
// marked so: the server reads and restores with it but writes and prunes
// nothing there until a person chooses it does (2026-11-07: opening a lost
// server's backups and not restoring had this server's backups pruning
// the lost one's).
func adoptBackupKey(app core.App, store scheduledBackupStore, given string, opened bool) error {
	backupProtectionMu.Lock()
	defer backupProtectionMu.Unlock()
	records, err := readScheduledBackupKeyRecords(store, scheduledBackupStoreJoin(scheduledBackupRootName, scheduledBackupKeysName))
	if err != nil {
		return err
	}
	secret := scheduledBackupSecret{Code: given, Password: given}
	for at := len(records) - 1; at >= 0; at-- {
		key, master := openScheduledBackupRecord(records[at], secret)
		if key == nil {
			continue
		}
		if master == nil {
			cipher, err := encryptAdministratorSetting(app, cipherScheduledBackup, given)
			if err != nil {
				return err
			}
			_, err = app.DB().NewQuery(`UPDATE {{leafen_scheduled_backups}} SET [[password_cipher]] = {:cipher},
				[[revision]] = [[revision]] + 1 WHERE [[id]] = 'default'`).Bind(dbx.Params{"cipher": cipher}).Execute()
			return err
		}
		masterCipher, err := encryptAdministratorSetting(app, cipherScheduledBackup, hex.EncodeToString(master))
		if err != nil {
			return err
		}
		_, err = app.DB().NewQuery(`UPDATE {{leafen_scheduled_backups}} SET [[backup_master_cipher]] = {:master},
			[[backup_key_record]] = {:record}, [[revision]] = [[revision]] + 1 WHERE [[id]] = 'default'`).Bind(dbx.Params{
			"master": masterCipher, "record": string(mustJSON(storedBackupKeyRecord{records[at], opened})),
		}).Execute()
		return err
	}
	return errBackupOpenerWrong
}

// openBackupsWith opens the backups at the place with a recovery code or a
// password a person gives.
func openBackupsWith(event *core.RequestEvent) error {
	stored, ok, err := backupProtectionRoutesAllowed(event)
	if !ok {
		return err
	}
	input := administratorBackupOpenInput{}
	if err := event.BindBody(&input); err != nil || input.Secret == "" || len(input.Secret) > maxScheduledBackupPasswordLength {
		return apiError(event, http.StatusBadRequest, errorCodeScheduledBackupInputInvalid, "Give the backups' recovery code or password.")
	}
	if !stored.targetSet() {
		return apiError(event, http.StatusConflict, errorCodeScheduledBackupInputInvalid, "Choose where backups go first.")
	}
	store, err := openScheduledBackupStore(event.Request.Context(), event.App, stored)
	if err != nil {
		return apiErrorFrom(event, http.StatusServiceUnavailable, errorCodeScheduledBackupFolderUnavailable, "Couldn't read the backups.", err)
	}
	defer closeScheduledBackupStore(store)
	// Backups this server couldn't open before are another server's.
	current, err := scheduledBackupSecretOf(event.App, stored)
	if err != nil {
		return backupProtectionError(event, err)
	}
	opened, err := scheduledBackupPlaceForeign(store, scheduledBackupRootName, current)
	if err != nil {
		return apiErrorFrom(event, http.StatusServiceUnavailable, errorCodeScheduledBackupFolderUnavailable, "Couldn't read the backups.", err)
	}
	if err := adoptBackupKey(event.App, store, strings.TrimSpace(input.Secret), opened); err != nil {
		if errors.Is(err, errBackupOpenerWrong) {
			// A password with spaces at its ends is tried as given.
			err = adoptBackupKey(event.App, store, input.Secret, opened)
		}
		if err != nil {
			return backupProtectionError(event, err)
		}
	}
	notifyScheduledBackupRunner(event.App)
	return answerSchedule(event)
}

// writeBackupsHere is a person choosing that this server writes its own
// backups at the place whose key it opened (another server's backups stay
// there, counted among the ones kept).
func writeBackupsHere(event *core.RequestEvent) error {
	stored, ok, err := backupProtectionRoutesAllowed(event)
	if !ok {
		return err
	}
	record := storedBackupKeyRecord{}
	if backupKeyOpened(stored) && json.Unmarshal([]byte(stored.KeyRecord), &record) == nil {
		record.Opened = false
		// Chosen, it backs up there now, as "back up now" does.
		if _, err := event.App.DB().NewQuery(`UPDATE {{leafen_scheduled_backups}} SET [[backup_key_record]] = {:record},
			[[run_requested]] = [[enabled]], [[revision]] = [[revision]] + 1 WHERE [[id]] = 'default'`).
			Bind(dbx.Params{"record": string(mustJSON(record))}).Execute(); err != nil {
			return backupProtectionError(event, err)
		}
		notifyScheduledBackupRunner(event.App)
	}
	return answerSchedule(event)
}

func registerBackupProtectionRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+"/admin/backups/protection/code", createBackupRecoveryCode)
	event.Router.PUT(apiRoot+"/admin/backups/protection/password", setBackupPassword)
	event.Router.DELETE(apiRoot+"/admin/backups/protection/{way}", dropBackupProtection)
	event.Router.POST(apiRoot+"/admin/backups/protection/open", openBackupsWith)
	event.Router.POST(apiRoot+"/admin/backups/protection/write-here", writeBackupsHere)
	if err := convertPasswordBackups(event.App); err != nil {
		event.App.Logger().Warn("backup protection: couldn't give the password-encrypted backups a backup key", "error", err)
	}
}
