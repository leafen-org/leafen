package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// notionBodyRefusal is a body an endpoint couldn't read into its request,
// said as Notion's request check says it: where, what it should be and what
// it was. It covers what the endpoint's shape (notion_request_shape_routes.go)
// doesn't check.
type notionBodyRefusal struct {
	message     string
	invalidJSON bool
	cause       error
}

func (refusal *notionBodyRefusal) Error() string { return refusal.message }
func (refusal *notionBodyRefusal) Unwrap() error { return refusal.cause }

// bindNotionStrictJSON reads a body into output, refusing a key output
// doesn't take unless the endpoint's shape, as Notion's, lets other keys be.
// An empty body reads as an empty object, as Notion reads one and a body of
// a type it doesn't read (seen 2026-11-10: a page made from a text/plain
// body is refused as one made from {}).
func bindNotionStrictJSON(event *core.RequestEvent, output any) error {
	event.Request.Body = http.MaxBytesReader(
		event.Response, event.Request.Body, notionPublicAPIMaxReadBytes,
	)
	raw, err := io.ReadAll(event.Request.Body)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = []byte("{}")
	}
	// The body as written keeps its keys' order (notionRequestKeyOrder).
	event.Set(notionRequestBodyKey, raw)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	open, _ := event.Get(notionBodyTakesOtherKeys).(bool)
	if !open {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(output); err != nil {
		if errors.Is(err, io.EOF) {
			return err
		}
		return notionBodyRefusalFor(raw, reflect.TypeOf(output), err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return &notionBodyRefusal{message: notionInvalidJSONMessage, invalidJSON: true, cause: err}
	}
	return nil
}

// notionWholeNumber is a count Notion reads from any JSON number, the
// fraction cut off (seen 2026-11-10: a search's page_size 1.5 lists one).
type notionWholeNumber int

func (number *notionWholeNumber) UnmarshalJSON(raw []byte) error {
	var read float64
	if err := json.Unmarshal(raw, &read); err != nil {
		return err
	}
	*number = notionWholeNumber(max(min(math.Trunc(read), math.MaxInt32), math.MinInt32))
	return nil
}

// notionBindRefused answers a body bindNotionStrictJSON refused.
func notionBindRefused(event *core.RequestEvent, err error) error {
	var refusal *notionBodyRefusal
	if errors.As(err, &refusal) && refusal.invalidJSON {
		return notionCompatibilityError(event, http.StatusBadRequest, notionErrorInvalidJSON, notionInvalidJSONMessage)
	}
	if errors.As(err, &refusal) {
		return notionValidationRefused(event, refusal.message)
	}
	return notionValidationRefused(event, notionAdminValidationReason(err))
}

func notionBodyRefusalFor(raw []byte, target reflect.Type, cause error) error {
	value, err := parseNotionValue(raw)
	if err != nil || value.kind != 'o' && value.kind != 'a' {
		return &notionBodyRefusal{message: notionInvalidJSONMessage, invalidJSON: true, cause: cause}
	}
	for target.Kind() == reflect.Pointer {
		target = target.Elem()
	}
	faults := notionGoShapeFaults(target, value, []string{"body"}, false)
	if len(faults) == 0 {
		return &notionBodyRefusal{message: notionAdminValidationReason(cause), cause: cause}
	}
	return &notionBodyRefusal{message: notionFaultsMessage("body", faults), cause: cause}
}

var (
	notionRawMessageType  = reflect.TypeOf(json.RawMessage{})
	notionUnmarshalerType = reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()
)

// notionGoShapeFaults is where value doesn't fit what an endpoint reads it
// into: the first key that has the wrong kind, else every key it doesn't
// take, as Notion's check names them.
func notionGoShapeFaults(target reflect.Type, value *notionValue, path []string, optional bool) []notionFault {
	nullable := false
	for target.Kind() == reflect.Pointer {
		target, nullable = target.Elem(), true
	}
	if value.kind == 'z' || target == notionRawMessageType || target.Kind() == reflect.Interface ||
		reflect.PointerTo(target).Implements(notionUnmarshalerType) {
		return nil
	}
	expected, fits := "", true
	switch target.Kind() {
	case reflect.Struct, reflect.Map:
		expected, fits = "an object", value.kind == 'o'
	case reflect.Slice, reflect.Array:
		expected, fits = "an array", value.kind == 'a'
	case reflect.String:
		expected, fits = "a string", value.kind == 's'
	case reflect.Bool:
		expected, fits = "a boolean", value.kind == 'b'
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		expected, fits = "a number", value.kind == 'n'
	}
	if !fits {
		expect := []string{expected}
		if optional || nullable {
			expect = append(expect, "`undefined`")
		}
		return []notionFault{{path: path, expect: expect, shown: notionShownValue(value)}}
	}
	switch target.Kind() {
	case reflect.Struct:
		return notionGoStructFaults(target, value, path)
	case reflect.Map:
		for _, key := range value.keys {
			if faults := notionGoShapeFaults(target.Elem(), value.fields[key], notionChildPath(path, key), false); len(faults) > 0 {
				return faults
			}
		}
	case reflect.Slice, reflect.Array:
		for index, item := range value.items {
			if faults := notionGoShapeFaults(target.Elem(), item, notionChildPath(path, "["+strconv.Itoa(index)+"]"), false); len(faults) > 0 {
				return faults
			}
		}
	}
	return nil
}

type notionGoField struct {
	field    reflect.StructField
	optional bool
}

func notionGoStructFields(target reflect.Type, fields map[string]notionGoField) {
	for index := 0; index < target.NumField(); index++ {
		field := target.Field(index)
		tag := field.Tag.Get("json")
		name, options, _ := strings.Cut(tag, ",")
		if name == "-" || !field.IsExported() && !field.Anonymous {
			continue
		}
		if field.Anonymous && name == "" && field.Type.Kind() == reflect.Struct {
			notionGoStructFields(field.Type, fields)
			continue
		}
		if name == "" {
			name = field.Name
		}
		fields[name] = notionGoField{field: field, optional: jsonTagOmits(options) || field.Type.Kind() == reflect.Pointer}
	}
}

func notionGoStructFaults(target reflect.Type, value *notionValue, path []string) []notionFault {
	fields := map[string]notionGoField{}
	notionGoStructFields(target, fields)
	lookup := func(key string) (notionGoField, bool) {
		if field, found := fields[key]; found {
			return field, true
		}
		// encoding/json matches a key in any case.
		for name, field := range fields {
			if strings.EqualFold(name, key) {
				return field, true
			}
		}
		return notionGoField{}, false
	}
	unknown := []notionFault{}
	for _, key := range value.keys {
		field, found := lookup(key)
		if !found {
			unknown = append(unknown, notionFault{path: notionChildPath(path, key), expect: []string{"not present"}, shown: notionShownValue(value.fields[key])})
			continue
		}
		if faults := notionGoShapeFaults(field.field.Type, value.fields[key], notionChildPath(path, key), field.optional); len(faults) > 0 {
			return faults
		}
	}
	return unknown
}
