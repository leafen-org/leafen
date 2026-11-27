package app

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// A page block's Public API ID is its own ID when that is a UUID. Blocks
// made before block IDs became UUIDs keep their stored ID and answer to a
// stored UUID alias instead; every request resolves the alias back. A
// database page body block keeps the ID the API gave it before database page
// bodies became page bodies.

const notionObjectIdentityBlock = "block"

// notionBlockPublicID is the ID the Public API shows for a page block.
func notionBlockPublicID(app core.App, workspaceID, blockID string) (string, error) {
	if publicID, err := notionObjectIdentityPublicID(app, notionObjectIdentityRowBlock, workspaceID, blockID); err == nil {
		return publicID, nil
	} else if !errors.Is(err, errInvalidNotionObjectIdentity) {
		return "", err
	}
	if normalized, ok := normalizeNotionUUIDRequest(blockID); ok && normalized == blockID {
		return blockID, nil
	}
	return ensureNotionObjectIdentity(app, notionObjectIdentityBlock, workspaceID, blockID)
}

// resolveNotionBlockID turns a requested page block ID, a block's own UUID
// or an alias, into the stored block ID.
func resolveNotionBlockID(app core.App, requested string) (string, bool) {
	normalized, ok := normalizeNotionUUIDRequest(requested)
	if !ok {
		return "", false
	}
	for _, kind := range []string{notionObjectIdentityBlock, notionObjectIdentityRowBlock} {
		if identity, err := resolveNotionObjectIdentity(app, kind, normalized); err == nil {
			return identity.InternalID, true
		}
	}
	return normalized, true
}
