package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// The manifest's JSON Schema and TypeScript types for the app SDK
// (docs/app-platform.md 18), made from the manifest's Go structure so they
// never drift from it: a field left out when empty (omitempty or omitzero)
// is optional and the rest required (a manifest
// may still leave out surfaces and network, which mean none), and keys,
// names, addresses and choices carry the forms the manifest checks want.
// The checks stay the authority — `leafen app validate` runs them — so the
// schema is a writing aid and a first filter, not a second rule book.

// appManifestOptional are fields never left out that a manifest may leave out.
var appManifestOptional = map[string]bool{"Manifest.surfaces": true, "Manifest.network": true}

func appManifestSchemaExtras() map[string]map[string]any {
	pattern := func(text string) map[string]any { return map[string]any{"pattern": text} }
	enum := func(values ...string) map[string]any { return map[string]any{"enum": values} }
	key := pattern(appKeyPattern.String())
	return map[string]map[string]any{
		"Manifest.schema":          {"const": 1},
		"Manifest.id":              {"pattern": appIDPattern.String(), "maxLength": 100},
		"Manifest.slug":            pattern(appSlugPattern.String()),
		"Manifest.version":         pattern(appVersionPattern.String()),
		"Manifest.minLeafen":       pattern(appVersionPattern.String()),
		"Manifest.category":        enum(appCategories...),
		"Manifest.personAccess":    enum(appPersonAccess[1:]...),
		"Manifest.device[]":        enum(appDeviceKinds...),
		"Manifest.tags":            {"maxItems": 8},
		"Manifest.tags[]":          {"minLength": 1, "maxLength": 24},
		"Manifest.icon":            pattern("^ui/"),
		"Developer.name":           {"minLength": 1, "maxLength": appNameMaxRunes},
		"Developer.url":            pattern("^https://"),
		"Developer.keyFingerprint": pattern("^[0-9a-f]{64}$"),
		"Entry.entry":              pattern("^ui/"),
		"Block.key":                key,
		"FileOpener.key":           key,
		"FileOpener.entry":         pattern("^ui/"),
		"FileOpener.types":         {"minItems": 1, "maxItems": 20},
		"FileOpener.types[]":       pattern(appFileOpenerTypePattern.String()),
		"FileOpener.newFiles":      {"maxItems": appNewFilesPerOpener},
		"NewFile.type":             pattern(appNewFileTypePattern.String()),
		"NewFile.template":         pattern("^templates/"),
		"Block.entry":              pattern("^ui/"),
		"Block.defaultHeight":      {"minimum": 0, "maximum": 2000},
		"Data.key":                 key,
		"Data.file":                pattern("^data/"),
		"Direct.url":               pattern("^https://"),
		"Direct.kinds[]":           enum(appDirectKinds...),
		"Relay.secrets[]":          pattern(appSecretPattern.String()),
		"OAuth.key":                key,
		"OAuth.owner":              enum("person", "app"),
		"OAuth.authorizeParams": {"maxProperties": 8, "propertyNames": map[string]any{"pattern": appOAuthParamPattern.String()},
			"additionalProperties": map[string]any{"type": "string", "maxLength": 200}},
		"Worker.name":           key,
		"Worker.module":         pattern("^workers/.+\\.wasm$"),
		"Worker.functions[]":    key,
		"Schedule.key":          key,
		"Webhook.key":           key,
		"Webhook.verify":        enum("token", "hmac-sha256"),
		"Endpoint.key":          key,
		"Endpoint.methods[]":    enum(appEndpointMethods...),
		"Endpoint.fileBodies[]": enum("PUT", "POST", "PATCH"),
		"Event.key":             key,
		"Event.types[]":         enum(eventWebhookTypes...),
		"Action.key":            key,
		"Action.runAs":          enum("person", "app"),
		"Action.risk":           enum("low", "medium", "high"),
		"Resource.key":          key,
		"Prompt.key":            key,
		"Prompt.template":       {"maxLength": 4000},
		"PWA.themeColor":        pattern(appColorPattern.String()),
	}
}

type appSchemaWriter struct {
	extras map[string]map[string]any
	defs   map[string]map[string]any
	root   map[string]any
	order  []reflect.Type
	seen   map[reflect.Type]bool
}

func appSchemaName(kind reflect.Type) string {
	if name := strings.TrimPrefix(kind.Name(), "appManifest"); name != "" {
		return name
	}
	return "Manifest"
}

// AppManifestSchemaFiles returns the manifest's JSON Schema and its
// TypeScript types, as the SDK keeps them.
func AppManifestSchemaFiles() ([]byte, []byte, error) {
	writer := &appSchemaWriter{extras: appManifestSchemaExtras(), defs: map[string]map[string]any{}, seen: map[reflect.Type]bool{}}
	writer.object(reflect.TypeOf(appManifest{}))
	root := writer.defs["Manifest"]
	writer.root = root
	delete(writer.defs, "Manifest")
	schema := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema", "$id": "https://leafen.org/schema/leafen-app.schema.json",
		"title": "Leafen app manifest (leafen-app.json)",
	}
	for name, value := range root {
		schema[name] = value
	}
	schema["$defs"] = writer.defs
	buffer := &bytes.Buffer{}
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(schema); err != nil {
		return nil, nil, err
	}
	return buffer.Bytes(), writer.typescript(), nil
}

func (writer *appSchemaWriter) object(kind reflect.Type) {
	if writer.seen[kind] {
		return
	}
	writer.seen[kind] = true
	writer.order = append(writer.order, kind)
	name := appSchemaName(kind)
	properties, required := map[string]any{}, []string{}
	for index := 0; index < kind.NumField(); index++ {
		field := kind.Field(index)
		jsonName, optional := appJSONField(field)
		if jsonName == "-" {
			// What Leafen keeps beside the manifest, never written in it.
			continue
		}
		path := name + "." + jsonName
		properties[jsonName] = writer.value(field.Type, path)
		if !optional && !appManifestOptional[path] {
			required = append(required, jsonName)
		}
	}
	definition := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		definition["required"] = required
	}
	writer.defs[name] = definition
}

