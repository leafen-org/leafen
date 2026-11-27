//go:build !leafen_noapps

package app

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// What would stop an app's screen once it runs, said while checking its
// folder: an app's frame runs only the scripts in the package's files
// (docs/apps.md "Screens"), so an inline <script> or onclick= did nothing
// and the screen stayed on "…" with the reason only in the browser's
// console (2026-10-16).

const appScreenScanBytes = 2 << 20

var (
	appInlineScript    = regexp.MustCompile(`(?is)<script\b([^>]*)>(.*?)</script>`)
	appScriptSource    = regexp.MustCompile(`(?i)\bsrc\s*=`)
	appScriptElsewhere = regexp.MustCompile(`(?i)\bsrc\s*=\s*["']?((?:https?:)?//[^"'\s>]+)`)
	appScriptType      = regexp.MustCompile(`(?i)\btype\s*=\s*["']?([^"'\s>]+)`)
	appInlineHandler   = regexp.MustCompile(`(?i)<[a-z][^>]*\s(on[a-z]+)\s*=`)
)

// appFolderScreenNotices names each screen file's inline script and inline
// handler and any script it loads from elsewhere, unless the app declares scriptEval.
func appFolderScreenNotices(folder string, manifest appManifest) []string {
	if manifest.ScriptEval {
		return nil
	}
	notices := []string{}
	_ = filepath.WalkDir(filepath.Join(folder, "ui"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".html") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil || len(raw) > appScreenScanBytes {
			return nil
		}
		relative, _ := filepath.Rel(folder, path)
		notices = append(notices, appScreenNotices(filepath.ToSlash(relative), string(raw))...)
		return nil
	})
	return notices
}

func appScreenNotices(file, page string) []string {
	notices := []string{}
	line := func(offset int) int { return strings.Count(page[:offset], "\n") + 1 }
	for _, match := range appInlineScript.FindAllStringSubmatchIndex(page, -1) {
		attributes, body := page[match[2]:match[3]], strings.TrimSpace(page[match[4]:match[5]])
		if elsewhere := appScriptElsewhere.FindStringSubmatch(attributes); elsewhere != nil {
			notices = append(notices, fmt.Sprintf("%s line %d: the script from %s won't load: an app's frame runs only the package's "+
				"script files, so put a copy in the package", file, line(match[0]), elsewhere[1]))
			continue
		}
		if appScriptSource.MatchString(attributes) || body == "" {
			continue
		}
		// Data a script reads, such as JSON, isn't run.
		if kind := appScriptType.FindStringSubmatch(attributes); kind != nil && !strings.Contains(strings.ToLower(kind[1]), "javascript") &&
			!strings.EqualFold(kind[1], "module") {
			continue
		}
		notices = append(notices, fmt.Sprintf("%s line %d: an inline <script> doesn't run in an app's frame, which runs only the package's "+
			"script files; move it into a .js file the page loads (or declare \"scriptEval\": true for a screen that makes scripts)", file, line(match[0])))
	}
	for _, match := range appInlineHandler.FindAllStringSubmatchIndex(page, -1) {
		notices = append(notices, fmt.Sprintf("%s line %d: the inline %s= handler doesn't run in an app's frame; add the listener from a .js file",
			file, line(match[0]), strings.ToLower(page[match[2]:match[3]])))
	}
	return notices
}
