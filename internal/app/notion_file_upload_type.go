package app

import "strings"

// notionFileUploadDefaultTypes is the content type Notion gives a new upload
// by its name's extension when the request names none (measured 2026-11-11,
// Notion-Version 2026-03-11), text types with "; charset=utf-8" as Notion's
// are. Its extensions are the ones uploads take.
var notionFileUploadDefaultTypes = map[string]string{
	"aac": "audio/aac", "adts": "audio/aac", "mid": "audio/midi", "midi": "audio/midi",
	"mp3": "audio/mpeg", "mpga": "audio/mpeg", "m4a": "audio/mp4", "m4b": "audio/mp4",
	"mp4": "application/mp4", "oga": "audio/ogg", "ogg": "audio/ogg", "opus": "audio/ogg",
	"wav": "audio/wav", "wma": "audio/x-ms-wma", "weba": "audio/webm", "flac": "audio/x-flac",
	"pdf": "application/pdf", "txt": "text/plain; charset=utf-8", "csv": "text/csv; charset=utf-8",
	"json": "application/json; charset=utf-8", "js": "text/javascript; charset=utf-8",
	"ts": "application/typescript", "tsx": "application/typescript", "py": "text/x-python",
	"doc": "application/msword", "dot": "application/msword",
	"docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	"dotx": "application/vnd.openxmlformats-officedocument.wordprocessingml.template",
	"xls":  "application/vnd.ms-excel", "xlt": "application/vnd.ms-excel",
	"xla":  "application/vnd.ms-excel",
	"xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	"xltx": "application/vnd.openxmlformats-officedocument.spreadsheetml.template",
	"ppt":  "application/vnd.ms-powerpoint", "pot": "application/vnd.ms-powerpoint",
	"pps": "application/vnd.ms-powerpoint", "ppa": "application/vnd.ms-powerpoint",
	"pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	"potx": "application/vnd.openxmlformats-officedocument.presentationml.template",
	"rtf":  "application/rtf", "md": "text/markdown; charset=utf-8",
	"markdown": "text/markdown; charset=utf-8", "html": "text/html; charset=utf-8",
	"htm": "text/html; charset=utf-8", "epub": "application/epub+zip", "xml": "application/xml",
	"css": "text/css; charset=utf-8", "odt": "application/vnd.oasis.opendocument.text",
	"ods": "application/vnd.oasis.opendocument.spreadsheet",
	"odp": "application/vnd.oasis.opendocument.presentation", "ics": "text/calendar; charset=utf-8",
	"yaml": "text/yaml; charset=utf-8", "yml": "text/yaml; charset=utf-8",
	"tsv": "text/tab-separated-values; charset=utf-8", "zip": "application/zip",
	"gz": "application/gzip", "gzip": "application/gzip", "tar": "application/x-tar",
	"7z": "application/x-7z-compressed", "bz2": "application/x-bzip2", "rar": "application/vnd.rar",
	"gif": "image/gif", "heic": "image/heic", "jpeg": "image/jpeg", "jpg": "image/jpeg",
	"png": "image/png", "svg": "image/svg+xml", "tif": "image/tiff", "tiff": "image/tiff",
	"webp": "image/webp", "ico": "image/vnd.microsoft.icon", "bmp": "image/bmp", "avif": "image/avif",
	"apng": "image/apng", "amv": "video/x-amv", "asf": "video/x-ms-asf", "wmv": "video/x-ms-asf",
	"avi": "video/x-msvideo", "f4v": "video/x-f4v", "flv": "video/x-flv", "gifv": "video/mp4",
	"m4v": "video/mp4", "mkv": "video/webm", "webm": "video/webm", "mov": "video/quicktime",
	"qt": "video/quicktime", "mpeg": "video/mpeg", "ogv": "video/ogg", "3gp": "video/3gpp",
	"3g2": "video/3gpp2", "dwg": "image/vnd.dwg", "dxf": "image/vnd.dxf", "dwf": "model/vnd.dwf",
	"rvt": "application/vnd.autodesk.revit", "rfa": "application/vnd.autodesk.revit",
	"fbx": "application/vnd.autodesk.fbx", "3dm": "application/vnd.mcneel.3dm", "step": "model/step",
	"stp": "model/step", "iges": "model/iges", "igs": "model/iges", "stl": "model/stl",
	"obj": "model/obj", "3mf": "model/3mf", "gltf": "model/gltf+json", "glb": "model/gltf-binary",
	"dae": "model/vnd.collada+xml", "usdz": "model/vnd.usdz+zip",
}

