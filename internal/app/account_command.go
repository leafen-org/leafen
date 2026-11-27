package app

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"
)

// `leafen account reset-password` (password recovery on a server that sends
// no mail, 2026-10-20): whoever runs the server's program on its data folder
// — the machine's or the container's administrator, who can read the whole
// folder anyway — gives any account a one-time password, which its person
// replaces at the next sign-in. The account's sign-ins end and the server
// audit log keeps the reset. Two-step sign-in stays unless asked to go too:
// a lost authenticator is the one thing a new password doesn't replace, and
// the person sets it up again afterwards.
//
// `leafen account device-code` (the multi-server decision of 2026-10-20:
// one who lost every device gets a new code from the server's command line)
// gives the same person a one-time code that signs one device in, as a code
// from a signed-in device does, and the audit log keeps it too.

const (
	accountAuditPasswordResetCommand = "password-reset-command"
	accountAuditDeviceCodeCommand    = "device-code-command"
)

func accountCommand(pb *pocketbase.PocketBase) *cobra.Command {
	command := &cobra.Command{Use: "account", Short: "Help an account on this server's data folder back in"}
	command.AddCommand(accountResetPasswordCommand(pb))
	command.AddCommand(accountDeviceCodeCommand(pb))
	return command
}

func accountDeviceCodeCommand(pb *pocketbase.PocketBase) *cobra.Command {
	var email, name, id string
	command := &cobra.Command{
		Use:   "device-code",
		Short: "Give an account a one-time code that logs a device in",
		Long: "Makes a one-time code that logs one device in to the account with --email, --name or --id, as a code from a " +
			"logged-in device does: for ten minutes, once. Open the address it shows on that device, or choose \"Log in with a code\" on the " +
			"server's login page and type the code. Works while the server runs.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			user, err := accountForCommand(pb, email, name, id)
			if err != nil {
				return err
			}
			code, err := deviceCodeFromCommand(pb, user)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "A code that logs one device in to %s, for ten minutes: %s\n", accountLabel(user), code.Code)
			if code.URL != "" {
				fmt.Fprintf(out, "Open on that device: %s\n", code.URL)
			} else {
				fmt.Fprintln(out, "On that device, open the server's login page, choose \"Log in with a code\" and type it.")
			}
			return nil
		},
	}
	command.Flags().StringVar(&email, "email", "", "the account's email")
	command.Flags().StringVar(&name, "name", "", "the account's name, where no other account has it")
	command.Flags().StringVar(&id, "id", "", "the account's ID")
	command.MarkFlagsOneRequired("email", "name", "id")
	command.MarkFlagsMutuallyExclusive("email", "name", "id")
	return command
}

// accountForCommand finds the account a command names by its email, its name
// (one only no other account has) or its ID.
func accountForCommand(app core.App, email, name, id string) (*core.Record, error) {
	switch {
	case strings.TrimSpace(email) != "":
		user, err := findAccountByEmail(app, strings.TrimSpace(email))
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("no account on this server has the email %s", strings.TrimSpace(email))
		}
		return user, err
	case strings.TrimSpace(id) != "":
		user, err := app.FindRecordById("users", strings.TrimSpace(id))
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("no account on this server has the ID %s", strings.TrimSpace(id))
		}
		return user, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		// An empty name names no account, though PocketBase's filter
		// matches it to the accounts without a name (since v0.40.2).
		return nil, errors.New("name the account with --email, --name or --id")
	}
	users, err := app.FindRecordsByFilter("users", "name = {:name}", "created", 0, 0, dbx.Params{"name": name})
	if err != nil {
		return nil, err
	}
	switch len(users) {
	case 0:
		return nil, fmt.Errorf("no account on this server has the name %s", name)
	case 1:
		return users[0], nil
	}
	lines := make([]string, 0, len(users))
	for _, user := range users {
		lines = append(lines, "  "+nameWithEmail(user.Id, user.Email(), " "))
	}
	return nil, fmt.Errorf("%d accounts have the name %s; name one with --id:\n%s", len(users), name, strings.Join(lines, "\n"))
}

// deviceCodeFromCommand makes the code and records that the command line did.
func deviceCodeFromCommand(app core.App, user *core.Record) (accountDeviceCodeResponse, error) {
	state, err := loadAdministratorAccountState(app, user.Id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return accountDeviceCodeResponse{}, err
	}
	if state.Status == administratorAccountStatusDeleting || state.Status == administratorAccountStatusDeleted {
		return accountDeviceCodeResponse{}, errors.New("the account is being deleted")
	}
	if state.Status == administratorAccountStatusDisabled {
		return accountDeviceCodeResponse{}, errors.New("the account is turned off; an administrator turns it on again first")
	}
	switch err := authorizeDeviceSignIn(app, user.Id); {
	case errors.Is(err, errSAMLRequired):
		return accountDeviceCodeResponse{}, errors.New("the account's organization logs it in with its single sign-on, not a device code")
	case errors.Is(err, errDeviceSignInRestricted):
		return accountDeviceCodeResponse{}, errors.New("the account's organization asks for a password and 2-step verification, not a device code; reset-password gives it a one-time password")
	case err != nil:
		return accountDeviceCodeResponse{}, err
	}
	code, err := issueAccountDeviceCode(app, user.Id, "", "", deviceCodeCommandOrigin(app))
	if err != nil {
		return code, err
	}
	err = recordAdministratorAccountAudit(app, "", user.Id, accountAuditDeviceCodeCommand,
		state.Status, state.Status, state.Revision, "command-"+core.GenerateDefaultRandomId(), time.Now().UTC().UnixMilli())
	return code, err
}

