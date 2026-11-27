package app

import (
	"net/url"
	"regexp"
	"strings"
)

// An MCP read names a stored file by its stable notion-file-block:// address
// (notion_page_markdown_file_blocks.go), which every read writes alike, as
// Notion's MCP names an uploaded file and finds a file, image or video line
// copied from fetch as it was written (seen 2026-11-22: <file
// source="notion-file-block://…" />, <image source="…" /> and an external
// <file source="…?a=1&amp;b=2" /> each matched as update_content's old_str;
// a file-upload://<ID> or an &amp; where fetch wrote & matched nothing). The
// public API's Markdown names one by a download address sealed for the read
// that wrote it (notionMarkdownReader.fileAddresses), so no two of its reads
// write one file's address alike, and so did an MCP read before 2026-11-22.
// Text a change looks for in the page — update_content's old_str,
// insert_content's after, replace_content_range's content_range, a comment's
// selection and a suggested edit's old_text — reads each stored file's
// address in it, either kind, as the page's own for the same block and file
// whenever it was read: as the page writes that kind there, its & written as
// the page writes it (& or &amp;); or, where the page writes the file only by
// the other kind, as the page writes it, so a line copied from any fetch, or
// from a public API read writing its line alike, finds its block (2026-11-22:
// a file line copied from fetch was "Nothing in the page matches"; a line
// written otherwise — the public API's ![](…) where fetch writes <image
// source="…" /> — still differs). An address the page names for no such
// block and file, or for more than one, is left as written, as is one of the
// page's own kind whose & is written otherwise: Notion's own file address
// with &amp; for its & matched nothing.

// notionMarkdownFileAddressPattern finds a stored file's address as a read
// writes it: a download address — its origin, the capability path and token,
// and after "#" its block, file and name — or a stable one, & written as & or
// &amp; in either.
var notionMarkdownFileAddressPattern = regexp.MustCompile(`https?://[^\s"'<>()\[\]]*?` +
	regexp.QuoteMeta(apiRoot+strings.TrimSuffix(fileTransferDownloadCapabilityPath, "{token}")) +
	`[A-Za-z0-9_%-]+#[A-Za-z0-9=&;%+._~-]*` +
	`|` + regexp.QuoteMeta(notionMarkdownFileBlockScheme) + `[A-Za-z0-9_-]+/[A-Za-z0-9_-]+(?:\?[A-Za-z0-9=&;%._~-]*)?`)

// notionMarkdownFileAddressKey is the block and file an address is for, its
// kind and how its & is written.
type notionMarkdownFileAddressKey struct {
	stable, escaped bool
	block, file     string
}

// notionMarkdownFileAddressOf reads an address's key; false for one that
// names no file.
func notionMarkdownFileAddressOf(address string) (notionMarkdownFileAddressKey, bool) {
	if blockID, fileID, _, stable := notionMarkdownFileBlockReference(address); stable {
		return notionMarkdownFileAddressKey{stable: true, escaped: strings.Contains(address, "&amp;"), block: blockID, file: fileID}, true
	}
	_, fragment, _ := strings.Cut(address, "#")
	values, err := url.ParseQuery(strings.ReplaceAll(fragment, "&amp;", "&"))
	if err != nil || values.Get("file") == "" {
		return notionMarkdownFileAddressKey{}, false
	}
	return notionMarkdownFileAddressKey{
		escaped: strings.Contains(fragment, "&amp;"), block: values.Get("block"), file: values.Get("file"),
	}, true
}

// notionMarkdownFileAddressesRebased is text, looked for in a page's
// Markdown, with each stored file's address in it written as the page's
// Markdown writes the same block's file.
func notionMarkdownFileAddressesRebased(page, text string) string {
	if !strings.Contains(text, "#") && !strings.Contains(text, notionMarkdownFileBlockScheme) ||
		!notionMarkdownFileAddressPattern.MatchString(text) {
		return text
	}
	type blockFile struct{ block, file string }
	written := map[notionMarkdownFileAddressKey]string{}
	// A block's file as the page writes it by either kind, and the kinds it
	// writes it by.
	anyKind := map[blockFile]string{}
	kinds := map[blockFile]map[bool]bool{}
	note := func(into map[blockFile]string, key blockFile, address string) {
		if before, seen := into[key]; !seen || before == address {
			into[key] = address
		} else {
			// Two ways of writing one block's file name neither.
			into[key] = ""
		}
	}
	for _, address := range notionMarkdownFileAddressPattern.FindAllString(page, -1) {
		key, named := notionMarkdownFileAddressOf(address)
		if !named {
			continue
		}
		if before, seen := written[key]; !seen || before == address {
			written[key] = address
		} else {
			written[key] = ""
		}
		file := blockFile{key.block, key.file}
		note(anyKind, file, address)
		if kinds[file] == nil {
			kinds[file] = map[bool]bool{}
		}
		kinds[file][key.stable] = true
	}
	return notionMarkdownFileAddressPattern.ReplaceAllStringFunc(text, func(address string) string {
		key, named := notionMarkdownFileAddressOf(address)
		if !named {
			return address
		}
		if written[key] != "" {
			return written[key]
		}
		file := blockFile{key.block, key.file}
		if !kinds[file][key.stable] && anyKind[file] != "" {
			return anyKind[file]
		}
		return address
	})
}