// notionFileUploadTypes is each media type uploads take, with the extension
// Notion gives a name without one (measured 2026-11-11). The active ones
// (scripts, styles, XML, SVG) pass here and are refused by Leafen's file
// policy after.
var notionFileUploadTypes = map[string]string{
	"audio/aac": "adts", "audio/midi": "mid", "audio/mpeg": "mpga", "audio/mp4": "m4a",
	"video/mp4": "mp4", "application/mp4": "mp4", "audio/ogg": "oga", "audio/wav": "wav",
	"audio/x-ms-wma": "wma", "audio/webm": "weba", "audio/x-flac": "flac", "application/pdf": "pdf",
	"text/plain": "txt", "text/csv": "csv", "application/csv": "csv", "application/json": "json",
	"application/typescript": "ts", "text/x-python": "py", "application/msword": "doc",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": "docx",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.template": "dotx",
	"application/vnd.ms-excel": "xls",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         "xlsx",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.template":      "xltx",
	"application/vnd.ms-powerpoint":                                             "ppt",
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": "pptx",
	"application/vnd.openxmlformats-officedocument.presentationml.template":     "potx",
	"application/rtf": "rtf", "text/markdown": "md", "text/html": "html",
	"application/epub+zip": "epub", "application/vnd.oasis.opendocument.text": "odt",
	"application/vnd.oasis.opendocument.spreadsheet":  "ods",
	"application/vnd.oasis.opendocument.presentation": "odp", "text/calendar": "ics",
	"text/yaml": "yaml", "text/tab-separated-values": "tsv", "application/zip": "zip",
	"application/gzip": "gz", "application/x-tar": "tar", "application/x-7z-compressed": "7z",
	"application/x-bzip2": "bz2", "application/vnd.rar": "rar", "image/gif": "gif",
	"image/heic": "heic", "image/jpeg": "jpg", "image/png": "png", "image/tiff": "tif",
	"image/webp": "webp", "image/vnd.microsoft.icon": "ico", "image/bmp": "bmp", "image/avif": "avif",
	"image/apng": "apng", "video/x-amv": "amv", "video/x-ms-asf": "asf", "video/x-msvideo": "avi",
	"video/x-f4v": "f4v", "video/x-flv": "flv", "video/webm": "webm", "video/quicktime": "qt",
	"video/mpeg": "mpeg", "video/ogg": "ogv", "video/3gpp": "3gp", "video/3gpp2": "3g2",
	"image/vnd.dwg": "dwg", "image/vnd.dxf": "dxf", "model/vnd.dwf": "dwf",
	"application/vnd.autodesk.revit": "rvt", "application/vnd.autodesk.fbx": "fbx",
	"application/vnd.mcneel.3dm": "3dm", "model/step": "step", "model/iges": "igs", "model/stl": "stl",
	"model/obj": "obj", "model/3mf": "3mf", "model/gltf+json": "gltf", "model/gltf-binary": "glb",
	"model/vnd.usdz+zip": "usdz", "text/javascript": "js", "application/javascript": "js",
	"image/svg+xml": "svg", "application/xml": "xml", "text/xml": "xml", "text/css": "css",
	"model/vnd.collada+xml": "dae",
}

