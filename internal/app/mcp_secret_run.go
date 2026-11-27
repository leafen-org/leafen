package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/zalando/go-keyring"
)

// What `leafen mcp secret` does once it has its connection and its fields
// (mcp_secret_command.go): reads the values, runs the operation, keeps what
// it answers with, and prints how it went with every value masked.

type mcpSecretCall struct {
	Method, Path, Body string
	Sources, Saves     []mcpSecretSource
	Replace            bool
	Confirmation       string
}

func runMCPSecret(ctx context.Context, out io.Writer, entry mcpCallEntry, call mcpSecretCall) error {
	if call.Method == "" || !strings.HasPrefix(call.Path, "/") || strings.Contains(call.Path, "?") {
		return errors.New("give the operation as leafen_operations names it: its method, then its path below /api/leafen/v1 with its values filled in")
	}
	body := map[string]any{}
	if err := json.Unmarshal([]byte(call.Body), &body); err != nil || body == nil {
		return fmt.Errorf("the body is one JSON object of the request's other fields ({} for none), without the secret ones: %s",
			jsonCommandHere("leafen mcp secret "+call.Method+" "+call.Path, "<the body>", " --from-file <field>=<path>"))
	}
	// Where the answer's secrets go is checked first: one made is not lost
	// for a place that can't take it.
	files := map[string]string{}
	for _, save := range call.Saves {
		if save.Kind != "file" {
			continue
		}
		path, err := mcpSecretSaveFile(save, call.Replace)
		if err != nil {
			return err
		}
		files[save.Field] = path
	}
	values := []string{}
	for _, source := range call.Sources {
		if personStepGiven(body, source.Field) {
			return fmt.Errorf("leave %s out of the body: it comes from the %s", source.Field, source)
		}
		value, err := readMCPSecret(source)
		if err != nil {
			return err
		}
		setPersonStepValue(body, source.Field, value)
		values = append(values, value)
	}
	arguments := map[string]any{"method": call.Method, "path": call.Path, "body": body}
	if call.Confirmation != "" {
		arguments["confirmation"] = strings.TrimSpace(call.Confirmation)
	}
	result, err := mcpCallAsk(ctx, entry, "tools/call", map[string]any{"name": "leafen_call", "arguments": arguments})
	if err != nil {
		return errors.New(maskMCPSecrets(err.Error(), values))
	}
	answer := struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Structured *nativeOperationResult `json:"structuredContent"`
		IsError    bool                   `json:"isError"`
	}{}
	if err := json.Unmarshal(result, &answer); err != nil {
		return errors.New("Leafen's answer couldn't be read")
	}
	if answer.IsError || answer.Structured == nil {
		said := []string{}
		for _, part := range answer.Content {
			said = append(said, part.Text)
		}
		return errors.New(maskMCPSecrets(firstNonempty(strings.Join(said, "\n"), "the call was refused"), values))
	}
	operation := answer.Structured
	answered, _ := operation.Body.(map[string]any)
	if operation.Status < 200 || operation.Status >= 300 {
		return errors.New(maskMCPSecrets(mcpSecretRefusal(operation, answered), values))
	}
	// The secrets it answers with go where they were asked to, never out.
	for _, save := range call.Saves {
		value, found := mcpSecretAnswerValue(answered, save.Field)
		if !found {
			return fmt.Errorf("the operation went through, but its answer has no %s, so nothing was kept: check what it answers with "+
				"(leafen_operations) and name the field again", save.Field)
		}
		values = append(values, strings.Split(value, "\n")...)
		if err := keepMCPSecret(save, files[save.Field], value, call.Replace); err != nil {
			return fmt.Errorf("the operation went through, but %s couldn't be kept in the %s: %s — make it again once that place takes it",
				save.Field, save, maskMCPSecrets(err.Error(), values))
		}
		setPersonStepValue(answered, save.Field, "<kept in the "+save.String()+">")
	}
	// A secret the answer carries that wasn't asked to be kept is masked by
	// its field's name too: a token, a password, a code.
	maskMCPSecretFields(answered)
	shown := strings.Builder{}
	encoder := json.NewEncoder(&shown)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(answered); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Leafen answered %d to %s %s; every secret is left out.\n%s", operation.Status, call.Method, call.Path,
		maskMCPSecrets(shown.String(), values))
	return err
}

