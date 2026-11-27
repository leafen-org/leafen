package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

const (
	notionUserIdentitySchemeVersion = 1
	notionUserIdentityAlphabet      = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ_abcdefghijklmnopqrstuvwxyz-"
	notionUserIdentityLength        = 15
)

type notionInternalUserID string
type notionPublicUserID string

var errInvalidNotionUserIdentity = errors.New("invalid Notion-compatible user identity")

// notionSystemUserID is who Notion's API names as making and editing what its
// automations do (seen 2026-11-05); Leafen's own such writes read the same.
const notionSystemUserID = "00000000-0000-0000-0000-000000000003"

func notionCompatibleUserID(internalID string) (string, error) {
	encoded, err := encodeNotionCompatibleUserID(notionInternalUserID(internalID))
	return string(encoded), err
}

func projectNotionUserIdentity(internalID string) (string, error) {
	return notionCompatibleUserID(internalID)
}

func encodeNotionCompatibleUserID(internalID notionInternalUserID) (notionPublicUserID, error) {
	if internalID == leafenSystemAuthorID || internalID == leafenAnonymousAuthorID {
		return notionSystemUserID, nil
	}
	if len(internalID) != notionUserIdentityLength {
		return "", errInvalidNotionUserIdentity
	}
	payload := make([]byte, 0, 122)
	payload = appendIdentityBits(payload, notionUserIdentitySchemeVersion, 4)
	for _, character := range []byte(internalID) {
		index := strings.IndexByte(notionUserIdentityAlphabet, character)
		if index < 0 {
			return "", errInvalidNotionUserIdentity
		}
		payload = appendIdentityBits(payload, index, 6)
	}
	checksum := notionUserIdentityChecksum(string(internalID))
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
	return notionPublicUserID(formatNotionUserUUID(encoded)), nil
}

func resolveNotionCompatibleUserID(publicID string) (string, error) {
	encoded, err := parseNotionUserUUID(notionPublicUserID(publicID))
	if err != nil || encoded[6]>>4 != 8 || encoded[8]>>6 != 2 {
		return "", errInvalidNotionUserIdentity
	}
	payload := make([]byte, 0, 122)
	for bit := 0; bit < 128; bit++ {
		if notionUserIdentityFixedBit(bit) {
			continue
		}
		payload = append(payload, (encoded[bit/8]>>uint(7-bit%8))&1)
	}
	if identityBitsValue(payload[:4]) != notionUserIdentitySchemeVersion {
		return "", errInvalidNotionUserIdentity
	}
	internal := make([]byte, notionUserIdentityLength)
	offset := 4
	for index := range internal {
		value := identityBitsValue(payload[offset : offset+6])
		internal[index] = notionUserIdentityAlphabet[value]
		offset += 6
	}
	checksum := notionUserIdentityChecksum(string(internal))
	for bit := 0; bit < 28; bit++ {
		if payload[offset+bit] != (checksum[bit/8]>>uint(7-bit%8))&1 {
			return "", errInvalidNotionUserIdentity
		}
	}
	canonical, err := notionCompatibleUserID(string(internal))
	if err != nil || canonical != publicID {
		return "", errInvalidNotionUserIdentity
	}
	return string(internal), nil
}

// notionPersonAddress is a person as the MCP tools name them — a value,
// a view's filter — by the ID notion-get-users gives: a person's
// Notion-compatible ID (Leafen's own writes Notion's system user's), and a
// bot's or an invitee's own (notion_people_value_bots.go,
// people_value_invitees.go), which already read so; one kept by an ID no one
// here has, as kept. A value read is written back as the same person
// (notionPersonReferenceID).
func notionPersonAddress(id string) string {
	return "user://" + notionPersonID(id)
}

// notionPersonID is notionPersonAddress's ID alone.
func notionPersonID(id string) string {
	if public, err := notionCompatibleUserID(id); err == nil {
		return public
	}
	return id
}

// notionPersonSQLPrefix starts a page's creator and last editor in an SQL
// query's rows, as Notion's do (seen 2026-11-22, worker w2369, twice:
// "notion_user-<UUID>" where fetch, view rows and the version tools give
// user://<UUID>); Notion takes it back as the same person in a people value.
const notionPersonSQLPrefix = "notion_user-"

