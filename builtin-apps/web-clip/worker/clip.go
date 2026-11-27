package main

import (
	"encoding/json"
	"mime"
	"net/url"
	"path"
	"strings"
	"unicode/utf8"
)

// A clip as a clipper sends it: JSON with the names MarkSnip's templates
// use ({title}, {content}, {pageURL}, …) or the common ones other tools
// send, a form with the same names, or the markdown alone.

type clipData struct {
	Title, URL, Content, Excerpt, Byline, Keywords, Note, Source string
}

const (
	titleMaxRunes = 500
	fieldMaxRunes = 2000
)

// clipNames are the names each part of a clip may come under, lowercased.
var clipNames = map[string][]string{
	"title":    {"title", "name", "pagetitle", "subject"},
	"content":  {"content", "markdown", "body", "text", "md"},
	"url":      {"pageurl", "url", "sourceurl", "source_url", "link", "href", "baseuri", "address"},
	"excerpt":  {"excerpt", "summary", "description"},
	"byline":   {"byline", "author"},
	"keywords": {"keywords", "tags"},
	"note":     {"note", "memo", "comment"},
	"source":   {"source", "clipper", "app"},
	"path":     {"path", "filename", "file"},
}

// readClip reads a clip from a request's body; refusal says why it can't.
func readClip(contentType, rawQuery string, body []byte) (clipData, string) {
	query, _ := url.ParseQuery(rawQuery)
	media, _, _ := mime.ParseMediaType(contentType)
	fields := map[string]string{}
	switch {
	case media == "application/json" || strings.HasSuffix(media, "+json") || (media == "" && json.Valid(body)):
		raw := map[string]any{}
		if json.Unmarshal(body, &raw) != nil {
			return clipData{}, "Send the clip as a JSON object."
		}
		for key, value := range raw {
			fields[strings.ToLower(key)] = textOf(value)
		}
	case media == "application/x-www-form-urlencoded":
		form, err := url.ParseQuery(string(body))
		if err != nil {
			return clipData{}, "The form couldn't be read."
		}
		for key := range form {
			fields[strings.ToLower(key)] = form.Get(key)
		}
	default:
		// The markdown alone: its first line names it.
		fields["content"] = string(body)
	}
	for key := range query {
		if name := strings.ToLower(key); fields[name] == "" {
			fields[name] = query.Get(key)
		}
	}
	// A note's front matter fills what the request left out, and stays out
	// of the page's text.
	name := firstPresent(fields, clipNames["content"])
	front, rest := frontMatter(strings.ReplaceAll(fields[name], "\r\n", "\n"))
	fields[name] = rest
	for key, value := range front {
		if fields[key] == "" {
			fields[key] = value
		}
	}
	pick := func(part string) string {
		for _, name := range clipNames[part] {
			if value := strings.TrimSpace(fields[name]); value != "" {
				return value
			}
		}
		return ""
	}
	c := clipData{Title: pick("title"), Content: fields[name], Excerpt: pick("excerpt"),
		Byline: pick("byline"), Keywords: pick("keywords"), Note: pick("note"), Source: pick("source")}
	if !utf8.ValidString(c.Content) {
		return clipData{}, "Send the clip as UTF-8 text."
	}
	// A link only when it is one: nothing a click could run.
	for _, candidate := range []string{pick("url"), c.Source} {
		if parsed, err := url.Parse(candidate); err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" {
			c.URL = parsed.String()
			break
		}
	}
	if c.URL != "" && c.Source == c.URL {
		c.Source = ""
	}
	if c.Title == "" {
		c.Title = titleFromPath(pick("path"))
	}
	if c.Title == "" {
		c.Title = titleFromMarkdown(c.Content)
	}
	if c.Title == "" && c.URL != "" {
		parsed, _ := url.Parse(c.URL)
		c.Title = parsed.Host + strings.TrimSuffix(parsed.Path, "/")
	}
	if c.Title == "" && strings.TrimSpace(c.Content) == "" {
		return clipData{}, "The clip has no title, address or text."
	}
	c.Title = cut(oneLine(c.Title), titleMaxRunes)
	c.Excerpt, c.Byline, c.Keywords = cut(c.Excerpt, fieldMaxRunes), cut(oneLine(c.Byline), fieldMaxRunes), cut(oneLine(c.Keywords), fieldMaxRunes)
	c.Note, c.Source = cut(c.Note, fieldMaxRunes), cut(oneLine(c.Source), 200)
	return c, ""
}

func firstPresent(fields map[string]string, names []string) string {
	for _, name := range names {
		if strings.TrimSpace(fields[name]) != "" {
			return name
		}
	}
	return names[0]
}

// textOf is a JSON value as text: a list of keywords joined, numbers and
// the like as written.
func textOf(value any) string {
	switch given := value.(type) {
	case nil:
		return ""
	case string:
		return given
	case []any:
		parts := []string{}
		for _, item := range given {
			if text := strings.TrimSpace(textOf(item)); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		return ""
	}
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

// frontMatter reads a note's leading YAML block of plain "key: value"
// lines and gives back the rest of the text.
func frontMatter(text string) (map[string]string, string) {
	fields := map[string]string{}
	body, found := strings.CutPrefix(strings.TrimLeft(text, "\ufeff"), "---\n")
	if !found {
		return fields, text
	}
	head, rest, closed := strings.Cut(body, "\n---")
	if !closed {
		return map[string]string{}, text
	}
	for _, line := range strings.Split(head, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.HasPrefix(line, " ") {
			continue
		}
		fields[strings.ToLower(strings.TrimSpace(key))] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	_, rest, _ = strings.Cut(rest, "\n")
	return fields, rest
}

// titleFromPath is a note's name from a path such as "Clippings/Title.md".
func titleFromPath(given string) string {
	name := path.Base(strings.ReplaceAll(given, "\\", "/"))
	if name == "." || name == "/" {
		return ""
	}
	return strings.TrimSuffix(name, path.Ext(name))
}

// titleFromMarkdown is the first heading, or else the first line.
func titleFromMarkdown(markdown string) string {
	first := ""
	for _, line := range strings.Split(markdown, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "---" {
			continue
		}
		if heading, ok := strings.CutPrefix(line, "# "); ok {
			return heading
		}
		if first == "" {
			first = strings.TrimLeft(line, "#> -*")
		}
	}
	return first
}

func oneLine(text string) string { return strings.Join(strings.Fields(text), " ") }

func cut(text string, max int) string {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) <= max {
		return text
	}
	return string([]rune(text)[:max-1]) + "…"
}

// pieces splits markdown into parts Leafen takes one at a time, at blank
// lines where it can.
func pieces(markdown string, size int) []string {
	parts := []string{}
	for len(markdown) > size {
		cut := strings.LastIndex(markdown[:size], "\n\n")
		if cut < size/2 {
			cut = strings.LastIndex(markdown[:size], "\n")
		}
		if cut < size/2 {
			cut = size
			for cut > 0 && !utf8.RuneStart(markdown[cut]) {
				cut--
			}
		}
		parts = append(parts, markdown[:cut])
		markdown = strings.TrimLeft(markdown[cut:], "\n")
	}
	if strings.TrimSpace(markdown) != "" {
		parts = append(parts, markdown)
	}
	return parts
}