// notionFileUploadOtherTypes is the extension Notion still finds for a media
// type uploads don't take (measured 2026-11-11): a name without one takes it
// and is then refused by it, or by the type. Notion finds none for a type
// in neither table, and says the name is missing one.
var notionFileUploadOtherTypes = map[string]string{
	"application/octet-stream": "bin", "video/x-ms-wmv": "wmv", "video/x-matroska": "mkv",
	"audio/mp3": "mp3", "image/x-icon": "ico", "audio/x-wav": "wav", "audio/wave": "wav",
	"audio/x-m4a": "m4a", "application/x-zip-compressed": "zip", "audio/x-aac": "aac",
	"video/x-m4v": "m4v", "application/x-rar-compressed": "rar", "text/rtf": "rtf",
	"text/x-markdown": "mkd", "application/x-msdownload": "exe", "image/heif": "heif",
	"image/pjpeg": "jfif", "application/x-tex": "tex",
}

const notionFileUploadUnknownType = "application/octet-stream"

// normalizeNotionFileUploadMetadata settles a new upload's name and content
// type as Notion does: the name as notionFileUploadName keeps it, its
// extension in lower case, or the given type's when it has none; the type as
// given, or the extension's default. A given type is kept as written, for
// any extension uploads take; application/octet-stream says nothing beside
// such an extension.
func normalizeNotionFileUploadMetadata(name string, contentType string) (string, string, error) {
	named, given := name != "", contentType != ""
	name = notionFileUploadName(name)
	mediaType := notionFileUploadMediaType(contentType)
	extension := notionFileUploadExtension(name)
	if named && extension == "" {
		extension = notionFileUploadTypeExtension(mediaType)
		if !given || extension == "" {
			return "", "", notionInvalid(errFileUploadInvalid, "The `filename` has no extension, and no `content_type` gives one.")
		}
		name += "." + extension
	}
	if named {
		lower := strings.ToLower(extension)
		if _, ok := notionFileUploadDefaultTypes[lower]; !ok {
			return "", "", notionInvalid(errFileUploadInvalid, "The File Upload API doesn't take a file with this `filename`'s extension.")
		}
		name = name[:len(name)-len(extension)] + lower
		if mediaType == notionFileUploadUnknownType {
			given = false
		}
		if !given {
			contentType = notionFileUploadDefaultTypes[lower]
		}
	}
	if _, ok := notionFileUploadTypes[mediaType]; given && !ok {
		return "", "", notionInvalid(errFileUploadInvalid, "The File Upload API doesn't take content type "+contentType+".")
	}
	if contentType != "" {
		if _, err := notionFileUploadStoredType(name, contentType); err != nil {
			return "", "", err
		}
	}
	return name, contentType, nil
}

// notionFileUploadSentMetadata settles a single_part upload's name and
// content type from the file sent to it, as Notion does (measured
// 2026-11-11): the upload's own name, or the file's kept as at creation;
// the file's media type when it names one, which must be the upload's own
// when the upload has one, or else the upload's type or the extension's
// default.
func notionFileUploadSentMetadata(upload storedProductFileUpload, fileName string, fileType string) (string, string, error) {
	name := upload.OriginalName
	if name == "" {
		name = notionFileUploadName(fileName)
	}
	sent := strings.TrimSpace(notionFileUploadMediaType(fileType))
	if sent == "" {
		sent = notionFileUploadUnknownType
	}
	if upload.ContentType != "" && sent != notionFileUploadUnknownType &&
		sent != strings.TrimSpace(notionFileUploadMediaType(upload.ContentType)) {
		return "", "", notionInvalid(errFileUploadInvalid, "The file's content type `"+sent+
			"` isn't the one the upload began with, `"+upload.ContentType+
			"`: send it as that type, or start a new File Upload.")
	}
	extension := notionFileUploadExtension(name)
	contentType := upload.ContentType
	if sent != notionFileUploadUnknownType {
		contentType = sent
	}
	if upload.ContentType == "" {
		known := false
		if sent == notionFileUploadUnknownType {
			contentType, known = notionFileUploadDefaultTypes[strings.ToLower(extension)]
		} else {
			_, known = notionFileUploadTypes[sent]
		}
		if !known {
			return "", "", notionInvalid(errFileUploadInvalid, "The file upload's content type `"+sent+"` can't be used.")
		}
	}
	if extension == "" {
		extension = notionFileUploadTypeExtension(notionFileUploadMediaType(contentType))
		name += "." + extension
	}
	lower := strings.ToLower(extension)
	if _, ok := notionFileUploadDefaultTypes[lower]; !ok {
		return "", "", notionInvalid(errFileUploadInvalid, "The file upload's extension can't be used.")
	}
	name = name[:len(name)-len(extension)] + lower
	if _, err := notionFileUploadStoredType(name, contentType); err != nil {
		return "", "", err
	}
	return name, contentType, nil
}

