package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

func authorizeNotionBlockCustomEmojiOperations(
	app core.App,
	identity notionOAuthBearerIdentity,
	current pageAuthorization,
	operations []graphOperation,
) error {
	checked := map[string]bool{}
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertBlock || operation.Block == nil {
			continue
		}
		content := struct {
			CustomEmojiID string `json:"customEmojiId"`
		}{}
		if json.Unmarshal(operation.Block.Content, &content) != nil {
			return graphInputError(errorCodeGraphReferenceInvalid, "The block icon is invalid.")
		}
		customEmojiID := strings.TrimSpace(content.CustomEmojiID)
		if customEmojiID == "" {
			continue
		}
		if operation.Block.Type != graphBlockTypeCallout || !validCustomEmojiID(customEmojiID) {
			return graphInputError(errorCodeGraphReferenceInvalid, "The custom emoji block icon is invalid.")
		}
		accessible, found := checked[customEmojiID]
		if !found {
			var err error
			_, accessible, err = loadNotionCustomEmojiMention(
				app, identity.PrincipalID, current.Workspace.WorkspaceID, customEmojiID,
			)
			if err != nil {
				return err
			}
			checked[customEmojiID] = accessible
		}
		if !accessible {
			return notionBlockCustomEmojiMissing(app, operation.Block.ID, customEmojiID)
		}
	}
	return nil
}

// notionCustomEmojiContentCheck is a new callout's custom emoji its
// workspace hasn't, as a fault in the content Notion's API would make, said
// with the request's others in their order (seen 2026-11-10); the write's
// own check says one it can't read.
func notionCustomEmojiContentCheck(app core.App, principalID, workspaceID string) func(*graphBlock) string {
	checked := map[string]bool{}
	return func(block *graphBlock) string {
		if block == nil || block.Type != graphBlockTypeCallout {
			return ""
		}
		content := struct {
			CustomEmojiID string `json:"customEmojiId"`
		}{}
		if json.Unmarshal(block.Content, &content) != nil {
			return ""
		}
		customEmojiID := strings.TrimSpace(content.CustomEmojiID)
		if customEmojiID == "" || !validCustomEmojiID(customEmojiID) {
			return ""
		}
		accessible, found := checked[customEmojiID]
		if !found {
			var err error
			if _, accessible, err = loadNotionCustomEmojiMention(app, principalID, workspaceID, customEmojiID); err != nil {
				return ""
			}
			checked[customEmojiID] = accessible
		}
		if accessible {
			return ""
		}
		return notionCustomEmojiMissing(customEmojiID)
	}
}

// notionBlockCustomEmojiMissing is Notion's refusal of a callout's custom
// emoji its workspace hasn't (notionCustomEmojiMissing): a new block's said
// as a fault in the content it would make, a changed one's alone.
func notionBlockCustomEmojiMissing(app core.App, blockID, customEmojiID string) error {
	message := notionCustomEmojiMissing(customEmojiID)
	if _, exists, err := loadStoredNotionBlock(app, blockID); err != nil {
		return err
	} else if !exists {
		message = notionContentCreationFailed(message)
	}
	return graphInputError(errorCodeGraphReferenceInvalid, message)
}

func projectNotionBlockCustomEmojiIcon(
	app core.App,
	issuer *notionBlockFileCapabilityIssuer,
	workspaceID string,
	content map[string]any,
) error {
	customEmojiID, _ := content["customEmojiId"].(string)
	customEmojiID = strings.TrimSpace(customEmojiID)
	if customEmojiID == "" {
		return nil
	}
	delete(content, "icon")
	if issuer == nil || !validCustomEmojiID(customEmojiID) {
		return nil
	}
	emoji, accessible, err := loadNotionCustomEmojiMention(
		app, issuer.principalID, workspaceID, customEmojiID,
	)
	if err != nil {
		return err
	}
	if !accessible {
		return nil
	}
	payload, err := issuer.customEmojiPayload(app, workspaceID, emoji)
	if err != nil {
		return err
	}
	content["icon"] = map[string]any{
		"type": "custom_emoji", "custom_emoji": payload,
	}
	return nil
}
