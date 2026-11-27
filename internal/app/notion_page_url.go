package app

import (
	"regexp"
	"strings"
)

// A page's url in the API names it as Notion's does (seen 2026-11-16, API
// 2025-09-03, twenty-one titles): /p/, its title's slug, a dash and its ID's
// 32 hexadecimal digits. The slug is the plain title with every run of
// anything but ASCII letters, digits and "_" made one "-", the dashes at
// either end left out, then cut at 100 characters ("Café Ünï" → "Caf-n",
// "w1365 한글 제목" → "w1365", "a/b c.d" → "a-b-c-d"). A title with nothing
// left — untitled, Korean alone, "---" — gives the ID alone, as a database's
// url does whatever its name. An ID Notion's could not be (one given through
// the API, such as one-database-2) keeps its own address, which no reader
// could part from a slug. The app opens either (readPageLink).

var notionPageSlugBreak = regexp.MustCompile(`[^A-Za-z0-9_]+`)

const notionPageSlugMaxLength = 100

// notionPageObjectURL is the url of a page object, its title as the API
// gives it (rich text).
func notionPageObjectURL(origin, publicID string, title []map[string]any) string {
	hexID := notionMentionID(publicID)
	if _, uuid := normalizeNotionUUIDRequest(hexID); !uuid || len(hexID) != 32 {
		return notionPageURL(origin, publicID)
	}
	if slug := notionPageTitleSlug(notionRichTextPlain(title)); slug != "" {
		return origin + "/p/" + slug + "-" + hexID
	}
	return origin + "/p/" + hexID
}

// notionRowPageObjectURL is the url of a database row's page, titled by its
// title property among the API's properties.
func notionRowPageObjectURL(origin, publicID string, properties map[string]any) string {
	for _, raw := range properties {
		property, ok := raw.(map[string]any)
		if !ok || property["type"] != "title" {
			continue
		}
		title, _ := property["title"].([]map[string]any)
		return notionPageObjectURL(origin, publicID, title)
	}
	return notionPageObjectURL(origin, publicID, nil)
}

func notionPageTitleSlug(plain string) string {
	slug := strings.Trim(notionPageSlugBreak.ReplaceAllString(plain, "-"), "-")
	if len(slug) > notionPageSlugMaxLength {
		slug = slug[:notionPageSlugMaxLength]
	}
	return slug
}

func notionRichTextPlain(items []map[string]any) string {
	var plain strings.Builder
	for _, item := range items {
		text, _ := item["plain_text"].(string)
		plain.WriteString(text)
	}
	return plain.String()
}

// notionSlugAddressID is the page ID a "<slug>-<32 hex>" address names, in
// its dashed form, or "" for an address that names none that way.
func notionSlugAddressID(id string) string {
	linked := notionLinkedID(id)
	if linked == "" || linked == strings.ToLower(id) || !strings.Contains(id, "-") {
		return ""
	}
	return linked
}
