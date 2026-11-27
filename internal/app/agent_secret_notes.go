package app

import (
	"slices"
	"strings"
)

// The operations that take or answer with a secret, and what the operations
// list (leafen_operations) says beside each (user decision 2026-11-22: an
// agent puts a secret in itself without the value entering its
// conversation): an agent that can run commands is pointed first to
// `leafen mcp secret` on its computer, naming the file, environment variable
// or keychain item that holds each value, and keeps what the operation
// answers with the same way (mcp_secret_command.go). It is guidance, never a
// gate: leafen_call takes and answers the same fields as before ("mcp로
// 모든게 다 되야되", 11:2x). Only an agent that may not handle secrets leaves
// them to its person (secretsFromPerson, where a person step holds the
// operation: person_step_operations.go).

// agentSecretFields are an operation's secret fields: in its body, by dotted
// name, and in its answer; held, a person step can take it instead.
//
// A field of the body takes either a value already held somewhere — an
// outside service's key or token, a mail server's password, a code another
// server made — which the agent puts in from where it is, or a value nobody
// holds yet: a password made for someone, a person's new one. Such a field is
// Made (user 2026-11-23 "에이전트가 알아서 해줘야지", "이거 대원칙 어기는거잖아"):
// left out, Leafen makes it (newMadePassword) and answers it
// this once in the field named, so an agent keeps it with --save-file and
// hands it over without seeing it, and an agent not allowed to handle
// secrets leaves it to a step that shows it to its person alone
// (person_step_operations.go). MadeBy names, for such a field, another
// operation that makes the same protection and answers it once instead.
type agentSecretFields struct {
	In, Out []string
	Made    map[string]string
	MadeBy  map[string]string
	Held    bool
}

// The backups open with a recovery code Leafen makes, or a password a person
// makes up; the one Leafen makes is its own operation's.
var backupPasswordMadeBy = map[string]string{"password": "POST /admin/backups/protection/code"}

// A person's new password, as both its fields are left out.
var ownPasswordMade = map[string]string{"newPassword": "password", "newPasswordConfirm": "password"}

var agentSecretOperations = map[string]agentSecretFields{
	"PUT /admin/backups/schedule":                               {In: []string{"s3AccessKey", "s3SecretKey", "webdavPassword", "password"}, MadeBy: backupPasswordMadeBy, Held: true},
	"POST /admin/backups/protection/code":                       {Out: []string{"code"}, Held: true},
	"PUT /admin/backups/protection/password":                    {In: []string{"password"}, MadeBy: backupPasswordMadeBy, Held: true},
	"POST /admin/backups/protection/open":                       {In: []string{"secret"}, Held: true},
	"POST /admin/backups/standby/code":                          {Out: []string{"code"}, Held: true},
	"PUT /admin/backups/standby/join":                           {In: []string{"code"}, Held: true},
	"PATCH /admin/settings":                                     {In: []string{"smtp.password", "providers.google.clientSecret", "providers.oidc.clientSecret", "placeSearch.apiKey"}, Held: true},
	"PUT /admin/network-proxy":                                  {In: []string{"password"}, Held: true},
	"PUT /admin/tunnel":                                         {In: []string{"token"}, Held: true},
	"POST /admin/accounts":                                      {In: []string{"password"}, Out: []string{"temporaryPassword"}, Made: map[string]string{"password": "temporaryPassword"}, Held: true},
	"PUT /admin/accounts/{accountId}/password":                  {In: []string{"password"}, Out: []string{"temporaryPassword"}, Made: map[string]string{"password": "temporaryPassword"}, Held: true},
	"PUT /admin/apps/installs/{installId}/secrets/{name}":       {In: []string{"value"}, Held: true},
	"POST /mcp-services":                                        {In: []string{"token", "auth.token", "auth.password", "auth.value"}, Held: true},
	"POST /account/mfa/totp/enroll":                             {Out: []string{"secret", "qrCodeUri", "recoveryCodes"}},
	"POST /account/app-passwords":                               {Out: []string{"password", "secret"}},
	"POST /account/switcher-keys":                               {Out: []string{"key"}},
	"POST /mcp/connections":                                     {Out: []string{"connectionUri"}},
	"POST /workspaces/{workspaceId}/webhooks":                   {Out: []string{"secret"}},
	"POST /account/mfa/recovery-codes":                          {In: []string{"password", "code"}, Out: []string{"recoveryCodes"}},
	"POST /account/password":                                    {In: []string{"newPassword", "newPasswordConfirm"}, Out: []string{"password"}, Made: ownPasswordMade},
	"PUT /account/password":                                     {In: []string{"currentPassword", "newPassword", "newPasswordConfirm"}, Out: []string{"password"}, Made: ownPasswordMade},
	"DELETE /account/mfa/totp":                                  {In: []string{"password", "code"}},
	"PUT /apps/{installId}/credentials/{key}":                   {In: []string{"values.<the app's secret field>"}},
	"POST /admin/apps/installs/{installId}/hooks/{hook}/secret": {Out: []string{"secret"}},
	"POST /organizations/{organizationId}/scim/tokens":          {Out: []string{"token"}},
	"POST /workspaces/{workspaceId}/notion-import/connections":  {In: []string{"token"}},
}

func init() {
	for id, fields := range agentSecretOperations {
		nativeOperationNotes[nativeOperationShape(id)] = agentSecretNote(id, fields)
	}
}

