package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

// `leafen mcp secret` puts a secret into a Leafen setting as the agent, with
// the value never in its conversation (user decision 2026-11-22, corrected
// 10:2x: "키는 니가 안보고 넣을수있는 경로" — the agent puts the key in, from
// where it already is, never reading it). On the agent's own computer it
// takes the agent's connection as `leafen mcp call` does (Claude Code's
// entry, or another agent's address and a key from an environment variable),
// reads each secret field from a file, an environment variable or the
// system's keychain — named on the command line, never the value itself, so
// none lands in shell history or a process's arguments —, puts it into the
// operation's body and runs the operation through leafen_call, as the
// agent's own call does. It prints only how the operation went, every value
// it sent or got masked; a secret the server answers with (a recovery code,
// a standby server's code, a hook's secret, a SCIM token, new 2-step
// recovery codes) goes to the file or keychain item --save-* names and is
// never printed. An agent that may not handle secrets at all leaves them to
// its person instead (secretsFromPerson, person_step_hold.go).

// mcpSecretMask stands in for a value in what the command prints.
const mcpSecretMask = "<secret>"

// mcpSecretSource is one field and where its value comes from or goes.
type mcpSecretSource struct {
	Field string
	Kind  string // file, env or keychain
	Where string
}

func (source mcpSecretSource) String() string {
	return source.Kind + " " + source.Where
}

func mcpSecretCommand() *cobra.Command {
	name := ""
	var fromFile, fromEnv, fromKeychain, saveFile, saveKeychain []string
	var server, keyEnv, confirmation string
	var replace bool
	command := &cobra.Command{
		Use:   "secret <METHOD> <path> [body]",
		Short: "Put a secret into a Leafen setting as the agent, from a file, an environment variable or the keychain, never showing it",
		Long: "Runs one Leafen operation (leafen_operations names them) as the agent's connection, with each secret field of its body read " +
			"from a file (--from-file field=path), an environment variable (--from-env field=NAME) or the system's keychain " +
			"(--from-keychain field=service/account: macOS Keychain, the Secret Service on Linux, Windows Credential Manager). The body is " +
			"the rest of the request as JSON (- reads it from standard input: in Windows PowerShell, which hands a program its argument " +
			"without the JSON's double quotes, give it there, as UTF-8 — " + jsonCommandPowerShell("leafen mcp secret <METHOD> <path>",
			"<body>", " --from-file <field>=<path>") + "); a field is a name in it, dotted for a nested one " +
			"(smtp.password, values.password). A secret the operation answers with is saved, never printed: --save-file field=path " +
			"(a new file, readable by you alone; --replace to write over one) or --save-keychain field=service/account. It prints only " +
			"how the operation went, every value masked. Name only where a value is, never the value: don't read or paste it. " +
			"It takes the connection from Claude Code (its one Leafen entry, or the one --entry names), or --server with Leafen's MCP address and " +
			"--key-env with the variable that holds the agent's key.",
		Args:         cobra.RangeArgs(2, 3),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			sources, err := mcpSecretSources(map[string][]string{"file": fromFile, "env": fromEnv, "keychain": fromKeychain})
			if err != nil {
				return err
			}
			saves, err := mcpSecretSources(map[string][]string{"file": saveFile, "keychain": saveKeychain})
			if err != nil {
				return err
			}
			if len(sources) == 0 && len(saves) == 0 {
				return errors.New("name a field to put in (--from-file, --from-env or --from-keychain) or to keep (--save-file or --save-keychain); " +
					"an operation without a secret goes through `leafen mcp call leafen_call`")
			}
			entry, err := mcpSecretEntry(cmd.Context(), name, server, keyEnv)
			if err != nil {
				return err
			}
			raw := "{}"
			if len(args) == 3 {
				raw = args[2]
			}
			if raw == "-" {
				read, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), maxMCPMessageBytes+1))
				if err != nil {
					return err
				}
				raw = string(read)
			}
			return runMCPSecret(cmd.Context(), cmd.OutOrStdout(), entry, mcpSecretCall{
				Method: strings.ToUpper(strings.TrimSpace(args[0])), Path: strings.TrimSpace(args[1]), Body: raw,
				Sources: sources, Saves: saves, Replace: replace, Confirmation: confirmation,
			})
		},
	}
	flags := command.Flags()
	flags.StringArrayVar(&fromFile, "from-file", nil, "field=path: the field's value is the file's text (its last line break left off)")
	flags.StringArrayVar(&fromEnv, "from-env", nil, "field=NAME: the field's value is that environment variable's")
	flags.StringArrayVar(&fromKeychain, "from-keychain", nil, "field=service/account: the field's value is that keychain item's")
	flags.StringArrayVar(&saveFile, "save-file", nil, "field=path: keep that field of the answer in a new file, readable by you alone")
	flags.StringArrayVar(&saveKeychain, "save-keychain", nil, "field=service/account: keep that field of the answer in the keychain")
	flags.BoolVar(&replace, "replace", false, "let --save-file write over a file that is there")
	flags.StringVar(&confirmation, "confirmation", "", "for an operation that asked first: the approvalId or confirmation its first run said")
	flags.StringVar(&name, "entry", "", agentEntryFlagUsage)
	flags.StringVar(&server, "server", "", "Leafen's MCP address, for an agent other than Claude Code (with --key-env)")
	flags.StringVar(&keyEnv, "key-env", "", "the environment variable holding the agent's key, with --server")
	return command
}

