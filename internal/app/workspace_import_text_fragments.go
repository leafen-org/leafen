package app

import "unicode/utf8"

const workspaceImportTextFragmentBytes = 128 << 10

// splitWorkspaceImportText cuts long text into fragments of at most
// workspaceImportTextFragmentBytes, each cut between characters.
func splitWorkspaceImportText(value string) []string {
	if value == "" {
		return nil
	}
	parts := []string{}
	for len(value) > workspaceImportTextFragmentBytes {
		cut := workspaceImportTextFragmentBytes
		for cut > 0 && !utf8.RuneStart(value[cut]) {
			cut--
		}
		if cut == 0 {
			cut = len(value)
		}
		parts = append(parts, value[:cut])
		value = value[cut:]
	}
	if value != "" {
		parts = append(parts, value)
	}
	return parts
}