// notionFileUploadName is a file's name as Notion keeps it (measured
// 2026-11-11): each whitespace character becomes "_", and of the rest only
// ASCII letters, digits and "!()-_.", Cyrillic, Hangul jamo, CJK punctuation
// and kana, the CJK and Hangul ranges from U+3130 and the full- and
// half-width forms through U+FF9F stay.
func notionFileUploadName(value string) string {
	var kept strings.Builder
	for _, current := range value {
		switch {
		case notionFileUploadSpace(current):
			kept.WriteByte('_')
		case current >= 'a' && current <= 'z', current >= 'A' && current <= 'Z', current >= '0' && current <= '9',
			strings.ContainsRune("!()-_.", current),
			current >= 0x0400 && current <= 0x052f, current >= 0x1100 && current <= 0x11ff,
			current >= 0x3001 && current <= 0x30ff, current >= 0x3130 && current <= 0xd7ff,
			current >= 0xff00 && current <= 0xff9f:
			kept.WriteRune(current)
		}
	}
	return kept.String()
}

// notionFileUploadSpace is JavaScript's whitespace, which Notion's names turn
// into "_" (U+FEFF among it, U+0085 not).
func notionFileUploadSpace(current rune) bool {
	switch current {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return current >= 0x2000 && current <= 0x200a
}

func notionFileUploadExtension(name string) string {
	index := strings.LastIndexByte(name, '.')
	if index < 0 {
		return ""
	}
	return name[index+1:]
}

// notionFileUploadMediaType is a content type's media type as Notion
// compares it: before any parameters, in lower case, with its spaces.
func notionFileUploadMediaType(contentType string) string {
	mediaType, _, _ := strings.Cut(contentType, ";")
	return strings.ToLower(mediaType)
}

// notionFileUploadSameType says whether two content types name one media type.
func notionFileUploadSameType(first string, second string) bool {
	return strings.TrimSpace(notionFileUploadMediaType(first)) ==
		strings.TrimSpace(notionFileUploadMediaType(second))
}

func notionFileUploadTypeExtension(mediaType string) string {
	if extension, ok := notionFileUploadTypes[mediaType]; ok {
		return extension
	}
	return notionFileUploadOtherTypes[mediaType]
}

// notionFileUploadStoredType is the type Leafen keeps and serves a Notion
// upload's file as: its media type alone, through the file policy, with an
// mp4 Notion calls application/mp4 kept as the video it is.
func notionFileUploadStoredType(name string, contentType string) (string, error) {
	stored, err := admitStoredFileType(name, contentType)
	if stored == "application/mp4" {
		stored = "video/mp4"
	}
	return stored, err
}

// storedContentType is the type an upload's published file keeps: a Notion
// upload shows its content type as Notion does and stores it plain.
func (upload storedProductFileUpload) storedContentType() string {
	if upload.NotionMode == "" || upload.ContentType == "" {
		return upload.ContentType
	}
	if stored, err := notionFileUploadStoredType(upload.OriginalName, upload.ContentType); err == nil {
		return stored
	}
	return upload.ContentType
}
