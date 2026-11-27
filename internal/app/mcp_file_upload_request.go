package app

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// An upload address says how a file goes to it, and a file sent to it any
// other way is told the way that works (2026-11-26: an agent given only
// upload_url and empty upload_headers sent its file another way first and
// heard "This server has no such API."). notion-create-file-upload's answer
// gives it as upload_request beside the address; a request by another
// method, or with a body that isn't one file in the multipart field "file",
// is refused with the same way and a curl line, as a refusal for its reason
// (refusedFor), never as a missing API or a bare "Check the file upload
// request."

const errorCodeMCPFileUploadMethod = "LEAFEN_MCP_FILE_UPLOAD_METHOD"

// mcpFileUploadFormField is the multipart field the file goes in.
const mcpFileUploadFormField = "file"

// mcpFileUploadRequest is how a file goes to an upload address.
type mcpFileUploadRequest struct {
	Method    string `json:"method" jsonschema:"The HTTP method: POST"`
	Body      string `json:"body" jsonschema:"The body's type: multipart/form-data, one file part"`
	FormField string `json:"form_field" jsonschema:"The multipart field the file goes in"`
	Curl      string `json:"curl" jsonschema:"The request as one curl line: put the local file's path for <local file>"`
}

func mcpFileUploadRequestTo(uploadURL string) *mcpFileUploadRequest {
	return &mcpFileUploadRequest{
		Method: http.MethodPost, Body: "multipart/form-data", FormField: mcpFileUploadFormField,
		Curl: mcpFileUploadCurl(uploadURL),
	}
}

// mcpFileUploadCurl is the upload as one curl line; the address is written
// in single quotes, which it never holds (its token is base64url).
func mcpFileUploadCurl(uploadURL string) string {
	return "curl -sS -F '" + mcpFileUploadFormField + "=@<local file>' '" + uploadURL + "'"
}

// mcpFileUploadWay is the way, said after what a request did otherwise.
func mcpFileUploadWay() string {
	return fmt.Sprintf("Send it by POST as multipart/form-data, one file of at most %d MiB in the field %q: %s.",
		mcpFileUploadMaximumBytes>>20, mcpFileUploadFormField, mcpFileUploadCurl("<upload_url>"))
}

// registerMCPFileUploadRoutes takes an upload at its address by POST, and
// answers any other method with the way.
func registerMCPFileUploadRoutes(event *core.ServeEvent) {
	event.Router.POST(mcpFileUploadCapabilityPath, uploadMCPFileWithCapability).
		Bind(apis.BodyLimit(mcpFileUploadMaximumBytes + mcpFileUploadMultipartAllowance))
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		event.Router.Route(method, mcpFileUploadCapabilityPath, refuseMCPFileUploadMethod)
	}
}

func refuseMCPFileUploadMethod(event *core.RequestEvent) error {
	event.Response.Header().Set("Allow", http.MethodPost)
	return writeMCPFileUploadCapabilityError(event, refusedFor(http.StatusMethodNotAllowed, errorCodeMCPFileUploadMethod,
		"This upload address takes no "+event.Request.Method+" request. "+mcpFileUploadWay()), "method")
}

// mcpFileUploadBodyRefused says what a POST's body held otherwise than one
// file in the field, and the way.
func mcpFileUploadBodyRefused(event *core.RequestEvent, files int, empty bool) error {
	said := "This upload holds no file in the field \"" + mcpFileUploadFormField + "\""
	contentType := strings.ToLower(strings.TrimSpace(event.Request.Header.Get("Content-Type")))
	switch {
	case !strings.HasPrefix(contentType, "multipart/form-data"):
		said = "This upload's body isn't multipart/form-data"
		if contentType != "" {
			said += " (it was " + strings.SplitN(contentType, ";", 2)[0] + ")"
		}
	case files > 1:
		said = fmt.Sprintf("This upload holds %d files in the field \"%s\", where it takes one", files, mcpFileUploadFormField)
	case empty:
		said = "The file this upload holds is empty"
	case event.Request.MultipartForm != nil && len(event.Request.MultipartForm.File) > 0:
		names := []string{}
		for name := range event.Request.MultipartForm.File {
			if len(name) > 40 {
				name = name[:40] + "…"
			}
			names = append(names, fmt.Sprintf("%q", name))
		}
		slices.Sort(names)
		if len(names) > 3 {
			names = append(names[:3], "…")
		}
		said += " — it sent " + strings.Join(names, ", ")
	}
	return refusedFor(http.StatusBadRequest, errorCodeFileUploadInvalid, said+". "+mcpFileUploadWay(),
		apiErrorField{Name: mcpFileUploadFormField, Problem: "one file as a multipart/form-data part"})
}
