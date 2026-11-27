package organization

import (
	"crypto/sha256"
	"errors"
	"strings"

	leafen "leafen.org/leafen/internal/app"
)

const notionAgentIdentitySchemeVersion = 1

type notionInternalAgentID string
type notionPublicAgentID string

var errInvalidNotionAgentIdentity = errors.New("invalid Notion-compatible agent identity")

func notionCompatibleAgentID(internalID string) (string, error) {
	encoded, err := encodeNotionCompatibleAgentID(notionInternalAgentID(internalID))
	return string(encoded), err
}

func encodeNotionCompatibleAgentID(internalID notionInternalAgentID) (notionPublicAgentID, error) {
	if len(internalID) != leafen.NotionUserIdentityLength {
		return "", errInvalidNotionAgentIdentity
	}
	payload := make([]byte, 0, 122)
	payload = leafen.AppendIdentityBits(payload, notionAgentIdentitySchemeVersion, 4)
	for _, character := range []byte(internalID) {
		index := strings.IndexByte(leafen.NotionUserIdentityAlphabet, character)
		if index < 0 {
			return "", errInvalidNotionAgentIdentity
		}
		payload = leafen.AppendIdentityBits(payload, index, 6)
	}
	checksum := notionAgentIdentityChecksum(string(internalID))
	for bit := 0; bit < 28; bit++ {
		payload = append(payload, (checksum[bit/8]>>uint(7-bit%8))&1)
	}
	var encoded [16]byte
	payloadIndex := 0
	for bit := 0; bit < 128; bit++ {
		if leafen.NotionUserIdentityFixedBit(bit) {
			continue
		}
		encoded[bit/8] |= payload[payloadIndex] << uint(7-bit%8)
		payloadIndex++
	}
	encoded[6] = (encoded[6] & 0x0f) | 0x80
	encoded[8] = (encoded[8] & 0x3f) | 0x80
	return notionPublicAgentID(leafen.FormatNotionUserUUID(encoded)), nil
}

func resolveNotionCompatibleAgentID(publicID string) (string, error) {
	encoded, err := leafen.ParseNotionUserUUID(leafen.NotionPublicUserID(publicID))
	if err != nil || encoded[6]>>4 != 8 || encoded[8]>>6 != 2 {
		return "", errInvalidNotionAgentIdentity
	}
	payload := make([]byte, 0, 122)
	for bit := 0; bit < 128; bit++ {
		if !leafen.NotionUserIdentityFixedBit(bit) {
			payload = append(payload, (encoded[bit/8]>>uint(7-bit%8))&1)
		}
	}
	if leafen.IdentityBitsValue(payload[:4]) != notionAgentIdentitySchemeVersion {
		return "", errInvalidNotionAgentIdentity
	}
	internal := make([]byte, leafen.NotionUserIdentityLength)
	offset := 4
	for index := range internal {
		value := leafen.IdentityBitsValue(payload[offset : offset+6])
		internal[index] = leafen.NotionUserIdentityAlphabet[value]
		offset += 6
	}
	checksum := notionAgentIdentityChecksum(string(internal))
	for bit := 0; bit < 28; bit++ {
		if payload[offset+bit] != (checksum[bit/8]>>uint(7-bit%8))&1 {
			return "", errInvalidNotionAgentIdentity
		}
	}
	canonical, err := notionCompatibleAgentID(string(internal))
	if err != nil || canonical != publicID {
		return "", errInvalidNotionAgentIdentity
	}
	return string(internal), nil
}

func notionAgentIdentityChecksum(internalID string) [sha256.Size]byte {
	return sha256.Sum256([]byte("leafen:notion-agent-identity:v1\x00" + internalID))
}
