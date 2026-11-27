package app

import (
	"fmt"
	"html"
	"io"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	nethtml "golang.org/x/net/html"
)

func parseURLMetadata(input io.Reader, finalURL string) (urlMetadata, error) {
	metadata, _, err := parseURLMetadataPage(input, finalURL)
	return metadata, err
}

// parseURLMetadataPage also gives the page's oEmbed address, where a video
// site says how big its player is.
func parseURLMetadataPage(input io.Reader, finalURL string) (urlMetadata, string, error) {
	bounded, err := io.ReadAll(io.LimitReader(input, urlMetadataMaxDecodedBodyBytes+1))
	if err != nil {
		return urlMetadata{}, "", fmt.Errorf("%w: %v", errURLMetadataParserLimit, err)
	}
	if len(bounded) > urlMetadataMaxDecodedBodyBytes {
		return urlMetadata{}, "", errURLMetadataParserLimit
	}
	tokenizer := nethtml.NewTokenizer(strings.NewReader(string(bounded)))
	tokenizer.SetMaxBuf(urlMetadataMaxDecodedBodyBytes)
	values := map[string]string{}
	icons := []urlMetadataIconLink{}
	oEmbed := ""
	// The page's own title, its first <title>: an icon's <title> in the
	// page's body (an inline SVG's) is no part of it.
	insideTitle, titleDone := 0, false
	var title strings.Builder
	// A page without a description tag is described by its first paragraph,
	// as Notion's bookmark describes one (seen 2026-10-17: example.com).
	paragraph := urlMetadataParagraph{}
	for {
		tokenType := tokenizer.Next()
		switch tokenType {
		case nethtml.ErrorToken:
			if tokenErr := tokenizer.Err(); tokenErr != nil && tokenErr != io.EOF {
				return urlMetadata{}, "", fmt.Errorf("%w: %v", errURLMetadataParserLimit, tokenErr)
			}
			return buildURLMetadata(values, icons, title.String(), paragraph.text(), finalURL), oEmbed, nil
		case nethtml.StartTagToken, nethtml.SelfClosingTagToken:
			token := tokenizer.Token()
			if tokenType == nethtml.StartTagToken || token.Data == "br" {
				paragraph.start(token.Data)
			}
			switch token.Data {
			case "title":
				if tokenType == nethtml.StartTagToken {
					insideTitle++
				}
			case "meta":
				captureURLMetadataMeta(values, token)
			case "link":
				if candidate, rank := urlMetadataIconCandidate(token); candidate != "" {
					icons = placeURLMetadataIcon(icons, candidate, rank)
				}
				if oEmbed == "" {
					oEmbed = urlMetadataOEmbedLink(token, finalURL)
				}
			}
		case nethtml.EndTagToken:
			name := tokenizer.Token().Data
			paragraph.end(name)
			if name == "title" && insideTitle > 0 {
				insideTitle--
				titleDone = titleDone || strings.TrimSpace(title.String()) != ""
			}
		case nethtml.TextToken:
			text := tokenizer.Token().Data
			if insideTitle > 0 && !titleDone {
				title.WriteString(text)
			}
			paragraph.write(text)
		}
	}
}

// urlMetadataParagraph keeps the text of a page's first paragraph with
// words in it, outside scripts and styles, up to a description's length, as
// a browser reads the page and as Notion's card describes it (seen
// 2026-11-12): its words and the links, bold and other inline parts among
// them run on as written ("In geometry, a square", not "In geometry , a
// square"), a line break parting them; and a paragraph ends where HTML ends
// it, at a block that cannot stand inside it (an unclosed <p> before a
// <blockquote> is an empty paragraph, the quotation none: an LWN comment's
// card gives its first own paragraph after the quotation).
type urlMetadataParagraph struct {
	open    bool
	hidden  int
	done    bool
	builder strings.Builder
}

// urlMetadataParagraphEnders are the tags whose start ends an open
// paragraph, as the HTML parser closes a p element before them.
var urlMetadataParagraphEnders = map[string]bool{
	"address": true, "article": true, "aside": true, "blockquote": true, "center": true, "details": true,
	"dialog": true, "dir": true, "div": true, "dl": true, "fieldset": true, "figcaption": true, "figure": true,
	"footer": true, "form": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"header": true, "hgroup": true, "hr": true, "li": true, "dd": true, "dt": true, "listing": true, "main": true,
	"menu": true, "nav": true, "ol": true, "p": true, "plaintext": true, "pre": true, "search": true,
	"section": true, "summary": true, "table": true, "ul": true, "xmp": true,
}

