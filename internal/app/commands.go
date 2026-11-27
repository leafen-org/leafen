package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pocketbase/pocketbase"
	pocketbasecmd "github.com/pocketbase/pocketbase/cmd"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func registerCommands(pb *pocketbase.PocketBase) {
	leafen := []*cobra.Command{
		dataFree(mcpCommand()),
		serverData(backupCommand(pb)),
		restoreCommand(pb),
		dataFree(backupCodeCommand()),
		dataFree(memoryLimitCommand()),
		appCommand(),
		serverData(tailscaleCommand(pb)),
		serverData(addressCommand(pb)),
		serverData(accountCommand(pb)),
		serverData(connectCommand(pb)),
		dataFree(setupCommand(pb)),
		dataFree(&cobra.Command{
			Use:   "doctor",
			Short: "Inspect the native Leafen runtime",
			Run: func(cmd *cobra.Command, _ []string) {
				slots, limit, source := plannedSQLiteLookaside()
				report := map[string]any{
					"status":     "ok",
					"generation": 2,
					"database":   "data.db",
					"processes":  1,
					// What a server here sets at its start (sqlite_lookaside.go).
					"sqliteLookaside": map[string]any{"slot": sqliteLookasideSlot, "slots": slots,
						"memoryLimitMiB": limit / memoryMiB, "limitFrom": source},
					// What the memory plan gives (memory_plan.go).
					"memoryPlan": serverMemoryPlan(),
					// Settings given that the server could not use (setting_refusals.go).
					"settingsNotUsed": settingRefusals(),
				}
				// A data folder the server's start refuses is said so, with
				// its reason (data_folder_doctor.go).
				if err := dataFolderRefusal(pb.DataDir()); err != nil {
					report["status"], report["reason"] = "refused", err.Error()
				}
				_ = json.NewEncoder(cmd.OutOrStdout()).Encode(report)
			},
		}),
	}
	// PocketBase silences its root's error output, since the program writes
	// the error a command returns itself (cmd/leafen); a line a Leafen
	// command writes to the person there — a notice that couldn't be mailed,
	// an app folder's warnings — goes to standard error (2026-11-17: the
	// reset-password command's "couldn't be mailed" line never showed).
	for _, command := range leafen {
		command.SetErr(os.Stderr)
	}
	describeRootCommand(pb.RootCmd)
	pb.RootCmd.AddCommand(pocketbasecmd.NewServeCommand(pb, false))
	pb.RootCmd.AddCommand(leafen...)
}

// describeRootCommand makes the program's own help and version Leafen's.
// PocketBase hides cobra's help command, so `help` was an unknown command
// (2026-11-19: `leafen help` and `leafen-desktop help`), and names itself:
// its root read "<program> CLI", --dir "the PocketBase data directory" and
// --version PocketBase's own, "(untracked)". The version is the build the
// server's start line names (server_build_start_line.go). The desktop app's
// program says what it does without a command (desktop/server_command.go).
func describeRootCommand(root *cobra.Command) {
	root.Short = "Leafen's server and its commands"
	root.Long = "Leafen's server: `serve` runs it, and the commands below set it up, back it up, restore it and connect agents to it, " +
		"working on the data folder --dir names."
	root.Version = serverBuildName(serverBuildStampOf())
	// nil gives back cobra's own `help [command]`, listed with the rest.
	root.SetHelpCommand(nil)
	if dir := root.PersistentFlags().Lookup("dir"); dir != nil {
		dir.Usage = "the folder Leafen keeps its data in"
	}
}

const dataFreeAnnotation = "leafenDataFree"

// dataFree marks a command that opens no data folder: it runs without the
// server's start, which makes one where it runs (2026-10-18: `leafen doctor`
// and the rest left data.db, auxiliary.db and .notify behind, and `leafen
// mcp` started beside a server opened that server's databases).
func dataFree(command *cobra.Command) *cobra.Command {
	if command.Annotations == nil {
		command.Annotations = map[string]string{}
	}
	command.Annotations[dataFreeAnnotation] = "true"
	return command
}

// isDataFree says whether a command, or the command it belongs to, is data-free.
func isDataFree(command *cobra.Command) bool {
	for ; command != nil; command = command.Parent() {
		if command.Annotations[dataFreeAnnotation] == "true" {
			return true
		}
	}
	return false
}

const serverDataAnnotation = "leafenServerData"

// serverData marks a command that works on a server's data folder as it is,
// beside the server or with it stopped: pointed at a folder that holds no
// Leafen data, it says which folder it looked in and makes nothing there,
// where the server's start would make a new, empty one (2026-11-17:
// `connect` given the wrong --dir made a database there and said the server
// wasn't set up yet).
func serverData(command *cobra.Command) *cobra.Command {
	if command.Annotations == nil {
		command.Annotations = map[string]string{}
	}
	command.Annotations[serverDataAnnotation] = "true"
	return command
}

// needsServerData says whether a command, or the command it belongs to, works on a server's data.
func needsServerData(command *cobra.Command) bool {
	for ; command != nil; command = command.Parent() {
		if command.Annotations[serverDataAnnotation] == "true" {
			return true
		}
	}
	return false
}

// holdsServerData says whether a folder holds a Leafen server's data.
func holdsServerData(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, dataDatabaseFile))
	return err == nil && info.Mode().IsRegular()
}

// NoServerDataError is a command on a server's data that found none in the
// folder it looked in, and made nothing there. The desktop app's program,
// which looks in the app's own data unless --dir names another, says it in
// its own words.
type NoServerDataError struct{ Dir string }

