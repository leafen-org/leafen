package app

import "github.com/pocketbase/pocketbase/core"

func validPageFont(font string) bool {
	switch font {
	case "default", "serif", "mono":
		return true
	default:
		return false
	}
}

func validPageStyleMetadataInput(input pageMetadataInput) bool {
	return input.Font == nil || validPageFont(*input.Font)
}

func applyPageStyleMetadata(page *core.Record, input pageMetadataInput) {
	if input.Font != nil {
		page.Set("font", *input.Font)
	}
	if input.SmallText != nil {
		page.Set("small_text", *input.SmallText)
	}
	if input.FullWidth != nil {
		page.Set("full_width", *input.FullWidth)
	}
}

func initializePageStyleMetadata(page *core.Record) {
	page.Set("font", pageFontDefault)
	page.Set("small_text", false)
	page.Set("full_width", false)
}

func normalizedPageFont(font *string) string {
	if font == nil {
		return pageFontDefault
	}
	return *font
}
