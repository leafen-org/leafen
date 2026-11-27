package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

type notionBlockFileCapabilityIssuer struct {
	request     *http.Request
	principalID string
	origin      string
	secret      string
	issuedAt    time.Time
	initialized bool
}

func notionManagedFileBlockCompatible(blockType string, raw json.RawMessage) bool {
	content, err := parseGraphFileBlockContent(raw)
	if err != nil {
		return false
	}
	return notionManagedFileContentCompatible(blockType, content)
}

// officeDocumentPreviewTypes are the files Leafen's own document preview
// draws (internal/app/documentpreview).
var officeDocumentPreviewTypes = map[string]bool{
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   true,
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         true,
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": true,
}

// officeDocumentPreviewable says a file is one of those, by its type or,
// stored without one, by its name's extension, as the client's documentKindOf.
func officeDocumentPreviewable(mediaType, name string) bool {
	if officeDocumentPreviewTypes[mediaType] {
		return true
	}
	if mediaType != "" && mediaType != "application/octet-stream" {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(name))
	return strings.HasSuffix(lower, ".docx") || strings.HasSuffix(lower, ".xlsx") || strings.HasSuffix(lower, ".pptx")
}

func notionManagedFileContentCompatible(blockType string, content graphFileBlockContent) bool {
	contentType := strings.ToLower(strings.TrimSpace(content.ContentType))
	switch blockType {
	case graphBlockTypeFile:
		return true
	case graphBlockTypeImage:
		return strings.HasPrefix(contentType, "image/")
	case graphBlockTypeVideo:
		// An MP4 typed application/mp4 is a video, as Notion's API names it
		// (notionFileUploadKind).
		return strings.HasPrefix(contentType, "video/") || strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]) == "application/mp4"
	case graphBlockTypeAudio:
		return strings.HasPrefix(contentType, "audio/")
	case graphBlockTypePDF:
		return strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]) == "application/pdf"
	case graphBlockTypeEmbed:
		// An uploaded embed holds any file the server keeps, as Notion's
		// embed of an uploaded file does (seen 2026-11-07): a document or
		// media file shown inline, a text file's words, any other file as
		// its row in the frame, and, in a build with the app platform, HTML
		// in a frame with no origin, as Notion's HTML embed. The upload's
		// own admission (admitStoredFileType) keeps active files out.
		if strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]) == sandboxedHTMLContentType {
			return appPlatformAvailable
		}
		_, err := admitStoredFileType(content.Name, contentType)
		return err == nil
	default:
		return false
	}
}

func newNotionBlockFileCapabilityIssuer(
	request *http.Request,
	identity notionOAuthBearerIdentity,
) *notionBlockFileCapabilityIssuer {
	return &notionBlockFileCapabilityIssuer{
		request: request, principalID: identity.PrincipalID, issuedAt: time.Now().UTC(),
	}
}

func (issuer *notionBlockFileCapabilityIssuer) initialize(app core.App) error {
	if issuer == nil || issuer.request == nil && issuer.origin == "" || strings.TrimSpace(issuer.principalID) == "" {
		return errors.New("Notion-compatible managed file response is unavailable")
	}
	if issuer.initialized {
		return nil
	}
	// An issuer made without a request, as MCP's, comes with its origin.
	if issuer.origin == "" {
		origin, err := outwardServerOrigin(issuer.request)
		if err != nil {
			return err
		}
		issuer.origin = origin
	}
	secret, err := fileDownloadCapabilitySecret(app)
	if err != nil {
		return err
	}
	issuer.secret = secret
	issuer.initialized = true
	return nil
}

func (issuer *notionBlockFileCapabilityIssuer) payload(
	app core.App,
	workspaceID string,
	blockType string,
	raw json.RawMessage,
) (map[string]any, error) {
	content, err := parseGraphFileBlockContent(raw)
	if err != nil {
		return nil, err
	}
	capabilityURL, expiresAt, err := issuer.managedFileCapability(
		app, workspaceID, content.FileID,
	)
	if err != nil {
		return nil, err
	}
	if blockType == graphBlockTypeEmbed {
		// An uploaded embed reads as an expiring embed url.
		return map[string]any{"url": capabilityURL}, nil
	}
	caption, err := notionManagedFileCaption(content.Caption)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"type": "file",
		"file": map[string]any{
			"url": capabilityURL, "expiry_time": expiresAt,
		},
		"caption": caption,
	}
	if blockType == graphBlockTypeFile {
		payload["name"] = content.Name
	}
	return payload, nil
}

func notionManagedFileCaption(spans []graphTextSpan) ([]map[string]any, error) {
	encoded, err := json.Marshal(spans)
	if err != nil {
		return nil, err
	}
	value := []any{}
	if json.Unmarshal(encoded, &value) != nil {
		return nil, errors.New("Notion-compatible managed file caption is unavailable")
	}
	caption, ok := notionBlockCaption(map[string]any{"caption": value})
	if !ok {
		return nil, errors.New("Notion-compatible managed file caption is unavailable")
	}
	return caption, nil
}

func (issuer *notionBlockFileCapabilityIssuer) managedFileCapability(
	app core.App,
	workspaceID, fileID string,
) (string, string, error) {
	if issuer == nil {
		return "", "", errors.New("Notion-compatible managed file response is unavailable")
	}
	record, authorization, err := authorizeWorkspaceFile(
		app, issuer.principalID, fileID, permissionWorkspaceRead,
	)
	if err != nil {
		return "", "", err
	}
	if authorization.WorkspaceID != workspaceID {
		return "", "", permissionDenied()
	}
	if organizationID := authorization.Organization.OrganizationID; organizationID != "" {
		if err := enforceOrganizationDLPOutbound(
			app, organizationID, dlpOutboundFileDownloads,
		); err != nil {
			return "", "", err
		}
	}
	if err := issuer.initialize(app); err != nil {
		return "", "", err
	}
	token, expiresAt, err := sealFileDownloadCapabilityToken(
		issuer.secret, issuer.principalID, record.Id, issuer.issuedAt,
	)
	if err != nil {
		return "", "", err
	}
	path := strings.Replace(
		fileTransferDownloadCapabilityPath, "{token}", url.PathEscape(token), 1,
	)
	return issuer.origin + apiRoot + path,
		time.UnixMilli(expiresAt).UTC().Format(time.RFC3339Nano), nil
}

func (issuer *notionBlockFileCapabilityIssuer) customEmojiPayload(
	app core.App,
	workspaceID string,
	emoji storedCustomEmoji,
) (map[string]any, error) {
	capabilityURL, _, err := issuer.managedFileCapability(app, workspaceID, emoji.FileID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id": emoji.ID, "name": emoji.Name, "url": capabilityURL,
	}, nil
}
