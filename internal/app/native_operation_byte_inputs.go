package app

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
)

// nativeOperationByteInput is what an operation that reads its body as
// bytes, not JSON, takes: what the bytes are, the content type they go as,
// and the headers beside them, each with what it holds. leafen_operations
// says so for the operation, and leafen_call sends body_base64 as those
// bytes with those headers and no others (2026-11-13: an archive's parts,
// taken only as bytes with a part hash header, couldn't go through
// leafen_call, which sent every body as JSON).
type nativeOperationByteInput struct {
	Body        string            `json:"body"`
	ContentType string            `json:"content_type"`
	Headers     map[string]string `json:"headers,omitempty"`
}

const (
	nativeOperationPartHashHeader = "X-Leafen-Part-SHA256"
	nativeOperationPartHash       = "the part's SHA-256, in hex"
)

// nativeOperationPartBytes says what one part of a resumable upload is and
// what completes the upload.
func nativeOperationPartBytes(what, complete string) string {
	return fmt.Sprintf("Part partNumber of %s: the %d bytes from partNumber × partBytes (the last part what remains), "+
		"in order from the nextPart preparing answered. Once every part is in, %s with the requestId the upload was prepared with.",
		what, fileTransferPartBytes, complete)
}

var nativeOperationByteInputs = withServerMoveDAV(map[string]nativeOperationByteInput{
	"POST " + workspaceImportArchiveCreatePath: {
		Body: fmt.Sprintf("A whole Leafen archive (.leafen.zip) of at most %d bytes, restored into this empty workspace; a larger one goes up in parts "+
			"(POST %s). It answers the import, which GET %s follows to completed.",
			workspaceImportArchiveDirectBytes, workspaceArchiveUploadPreparePath, workspaceImportReadPath),
		ContentType: workspaceExportArchiveMIME,
		Headers: map[string]string{
			"X-Leafen-Request-Id": "an ID you choose for this import; the same ID with the same archive answers the same import",
		},
	},
	"PUT " + workspaceArchiveUploadPartPath: {
		Body: nativeOperationPartBytes("the Leafen archive (prepared with its whole size as bytes and its SHA-256 as sha256)",
			"POST "+workspaceArchiveUploadCompletePath+" — it answers the import, which GET "+workspaceImportReadPath+" follows —"),
		ContentType: "application/octet-stream",
		Headers:     map[string]string{nativeOperationPartHashHeader: nativeOperationPartHash},
	},
	"PUT " + fileTransferPartPath: {
		Body:        nativeOperationPartBytes("the file", "POST "+fileTransferCompletePath),
		ContentType: "application/octet-stream",
		Headers:     map[string]string{nativeOperationPartHashHeader: nativeOperationPartHash},
	},
	"PUT /forms/files/uploads/{uploadId}/parts/{partNumber}": {
		Body:        nativeOperationPartBytes("a form's file", "POST /forms/files/uploads/{uploadId}/complete with the form's token and"),
		ContentType: "application/octet-stream",
		Headers: map[string]string{
			nativeOperationPartHashHeader: nativeOperationPartHash,
			formFileTokenHeader:           "the form's token, as preparing the upload took it",
		},
	},
	"POST " + fileTransferDirectUploadPath: {
		Body: fmt.Sprintf("A multipart form: file (one file of at most %d bytes, with its name and content type), requestId, workspaceId, "+
			"and pageId with purpose for a page's file; a larger file goes up in parts (POST %s).", fileTransferDirectBytes, fileTransferPreparePath),
		ContentType: "multipart/form-data; boundary=… (the boundary your form uses)",
	},
	"POST /forms/files": {
		Body: fmt.Sprintf("A multipart form: file (one file of at most %d bytes), token (the form's), propertyId and requestId; "+
			"a larger file goes up in parts (POST /forms/files/uploads).", fileTransferDirectBytes),
		ContentType: "multipart/form-data; boundary=… (the boundary your form uses)",
	},
	"POST /account/avatar-photo": {
		Body: fmt.Sprintf("A JPEG, PNG, WebP or GIF image of at most %d bytes. It answers the photo's address as avatar, "+
			"which PATCH /account/profile saves as the person's picture.", accountPhotoMaxBytes),
		ContentType: "the image's own, such as image/png",
	},
	"POST " + appPlatformPackageUploadPath: {
		Body:        "A .leafenapp package, up to this server's package size. It answers what the app asks for, not yet installed.",
		ContentType: "application/zip",
	},
	// A closed network's catalog, trusted from its bundle file rather than
	// fetched from an address (POST /admin/apps/catalogs).
	"POST " + appPlatformCatalogUploadPath: {
		Body: fmt.Sprintf("A multipart form: bundle (the catalog's bundle file — a zip of its signed index, its signature and the packages "+
			"the index names — of at most %d bytes), name (what the catalog is called here) and publicKey (the key its index is signed with). "+
			"It answers the catalogs as they now are.", appCatalogBundleMaxBytes),
		ContentType: "multipart/form-data; boundary=… (the boundary your form uses)",
	},
	// What another program, not a connection, sends: the server a workspace
	// moves from and an organization's identity provider. A connection's key
	// opens none of them (each checks its own move code or signed answer),
	// but each is listed, so its input is said rather than left as JSON it
	// doesn't read.
	"PUT /workspace-move/in/{id}/bundle": {
		Body:        fmt.Sprintf("The moving workspace's Leafen archive, of at most %d bytes, %s", workspaceMoveBundleBytes, nativeOperationMoveSender),
		ContentType: workspaceExportArchiveMIME,
	},
	"PUT /workspace-move/in/{id}/history": {
		Body:        fmt.Sprintf("The moving workspace's history, one JSON record a line, of at most %d bytes, %s", workspaceMoveBundleBytes, nativeOperationMoveSender),
		ContentType: "application/x-ndjson",
	},
	"PUT /workspace-move/in/{id}/history-files/{file}": {
		Body:        fmt.Sprintf("A file only the moving workspace's history names, of at most %d bytes, %s", workspaceMoveBundleBytes, nativeOperationMoveSender),
		ContentType: "application/octet-stream",
		Headers:     map[string]string{"X-Leafen-File-Sha256": "the file's SHA-256, in hex"},
	},
	"POST /auth/saml/acs": {
		Body: "The organization's identity provider's signed answer to a single sign-on, posted by the person's browser as a form: " +
			"SAMLResponse (base64) and RelayState. Only that signed answer logs anyone in.",
		ContentType: "application/x-www-form-urlencoded",
	},
	"POST " + strings.TrimPrefix(collaborationStateMissingPath, apiRoot): {
		Body: fmt.Sprintf("The state vector of a copy of the room's document (Yjs's, version 1 encoding), of at most %d bytes: "+
			"it answers what that copy lacks, as an update.", collaborationMissingMaxBodyBytes),
		ContentType: "application/octet-stream",
	},
})