// urlMetadataParagraphHolders are the tags whose end ends a paragraph still
// open inside them.
var urlMetadataParagraphHolders = map[string]bool{
	"p": true, "body": true, "html": true, "div": true, "td": true, "th": true, "li": true, "dd": true, "dt": true,
	"blockquote": true, "section": true, "article": true, "main": true, "aside": true, "header": true,
	"footer": true, "nav": true, "figure": true, "figcaption": true, "form": true, "caption": true, "details": true,
}

func (p *urlMetadataParagraph) start(tag string) {
	switch {
	case tag == "script" || tag == "style" || tag == "noscript" || tag == "template":
		p.hidden++
	case tag == "br":
		p.write(" ")
	}
	if urlMetadataParagraphEnders[tag] {
		p.close()
	}
	if tag == "p" && !p.done {
		p.open = true
	}
}

func (p *urlMetadataParagraph) end(tag string) {
	switch {
	case tag == "script" || tag == "style" || tag == "noscript" || tag == "template":
		if p.hidden > 0 {
			p.hidden--
		}
	case urlMetadataParagraphHolders[tag]:
		p.close()
	}
}

// close ends the open paragraph: the description, if it has words.
func (p *urlMetadataParagraph) close() {
	if !p.open || p.done {
		return
	}
	p.open = false
	if strings.TrimSpace(p.builder.String()) != "" {
		p.done = true
	} else {
		p.builder.Reset()
	}
}

func (p *urlMetadataParagraph) write(text string) {
	if p.done || !p.open || p.hidden > 0 || p.builder.Len() > 4*urlMetadataMaxDescriptionRunes {
		return
	}
	p.builder.WriteString(text)
}

func (p *urlMetadataParagraph) text() string {
	if !p.done {
		return ""
	}
	return p.builder.String()
}

func captureURLMetadataMeta(values map[string]string, token nethtml.Token) {
	attributes := urlMetadataAttributes(token)
	name := strings.ToLower(firstNonempty(attributes["property"], attributes["name"], attributes["itemprop"]))
	if attributes["content"] == "" || values[name] != "" {
		return
	}
	switch name {
	case "og:title", "twitter:title", "og:site_name", "application-name",
		"og:description", "twitter:description", "description",
		"og:image", "og:image:secure_url", "og:image:url", "twitter:image", "twitter:image:src":
		values[name] = attributes["content"]
	}
}

// urlMetadataIconCandidate is a link's icon and how much the card wants it:
// a site's large icons first, its touch icon (0) and then its "fluid-icon"
// (1), and only then its tab icon (2), as Notion's card and link mention
// choose (seen 2026-11-12: github.com's fluidicon.png, which shows on a dark
// page, over the dark favicon.png its tab uses; Wikipedia's apple-touch
// icon).
func urlMetadataIconCandidate(token nethtml.Token) (string, int) {
	attributes := urlMetadataAttributes(token)
	rank := -1
	for _, relation := range strings.Fields(strings.ToLower(attributes["rel"])) {
		switch relation {
		case "apple-touch-icon", "apple-touch-icon-precomposed":
			rank = 0
		case "fluid-icon":
			if rank < 0 || rank > 1 {
				rank = 1
			}
		case "icon", "shortcut":
			if rank < 0 {
				rank = 2
			}
		}
	}
	if rank < 0 {
		return "", rank
	}
	return attributes["href"], rank
}

// placeURLMetadataIcon keeps the icons best first, in page order within a rank.
func placeURLMetadataIcon(icons []urlMetadataIconLink, href string, rank int) []urlMetadataIconLink {
	at := len(icons)
	for at > 0 && icons[at-1].rank > rank {
		at--
	}
	icons = append(icons, urlMetadataIconLink{})
	copy(icons[at+1:], icons[at:])
	icons[at] = urlMetadataIconLink{href: href, rank: rank}
	return icons
}

type urlMetadataIconLink struct {
	href string
	rank int
}

// urlMetadataOEmbedLink is a page's JSON oEmbed address (oembed.com's
// discovery link), made whole against the page's own.
func urlMetadataOEmbedLink(token nethtml.Token, finalURL string) string {
	attributes := urlMetadataAttributes(token)
	if !strings.EqualFold(strings.TrimSpace(attributes["type"]), "application/json+oembed") {
		return ""
	}
	for _, relation := range strings.Fields(strings.ToLower(attributes["rel"])) {
		if relation == "alternate" {
			return resolveURLMetadataIcon(attributes["href"], finalURL)
		}
	}
	return ""
}