// Error says which folder the command looked in and how to name the server's own.
func (failure *NoServerDataError) Error() string {
	return fmt.Sprintf("no Leafen data in %s, and nothing was made there: name the folder the server keeps its data in "+
		"with --dir, the one its `leafen serve --dir` names (with Docker, run the command as `docker compose exec leafen leafen …`)", failure.Dir)
}

func noServerDataError(dir string) error {
	if absolute, err := filepath.Abs(dir); err == nil {
		dir = absolute
	}
	return &NoServerDataError{Dir: dir}
}

// commandNames are the commands registerCommands gives the server, with
// cobra's help (PocketBase turns its completion command off, so there is
// none); the program runs a command line that names one without a window
// (IsCommand).
var commandNames = []string{"serve", "mcp", "backup", "restore", "backup-code", "memory-limit", "app", "tailscale", "address",
	"account", "connect", "setup", "doctor", "help"}

// IsCommand says whether a command line is the server's: it names one of its
// commands, or starts with a flag, as `leafen --dev serve` does.
func IsCommand(args []string) bool {
	return len(args) > 0 && (strings.HasPrefix(args[0], "-") || slices.Contains(commandNames, args[0]))
}

// Execute runs the command line: a data-free command on its own, any other
// after the server's start. A line that doesn't read — an unknown command or
// flag, a flag without its value — ends with its error before the server's
// start, which makes the data folder (2026-10-29: `restore --file` left
// data.db, auxiliary.db and .notify in the empty folder it named), as does
// the program named alone, which says its help, and a command on a server's
// data pointed at a folder with none (serverData).
func Execute(pb *pocketbase.PocketBase) error {
	command, rest, err := pb.RootCmd.Find(os.Args[1:])
	if err != nil || command == pb.RootCmd || isDataFree(command) || !commandLineReads(command, rest) {
		return pb.RootCmd.Execute()
	}
	if needsServerData(command) && !holdsServerData(pb.DataDir()) {
		return noServerDataError(pb.DataDir())
	}
	serveCommandLine = command.Name() == "serve"
	if serveCommandLine {
		if err := serveAddressesFree(command, rest); err != nil {
			return err
		}
		holdFailedUpdate = true
	}
	err = pb.Execute()
	// A serve whose update failed waits, saying so, rather than ending to be
	// started again into the same failure (schema_migration_failure.go).
	if failure := currentUpdateFailure(); err != nil && serveCommandLine && failure != nil {
		waitOnFailedUpdate(pb, failure)
	}
	return err
}

// serveAddressesFree says whether the addresses `serve` will listen on can be
// taken, before the server's start opens the data folder or brings it up to
// date: on one another program holds, the server used to bring old data up to
// date and only then stop, leaving it newer than the program it serves with
// (2026-11-06: "bind: address already in use" after the migration). The
// addresses are PocketBase's: --http and --https, or theirs by default.
func serveAddressesFree(command *cobra.Command, args []string) error {
	// Read as the command reads them (commandLineReads), so another flag's
	// value isn't taken for a domain.
	flags := pflag.NewFlagSet(command.Name(), pflag.ContinueOnError)
	flags.SetOutput(io.Discard)
	add := func(flag *pflag.Flag) {
		if flags.Lookup(flag.Name) == nil {
			flags.AddFlag(&pflag.Flag{Name: flag.Name, Shorthand: flag.Shorthand, NoOptDefVal: flag.NoOptDefVal,
				Value: &standInFlagValue{kind: flag.Value.Type()}})
		}
	}
	command.Flags().VisitAll(add)
	command.InheritedFlags().VisitAll(add)
	if err := flags.Parse(args); err != nil {
		return nil
	}
	value := func(name string) string {
		if flag := flags.Lookup(name); flag != nil {
			return strings.TrimSpace(flag.Value.String())
		}
		return ""
	}
	domains := flags.NArg() > 0
	addresses := []string{value("http"), value("https")}
	if addresses[0] == "" {
		addresses[0] = map[bool]string{true: "0.0.0.0:80", false: "127.0.0.1:8090"}[domains]
	}
	if addresses[1] == "" && domains {
		addresses[1] = "0.0.0.0:443"
	}
	// Plain HTTP is answered at once while the data is brought up to date
	// (schema_migration_answer.go).
	if addresses[1] == "" {
		serveEarlyAddress = addresses[0]
	}
	for _, address := range addresses {
		if address == "" {
			continue
		}
		listener, err := net.Listen("tcp", address)
		if err != nil {
			return fmt.Errorf("%s can't be listened on (%w); the data folder is left as it is", address, err)
		}
		_ = listener.Close()
	}
	return nil
}

// commandLineReads says whether args name only flags the command has, each
// with the value it needs, reading them into stand-ins so the command's own
// flags stay unset for its run.
func commandLineReads(command *cobra.Command, args []string) bool {
	check := pflag.NewFlagSet(command.Name(), pflag.ContinueOnError)
	check.SetOutput(io.Discard)
	add := func(flag *pflag.Flag) {
		if check.Lookup(flag.Name) == nil {
			check.AddFlag(&pflag.Flag{Name: flag.Name, Shorthand: flag.Shorthand, NoOptDefVal: flag.NoOptDefVal,
				Value: &standInFlagValue{kind: flag.Value.Type()}})
		}
	}
	command.Flags().VisitAll(add)
	command.InheritedFlags().VisitAll(add)
	return check.Parse(args) == nil
}

// standInFlagValue takes any value for a flag of its kind.
type standInFlagValue struct{ kind, text string }

func (value *standInFlagValue) String() string        { return value.text }
func (value *standInFlagValue) Set(text string) error { value.text = text; return nil }
func (value *standInFlagValue) Type() string          { return value.kind }