// withServerMoveDAV adds what the WebDAV methods that take a body take of
// the server this one moves from: a file of its data folder, or WebDAV's
// XML. Only that server opens them, with its move code.
func withServerMoveDAV(inputs map[string]nativeOperationByteInput) map[string]nativeOperationByteInput {
	path := " /server-move/in/{id}/dav/{path...}"
	sender := "sent over WebDAV by the server this one moves from, which logs in with the move code; a connection's key doesn't open it."
	inputs[http.MethodPut+path] = nativeOperationByteInput{Body: "A file of the moving server's data folder, written at path, " + sender,
		ContentType: "application/octet-stream"}
	for _, method := range []string{"PROPFIND", "PROPPATCH", "LOCK"} {
		inputs[method+path] = nativeOperationByteInput{Body: "WebDAV's " + method + " request in XML, " + sender, ContentType: "application/xml"}
	}
	return inputs
}

// nativeOperationMoveSender says who sends a workspace moving here its bytes.
const nativeOperationMoveSender = "sent by the server the workspace moves from, which logs in with the move code " +
	"(its id and secret, as basic login); a connection's key doesn't open it."

// nativeOperationByteInputFor is the byte input of the operation a method
// and a filled-in path name, if it takes one.
func nativeOperationByteInputFor(method, path string) (nativeOperationByteInput, bool) {
	for id, input := range nativeOperationByteInputs {
		routeMethod, routePath, _ := strings.Cut(id, " ")
		route := userKeyRoute(routeMethod, routePath, userKeyScopeClosed, userKeyPlacePerson, "")
		if route.allows(strings.ToUpper(method)) && route.pattern.MatchString(path) {
			return input, true
		}
	}
	return nativeOperationByteInput{}, false
}

// leafenCallRequestHeaders are the headers leafen_call sends: an operation
// that takes bytes gets its content type and the headers its input names;
// any other operation takes a JSON body and no headers.
func leafenCallRequestHeaders(method, path string, bytes bool, given map[string]string) (http.Header, error) {
	input, takesBytes := nativeOperationByteInputFor(method, path)
	header := http.Header{}
	if !takesBytes {
		if bytes || len(given) > 0 {
			return nil, fmt.Errorf("%w: %s %s takes a JSON body (body) and no headers; body_base64 and headers are for an operation whose input is bytes",
				errMCPToolInvalid, strings.ToUpper(method), path)
		}
		header.Set("Content-Type", "application/json")
		return header, nil
	}
	if !bytes {
		return nil, fmt.Errorf("%w: %s %s takes bytes: give them as body_base64 with the headers leafen_operations names for it",
			errMCPToolInvalid, strings.ToUpper(method), path)
	}
	if !strings.Contains(input.ContentType, "…") && !strings.Contains(input.ContentType, " ") {
		header.Set("Content-Type", input.ContentType)
	}
	named := []string{"Content-Type"}
	for name := range input.Headers {
		named = append(named, name)
	}
	slices.Sort(named)
	for name, value := range given {
		known := slices.IndexFunc(named, func(candidate string) bool { return strings.EqualFold(candidate, name) })
		if known < 0 {
			return nil, fmt.Errorf("%w: %s %s takes the headers %s, not %s",
				errMCPToolInvalid, strings.ToUpper(method), path, strings.Join(named, ", "), name)
		}
		header.Set(named[known], strings.TrimSpace(value))
	}
	if header.Get("Content-Type") == "" {
		return nil, fmt.Errorf("%w: %s %s takes the Content-Type header: %s", errMCPToolInvalid, strings.ToUpper(method), path, input.ContentType)
	}
	return header, nil
}
