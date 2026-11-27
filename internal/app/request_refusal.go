package app

import (
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// One shape for a refused request that says what to fix: the refusal's own
// words, then the field as the request names it and why — "Check the content
// order: operations[0].block.position is empty …." — as Notion's API names
// the path and the shape it wanted (notionFaultLine: "body.properties.title.title
// takes an array, not `5`."). A route keeps its status and code; only the
// words gain the reason.

// requestRefusal is a refused request whose kind (a route's sentinel error,
// errFormInvalid, errCommentInvalid …) picks the status and code, with the
// reason said after the route's words. One about what given fields hold
// names them too, each with why (refuseFields), for the answer's fields
// (apiErrorAbout): a screen says each under its own field.
type requestRefusal struct {
	kind   error
	reason string
	fields []apiErrorField
}

func (refusal *requestRefusal) Error() string { return refusal.kind.Error() + ": " + refusal.reason }
func (refusal *requestRefusal) Unwrap() error { return refusal.kind }

// refuse is a refusal of the given kind for the reason given.
func refuse(kind error, format string, args ...any) error {
	return &requestRefusal{kind: kind, reason: fmt.Sprintf(format, args...)}
}

// refuseFields is a refusal of the given kind about the fields named, each
// with its own reason; their reasons, one after another, are the refusal's.
func refuseFields(kind error, fields ...apiErrorField) error {
	reasons := make([]string, 0, len(fields))
	for _, field := range fields {
		reasons = append(reasons, field.Problem)
	}
	return &requestRefusal{kind: kind, reason: strings.Join(reasons, "; "), fields: fields}
}

// refusalAt says where in the request a refused field is — at
// "operations[2]." the reason "templateId names …" names
// operations[2].templateId — or leaves err as it is.
func refusalAt(err error, at string) error {
	var refusal *requestRefusal
	if !errors.As(err, &refusal) {
		return err
	}
	fields := make([]apiErrorField, 0, len(refusal.fields))
	for _, field := range refusal.fields {
		fields = append(fields, apiErrorField{Name: at + field.Name, Problem: at + field.Problem, Allowed: field.Allowed})
	}
	return &requestRefusal{kind: refusal.kind, reason: at + refusal.reason, fields: fields}
}

// refusalFields are the request's fields a refusal is about, each with why,
// or none.
func refusalFields(err error) []apiErrorField {
	var refusal *requestRefusal
	if errors.As(err, &refusal) {
		return refusal.fields
	}
	return nil
}

// refusalReason is what a refusal says to fix, or "".
func refusalReason(err error) string {
	var refusal *requestRefusal
	if errors.As(err, &refusal) {
		return refusal.reason
	}
	var graphRefusal *graphRequestError
	if errors.As(err, &graphRefusal) {
		return graphRefusal.reason
	}
	return ""
}

// refusalMessage is a route's words with the reason after them, or the words
// alone when there is none: "Check the import details." and "pageId is empty"
// make "Check the import details: pageId is empty."
func refusalMessage(message, reason string) string {
	reason = strings.TrimSuffix(strings.TrimSpace(reason), ".")
	if reason == "" {
		return message
	}
	return strings.TrimSuffix(message, ".") + ": " + reason + "."
}

// bodyRefusalReason is why a body doesn't read as the request it should be:
// the field holding the wrong kind of value or one it doesn't take, or that
// it isn't JSON. PocketBase's BindBody decodes with encoding/json/v2, a
// route's own json.Unmarshal with encoding/json.
func bodyRefusalReason(request string, err error) string {
	var wrongKind *json.UnmarshalTypeError
	if errors.As(err, &wrongKind) && wrongKind.Field != "" {
		sent := map[string]string{"string": "text", "number": "a number", "bool": "true or false", "array": "a list", "object": "an object"}[wrongKind.Value]
		return fmt.Sprintf("%s takes %s, not %s", requestFieldIndexes.ReplaceAllString(wrongKind.Field, "[$1]"),
			requestValueKind(wrongKind.Type), firstNonempty(sent, wrongKind.Value))
	}
	var semantic *jsonv2.SemanticError
	if errors.As(err, &semantic) {
		field := requestFieldPath(slices.Collect(semantic.JSONPointer.Tokens()))
		switch {
		case errors.Is(semantic.Err, jsonv2.ErrUnknownName) && field != "":
			return field + " isn't a field this request takes"
		case semantic.GoType != nil && field != "":
			sent := map[jsontext.Kind]string{'"': "text", '0': "a number", 't': "true or false", 'f': "true or false", '[': "a list", '{': "an object", 'n': "null"}[semantic.JSONKind]
			if sent == "" {
				return field + " takes " + requestValueKind(semantic.GoType)
			}
			return field + " takes " + requestValueKind(semantic.GoType) + ", not " + sent
		}
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return "the body is empty or cut short"
	}
	if request == "" {
		return "the body isn't JSON"
	}
	return "the body isn't a " + request + " in JSON"
}

// requestValueKind is what a Go field takes, in a request's words.
func requestValueKind(goType reflect.Type) string {
	for goType != nil && goType.Kind() == reflect.Pointer {
		goType = goType.Elem()
	}
	if goType == nil {
		return "a value"
	}
	switch goType.Kind() {
	case reflect.String:
		return "text"
	case reflect.Bool:
		return "true or false"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "a number"
	case reflect.Slice, reflect.Array:
		if goType.Elem().Kind() == reflect.Uint8 && goType.Name() == "RawMessage" {
			return "a JSON value"
		}
		return "a list"
	case reflect.Interface:
		return "a value"
	}
	return "an object"
}

// requestFieldPath writes a JSON pointer's tokens as a request names the
// field: "operations", "0", "block" is "operations[0].block".
func requestFieldPath(tokens []string) string {
	var path strings.Builder
	for _, token := range tokens {
		if _, err := strconv.Atoi(token); err == nil && path.Len() > 0 {
			path.WriteString("[" + token + "]")
			continue
		}
		if path.Len() > 0 {
			path.WriteString(".")
		}
		path.WriteString(token)
	}
	return path.String()
}

// bodyRefusal refuses a body that doesn't read as its request.
func bodyRefusal(kind error, request string, err error) error {
	return &requestRefusal{kind: kind, reason: bodyRefusalReason(request, err)}
}

// requestFieldIndexes are a field path's list places, ".0" in
// "form.questions.0.position", written as the request writes them, "[0]".
var requestFieldIndexes = regexp.MustCompile(`\.(\d+)\b`)

// readBodyRefusalReason is why a body read as far as limit bytes, one past
// it to tell, can't be the request before it is decoded: unreadable, or
// over the limit — or "". The route decodes it itself, so what it binds
// stays where scripts/native-operation-inputs reads it.
func readBodyRefusalReason(raw []byte, readErr error, limit int) string {
	switch {
	case readErr != nil:
		return "the body can't be read"
	case len(raw) > limit:
		return "the body is over " + requestByteSize(limit)
	}
	return ""
}

// requestByteSize writes a limit as a request's words do: 64 KB, 1 MB.
func requestByteSize(limit int) string {
	switch {
	case limit >= 1<<20 && limit%(1<<20) == 0:
		return strconv.Itoa(limit>>20) + " MB"
	case limit >= 1<<10 && limit%(1<<10) == 0:
		return strconv.Itoa(limit>>10) + " KB"
	}
	return strconv.Itoa(limit) + " bytes"
}
