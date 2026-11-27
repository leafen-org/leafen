package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

var errSetupAlreadyClaimed = errors.New(errorCodeSetupClosed)

func registerAdministratorRoutes(
	event *core.ServeEvent,
	mutations *administratorMutationLifecycle,
) {
	event.Router.GET(apiRoot+"/setup/status", readAdministratorSetupStatus)
	event.Router.POST(apiRoot+"/setup/claim", mutations.Wrap(errorCodeSetupClaimFailed, "Couldn't create the administrator.", claimAdministrator))
	registerSetupRestoreRoutes(event)
	event.Router.GET(apiRoot+"/admin/settings", readAdministratorSettings)
	event.Router.PATCH(apiRoot+"/admin/settings", mutations.Wrap(errorCodeAdministratorSettingsSaveFailed, "Couldn't save the administrator settings.", updateAdministratorSettings))
	event.Router.POST(apiRoot+"/admin/auth-providers/{provider}/return", mutations.Wrap(errorCodeAdministratorSettingsSaveFailed, "Couldn't save the administrator settings.", setAuthProviderReturnAdded))
	registerAdministratorSMTPTestRoutes(event)
}

func readAdministratorSetupStatus(event *core.RequestEvent) error {
	configured, err := administratorConfiguredContext(event.Request.Context(), event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the setup state.", err)
	}
	status := administratorSetupStatus{Configured: configured, CodeRequired: !configured && setupCodeAsked(event),
		// Before the first administrator, the setup screen says backups are off (backup_off_warning.go).
		BackupsOff: !configured && scheduledBackupsOff(event.App)}
	if !configured {
		// A restore from a folder at setup names the container's (scheduled_backup_targets.go).
		status.ContainerFolder = scheduledBackupContainerFolder()
	}
	if status.CodeRequired {
		// Opened from outside: the screen names where the server opens without a code.
		status.HomeAddress = setupHomeAddress(event.Request)
	}
	return event.JSON(http.StatusOK, status)
}

func administratorConfigured(app core.App) (bool, error) {
	return administratorConfiguredContext(context.Background(), app)
}

func administratorConfiguredContext(ctx context.Context, app core.App) (bool, error) {
	var setupAdminID string
	err := app.DB().NewQuery(`
		SELECT [[setup_admin_id]] FROM {{leafen_server_settings}} WHERE [[id]] = 'default'
	`).WithContext(ctx).Row(&setupAdminID)
	return setupAdminID != "", err
}

// claimAdministrator makes the first administrator from a name — from
// outside the server's network once the setup code opens it (setup_code.go);
// an email and a password are optional, set later in the account's settings
// as a backup sign-in. The device that sets the server up is its first
// registered one, signed in until it is signed out (account_device_codes.go).
func claimAdministrator(event *core.RequestEvent) error {
	requestContext := event.Request.Context()
	input := administratorSetupInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the administrator details.")
	}
	if !admitFirstSetup(event, input.SetupCode) {
		return nil
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || utf8.RuneCountInString(input.Name) > maxAuthNameLength {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the administrator details.")
	}
	if strings.TrimSpace(input.Email) != "" {
		email, emailOK := normalizeEmailIdentity(input.Email)
		if !emailOK {
			return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the administrator details.")
		}
		input.Email = email
	} else {
		input.Email = ""
	}
	passwordKnown := input.Password != "" || input.PasswordConfirm != ""
	if passwordKnown {
		if !authPasswordAcceptable(input.Password) || input.Password != input.PasswordConfirm {
			return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the administrator details.")
		}
	} else {
		// No password to sign in with until the person sets one.
		input.Password = security.RandomString(48)
	}

	// The screen that sent this claim learns it set the server up from the
	// moment it is (setup_claims.go).
	claimed := setupClaimsOf(event.App).begin(input.RequestID)
	administrator, err := claimSetupAdministrator(requestContext, event.App, input, passwordKnown)
	claimed(err == nil)
	if errors.Is(err, errSetupAlreadyClaimed) {
		return apiError(event, http.StatusConflict, errorCodeSetupClosed, "Server setup is already complete.")
	}
	if requestContext.Err() != nil || errors.Is(err, context.Canceled) {
		return apiErrorAndFlush(event, http.StatusServiceUnavailable, errorCodeSetupClaimFailed, "Couldn't create the administrator.")
	}
	if err != nil {
		return apiError(event, http.StatusConflict, errorCodeSetupClaimFailed, "Couldn't create the administrator.")
	}
	// Whoever set the server up goes on to the first run's steps (first_run_state.go).
	setupKeeperOf(event.App).begin(event.App, administrator.Id)
	// The language the person chose on the setup's first screen is their
	// account's, over one a command chose before (the multi-server decision
	// of 2026-10-19, 1): the language step is not asked again. One the
	// account can't take is left, and the step asks.
	if input.Language != "" {
		_ = saveSetupLanguage(event.App, administrator.Id, input.Language)
	}
	return writeAdministratorSession(event, administrator, claimFirstPage(event.App, administrator, input.Language))
}

// claimFirstPage makes the first administrator's first workspace in the
// language chosen on the setup's first screen, as an agent's start does
// (workspace_initial.go), so the claim's one answer carries the account, its
// sign-in and the page it opens on: nothing the screen still has to make
// stands between the server being set up and the sign-in kept (2026-11-19).
// "Follow the device" and a language the account can't take are left to the
// screen, which knows the device's language and asks.
func claimFirstPage(app core.App, administrator *core.Record, language string) string {
	if _, supported := supportedAccountLanguagePreferences[language]; !supported || language == accountLanguageSystem {
		return ""
	}
	page, err := startFirstWorkspace(app, administrator, language)
	if err != nil {
		// The screen makes it then, as before.
		app.Logger().Warn("leafen setup first workspace failed", "error", err)
		return ""
	}
	return page
}

