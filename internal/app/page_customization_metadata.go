package app

import "github.com/pocketbase/pocketbase/core"

func validPageCustomizationDisplay(value string) bool {
	switch value {
	case "default", "expanded", "off":
		return true
	default:
		return false
	}
}

func validPageCustomizationMetadataInput(input pageMetadataInput) bool {
	return (input.BacklinksDisplay == nil || validPageCustomizationDisplay(*input.BacklinksDisplay)) &&
		(input.PageCommentsDisplay == nil || validPageCustomizationDisplay(*input.PageCommentsDisplay)) &&
		(input.TableOfContentsDisplay == nil || validPageTableOfContentsDisplay(*input.TableOfContentsDisplay)) &&
		(input.InlineCommentsDisplay == nil || validPageInlineCommentsDisplay(*input.InlineCommentsDisplay))
}

func applyPageCustomizationMetadata(page *core.Record, input pageMetadataInput) {
	if input.TableOfContentsDisplay != nil {
		page.Set("table_of_contents_display", *input.TableOfContentsDisplay)
	}
	if input.InlineCommentsDisplay != nil {
		page.Set("inline_comments_display", *input.InlineCommentsDisplay)
	}
	if input.BacklinksDisplay != nil {
		page.Set("backlinks_display", *input.BacklinksDisplay)
	}
	if input.PageCommentsDisplay != nil {
		page.Set("page_comments_display", *input.PageCommentsDisplay)
	}
}

func initializePageCustomizationMetadata(page *core.Record) {
	page.Set("table_of_contents_display", pageTableOfContentsDisplayDefault)
	page.Set("inline_comments_display", pageInlineCommentsDisplayDefault)
	page.Set("backlinks_display", pageBacklinksDisplayDefault)
	page.Set("page_comments_display", pageCommentsDisplayDefault)
}

func normalizedPageCustomizationDisplay(value *string) string {
	if value == nil || *value == "" {
		return "default"
	}
	return *value
}

func validPageTableOfContentsDisplay(value string) bool {
	return value == pageTableOfContentsDisplayDefault || value == pageTableOfContentsDisplayOff
}
func validPageInlineCommentsDisplay(value string) bool {
	return value == pageInlineCommentsDisplayDefault || value == pageInlineCommentsDisplayMinimal
}
func normalizedPageTableOfContentsDisplay(value *string) string {
	if value == nil || *value == "" {
		return pageTableOfContentsDisplayDefault
	}
	return *value
}
func normalizedPageInlineCommentsDisplay(value *string) string {
	if value == nil || *value == "" {
		return pageInlineCommentsDisplayDefault
	}
	return *value
}
func pageTableOfContentsDisplay(page *core.Record) string {
	value := page.GetString("table_of_contents_display")
	return normalizedPageTableOfContentsDisplay(&value)
}
func pageInlineCommentsDisplay(page *core.Record) string {
	value := page.GetString("inline_comments_display")
	return normalizedPageInlineCommentsDisplay(&value)
}
func pageCustomizationRequestHashInput(input pageMetadataInput) string {
	if input.TableOfContentsDisplay == nil && input.InlineCommentsDisplay == nil {
		return ""
	}
	return "\x00customization-v2\x00" + pageMetadataOptionalStringHash(input.TableOfContentsDisplay) + pageMetadataOptionalStringHash(input.InlineCommentsDisplay)
}
