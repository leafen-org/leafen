package app

import (
	"crypto/sha256"
	"errors"
	"strings"
)

const notionWorkspaceIdentitySchemeVersion = 1

type notionInternalWorkspaceID string
type notionPublicWorkspaceID string

var errInvalidNotionWorkspaceIdentity = errors.New("invalid Notion-compatible workspace identity")

func notionCompatibleWorkspaceID(internalID string) (string, error) {
	encoded, err := encodeNotionCompatibleWorkspaceID(notionInternalWorkspaceID(internalID))
	return string(encoded), err
}

func encodeNotionCompatibleWorkspaceID(
	internalID notionInternalWorkspaceID,
) (notionPublicWorkspaceID, error) {
	if len(internalID) != notionUserIdentityLength {
		return "", errInvalidNotionWorkspaceIdentity
	}
	payload := make([]byte, 0, 122)
	payload = appendIdentityBits(payload, notionWorkspaceIdentitySchemeVersion, 4)
	for _, character := range []byte(internalID) {
		index := strings.IndexByte(notionUserIdentityAlphabet, character)
		if index < 0 {
			return "", errInvalidNotionWorkspaceIdentity
		}
		payload = appendIdentityBits(payload, index, 6)
	}
	checksum := notionWorkspaceIdentityChecksum(string(internalID))
	for bit := 0; bit < 28; bit++ {
		payload = append(payload, (checksum[bit/8]>>uint(7-bit%8))&1)
	}

	var encoded [16]byte
	payloadIndex := 0
	for bit := 0; bit < 128; bit++ {
		if notionUserIdentityFixedBit(bit) {
			continue
		}
		encoded[bit/8] |= payload[payloadIndex] << uint(7-bit%8)
		payloadIndex++
	}
	encoded[6] = (encoded[6] & 0x0f) | 0x80
	encoded[8] = (encoded[8] & 0x3f) | 0x80
	return notionPublicWorkspaceID(formatNotionUserUUID(encoded)), nil
}

func resolveNotionCompatibleWorkspaceID(publicID string) (string, error) {
	// The settings show a workspace's own ID, and the Admin API takes it as
	// Notion's takes the ID its settings show (2026-11-07: the ID Leafen's
	// settings › 일반 shows was refused as not a UUID).
	if _, err := notionCompatibleWorkspaceID(publicID); err == nil {
		return publicID, nil
	}
	encoded, err := parseNotionUserUUID(notionPublicUserID(publicID))
	if err != nil || encoded[6]>>4 != 8 || encoded[8]>>6 != 2 {
		return "", errInvalidNotionWorkspaceIdentity
	}
	payload := make([]byte, 0, 122)
	for bit := 0; bit < 128; bit++ {
		if notionUserIdentityFixedBit(bit) {
			continue
		}
		payload = append(payload, (encoded[bit/8]>>uint(7-bit%8))&1)
	}
	if identityBitsValue(payload[:4]) != notionWorkspaceIdentitySchemeVersion {
		return "", errInvalidNotionWorkspaceIdentity
	}
	internal := make([]byte, notionUserIdentityLength)
	offset := 4
	for index := range internal {
		value := identityBitsValue(payload[offset : offset+6])
		internal[index] = notionUserIdentityAlphabet[value]
		offset += 6
	}
	checksum := notionWorkspaceIdentityChecksum(string(internal))
	for bit := 0; bit < 28; bit++ {
		if payload[offset+bit] != (checksum[bit/8]>>uint(7-bit%8))&1 {
			return "", errInvalidNotionWorkspaceIdentity
		}
	}
	canonical, err := notionCompatibleWorkspaceID(string(internal))
	if err != nil || canonical != publicID {
		return "", errInvalidNotionWorkspaceIdentity
	}
	return string(internal), nil
}

func notionWorkspaceIdentityChecksum(internalID string) [sha256.Size]byte {
	return sha256.Sum256([]byte("leafen:notion-workspace-identity:v1\x00" + internalID))
}
