package app

import (
	"regexp"
	"strings"
)

// A picture's, video's, PDF's, sound's or file's external address as
// Notion's API keeps it (seen 2026-11-10, Notion-Version 2026-03-11): a new
// one any address it takes (notionLinkTaken), as given, spaces too; a changed
// picture, video, PDF or sound one whose path names its kind of file
// (notionMediaUpdateTaken), refused as "Invalid URL" otherwise; a changed
// file any text. A file is named from its address (notionExternalFileName).
// The address reads out as it was given; the editor opens it as a text
// link's (safeMediaUrl, mediaBlockUrl.ts).

// The endings of a path Notion's API takes for a changed picture, video or
// sound, whatever their letters' case (seen 2026-11-10: .ico, .avif, .jfif,
// .heif, .m2ts, .ts, .hevc, .aac, .flac, .opus and others refused).
var notionMediaUpdateEndings = map[string][]string{
	"image": {"png", "jpg", "jpeg", "gif", "webp", "svg", "heic", "tiff", "tif", "bmp"},
	"video": {"mp4", "mov", "webm", "ogv", "ogg", "mkv", "avi", "m4v", "wmv", "flv", "3gp", "3g2", "mpeg", "mpg", "mpe",
		"qt", "f4v", "m2v", "vob", "asf"},
	"audio": {"mp3", "wav", "m4a", "ogg", "oga"},
}

// The video sites whose page a changed video takes, as Notion's API does
// (seen 2026-11-10): YouTube's watch, embed, shorts, live and playlist pages,
// a youtu.be link, and a numbered Vimeo video; not YouTube's home or a
// channel, player.vimeo.com, www.vimeo.com or another site.
var notionVideoSitePattern = regexp.MustCompile(`(?i)^(?:https?://)?(?:(?:[a-z0-9-]+\.)?youtube\.com/(?:watch|embed|shorts|live|playlist)(?:[/?#]|$)|(?:www\.)?youtu\.be/[^/?#\s]|vimeo\.com/[0-9]+(?:[/?#]|$))`)

// notionMediaUpdateTaken tells whether Notion's API takes text as a changed
// picture's, video's, PDF's or sound's address: its path (notionLinkPath)
// ends in its kind's file ending, a PDF's holding ".pdf" anywhere in its
// letters' case, or a video is a video site's page.
func notionMediaUpdateTaken(blockType, text string) bool {
	path, found := notionLinkPath(text)
	switch blockType {
	case graphBlockTypePDF:
		return found && strings.Contains(path, ".pdf")
	case graphBlockTypeVideo:
		if notionVideoSitePattern.MatchString(strings.TrimSpace(text)) {
			return true
		}
	}
	lower := strings.ToLower(path)
	for _, ending := range notionMediaUpdateEndings[blockType] {
		if found && strings.HasSuffix(lower, "."+ending) {
			return true
		}
	}
	return false
}

// notionMediaUpdateRefused is Notion's refusal of a changed picture's,
// video's, PDF's or sound's address it doesn't take, said alone.
func notionMediaUpdateRefused(blockType, text string) notionContentFault {
	return notionContentFault(`Invalid URL "` + text + `" for blockType ` + blockType)
}

// notionLinkPath is the path an address has as Node's url.parse reads it,
// which Notion's API reads a file's address by: trimmed, after its scheme
// and the host a slashed or a mailto-like scheme names, up to its query or
// fragment, the characters an address may not hold bare percent-encoded
// (notionLinkEscaped), "/" for a web address with none. A scheme-less
// address is all path ("example.com/x", "//e.com/x"); javascript: has no
// host. False where it has none ("mailto:a@b.com", "notion://x", "   ").
func notionLinkPath(address string) (string, bool) {
	rest := strings.TrimFunc(address, func(r rune) bool { return r < 33 || r == '\u00a0' || r == '\ufeff' })
	// Backslashes before the query or fragment are slashes.
	if split := strings.IndexAny(rest, "?#"); split >= 0 {
		rest = strings.ReplaceAll(rest[:split], `\`, "/") + rest[split:]
	} else {
		rest = strings.ReplaceAll(rest, `\`, "/")
	}
	scheme := strings.ToLower(notionLinkSchemePattern.FindString(rest))
	rest = rest[len(scheme):]
	slashes := scheme != "" && strings.HasPrefix(rest, "//")
	if slashes {
		rest = rest[2:]
	}
	hostname := ""
	if scheme != "" && scheme != "javascript:" && (slashes || !notionLinkSlashedSchemes[scheme]) {
		_, hostname, _, rest = notionLinkHost(rest)
	}
	if at := strings.IndexAny(rest, "?#"); at >= 0 {
		rest = rest[:at]
	}
	if rest == "" && notionLinkSlashedSchemes[scheme] && hostname != "" {
		rest = "/"
	}
	return notionLinkEscaped(rest), rest != ""
}

// notionExternalFileName is the name Notion's API gives a file of an
// external address (seen 2026-11-10): one given keeps its letters and takes
// the address's file ending after them, unless it ends so already ("given" +
// a.pdf → "given.pdf", "doc.pdf" stays, "n.PDF" → "n.PDF.pdf"); without one,
// the last part of the address's path ("a%20b.pdf"), or, where that is
// none, the whole address as given ("https://example.com/").
func notionExternalFileName(given *string, address string) string {
	path, found := notionLinkPath(address)
	last := ""
	if found {
		last = path[strings.LastIndex(path, "/")+1:]
	}
	if given == nil {
		if last == "" {
			return address
		}
		return last
	}
	ending := ""
	if dot := strings.LastIndex(last, "."); dot >= 0 && dot < len(last)-1 {
		ending = last[dot:]
	}
	if strings.HasSuffix(*given, ending) {
		return *given
	}
	return *given + ending
}

// notionExternalFileRenamed is a file's name once its address changes, as
// Notion's API names it (seen 2026-11-10): a name given through it
// (givenName) still naming the file is given again with the new address's
// ending ("doc.pdf" on a.pdf, then b.zip → "doc.pdf.zip"; "given" →
// "given.pdf" → "given.zip"); a name its last address gave it comes from
// the new one; another name keeps its letters, the last address's ending,
// where it took one, giving way to the new one's ("report.pdf" →
// "report.zip").
func notionExternalFileRenamed(name, givenName, lastAddress, address string) string {
	if givenName != "" && name == notionExternalFileName(&givenName, lastAddress) {
		return notionExternalFileName(&givenName, address)
	}
	if name == notionExternalFileName(nil, lastAddress) {
		return notionExternalFileName(nil, address)
	}
	if lastPath, found := notionLinkPath(lastAddress); found {
		last := lastPath[strings.LastIndex(lastPath, "/")+1:]
		if dot := strings.LastIndex(last, "."); dot >= 0 && dot < len(last)-1 && strings.HasSuffix(name, last[dot:]) &&
			len(name) > len(last[dot:]) {
			name = strings.TrimSuffix(name, last[dot:])
		}
	}
	return notionExternalFileName(&name, address)
}
