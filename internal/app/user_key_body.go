package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/url"
	"strings"
)

// userKeyBody is a request body as a key's checks read its fields: the way
// its handler will, so the two never see different targets. A JSON body's
// names match without regard to case, as the handler's decoder takes them
// (PocketBase's BindBody), so every spelling of a field is read; a form —
// multipart or URL-encoded — gives its values by name the same way.
// (2026-10-13: the checks read JSON by exact name and passed what they
// couldn't read, so TargetPageId or a multipart body slipped past them, and
// a key's multipart file upload was refused.)
type userKeyBody struct {
	json map[string]json.RawMessage
	form map[string][]string
	// readable is false when the body is neither JSON nor a form, or doesn't
	// parse as what it says it is: nothing in it can be checked.
	readable bool
	empty    bool
}

func readUserKeyBodyFields(contentType string, body []byte) userKeyBody {
	if len(bytes.TrimSpace(body)) == 0 {
		return userKeyBody{readable: true, empty: true}
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = ""
	}
	switch {
	case mediaType == "" || mediaType == "application/json" || strings.HasSuffix(mediaType, "+json"):
		fields := map[string]json.RawMessage{}
		if json.Unmarshal(body, &fields) != nil {
			return userKeyBody{}
		}
		return userKeyBody{json: fields, readable: true}
	case mediaType == "application/x-www-form-urlencoded":
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return userKeyBody{}
		}
		return userKeyBody{form: values, readable: true}
	case mediaType == "multipart/form-data" && params["boundary"] != "":
		form := map[string][]string{}
		reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
		for {
			part, err := reader.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				// A body longer than the checks read ends early; the fields
				// before its end are what can be checked.
				break
			}
			if part.FileName() == "" && part.FormName() != "" {
				value, readErr := io.ReadAll(io.LimitReader(part, 1<<20))
				if readErr == nil {
					form[part.FormName()] = append(form[part.FormName()], string(value))
				}
			}
			_ = part.Close()
		}
		return userKeyBody{form: form, readable: true}
	}
	return userKeyBody{}
}

// values are the strings a body gives a field, by any spelling of its name:
// a string, a list's strings, a form's values.
func (body userKeyBody) values(field string) []string {
	found := []string{}
	for name, raw := range body.json {
		if !strings.EqualFold(name, field) {
			continue
		}
		var single string
		var several []string
		if json.Unmarshal(raw, &single) == nil {
			found = append(found, single)
		} else if json.Unmarshal(raw, &several) == nil {
			found = append(found, several...)
		}
	}
	for name, values := range body.form {
		if strings.EqualFold(name, field) {
			found = append(found, values...)
		}
	}
	return found
}

// one is the single value a body gives a field; two different ones, under
// different spellings, can't tell which the handler takes.
func (body userKeyBody) one(field string) (string, bool) {
	value := ""
	for _, candidate := range body.values(field) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if value != "" && value != candidate {
			return "", false
		}
		value = candidate
	}
	return value, true
}
