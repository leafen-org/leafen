package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

const notionPageIdentitySchemeVersion = 1

type notionInternalPageID string
type notionPublicPageID string

var errInvalidNotionPageIdentity = errors.New("invalid Notion-compatible page identity")

func notionCompatiblePageID(internalID string) (string, error) {
	encoded, err := encodeNotionCompatiblePageID(notionInternalPageID(internalID))
	return string(encoded), err
}

func encodeNotionCompatiblePageID(internalID notionInternalPageID) (notionPublicPageID, error) {
	if !validNotionCompatibleInternalID(string(internalID)) {
		return "", errInvalidNotionPageIdentity
	}
	payload := make([]byte, 0, 122)
	payload = appendIdentityBits(payload, notionPageIdentitySchemeVersion, 4)
	for _, character := range []byte(internalID) {
		index := strings.IndexByte(notionUserIdentityAlphabet, character)
		if index < 0 {
			return "", errInvalidNotionPageIdentity
		}
		payload = appendIdentityBits(payload, index, 6)
	}
	checksum := notionPageIdentityChecksum(string(internalID))
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
	return notionPublicPageID(formatNotionUserUUID(encoded)), nil
}

func resolveNotionCompatiblePageID(publicID string) (string, error) {
	publicID, ok := normalizeNotionUUIDRequest(publicID)
	if !ok {
		return "", errInvalidNotionPageIdentity
	}
	encoded, err := parseNotionUserUUID(notionPublicUserID(publicID))
	if err != nil || encoded[6]>>4 != 8 || encoded[8]>>6 != 2 {
		return "", errInvalidNotionPageIdentity
	}
	payload := make([]byte, 0, 122)
	for bit := 0; bit < 128; bit++ {
		if notionUserIdentityFixedBit(bit) {
			continue
		}
		payload = append(payload, (encoded[bit/8]>>uint(7-bit%8))&1)
	}
	if identityBitsValue(payload[:4]) != notionPageIdentitySchemeVersion {
		return "", errInvalidNotionPageIdentity
	}
	internal := make([]byte, notionUserIdentityLength)
	offset := 4
	for index := range internal {
		value := identityBitsValue(payload[offset : offset+6])
		internal[index] = notionUserIdentityAlphabet[value]
		offset += 6
	}
	checksum := notionPageIdentityChecksum(string(internal))
	for bit := 0; bit < 28; bit++ {
		if payload[offset+bit] != (checksum[bit/8]>>uint(7-bit%8))&1 {
			return "", errInvalidNotionPageIdentity
		}
	}
	canonical, err := notionCompatiblePageID(string(internal))
	if err != nil || canonical != publicID {
		return "", errInvalidNotionPageIdentity
	}
	return string(internal), nil
}

// normalizeNotionUUIDRequest reads an ID a request gives as Notion's API
// reads one (seen 2026-11-12): dashed, its letters either case, or 32
// lowercase hexadecimal digits, which it dashes. Its letters stay as
// written, and Notion's IDs are lowercase, so one in capitals names nothing:
// Notion finds no page, data source or relation target by it, and a mention
// by it reads "Untitled". Undashed capitals, stray dashes and spaces around
// it are no UUID.
func normalizeNotionUUIDRequest(value string) (string, bool) {
	if len(value) == 32 {
		if _, err := hex.DecodeString(value); err != nil {
			return "", false
		}
		if strings.ToLower(value) != value {
			return value, false
		}
		value = value[:8] + "-" + value[8:12] + "-" + value[12:16] + "-" + value[16:20] + "-" + value[20:]
	}
	lower := strings.ToLower(value)
	decoded, err := parseNotionUserUUID(notionPublicUserID(lower))
	return value, err == nil && formatNotionUserUUID(decoded) == lower
}

func notionPageIdentityChecksum(internalID string) [sha256.Size]byte {
	return sha256.Sum256([]byte("leafen:notion-page-identity:v1\x00" + internalID))
}
