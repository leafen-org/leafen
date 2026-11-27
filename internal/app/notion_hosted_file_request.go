package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A Notion client may send back a Notion-hosted file object copied from a
// recent response to keep that file. Leafen's hosted file URLs are expiring
// capabilities sealed for the principal that read them, so a copied object
// names its file only through that capability: it must be unmodified and
// unexpired, issued to the same principal, and its file must still be
// readable and downloadable in the destination workspace. The resolved file
// then takes the same path as a completed file_upload.

type notionHostedFileRequest struct {
	URL        string `json:"url"`
	ExpiryTime string `json:"expiry_time"`
}

// parseNotionHostedFileRequest reads the `file` member of a copied file object.
func parseNotionHostedFileRequest(raw any) (notionHostedFileRequest, error) {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return notionHostedFileRequest{}, errors.New("file content is invalid")
	}
	request := notionHostedFileRequest{}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		strings.TrimSpace(request.URL) == "" || strings.TrimSpace(request.ExpiryTime) == "" {
		return notionHostedFileRequest{}, errors.New("file content is invalid")
	}
	return request, nil
}

// notionHostedFileURL reports whether a URL is one of Leafen's hosted file
// capabilities. Such a URL is never stored as an external link: it is bound to
// the principal that read it and expires.
func notionHostedFileURL(value string) bool {
	_, ok := notionHostedFileToken(value)
	return ok
}

func notionHostedFileToken(value string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	prefix := apiRoot + strings.TrimSuffix(fileTransferDownloadCapabilityPath, "{token}")
	if err != nil || parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Host == "" ||
		!strings.HasPrefix(parsed.Path, prefix) {
		return "", false
	}
	return strings.TrimPrefix(parsed.Path, prefix), parsed.RawQuery == "" && parsed.Fragment == ""
}

func notionHostedFileUnavailable() error {
	return graphInputError(errorCodeGraphReferenceInvalid, "The Notion-hosted file is unavailable or its URL has expired.")
}

// resolveNotionHostedFile recovers the managed file a copied hosted file
// object names, checked for this identity and workspace.
func resolveNotionHostedFile(
	app core.App,
	identity notionOAuthBearerIdentity,
	request notionHostedFileRequest,
) (graphFileBlockContent, error) {
	token, ok := notionHostedFileToken(request.URL)
	if !ok {
		return graphFileBlockContent{}, notionHostedFileUnavailable()
	}
	secret, err := fileDownloadCapabilitySecret(app)
	if err != nil {
		return graphFileBlockContent{}, err
	}
	payload, err := openFileDownloadCapabilityToken(secret, token, time.Now().UTC())
	// An embed's response is its URL alone, so it has no expiry time to match.
	if err != nil || payload.UserID != identity.PrincipalID || request.ExpiryTime != "" &&
		strings.TrimSpace(request.ExpiryTime) != time.UnixMilli(payload.ExpiresAt).UTC().Format(time.RFC3339Nano) {
		return graphFileBlockContent{}, notionHostedFileUnavailable()
	}
	record, workspace, err := authorizeWorkspaceFile(app, identity.PrincipalID, payload.FileID, permissionWorkspaceRead)
	if err != nil || workspace.WorkspaceID != identity.WorkspaceID {
		return graphFileBlockContent{}, notionHostedFileUnavailable()
	}
	if organizationID := workspace.Organization.OrganizationID; organizationID != "" {
		if err := enforceOrganizationDLPOutbound(app, organizationID, dlpOutboundFileDownloads); err != nil {
			return graphFileBlockContent{}, err
		}
	}
	return canonicalGraphFileContent(record), nil
}
