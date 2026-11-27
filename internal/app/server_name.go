package app

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"
)

// A server's name, which tells servers apart wherever a person or an agent
// meets more than one: every server gets a friendly alias when it is made
// (a plant, shown in the person's language and in English), and its
// administrators may name it anything later. Names already made from it —
// an agent's connection name, the Tailscale device's — are kept when it is
// renamed; only what the screens show changes.

// serverAliases are the default names, as their English words; the web
// shows each in the person's language (common:serverAliases).
var serverAliases = []string{
	"maple", "clover", "acorn", "sprout", "fern", "moss", "willow", "birch",
	"pinecone", "ginkgo", "bamboo", "petal", "dewdrop", "mint", "basil", "olive",
	"tulip", "dandelion", "sunflower", "camellia", "lavender", "daisy", "blossom", "hazel",
}

// serverAliasesKorean are the aliases in Korean, as the web says them
// (common:serverAliases), for what the server itself says in Korean: its
// link preview's title (link_preview_card.go).
var serverAliasesKorean = map[string]string{
	"maple": "단풍", "clover": "클로버", "acorn": "도토리", "sprout": "새싹", "fern": "고사리", "moss": "이끼",
	"willow": "버들", "birch": "자작", "pinecone": "솔방울", "ginkgo": "은행잎", "bamboo": "대나무", "petal": "꽃잎",
	"dewdrop": "이슬", "mint": "박하", "basil": "바질", "olive": "올리브", "tulip": "튤립", "dandelion": "민들레",
	"sunflower": "해바라기", "camellia": "동백", "lavender": "라벤더", "daisy": "데이지", "blossom": "벚꽃", "hazel": "개암",
}

// serverNameRunes is how long an administrator's name for the server may be.
const serverNameRunes = 40

var errServerNameInvalid = errors.New(errorCodeAdministratorSettingsInvalid)

// renewServerAlias picks the server's alias from its own number: when the
// migration runs, and for a server made from the fresh schema snapshot.
func renewServerAlias(app core.App) error {
	serverID, err := loadServerID(app)
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_server_identity}} SET [[alias]] = {:alias} WHERE [[id]] = 'current'
	`).Bind(map[string]any{"alias": serverAliasFor(serverID)}).Execute()
	return err
}

// serverAliasFor picks the alias a server keeps, from its own number.
func serverAliasFor(serverID string) string {
	digest := sha256.Sum256([]byte(serverID))
	return serverAliases[binary.BigEndian.Uint32(digest[:4])%uint32(len(serverAliases))]
}

// normalizeServerName keeps a name to one line of at most serverNameRunes.
func normalizeServerName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if utf8.RuneCountInString(name) > serverNameRunes || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", errServerNameInvalid
	}
	return name, nil
}

// serverDisplayName is the server's name as an agent reads it: its
// administrators' name, or its alias in English.
func serverDisplayName(name, alias string) string {
	if name != "" {
		return name
	}
	if alias == "" {
		return "Leafen"
	}
	return strings.ToUpper(alias[:1]) + alias[1:]
}

// serverLatinName is the name in plain letters, for names made from it — a
// device's or a connection's: the administrators' name where it has such
// letters, otherwise the alias.
func serverLatinName(name, alias string) string {
	var built strings.Builder
	dash := false
	for _, letter := range strings.ToLower(name) {
		switch {
		case letter >= 'a' && letter <= 'z', letter >= '0' && letter <= '9':
			built.WriteRune(letter)
			dash = false
		case built.Len() > 0 && !dash:
			built.WriteByte('-')
			dash = true
		}
	}
	slug := strings.Trim(built.String(), "-")
	if len(slug) > 30 {
		slug = strings.Trim(slug[:30], "-")
	}
	if slug == "" {
		return alias
	}
	return slug
}
