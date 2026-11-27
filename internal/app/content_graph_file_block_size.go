package app

import "strings"

// graphFileBlockSizes is which of its size keys a file block keeps, by its
// type and the file it holds: a width (width, sizedWidth) and an alignment
// for visual media, a height for a PDF or an embed, its own size
// (naturalSize) for a picture and its column (columnWide) for a video or a
// PDF. The content graph refuses a key a block doesn't keep; the block API,
// which gives one (notionBlockSize), leaves it.
type graphFileBlockSizes struct {
	Width, Height, Natural, Column bool
}

func graphFileBlockSizesOf(blockType, contentType string) graphFileBlockSizes {
	mime := strings.ToLower(strings.SplitN(contentType, ";", 2)[0])
	return graphFileBlockSizes{
		Width: blockType == graphBlockTypeImage || blockType == graphBlockTypeVideo || blockType == graphBlockTypePDF ||
			blockType == graphBlockTypeEmbed || blockType == graphBlockTypeFile &&
			(strings.HasPrefix(mime, "image/") || strings.HasPrefix(mime, "video/") || mime == "application/pdf" || mime == "text/html"),
		Height:  blockType == graphBlockTypePDF || blockType == graphBlockTypeEmbed,
		Natural: blockType == graphBlockTypeImage,
		Column:  blockType == graphBlockTypeVideo || blockType == graphBlockTypePDF,
	}
}
