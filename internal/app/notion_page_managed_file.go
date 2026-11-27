package app

import (
	"errors"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

func validateNotionPagePresentationReferences(
	app core.App,
	principalID, workspaceID string,
	presentation *notionPagePresentationRequest,
) error {
	if presentation == nil {
		return graphInputError(errorCodeGraphReferenceInvalid, "Page presentation is invalid.")
	}
	if presentation.IconCustomEmoji {
		if presentation.Icon == nil {
			return graphInputError(errorCodeGraphReferenceInvalid, "Page custom_emoji is invalid.")
		}
		_, accessible, err := loadNotionCustomEmojiMention(
			app, principalID, workspaceID, *presentation.Icon,
		)
		if err != nil {
			return err
		}
		if !accessible {
			return graphInputError(errorCodeGraphReferenceInvalid, notionCustomEmojiMissing(*presentation.Icon))
		}
	}
	identity := notionOAuthBearerIdentity{PrincipalID: principalID, WorkspaceID: workspaceID}
	resolved := map[string]graphFileBlockContent{}
	for _, value := range []*string{presentation.Icon, presentation.Cover} {
		if value == nil {
			continue
		}
		managed := value == presentation.Icon && presentation.IconManaged ||
			value == presentation.Cover && presentation.CoverManaged
		if !managed {
			continue
		}
		publicID := *value
		file, found := resolved[publicID]
		if !found {
			var err error
			file, err = resolveNotionCompletedFileUpload(app, identity, publicID)
			if err != nil {
				return err
			}
			resolved[publicID] = file
		}
		if kind := notionFileUploadKind(file.ContentType); kind != "image" {
			return notionFileUploadKindRefused(publicID, kind, "image")
		}
		*value = file.FileID
	}
	return nil
}

// notionFileUploadKind is the kind Notion's API names an uploaded file by
// where it takes one kind (seen 2026-11-10: a page's icon and cover, and a
// picture's, video's, sound's and PDF's file): a picture, a video
// (application/mp4 too), a sound, a PDF, or productivity for the rest, text,
// tables, archives, models and drawings (image/vnd.dwg, image/vnd.dxf) alike.
func notionFileUploadKind(contentType string) string {
	contentType = strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	switch {
	case contentType == "image/vnd.dwg" || contentType == "image/vnd.dxf":
		return "productivity"
	case strings.HasPrefix(contentType, "image/"):
		return "image"
	case strings.HasPrefix(contentType, "video/") || contentType == "application/mp4":
		return "video"
	case strings.HasPrefix(contentType, "audio/"):
		return "audio"
	case contentType == "application/pdf":
		return "pdf"
	}
	return "productivity"
}

// notionFileUploadKindRefused is Notion's refusal of an upload of another
// kind (notionFileUploadKind) where it takes only expected's.
func notionFileUploadKindRefused(publicID, kind, expected string) error {
	return graphInputError(errorCodeGraphReferenceInvalid, "File upload "+notionFileUploadNamed(publicID)+
		" is a "+kind+" file, and only "+expected+" files go here.")
}

// notionCustomEmojiMissing is Notion's refusal of a custom emoji its
// workspace hasn't (seen 2026-11-10: a page's icon, and a callout's, there in
// a new block's faults), naming it as given, with its dashes (2026-11-11).
func notionCustomEmojiMissing(id string) string {
	return `This workspace has no custom emoji "` + notionDashedUUID(strings.TrimSpace(id)) + `".`
}

func notionPageManagedFileObject(
	app core.App,
	issuer *notionBlockFileCapabilityIssuer,
	workspaceID, fileID string,
) (map[string]any, error) {
	if issuer == nil {
		return nil, errors.New("Notion-compatible page file response is unavailable")
	}
	url, expiresAt, err := issuer.managedFileCapability(app, workspaceID, fileID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"type": "file",
		"file": map[string]any{"url": url, "expiry_time": expiresAt},
	}, nil
}

func notionPageIconWithManagedFile(
	app core.App,
	issuer *notionBlockFileCapabilityIssuer,
	workspaceID, kind, value string,
) (any, error) {
	value = strings.TrimSpace(value)
	if kind == "custom_emoji" {
		if issuer == nil {
			return nil, errors.New("Notion-compatible custom emoji response is unavailable")
		}
		emoji, accessible, err := loadNotionCustomEmojiMention(
			app, issuer.principalID, workspaceID, value,
		)
		if err != nil || !accessible {
			return nil, err
		}
		payload, err := issuer.customEmojiPayload(app, workspaceID, emoji)
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "custom_emoji", "custom_emoji": payload}, nil
	}
	if fileID, managed := productFilePresentationID(value); kind == "image" && managed {
		return notionPageManagedFileObject(app, issuer, workspaceID, fileID)
	}
	return notionPageIcon(kind, value), nil
}

func notionPageCoverWithManagedFile(
	app core.App,
	issuer *notionBlockFileCapabilityIssuer,
	workspaceID, value string,
) (any, error) {
	value = strings.TrimSpace(value)
	if fileID, managed := productFilePresentationID(value); managed {
		return notionPageManagedFileObject(app, issuer, workspaceID, fileID)
	}
	if bundledGalleryCoverPaths[value] {
		if issuer == nil {
			return nil, errors.New("Notion-compatible cover origin is unavailable")
		}
		origin := issuer.origin
		if origin == "" && issuer.request != nil {
			var err error
			origin, err = outwardServerOrigin(issuer.request)
			if err != nil {
				return nil, err
			}
		}
		if origin == "" {
			return nil, errors.New("Notion-compatible cover origin is unavailable")
		}
		return notionPageExternalFile(origin + value), nil
	}
	return notionPageExternalFile(value), nil
}