// mcpSecretSources reads field=where pairs by kind; a field is named once.
func mcpSecretSources(byKind map[string][]string) ([]mcpSecretSource, error) {
	sources := []mcpSecretSource{}
	for _, kind := range []string{"file", "env", "keychain"} {
		for _, pair := range byKind[kind] {
			field, where, found := strings.Cut(pair, "=")
			field, where = strings.TrimSpace(field), strings.TrimSpace(where)
			if !found || field == "" || where == "" {
				return nil, fmt.Errorf("give field=%s, as %s", map[string]string{"file": "path", "env": "NAME", "keychain": "service/account"}[kind],
					map[string]string{"file": "s3SecretKey=~/keys/s3-secret", "env": "s3SecretKey=S3_SECRET_KEY", "keychain": "password=leafen/backup"}[kind])
			}
			if slices.ContainsFunc(sources, func(source mcpSecretSource) bool { return source.Field == field }) {
				return nil, fmt.Errorf("%s is named twice", field)
			}
			sources = append(sources, mcpSecretSource{Field: field, Kind: kind, Where: where})
		}
	}
	return sources, nil
}

// mcpSecretEntry is the agent's connection: Claude Code's entry, or an
// address with a key from the environment.
func mcpSecretEntry(ctx context.Context, name, server, keyEnv string) (mcpCallEntry, error) {
	if server == "" && keyEnv == "" {
		return claudeCodeLeafenEntry(ctx, name)
	}
	if server == "" || keyEnv == "" {
		return mcpCallEntry{}, errors.New("--server and --key-env go together: Leafen's MCP address and the variable holding the agent's key")
	}
	key := strings.TrimSpace(os.Getenv(keyEnv))
	if key == "" {
		return mcpCallEntry{}, fmt.Errorf("the environment variable %s is empty", keyEnv)
	}
	return mcpCallEntry{URL: strings.TrimSpace(server), Authorization: "Bearer " + key}, nil
}

// mcpSecretPath is a path the command reads or writes, ~ for the home folder.
func mcpSecretPath(path string) (string, error) {
	if rest, found := strings.CutPrefix(path, "~"); found && (rest == "" || strings.HasPrefix(rest, "/") || strings.HasPrefix(rest, `\`)) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, rest)
	}
	return filepath.Abs(path)
}

// readMCPSecret is a source's value; what fails says where, never a value.
func readMCPSecret(source mcpSecretSource) (string, error) {
	switch source.Kind {
	case "file":
		path, err := mcpSecretPath(source.Where)
		if err != nil {
			return "", err
		}
		read, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("%s: couldn't read the file %s (%s)", source.Field, path, mcpSecretFileProblem(err))
		}
		value := strings.TrimSuffix(strings.TrimSuffix(string(read), "\n"), "\r")
		if strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("%s: the file %s is empty", source.Field, path)
		}
		return value, nil
	case "env":
		value, set := os.LookupEnv(source.Where)
		if !set || strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("%s: the environment variable %s isn't set here", source.Field, source.Where)
		}
		return value, nil
	}
	return readKeychainSecret(source)
}

func mcpSecretFileProblem(err error) string {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "there is no such file"
	case errors.Is(err, os.ErrPermission):
		return "not allowed to read it"
	}
	return "it can't be read"
}