// agentSecretNote is what the operations list says beside one: the command
// with its fields, and the person's field where an agent may not handle
// secrets.
func agentSecretNote(id string, fields agentSecretFields) string {
	method, path, _ := strings.Cut(id, " ")
	// The command with flags, for a POSIX shell and Windows PowerShell (command_json_argument.go).
	command := func(flags string) string {
		return jsonCommandWords("leafen mcp secret "+method+" "+path, "<the body without them>", flags, true)
	}
	givenIn := false
	made, madeOut := agentSecretMade(fields)
	parts := []string{}
	if len(made) > 0 {
		parts = append(parts, "Leave "+strings.Join(made, " and ")+" out and Leafen makes a new one nobody holds yet, answering it this once as "+
			strings.Join(madeOut, ", ")+": if you can run commands, "+command(agentSecretSaveFlags(madeOut))+" (or --save-keychain) "+
			"keeps it out of your conversation, in a new file your person opens. ")
	}
	if len(fields.In) > 0 {
		lead := "If you can run commands, put "
		if len(made) > 0 {
			lead = "To give a value already held, put "
		}
		parts = append(parts, lead+strings.Join(fields.In, ", ")+" in with "+command(" --from-file <field>=<path>")+" "+
			"(or --from-env <field>=<NAME>, --from-keychain <field>=<service>/<account>)")
		givenIn = true
	}
	if kept := agentSecretOnlyOut(fields, madeOut); len(kept) > 0 {
		keep := "keep " + strings.Join(kept, ", ") + " of its answer with --save-file <field>=<a new file> (or --save-keychain)"
		if !givenIn {
			keep = "If you can run commands, run it with " + command("") + " and " + keep
		} else {
			keep = "; " + keep
		}
		parts = append(parts, keep)
	}
	for _, field := range agentSecretSorted(fields.MadeBy) {
		maker := fields.MadeBy[field]
		parts = append(parts, "; or, for "+field+", have Leafen make the protection with "+
			jsonCommandWords("leafen mcp secret "+maker, "{}", agentSecretSaveFlags(agentSecretOperations[maker].Out), true)+
			": a recovery code, answered once")
	}
	note := strings.Join(parts, "") + ", so the value stays out of your conversation; else leafen_call takes and answers them as usual. "
	switch {
	case fields.Held && len(made) > 0:
		note += `Only an agent not allowed to handle secrets sends "secretsFromPerson": true instead: Leafen makes it as its person ` +
			"opens the step, shown to them alone (a list of the fields has them type those); GET /person-steps/{id} follows it."
	case fields.Held:
		note += `Only an agent not allowed to handle secrets sends "secretsFromPerson": true instead, and its person gives them; ` +
			"GET /person-steps/{id} follows it."
	}
	return strings.TrimSpace(note)
}

// agentSecretMade is the operation's fields Leafen makes when they are left
// out, and the answer fields it gives them in, each once and in order.
func agentSecretMade(fields agentSecretFields) ([]string, []string) {
	made, out := []string{}, []string{}
	for _, field := range fields.In {
		answer, ok := fields.Made[field]
		if !ok {
			continue
		}
		made = append(made, field)
		if !slices.Contains(out, answer) {
			out = append(out, answer)
		}
	}
	return made, out
}

// agentSecretOnlyOut is what the operation answers besides what it made of
// a field left out.
func agentSecretOnlyOut(fields agentSecretFields, madeOut []string) []string {
	return slices.DeleteFunc(slices.Clone(fields.Out), func(field string) bool { return slices.Contains(madeOut, field) })
}

func agentSecretSaveFlags(fields []string) string {
	flags := ""
	for _, field := range fields {
		flags += " --save-file " + field + "=<a new file>"
	}
	return flags
}

func agentSecretSorted(fields map[string]string) []string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// agentSecretAnswerNote is what leafen_call's answer says beside a secret an
// operation made and answered into the conversation: the command that keeps
// the next one out of it. Guidance only — the value is answered as always.
func agentSecretAnswerNote(method, path string, status int) string {
	if status < 200 || status >= 300 {
		return ""
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	for id, fields := range agentSecretOperations {
		operationMethod, template, _ := strings.Cut(id, " ")
		if len(fields.Out) == 0 || operationMethod != method || !agentSecretPathMatches(template, path) {
			continue
		}
		_, madeOut := agentSecretMade(fields)
		holds := "This answer holds " + strings.Join(fields.Out, ", ") + ", now in this conversation"
		if len(madeOut) == len(fields.Out) {
			holds = "This answer holds " + strings.Join(madeOut, ", ") + " where Leafen made it, now in this conversation"
		}
		note := holds + ". If you can run commands, " +
			jsonCommandWords("leafen mcp secret "+id, "<the body>", " --save-file <field>=<a new file>", true) +
			" (or --save-keychain) keeps such a value out of it next time."
		if fields.Held && len(madeOut) > 0 {
			note += ` An agent not allowed to handle secrets sends "secretsFromPerson": true instead, and Leafen shows it to its person alone.`
		}
		return note
	}
	return ""
}

// agentSecretPathMatches is whether a path is the operation's, a {name} in
// it standing for any one segment.
func agentSecretPathMatches(template, path string) bool {
	want, got := strings.Split(template, "/"), strings.Split(strings.TrimSuffix(path, "/"), "/")
	if len(want) != len(got) {
		return false
	}
	for index, segment := range want {
		if strings.HasPrefix(segment, "{") && got[index] != "" {
			continue
		}
		if segment != got[index] {
			return false
		}
	}
	return true
}
