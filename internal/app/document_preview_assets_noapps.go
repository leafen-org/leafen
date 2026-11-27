//go:build leafen_noapps

package app

import "embed"

// The edition without the app platform ships no built-in apps, but the file
// preview still draws PDFs with the PDF app's pdf.js and reads pdf.js's words
// in the person's language from the app's l10n folder and pdf.js's
// translations: only those folders are carried, so a PDF previews here as in
// every other edition.
//
//go:embed builtinapps/document-viewer/ui/vendor/pdfjs-dist-* builtinapps/document-viewer/ui/vendor/pdfjs-locale-* builtinapps/document-viewer/ui/l10n
var documentPreviewAssets embed.FS
