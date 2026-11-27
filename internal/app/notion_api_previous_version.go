package app

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// Notion's 2025-09-03 API version — the one data sources came with, and the
// official SDK's default — differs from the version served
// (notionCurrentAPIVersion, 2026-03-11) in three names, as Notion's upgrade
// guide for 2026-03-11 lists them: "archived" where "in_trash" is now, in
// request and response bodies; append children's "after" where "position"
// is now; and the "transcription" block where "meeting_notes" is now. A
// request at 2025-09-03 is turned into the served version's at the door and
// its answer back, so a program at that version works here as on Notion.
// Webhook payloads are the same at both versions and aren't touched.
const notionPreviousAPIVersion = "2025-09-03"

// A JSON body larger than this passes as it is; the handlers' own limits
// are lower.
const notionPreviousVersionBodyLimit = 8 << 20

func serveNotionAtPreviousVersion(event *core.RequestEvent, serve func() error) error {
	if strings.TrimSpace(event.Request.Header.Get("Notion-Version")) != notionPreviousAPIVersion {
		return serve()
	}
	event.Request.Header.Set("Notion-Version", notionCurrentAPIVersion)
	notionPreviousVersionRequest(event.Request)
	original := event.Response
	recorder := &notionVersionRecorder{header: http.Header{}, status: http.StatusOK}
	event.Response = recorder
	err := serve()
	event.Response = original
	if !recorder.wrote {
		// Nothing was answered: the error, if any, is answered as usual.
		return err
	}
	body := recorder.body.Bytes()
	if notionJSONContent(recorder.header.Get("Content-Type")) {
		body = notionPreviousVersionAnswer(body)
	}
	for key, values := range recorder.header {
		original.Header()[key] = values
	}
	original.Header().Set("Content-Length", strconv.Itoa(len(body)))
	original.WriteHeader(recorder.status)
	_, _ = original.Write(body)
	return err
}

// notionPreviousVersionRequest names a 2025-09-03 request body's fields as
// the served version does: "archived" is "in_trash" (Notion took either at
// that version, "in_trash" first), and append children's "after" is a
// position after that block.
func notionPreviousVersionRequest(request *http.Request) {
	if request.Body == nil || request.Body == http.NoBody {
		return
	}
	if kind := request.Header.Get("Content-Type"); kind != "" && !notionJSONContent(kind) {
		return
	}
	raw, err := io.ReadAll(io.LimitReader(request.Body, notionPreviousVersionBodyLimit+1))
	if err != nil || len(raw) > notionPreviousVersionBodyLimit {
		request.Body = io.NopCloser(io.MultiReader(bytes.NewReader(raw), request.Body))
		return
	}
	body := map[string]json.RawMessage{}
	changed := false
	if json.Unmarshal(raw, &body) == nil && body != nil {
		if archived, ok := body["archived"]; ok {
			if _, given := body["in_trash"]; !given {
				body["in_trash"] = archived
			}
			delete(body, "archived")
			changed = true
		}
		after, ok := body["after"]
		afterID := ""
		if ok && request.Method == http.MethodPatch && strings.HasSuffix(request.URL.Path, "/children") &&
			json.Unmarshal(after, &afterID) == nil {
			if _, given := body["position"]; !given {
				body["position"], _ = json.Marshal(map[string]any{
					"type": "after_block", "after_block": map[string]string{"id": afterID},
				})
			}
			delete(body, "after")
			changed = true
		}
	}
	if changed {
		if rewritten, err := json.Marshal(body); err == nil {
			raw = rewritten
		}
	}
	request.Body = io.NopCloser(bytes.NewReader(raw))
	request.ContentLength = int64(len(raw))
	request.Header.Set("Content-Length", strconv.Itoa(len(raw)))
}

// notionPreviousVersionAnswer gives an answer the 2025-09-03 names: every
// object with "in_trash" also says "archived", and a "meeting_notes" block is
// a "transcription" block.
func notionPreviousVersionAnswer(body []byte) []byte {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil || !notionPreviousVersionValue(value) {
		return body
	}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if encoder.Encode(value) != nil {
		return body
	}
	return bytes.TrimRight(out.Bytes(), "\n")
}

func notionPreviousVersionValue(value any) bool {
	changed := false
	switch typed := value.(type) {
	case map[string]any:
		for _, child := range typed {
			if notionPreviousVersionValue(child) {
				changed = true
			}
		}
		if trash, ok := typed["in_trash"].(bool); ok {
			if _, has := typed["archived"]; !has {
				typed["archived"] = trash
				changed = true
			}
		}
		if typed["object"] == "block" && typed["type"] == "meeting_notes" {
			typed["type"] = "transcription"
			if content, ok := typed["meeting_notes"]; ok {
				typed["transcription"] = content
				delete(typed, "meeting_notes")
			}
			changed = true
		}
	case []any:
		for _, child := range typed {
			if notionPreviousVersionValue(child) {
				changed = true
			}
		}
	}
	return changed
}

func notionJSONContent(kind string) bool {
	media, _, err := mime.ParseMediaType(kind)
	return err == nil && (media == "application/json" || strings.HasSuffix(media, "+json"))
}

// notionVersionRecorder holds an answer until its names are turned back.
type notionVersionRecorder struct {
	header http.Header
	status int
	body   bytes.Buffer
	wrote  bool
}

func (recorder *notionVersionRecorder) Header() http.Header { return recorder.header }

func (recorder *notionVersionRecorder) WriteHeader(status int) {
	if recorder.wrote {
		return
	}
	recorder.status, recorder.wrote = status, true
}

func (recorder *notionVersionRecorder) Write(data []byte) (int, error) {
	recorder.wrote = true
	return recorder.body.Write(data)
}
