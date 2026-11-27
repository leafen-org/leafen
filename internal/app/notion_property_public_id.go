package app

import (
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A data source's properties go out of the Public API, its webhooks and the
// MCP under IDs in Notion's form, never under Leafen's own: "title" for the
// title, and for any other four characters from ":" to "~" (seen 2026-11-09:
// ";y~T", "N:LB", "y|;\", "qNQ`", no digits). Notion's API writes them
// URL-encoded where it names a property itself — the schema, a page's
// properties, a property item, a dual relation's synced_property_id, a
// formula's block_property — and as they are where a rollup's settings or a
// view names one ("y%7C%3B%5C" and "y|;\" are one property); its MCP names
// one by the unpadded URL base64 of the characters ("O3l-VA" for ";y~T").
//
// A property's public ID follows from Leafen's, so nothing stores it. Two of
// a data source's properties whose IDs would meet are told apart by when
// they were made: the later one takes the next ID, so a property keeps its ID
// as others come. A request may name a property by its public ID, encoded or
// not, or by Leafen's own.

const (
	notionPropertyIDFirst  = ':'
	notionPropertyIDLast   = '~'
	notionPropertyIDLength = 4
)

// notionPropertyIDs are a data source's properties' public IDs.
type notionPropertyIDs struct {
	public   map[string]string // Leafen's ID → public ID, encoded
	internal map[string]string // notionPropertyIDKey(public ID) → Leafen's ID
}

type notionPropertyIDSource struct {
	ID      string `db:"id"`
	Type    string `db:"type"`
	Created int64  `db:"created"`
}

// loadNotionPropertyIDs reads a data source's properties' public IDs, its
// trashed ones too, so a property put back finds its ID as it was.
func loadNotionPropertyIDs(app core.App, dataSourceID string) (notionPropertyIDs, error) {
	sources := []notionPropertyIDSource{}
	if dataSourceID != "" {
		if err := app.DB().NewQuery(`
			SELECT [[id]], [[type]], [[created]] FROM {{leafen_database_properties}}
			WHERE [[data_source_id]] = {:dataSourceId}
			ORDER BY [[created]], [[id]]
		`).Bind(dbx.Params{"dataSourceId": dataSourceID}).All(&sources); err != nil {
			return notionPropertyIDs{}, err
		}
	}
	return newNotionPropertyIDs(sources), nil
}

// newNotionPropertyIDs gives each property its public ID, in the order they
// were made.
func newNotionPropertyIDs(sources []notionPropertyIDSource) notionPropertyIDs {
	ids := notionPropertyIDs{
		public: make(map[string]string, len(sources)), internal: make(map[string]string, len(sources)),
	}
	for _, source := range sources {
		if _, done := ids.public[source.ID]; done || source.ID == "" {
			continue
		}
		publicID := ""
		if normalizeNotionDatabaseRowPagePropertyType(source.Type) == graphValueTypeTitle {
			if _, taken := ids.internal[notionTitlePropertyID]; !taken {
				publicID = notionTitlePropertyID
			}
		}
		for attempt := 0; publicID == ""; attempt++ {
			candidate := notionPropertyIDKey(notionDerivedPropertyID(source.ID, attempt))
			if _, taken := ids.internal[candidate]; !taken {
				publicID = candidate
			}
		}
		ids.public[source.ID] = publicID
		ids.internal[publicID] = source.ID
	}
	return ids
}

// publicID is a property's public ID, URL-encoded as Notion's schema gives
// it. One the data source doesn't hold any more — a webhook's deleted
// property — takes the ID it would have had first.
func (ids notionPropertyIDs) publicID(internalID string) string {
	if publicID, found := ids.public[internalID]; found || internalID == "" {
		return publicID
	}
	return notionPropertyIDKey(notionDerivedPropertyID(internalID, 0))
}

// internalID is the property a request names by ID: its public one, encoded
// or not, or Leafen's own.
func (ids notionPropertyIDs) internalID(given string) (string, bool) {
	given = strings.TrimSpace(given)
	if given == "" {
		return "", false
	}
	if internalID, found := ids.internal[notionPropertyIDKey(given)]; found {
		return internalID, true
	}
	if _, found := ids.public[given]; found {
		return given, true
	}
	return "", false
}

// notionDerivedPropertyID is the attempt-th public ID Leafen's ID can take,
// before encoding: four characters from ":" to "~".
func notionDerivedPropertyID(internalID string, attempt int) string {
	digest := sha256.Sum256([]byte("leafen:notion-property-id\x00" + internalID + "\x00" + strconv.Itoa(attempt)))
	const span = notionPropertyIDLast - notionPropertyIDFirst + 1
	characters := make([]byte, notionPropertyIDLength)
	for index := range characters {
		characters[index] = byte(notionPropertyIDFirst + int(digest[index])%span)
	}
	return string(characters)
}

// notionPropertyIDKey is a property ID as Notion's schema writes it, from
// either form: decoded, then encoded as encodeURIComponent encodes.
func notionPropertyIDKey(id string) string {
	id = strings.TrimSpace(id)
	if decoded, err := url.PathUnescape(id); err == nil {
		id = decoded
	}
	const hex = "0123456789ABCDEF"
	key := make([]byte, 0, len(id))
	for index := 0; index < len(id); index++ {
		character := id[index]
		if 'a' <= character && character <= 'z' || 'A' <= character && character <= 'Z' ||
			'0' <= character && character <= '9' || strings.IndexByte("-_.!~*'()", character) >= 0 {
			key = append(key, character)
			continue
		}
		key = append(key, '%', hex[character>>4], hex[character&15])
	}
	return string(key)
}

// notionPropertyRawID is a public ID as a rollup's settings and a view give
// it: not encoded.
func notionPropertyRawID(publicID string) string {
	if decoded, err := url.PathUnescape(publicID); err == nil {
		return decoded
	}
	return publicID
}

// notionMCPPropertyID is a public ID as Notion's MCP writes it in its
// collectionProperty:// and formulaCode:// addresses.
func notionMCPPropertyID(publicID string) string {
	if publicID == "" {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(notionPropertyRawID(publicID)))
}

// notionPublicPropertyIDsOf gives each of the properties named by Leafen's
// IDs its public ID, from the data sources that hold them; for an event
// recorded before the delivery that tells it.
func notionPublicPropertyIDsOf(app core.App, internalIDs []string) (map[string]string, error) {
	result := make(map[string]string, len(internalIDs))
	if len(internalIDs) == 0 {
		return result, nil
	}
	wanted := make([]any, 0, len(internalIDs))
	for _, id := range internalIDs {
		wanted = append(wanted, id)
	}
	sources := []string{}
	if err := app.DB().Select("data_source_id").Distinct(true).From("leafen_database_properties").
		Where(dbx.In("id", wanted...)).Column(&sources); err != nil {
		return nil, err
	}
	sets := make([]notionPropertyIDs, 0, len(sources))
	for _, source := range sources {
		ids, err := loadNotionPropertyIDs(app, source)
		if err != nil {
			return nil, err
		}
		sets = append(sets, ids)
	}
	for _, id := range internalIDs {
		result[id] = notionPropertyIDs{}.publicID(id)
		for _, ids := range sets {
			if publicID, found := ids.public[id]; found {
				result[id] = publicID
				break
			}
		}
	}
	return result, nil
}

// viewID is how the views API names a property: by its public ID as it is,
// not encoded (seen 2026-11-09: a view's "y|;\", "qNQ`" and "title").
func (scope notionViewFilterScope) viewID(internalID string) string {
	return notionPropertyRawID(scope.ids.publicID(internalID))
}

// named is the property a request names: by Leafen's ID, its public ID,
// encoded or not, or its name.
func (scope notionViewFilterScope) named(reference string) (notionViewProperty, bool) {
	if property, found := scope.byViewID(reference); found {
		return property, true
	}
	property, found := scope.byName[reference]
	return property, found
}