// mcpSecretRefusal says why the operation didn't go through, and what to
// run again where it asks first.
func mcpSecretRefusal(operation *nativeOperationResult, answered map[string]any) string {
	encoded, _ := json.Marshal(answered)
	said := fmt.Sprintf("Leafen answered %d: %s", operation.Status, firstNonempty(string(encoded), appOneLine(operation.Text, 500)))
	if operation.Confirmation != "" {
		said += " — to go on, run the same command again with --confirmation " + operation.Confirmation
	}
	if approval, found := answered["approval"].(map[string]any); found {
		if id, _ := approval["approvalId"].(string); id != "" {
			said += " — ask your person as it says; on their yes, run the same command again with --confirmation " + id
		}
	}
	return said
}

// mcpSecretAnswerValue is the answer's field at a dotted path: a text, or a
// list of them one to a line.
func mcpSecretAnswerValue(answered map[string]any, field string) (string, bool) {
	switch value := personStepValue(answered, field).(type) {
	case string:
		return value, value != ""
	case []any:
		lines := []string{}
		for _, item := range value {
			if line, ok := item.(string); ok {
				lines = append(lines, line)
			}
		}
		return strings.Join(lines, "\n"), len(lines) > 0
	}
	return "", false
}

// mcpSecretSaveFile is where a --save-file goes: a new file in a folder that
// is there, or one written over with --replace.
func mcpSecretSaveFile(save mcpSecretSource, replace bool) (string, error) {
	path, err := mcpSecretPath(save.Where)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(filepath.Dir(path)); err != nil || !info.IsDir() {
		return "", fmt.Errorf("%s: the folder %s isn't there", save.Field, filepath.Dir(path))
	}
	if _, err := os.Stat(path); err == nil && !replace {
		return "", fmt.Errorf("%s: %s is there already; name a new file, or add --replace to write over it", save.Field, path)
	}
	return path, nil
}

// keepMCPSecret writes a value the answer carried to its place.
func keepMCPSecret(save mcpSecretSource, path, value string, replace bool) error {
	if save.Kind == "keychain" {
		service, account := keychainSecretItem(save.Where)
		return keyring.Set(service, account, value)
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if replace {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	file, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return errors.New(mcpSecretFileProblem(err))
	}
	_, err = file.WriteString(value + "\n")
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}

// readKeychainSecret is a keychain item's value: macOS Keychain, the Secret
// Service on Linux, Windows Credential Manager (go-keyring, as the desktop
// app keeps its sign-ins).
func readKeychainSecret(source mcpSecretSource) (string, error) {
	service, account := keychainSecretItem(source.Where)
	value, err := keyring.Get(service, account)
	switch {
	case errors.Is(err, keyring.ErrNotFound):
		return "", fmt.Errorf("%s: the keychain has no item %s for %s", source.Field, service, account)
	case err != nil:
		return "", fmt.Errorf("%s: this computer's keychain couldn't be read (%s): put the value in with --from-env or --from-file instead",
			source.Field, appOneLine(err.Error(), 200))
	case strings.TrimSpace(value) == "":
		return "", fmt.Errorf("%s: the keychain item %s for %s is empty", source.Field, service, account)
	}
	return value, nil
}

// keychainSecretItem reads service/account; an item named alone is under
// the account "leafen".
func keychainSecretItem(where string) (string, string) {
	service, account, found := strings.Cut(where, "/")
	if !found || strings.TrimSpace(account) == "" {
		return strings.TrimSpace(service), "leafen"
	}
	return strings.TrimSpace(service), strings.TrimSpace(account)
}

// maskMCPSecrets leaves every value out of a text, as written and as JSON
// writes it inside a string.
func maskMCPSecrets(text string, values []string) string {
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		text = strings.ReplaceAll(text, value, mcpSecretMask)
		if encoded, err := json.Marshal(value); err == nil {
			if escaped := strings.Trim(string(encoded), `"`); escaped != value {
				text = strings.ReplaceAll(text, escaped, mcpSecretMask)
			}
		}
	}
	return text
}

// maskMCPSecretFields masks, through the whole answer, each text under a
// field named as a secret (mcpActionSecretFields), and an access or refresh
// token.
func maskMCPSecretFields(value any) {
	switch node := value.(type) {
	case map[string]any:
		for key, item := range node {
			if text, isText := item.(string); isText && text != "" && !strings.HasPrefix(text, "<kept in the ") &&
				(mcpActionSecretFields[key] || key == "accessToken" || key == "refreshToken") {
				node[key] = mcpSecretMask
				continue
			}
			maskMCPSecretFields(item)
		}
	case []any:
		for _, item := range node {
			maskMCPSecretFields(item)
		}
	}
}
