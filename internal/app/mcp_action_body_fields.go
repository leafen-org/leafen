package app

import (
	"bytes"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
)

// A request waiting for its person is read field by field for its question
// (mcp_action_approval_words.go): each field a body sets, in the order sent,
// an object's own fields each by its dotted path ("smtp.host",
// "providers.google.enabled"), so a change to one setting in a settings
// object reads as that setting. A secret it sends — a password, a client
// secret, an API key — is never told nor kept with the request: its line
// says it is hidden, and the request as kept and shown has it hidden too.

type mcpActionField struct {
	Key   string
	Value any
}

// mcpActionSecretFields are the fields holding a secret, by their own name;
// "code" is the authentication code that proves the person, beside their password.
var mcpActionSecretFields = map[string]bool{"password": true, "clientSecret": true, "apiKey": true, "secret": true, "code": true,
	"currentPassword": true, "newPassword": true, "newPasswordConfirm": true, "s3SecretKey": true, "webdavPassword": true, "token": true}

var mcpActionSecretValues = regexp.MustCompile(
	`"(password|clientSecret|apiKey|secret|code|currentPassword|newPassword|newPasswordConfirm|s3SecretKey|webdavPassword|token)"(\s*:\s*)"(?:[^"\\]|\\.)*"`)

// mcpActionSecret says the field, by its dotted path, holds a secret.
func mcpActionSecret(key string) bool {
	return mcpActionSecretFields[key[strings.LastIndex(key, ".")+1:]]
}

// mcpActionAsksNothing says the field, as sent, asks for nothing: a saved
// secret's or setting's "clear" left off ("clearSecret": false).
func mcpActionAsksNothing(key string, value any) bool {
	leaf := key[strings.LastIndex(key, ".")+1:]
	return strings.HasPrefix(leaf, "clear") && value == false
}

// mcpActionHideSecrets is a body with the secrets it sends hidden.
func mcpActionHideSecrets(body string) string {
	return mcpActionSecretValues.ReplaceAllString(body, `"$1"$2"••••"`)
}

// mcpActionBodyFields are a JSON object's fields in the order they were
// sent, an object's fields each by its dotted path.
func mcpActionBodyFields(body []byte) []mcpActionField {
	fields := mcpActionObjectFields(body, "")
	// What kind of principal reads before which one (a body sent through
	// leafen_call comes with its keys sorted).
	kind := slices.IndexFunc(fields, func(field mcpActionField) bool { return field.Key == "principalType" })
	who := slices.IndexFunc(fields, func(field mcpActionField) bool { return field.Key == "principalId" })
	if kind > who && who >= 0 {
		moved := fields[kind]
		fields = slices.Insert(slices.Delete(fields, kind, kind+1), who, moved)
	}
	return fields
}

func mcpActionObjectFields(body []byte, prefix string) []mcpActionField {
	decoder := json.NewDecoder(bytes.NewReader(body))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil
	}
	fields := []mcpActionField{}
	for decoder.More() {
		token, err := decoder.Token()
		key, isKey := token.(string)
		if err != nil || !isKey {
			return fields
		}
		var raw json.RawMessage
		if decoder.Decode(&raw) != nil {
			break
		}
		if raw = bytes.TrimSpace(raw); len(raw) > 0 && raw[0] == '{' {
			fields = append(fields, mcpActionObjectFields(raw, prefix+key+".")...)
			continue
		}
		var value any
		reader := json.NewDecoder(bytes.NewReader(raw))
		reader.UseNumber()
		_ = reader.Decode(&value)
		fields = append(fields, mcpActionField{Key: prefix + key, Value: value})
	}
	return fields
}

// mcpActionFlatten is what a setting holds now, by the same dotted paths.
func mcpActionFlatten(prefix string, object map[string]any, into map[string]any) map[string]any {
	for key, value := range object {
		if nested, isObject := value.(map[string]any); isObject {
			mcpActionFlatten(prefix+key+".", nested, into)
			continue
		}
		into[prefix+key] = value
	}
	return into
}