func urlMetadataAttributes(token nethtml.Token) map[string]string {
	result := map[string]string{}
	for _, attribute := range token.Attr {
		if _, exists := result[attribute.Key]; !exists {
			result[strings.ToLower(attribute.Key)] = attribute.Val
		}
	}
	return result
}

func buildURLMetadata(values map[string]string, icons []urlMetadataIconLink, title, paragraph, finalURL string) urlMetadata {
	metadata := urlMetadata{
		URL:      finalURL,
		Title:    truncateURLMetadataText(firstNonempty(values["og:title"], values["twitter:title"], title), urlMetadataMaxTitleRunes),
		SiteName: truncateURLMetadataText(firstNonempty(values["og:site_name"], values["application-name"]), urlMetadataMaxSiteNameRunes),
		Description: truncateURLMetadataText(
			firstNonempty(values["og:description"], values["twitter:description"], values["description"], paragraph),
			urlMetadataMaxDescriptionRunes,
		),
	}
	// A page that names its site keeps its title whole, and one that doesn't
	// has the site's name it ends with moved out to be the site's name, as
	// Notion reads them (seen 2026-11-11: github.com's "GitHub · … · GitHub"
	// under the site "GitHub" stays whole; a Wikipedia article's "Notion
	// (productivity software) - Wikipedia", with no site name, is "Notion
	// (productivity software)" of the site "Wikipedia").
	if metadata.SiteName == "" {
		metadata.Title, metadata.SiteName = urlMetadataTitleWithoutSite(metadata.Title, finalURL)
	}
	if metadata.Title == "" {
		metadata.Title = urlMetadataHostnameTitle(finalURL)
	}
	// The page's own picture, as Notion's card shows it beside the words (seen 2026-11-05).
	if image := firstNonempty(values["og:image:secure_url"], values["og:image"], values["og:image:url"],
		values["twitter:image"], values["twitter:image:src"]); image != "" {
		metadata.ImageURL = resolveURLMetadataIcon(image, finalURL)
	}
	for _, candidate := range append(icons, urlMetadataIconLink{href: "/favicon.ico", rank: 3}) {
		if normalized := resolveURLMetadataIcon(candidate.href, finalURL); normalized != "" {
			metadata.IconURL = normalized
			break
		}
	}
	return metadata
}

func truncateURLMetadataText(value string, limit int) string {
	cleaned := strings.Join(strings.Fields(html.UnescapeString(value)), " ")
	if utf8.RuneCountInString(cleaned) <= limit {
		return cleaned
	}
	return string([]rune(cleaned)[:limit])
}

func resolveURLMetadataIcon(candidate, baseURL string) string {
	base, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	reference, err := url.Parse(strings.TrimSpace(candidate))
	if err != nil {
		return ""
	}
	normalized, err := normalizeOutboundPublicURL(
		base.ResolveReference(reference).String(), urlMetadataMaxURLChars, true,
	)
	if err != nil {
		return ""
	}
	return normalized
}

// urlMetadataTitleWithoutSite takes the site's name off the end of a page's
// title, as Notion's bookmark card does (seen 2026-11-07: a Wikipedia article
// titled "Notion (productivity software) - Wikipedia" shows as "Notion
// (productivity software)"): the words after the last " - ", " | ", " – ",
// " — " or " · " go when they name the site as its address does (wikipedia
// for en.wikipedia.org), and are given back as the site's name.
func urlMetadataTitleWithoutSite(title, pageURL string) (string, string) {
	name := ""
	if parsed, err := url.Parse(pageURL); err == nil {
		labels := strings.Split(strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www."), ".")
		if len(labels) >= 2 {
			name = urlMetadataNameKey(labels[len(labels)-2])
		}
	}
	if name == "" {
		return title, ""
	}
	cut, site := -1, ""
	for _, separator := range []string{" - ", " | ", " – ", " — ", " · "} {
		if index := strings.LastIndex(title, separator); index > cut {
			if tail := title[index+len(separator):]; urlMetadataNameKey(tail) == name {
				cut, site = index, strings.TrimSpace(tail)
			}
		}
	}
	if cut <= 0 {
		return title, ""
	}
	return strings.TrimSpace(title[:cut]), site
}

// urlMetadataNameKey is a name as letters and digits only, in lower case.
func urlMetadataNameKey(value string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func urlMetadataHostnameTitle(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Hostname() == "" {
		return "Link"
	}
	return strings.TrimPrefix(parsed.Hostname(), "www.")
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
