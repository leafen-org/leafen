package app

import (
	"crypto/sha256"
	"errors"
	"strings"
)

const notionGroupIdentitySchemeVersion = 1

type notionInternalGroupID string
type notionPublicGroupID string

var errInvalidNotionGroupIdentity = errors.New("invalid Notion-compatible group identity")

func notionCompatibleGroupID(internalID string) (string, error) {
	encoded, err := encodeNotionCompatibleGroupID(notionInternalGroupID(internalID))
	return string(encoded), err
}

func encodeNotionCompatibleGroupID(internalID notionInternalGroupID) (notionPublicGroupID, error) {
	if len(internalID) != notionUserIdentityLength {
		return "", errInvalidNotionGroupIdentity
	}
	payload := make([]byte, 0, 122)
	payload = appendIdentityBits(payload, notionGroupIdentitySchemeVersion, 4)
	for _, character := range []byte(internalID) {
		index := strings.IndexByte(notionUserIdentityAlphabet, character)
		if index < 0 {
			return "", errInvalidNotionGroupIdentity
		}
		payload = appendIdentityBits(payload, index, 6)
	}
	checksum := notionGroupIdentityChecksum(string(internalID))
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
	return notionPublicGroupID(formatNotionUserUUID(encoded)), nil
}

func resolveNotionCompatibleGroupID(publicID string) (string, error) {
	encoded, err := parseNotionUserUUID(notionPublicUserID(publicID))
	if err != nil || encoded[6]>>4 != 8 || encoded[8]>>6 != 2 {
		return "", errInvalidNotionGroupIdentity
	}
	payload := make([]byte, 0, 122)
	for bit := 0; bit < 128; bit++ {
		if notionUserIdentityFixedBit(bit) {
			continue
		}
		payload = append(payload, (encoded[bit/8]>>uint(7-bit%8))&1)
	}
	if identityBitsValue(payload[:4]) != notionGroupIdentitySchemeVersion {
		return "", errInvalidNotionGroupIdentity
	}
	internal := make([]byte, notionUserIdentityLength)
	offset := 4
	for index := range internal {
		value := identityBitsValue(payload[offset : offset+6])
		internal[index] = notionUserIdentityAlphabet[value]
		offset += 6
	}
	checksum := notionGroupIdentityChecksum(string(internal))
	for bit := 0; bit < 28; bit++ {
		if payload[offset+bit] != (checksum[bit/8]>>uint(7-bit%8))&1 {
			return "", errInvalidNotionGroupIdentity
		}
	}
	canonical, err := notionCompatibleGroupID(string(internal))
	if err != nil || canonical != publicID {
		return "", errInvalidNotionGroupIdentity
	}
	return string(internal), nil
}

func notionGroupIdentityChecksum(internalID string) [sha256.Size]byte {
	return sha256.Sum256([]byte("leafen:notion-group-identity:v1\x00" + internalID))
}
