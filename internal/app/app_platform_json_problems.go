package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// An app's JSON files said what was wrong in Go's words — "cannot unmarshal
// object into Go struct field appManifest.agent.prompts.0.name of type
// string", "invalid character '.' after object key:value pair" — with no
// line (2026-10-16). A problem now names the file, the line and column, the
// field as the file writes it, and what belongs there.

// appJSONProblem says why file's raw JSON doesn't decode, as its author reads it.
func appJSONProblem(file string, raw []byte, err error) string {
	var syntax *json.SyntaxError
	var mistyped *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syntax):
		return fmt.Sprintf("%s %s: %s", file, appJSONPlace(raw, syntax.Offset), appJSONSyntaxReason(syntax.Error()))
	case errors.As(err, &mistyped):
		field := appJSONFieldPath(mistyped.Field)
		where := appJSONPlace(raw, mistyped.Offset)
		if field == "" {
			return fmt.Sprintf("%s %s: this should be %s, not %s", file, where, appJSONKind(mistyped.Type), appJSONValueKind(mistyped.Value))
		}
		return fmt.Sprintf("%s %s: %q should be %s, not %s", file, where, field, appJSONKind(mistyped.Type), appJSONValueKind(mistyped.Value))
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		name := strings.Trim(strings.TrimPrefix(err.Error(), "json: unknown field "), `"`)
		return fmt.Sprintf("%s %s: %q isn't a field Leafen knows there (check its spelling and where it sits)", file, appJSONFieldPlaces(raw, name), name)
	case errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF):
		return fmt.Sprintf("%s: it ends before its last value closes (a missing } or ])", file)
	}
	return fmt.Sprintf("%s: %s", file, strings.TrimPrefix(err.Error(), "json: "))
}

// appJSONPlace is "line L, column C" of a byte offset (1-based).
func appJSONPlace(raw []byte, offset int64) string {
	if offset < 0 || offset > int64(len(raw)) {
		return ""
	}
	line, column := 1, 1
	for _, char := range string(raw[:offset]) {
		if char == '\n' {
			line, column = line+1, 1
		} else {
			column++
		}
	}
	return fmt.Sprintf("line %d, column %d", line, column)
}

// appJSONFieldPlaces says where an unknown field may be: its one line, or each
// line that names it, as the decoder doesn't say which of them it stopped at.
func appJSONFieldPlaces(raw []byte, name string) string {
	lines := []string{}
	for number, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, `"`+name+`"`) {
			lines = append(lines, strconv.Itoa(number+1))
		}
	}
	if len(lines) == 1 {
		return "line " + lines[0]
	}
	return "on one of lines " + strings.Join(lines, ", ")
}

var appJSONIndex = regexp.MustCompile(`\.(\d+)`)

// appJSONFieldPath writes Go's field path as the file does: agent.prompts[0].name.
func appJSONFieldPath(field string) string {
	return appJSONIndex.ReplaceAllString(field, "[$1]")
}

func appJSONSyntaxReason(message string) string {
	switch {
	case strings.Contains(message, "after object key:value pair"):
		return "a comma or } is missing after the value before this"
	case strings.Contains(message, "after object key"):
		return "a : is missing after the field name before this"
	case strings.Contains(message, "after array element"):
		return "a comma or ] is missing after the item before this"
	case strings.Contains(message, "looking for beginning of object key string"):
		return "a field name in double quotes belongs here (a trailing comma, or a name without quotes?)"
	case strings.Contains(message, "looking for beginning of value"):
		return "a value belongs here (a trailing comma, a single quote or a comment?)"
	}
	return strings.TrimPrefix(message, "json: ")
}

// appJSONKind names what a Go type takes in JSON words.
func appJSONKind(kind reflect.Type) string {
	if kind == nil {
		return "something else"
	}
	switch kind.Kind() {
	case reflect.String:
		return "text"
	case reflect.Bool:
		return "true or false"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16,
		reflect.Uint32, reflect.Uint64:
		return "a whole number"
	case reflect.Float32, reflect.Float64:
		return "a number"
	case reflect.Slice, reflect.Array:
		return "a list"
	case reflect.Map:
		if kind.Key().Kind() == reflect.String && kind.Elem().Kind() == reflect.String {
			return `an object of language codes to text, such as {"en": "…", "ko": "…"}`
		}
		return "an object"
	case reflect.Struct, reflect.Pointer:
		return "an object"
	}
	return "something else"
}

// appJSONValueKind names what the file wrote ("number 3" → "a number").
func appJSONValueKind(value string) string {
	switch {
	case value == "object":
		return "an object"
	case value == "array":
		return "a list"
	case value == "string":
		return "text"
	case value == "bool":
		return "true or false"
	case strings.HasPrefix(value, "number"):
		return "a number"
	}
	return value
}