// deviceCodeCommandOrigin is the address the code's link opens: the one the
// command was given, else the one the running server says it is at — from
// anywhere where it has one, else on this computer (first_run_state.go), so
// an agent can hand its person a link (user decision 2026-11-13: signing a
// device in goes through the conversation too).
func deviceCodeCommandOrigin(app core.App) string {
	if origin := configuredPublicOrigin(); origin != "" {
		return origin
	}
	facts := readSetupState(app.DataDir()).Facts
	if facts.Outside != "" {
		return strings.TrimRight(facts.Outside, "/")
	}
	return strings.TrimRight(facts.Here, "/")
}

func accountResetPasswordCommand(pb *pocketbase.PocketBase) *cobra.Command {
	var email string
	var fromStdin, twoStepOff bool
	command := &cobra.Command{
		Use:   "reset-password",
		Short: "Give an account a one-time password, replaced at its next login",
		Long: "Gives the account with --email a one-time password and ends its logins; its person chooses a new password at the next " +
			"login. Without --password-stdin the password is made and shown once. Works while the server runs.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var given *string
			if fromStdin {
				line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if err != nil && !errors.Is(err, io.EOF) {
					return err
				}
				line = strings.TrimRight(line, "\r\n")
				given = &line
			}
			password, made, err := administratorTemporaryPassword(given)
			if err != nil {
				return fmt.Errorf("the password needs %d characters, or %d with both letters and digits", authPasswordLongEnough, minimumAuthPasswordLength)
			}
			reset, err := resetAccountPasswordFromCommand(pb, email, password, twoStepOff)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			// Its person is told by mail at once, before the command ends (2026-10-26: nothing went).
			changes := []accountSecurityChange{securityChangePasswordResetBy}
			if twoStepOff {
				changes = append(changes, securityChangeTwoStepOff)
			}
			for _, change := range changes {
				send := accountSecurityNoticeMail(pb, reset.userID, change,
					accountSecurityNoticeFacts{ByCommandLine: true, Origin: configuredPublicOrigin()}, nil)
				if send != nil && send() != nil {
					fmt.Fprintln(cmd.ErrOrStderr(), "The account's security notice couldn't be mailed; the server's log says why.")
				}
			}
			fmt.Fprintf(out, "%s logs in with a one-time password now and chooses a new one then.\n", strings.TrimSpace(email))
			if made != nil {
				fmt.Fprintf(out, "One-time password: %s\n", *made)
			}
			fmt.Fprintf(out, "Logins ended: %d.\n", reset.count)
			if twoStepOff {
				fmt.Fprintln(out, "2-step verification is off for the account; it can turn it on again in its settings.")
			}
			return nil
		},
	}
	command.Flags().StringVar(&email, "email", "", "the account's email")
	command.Flags().BoolVar(&fromStdin, "password-stdin", false, "read the one-time password from standard input instead of making one")
	command.Flags().BoolVar(&twoStepOff, "turn-off-two-step", false, "also turn off the account's 2-step verification, for a lost authenticator")
	_ = command.MarkFlagRequired("email")
	return command
}

// resetAccountPasswordFromCommand sets the password, ends the sign-ins,
// asks for a new password at the next one and records it; it answers the
// account and how many sign-ins ended.
func resetAccountPasswordFromCommand(app core.App, email, password string, twoStepOff bool) (commandPasswordReset, error) {
	email = strings.TrimSpace(email)
	user, err := findAccountByEmail(app, email)
	if errors.Is(err, sql.ErrNoRows) {
		return commandPasswordReset{}, fmt.Errorf("no account on this server has the email %s", email)
	}
	if err != nil {
		return commandPasswordReset{}, err
	}
	var ended int64
	// Hashed before the transaction (password_outside_transaction.go).
	prepared := preparePassword(app, "users", password)
	err = app.RunInTransaction(func(txApp core.App) error {
		current, err := txApp.FindRecordById("users", user.Id)
		if err != nil {
			return err
		}
		state, err := loadAdministratorAccountState(txApp, current.Id)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if state.Status == administratorAccountStatusDeleting || state.Status == administratorAccountStatusDeleted {
			return errors.New("the account is being deleted")
		}
		prepared.set(current)
		if err := txApp.Save(current); err != nil {
			return err
		}
		if err := markAccountPasswordKnown(txApp, current.Id); err != nil {
			return err
		}
		if ended, err = revokeAllBrowserSessionsCount(txApp, current.Id); err != nil {
			return err
		}
		// No account issued it; the audit log says the command line did.
		if err := setMustChangePasswordInTransaction(txApp, current.Id, current.Id, accountPasswordReasonReset); err != nil {
			return err
		}
		if twoStepOff {
			for _, table := range []string{authMFAChallengesTable, authMFARecoveryCodesTable, authMFAFactorsTable} {
				if _, err := txApp.DB().Delete(table, dbx.HashExp{"owner_id": current.Id}).Execute(); err != nil {
					return err
				}
			}
		}
		now := time.Now().UTC().UnixMilli()
		revision := state.Revision
		if state.UserID != "" {
			revision++
			if _, err := txApp.DB().NewQuery(`UPDATE {{leafen_administrator_account_states}}
				SET [[revision]]=[[revision]]+1,[[updated_by]]='',[[updated_at]]={:now} WHERE [[user_id]]={:userId}`).
				Bind(dbx.Params{"now": now, "userId": current.Id}).Execute(); err != nil {
				return err
			}
		}
		// Each run is its own request: the log keeps one event per account and request.
		return recordAdministratorAccountAudit(txApp, "", current.Id, accountAuditPasswordResetCommand,
			state.Status, state.Status, revision, "command-"+core.GenerateDefaultRandomId(), now)
	})
	return commandPasswordReset{userID: user.Id, count: ended}, err
}

type commandPasswordReset struct {
	userID string
	count  int64
}