// notionPersonReferenceID is the ID a person reference names: one written as
// a user:// link or as an SQL query's notion_user- value, or bare.
func notionPersonReferenceID(reference string) string {
	reference = strings.TrimSpace(reference)
	if rest, cut := strings.CutPrefix(reference, "user://"); cut {
		return strings.TrimSpace(rest)
	}
	if rest, cut := strings.CutPrefix(reference, notionPersonSQLPrefix); cut {
		return strings.TrimSpace(rest)
	}
	return reference
}

// leafenPersonReference is the Leafen ID of a person an agent names as the
// Notion-compatible tools and API give them (notion-get-users, /v1/users),
// by a user:// link, or by their Leafen ID, which stays as it is; anything
// else stays as given for the caller to refuse.
func leafenPersonReference(reference string) string {
	reference = notionPersonReferenceID(reference)
	if internal, err := resolveNotionCompatibleUserID(strings.ToLower(reference)); err == nil {
		return internal
	}
	return reference
}

func resolveNotionUserIdentityFilter(publicID string) (string, error) {
	if publicID == notionSystemUserID {
		return leafenSystemAuthorID, nil
	}
	return resolveNotionCompatibleUserID(publicID)
}

// notionUserObject is a person as the API shows them; origin is the server's
// public address, which a line icon's avatar_url starts with.
func notionUserObject(user *core.Record, origin string) (map[string]any, error) {
	return notionUserObjectFromFields(
		user.Id,
		user.GetString("name"),
		user.GetString("profile_avatar"),
		user.Email(),
		user.Verified(),
		origin,
	)
}

func notionUserObjectFromFields(id, name, avatar, email string, emailVerified bool, origin string) (map[string]any, error) {
	publicID, err := notionCompatibleUserID(id)
	if err != nil {
		return nil, err
	}
	avatarURL := notionProfileAvatarURL(avatar, origin)
	return map[string]any{
		"object": "user", "id": publicID, "type": "person",
		"name": name, "avatar_url": avatarURL,
		"person": notionPersonFields(email, emailVerified),
	}, nil
}

func notionProfileAvatarURL(raw, origin string) any {
	raw = strings.TrimSpace(raw)
	if icon, ok := accountAvatarIconName(raw); ok {
		if origin == "" {
			return nil
		}
		return origin + accountAvatarIconPath(icon)
	}
	if accountPhotoID(raw) != "" {
		if origin == "" {
			return nil
		}
		return origin + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil
	}
	return raw
}

func notionUserIdentityChecksum(internalID string) [sha256.Size]byte {
	return sha256.Sum256([]byte("leafen:notion-user-identity:v1\x00" + internalID))
}

func validNotionCompatibleInternalID(value string) bool {
	if len(value) != notionUserIdentityLength {
		return false
	}
	for _, character := range []byte(value) {
		if strings.IndexByte(notionUserIdentityAlphabet, character) < 0 {
			return false
		}
	}
	return true
}

func appendIdentityBits(bits []byte, value, width int) []byte {
	for shift := width - 1; shift >= 0; shift-- {
		bits = append(bits, byte(value>>uint(shift))&1)
	}
	return bits
}

func identityBitsValue(bits []byte) int {
	value := 0
	for _, bit := range bits {
		value = value<<1 | int(bit)
	}
	return value
}

func notionUserIdentityFixedBit(bit int) bool {
	return bit >= 48 && bit < 52 || bit >= 64 && bit < 66
}

func formatNotionUserUUID(value [16]byte) string {
	encoded := hex.EncodeToString(value[:])
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:]
}

func parseNotionUserUUID(value notionPublicUserID) ([16]byte, error) {
	var decoded [16]byte
	text := string(value)
	if len(text) != 36 || text[8] != '-' || text[13] != '-' || text[18] != '-' || text[23] != '-' || text != strings.ToLower(text) {
		return decoded, errInvalidNotionUserIdentity
	}
	raw, err := hex.DecodeString(strings.ReplaceAll(text, "-", ""))
	if err != nil || len(raw) != len(decoded) {
		return decoded, errInvalidNotionUserIdentity
	}
	copy(decoded[:], raw)
	return decoded, nil
}

// notionPersonFields is a person's email as Notion's API shows it; an
// account with none shows an empty person, as Notion's does for a person
// whose email the integration can't read.
func notionPersonFields(email string, verified bool) map[string]any {
	if strings.TrimSpace(email) == "" {
		return map[string]any{}
	}
	return map[string]any{"email": email, "email_verified": verified}
}