// claimSetupAdministrator makes the server's first administrator, once: by
// the setup page, or by a packaged client's own server for its local owner.
func claimSetupAdministrator(ctx context.Context, app core.App, input administratorSetupInput, passwordKnown bool) (*core.Record, error) {
	var administrator *core.Record
	// Hashed before the transaction (password_outside_transaction.go).
	password := preparePassword(app, "users", input.Password)
	err := app.RunInTransaction(func(txApp core.App) error {
		configured, err := administratorConfiguredContext(ctx, txApp)
		if err != nil {
			return err
		}
		if configured {
			return errSetupAlreadyClaimed
		}
		if _, err := findAccountByEmail(txApp, input.Email); err == nil {
			return errors.New(errorCodeAccountExists)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		users, err := txApp.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}
		administrator = core.NewRecord(users)
		administrator.SetEmail(input.Email)
		password.set(administrator)
		administrator.Set("name", input.Name)
		administrator.SetVerified(true)
		if err := txApp.Save(administrator); err != nil {
			return err
		}
		if !passwordKnown {
			if err := markAccountPasswordUnknown(txApp, administrator.Id); err != nil {
				return err
			}
		}

		// A server set up from now on starts with sign-up closed: only the
		// invited, people of a workspace's allowed domains and an
		// organization's SSO make accounts (2026-10-18). A server set up
		// before keeps the policy it has.
		result, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_server_settings}}
			SET [[setup_admin_id]] = {:userId}, [[signup_policy]] = {:closed},
				[[revision]] = [[revision]] + 1, [[updated_at]] = {:now}
			WHERE [[id]] = 'default' AND [[setup_admin_id]] = ''
		`).WithContext(ctx).
			Bind(dbx.Params{"userId": administrator.Id, "closed": administratorSignupPolicyClosed, "now": time.Now().UnixMilli()}).
			Execute()
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return refusedUnlessStorage(errSetupAlreadyClaimed, err)
		}
		now := time.Now().UnixMilli()
		_, err = txApp.DB().Insert(administratorsTable, dbx.Params{
			"user_id": administrator.Id, "created_at": now,
		}).WithContext(ctx).Execute()
		if err != nil {
			return err
		}
		return recordAdministratorAccountAudit(txApp, administrator.Id, administrator.Id, "setup", "",
			administratorAccountStatusActive, 1, "setup", now)
	})
	return administrator, err
}

// writeAdministratorSession signs the first administrator in, with the page
// their first workspace opens on when the same request made it.
func writeAdministratorSession(event *core.RequestEvent, user *core.Record, firstPage string) error {
	session, err := issueAuthenticatedBrowserSession(
		event.App, user, browserSessionMetadataFromEvent(event), false,
		authSessionMethodDevice, "",
	)
	if err != nil {
		return browserSessionAPIError(event, err)
	}
	// The first sign-in is in the records too, as setup (2026-10-26: the
	// records began at a later device). The server is claimed by now, so a
	// record that fails is logged rather than keeping its first
	// administrator out.
	if _, err := recordAuthAttempt(event.App, authAuditAttempt{
		OwnerID: user.Id, TargetEmail: user.Email(), Method: authAuditMethodSetup, Phase: authAuditPhaseVerify,
		Outcome: authAuditOutcomeSuccess, Metadata: browserSessionMetadataFromEvent(event),
	}); err != nil {
		event.App.Logger().Warn("leafen setup sign-in audit failed", "error", err)
	}
	return event.JSON(http.StatusCreated, administratorSession{
		Token: session.Token,
		User: administratorSessionUser{
			ID: user.Id, Email: user.Email(), Name: user.GetString("name"), Role: administratorRole,
		},
		FirstPage: firstPage,
	})
}

func readAdministratorSettings(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	settings, err := loadAdministratorSettings(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the administrator settings.", err)
	}
	return event.JSON(http.StatusOK, projectAdministratorSettings(event.App, settings, authProviderReturnOrigin(event)))
}

func authorizeAdministratorRequest(event *core.RequestEvent) bool {
	user, ok := authenticatedUser(event)
	if !ok {
		_ = apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
		return false
	}
	administrator, err := isAdministrator(event.App, user.Id)
	if err != nil {
		_ = apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't check administrator permission.", err)
		return false
	}
	if !administrator {
		_ = apiError(event, http.StatusNotFound, errorCodeAdministratorNotFound, "Administrator settings not found.")
		return false
	}
	noteAdministratorAddress(event, user.Id)
	return true
}

func isAdministrator(app core.App, userID string) (bool, error) {
	var count int
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_administrators}} WHERE [[user_id]] = {:userId}
	`).Bind(dbx.Params{"userId": userID}).Row(&count)
	return count == 1, err
}

// serverAdministrators lists the server's administrators, whom its notices
// go to.
func serverAdministrators(app core.App) ([]string, error) {
	administrators := []string{}
	err := app.DB().NewQuery(`SELECT [[user_id]] FROM {{leafen_administrators}} ORDER BY [[user_id]]`).Column(&administrators)
	return administrators, err
}
