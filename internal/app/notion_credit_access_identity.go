package app

import (
	"crypto/sha256"
	"errors"
	"strings"
)

// A credit access policy and a credit access assignment show as UUIDs, as
// Notion's Admin API shows them: the row's own 15-character ID in the
// version-8 UUID's free bits with a checksum, one domain for each kind so
// that neither kind's ID names the other's.

const (
	notionCreditAccessIdentitySchemeVersion = 1
	notionCreditAccessPolicyIdentity        = "leafen:notion-credit-access-policy-identity:v1"
	notionCreditAccessAssignmentIdentity    = "leafen:notion-credit-access-assignment-identity:v1"
)

var errInvalidNotionCreditAccessIdentity = errors.New("invalid Notion-compatible credit access identity")

func notionCreditAccessPublicID(domain, internalID string) (string, error) {
	if len(internalID) != notionUserIdentityLength {
		return "", errInvalidNotionCreditAccessIdentity
	}
	payload := make([]byte, 0, 122)
	payload = appendIdentityBits(payload, notionCreditAccessIdentitySchemeVersion, 4)
	for _, character := range []byte(internalID) {
		index := strings.IndexByte(notionUserIdentityAlphabet, character)
		if index < 0 {
			return "", errInvalidNotionCreditAccessIdentity
		}
		payload = appendIdentityBits(payload, index, 6)
	}
	checksum := notionCreditAccessIdentityChecksum(domain, internalID)
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
	return formatNotionUserUUID(encoded), nil
}

func resolveNotionCreditAccessID(domain, publicID string) (string, error) {
	encoded, err := parseNotionUserUUID(notionPublicUserID(publicID))
	if err != nil || encoded[6]>>4 != 8 || encoded[8]>>6 != 2 {
		return "", errInvalidNotionCreditAccessIdentity
	}
	payload := make([]byte, 0, 122)
	for bit := 0; bit < 128; bit++ {
		if notionUserIdentityFixedBit(bit) {
			continue
		}
		payload = append(payload, (encoded[bit/8]>>uint(7-bit%8))&1)
	}
	if identityBitsValue(payload[:4]) != notionCreditAccessIdentitySchemeVersion {
		return "", errInvalidNotionCreditAccessIdentity
	}
	internal := make([]byte, notionUserIdentityLength)
	offset := 4
	for index := range internal {
		internal[index] = notionUserIdentityAlphabet[identityBitsValue(payload[offset:offset+6])]
		offset += 6
	}
	canonical, err := notionCreditAccessPublicID(domain, string(internal))
	if err != nil || canonical != publicID {
		return "", refusedUnlessStorage(errInvalidNotionCreditAccessIdentity, err)
	}
	return string(internal), nil
}

func notionCreditAccessIdentityChecksum(domain, internalID string) [sha256.Size]byte {
	return sha256.Sum256([]byte(domain + "\x00" + internalID))
}