func appJSONField(field reflect.StructField) (string, bool) {
	name, options, _ := strings.Cut(field.Tag.Get("json"), ",")
	return name, jsonTagOmits(options)
}

// jsonTagOmits is whether a field's json options leave it out when it holds
// nothing: omitempty, or omitzero for a number or a bool, which
// encoding/json/v2 writes even at 0 or false under omitempty.
func jsonTagOmits(options string) bool {
	for option := range strings.SplitSeq(options, ",") {
		if option == "omitempty" || option == "omitzero" {
			return true
		}
	}
	return false
}

func (writer *appSchemaWriter) value(kind reflect.Type, path string) map[string]any {
	schema := map[string]any{}
	switch {
	case kind == reflect.TypeOf(json.RawMessage{}):
		schema["description"] = "A JSON Schema of the input."
	case kind == reflect.TypeOf(appNameText("")):
		// Text, or an object of language codes to text.
		schema["anyOf"] = []any{map[string]any{"type": "string"}, writer.value(reflect.TypeOf(map[string]string{}), path)}
	case kind.Kind() == reflect.Pointer:
		return writer.value(kind.Elem(), path)
	case kind.Kind() == reflect.Struct:
		writer.object(kind)
		return map[string]any{"$ref": "#/$defs/" + appSchemaName(kind)}
	case kind.Kind() == reflect.Slice:
		schema["type"], schema["items"] = "array", writer.value(kind.Elem(), path+"[]")
	case kind.Kind() == reflect.Map && kind.Elem().Kind() == reflect.Map:
		// Keyed by the app's own keys (a guest room's), each in languages.
		schema["type"], schema["additionalProperties"] = "object", writer.value(kind.Elem(), path+"{}")
	case kind.Kind() == reflect.Map:
		schema["type"] = "object"
		schema["propertyNames"] = map[string]any{"pattern": appLocalePattern.String()}
		schema["additionalProperties"] = map[string]any{"type": "string", "minLength": 1}
	case kind.Kind() == reflect.String:
		schema["type"] = "string"
	case kind.Kind() == reflect.Bool:
		schema["type"] = "boolean"
	case kind.Kind() == reflect.Int || kind.Kind() == reflect.Int64:
		schema["type"] = "integer"
	case kind.Kind() == reflect.Float64:
		schema["type"] = "number"
	default:
		panic(fmt.Sprintf("manifest schema: %s has an unknown kind", path))
	}
	for name, value := range writer.extras[path] {
		schema[name] = value
	}
	return schema
}

// typescript writes one interface per manifest structure, in the order the
// manifest reaches them.
func (writer *appSchemaWriter) typescript() []byte {
	buffer := &bytes.Buffer{}
	buffer.WriteString("// Code generated by `go run ./cmd/leafen-app-schema` from the manifest's Go structure. DO NOT EDIT.\n")
	buffer.WriteString("// Leafen app SDK (Apache-2.0, see ../LICENSE): the shape of leafen-app.json.\n")
	for _, kind := range writer.order {
		name := appSchemaName(kind)
		typeName := "Manifest" + strings.TrimPrefix(name, "Manifest")
		fmt.Fprintf(buffer, "\nexport interface %s {\n", typeName)
		definition := writer.defs[name]
		if name == "Manifest" {
			definition = writer.root
		}
		required := map[string]bool{}
		if list, ok := definition["required"].([]string); ok {
			for _, field := range list {
				required[field] = true
			}
		}
		for index := 0; index < kind.NumField(); index++ {
			field := kind.Field(index)
			jsonName, _ := appJSONField(field)
			if jsonName == "-" {
				continue
			}
			mark := "?"
			if required[jsonName] {
				mark = ""
			}
			fmt.Fprintf(buffer, "  %s%s: %s;\n", jsonName, mark, writer.tsType(field.Type, name+"."+jsonName))
		}
		buffer.WriteString("}\n")
	}
	return buffer.Bytes()
}

func (writer *appSchemaWriter) tsType(kind reflect.Type, path string) string {
	if values, ok := writer.extras[path]["enum"].([]string); ok {
		quoted := []string{}
		for _, value := range values {
			quoted = append(quoted, fmt.Sprintf("%q", value))
		}
		return strings.Join(quoted, " | ")
	}
	if value, ok := writer.extras[path]["const"]; ok {
		return fmt.Sprint(value)
	}
	switch {
	case kind == reflect.TypeOf(json.RawMessage{}):
		return "unknown"
	case kind == reflect.TypeOf(appNameText("")):
		return "string | Record<string, string>"
	case kind.Kind() == reflect.Pointer:
		return writer.tsType(kind.Elem(), path)
	case kind.Kind() == reflect.Struct:
		return "Manifest" + strings.TrimPrefix(appSchemaName(kind), "Manifest")
	case kind.Kind() == reflect.Slice:
		element := writer.tsType(kind.Elem(), path+"[]")
		if strings.Contains(element, " | ") {
			element = "(" + element + ")"
		}
		return element + "[]"
	case kind.Kind() == reflect.Map && kind.Elem().Kind() == reflect.Map:
		return "Record<string, " + writer.tsType(kind.Elem(), path+"{}") + ">"
	case kind.Kind() == reflect.Map:
		return "Record<string, string>"
	case kind.Kind() == reflect.String:
		return "string"
	case kind.Kind() == reflect.Bool:
		return "boolean"
	}
	return "number"
}
