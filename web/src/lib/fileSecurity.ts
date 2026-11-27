import {
  activeFileContentTypes,
  activeFileExtensions,
  sandboxedHtmlContentType,
  sandboxedHtmlExtensions,
} from "@/fileSecurityPolicy.generated";

const ACTIVE_CONTENT_MIME_TYPES = new Set<string>(activeFileContentTypes);
const ACTIVE_CONTENT_EXTENSION_RE = new RegExp(
  `\\.(?:${activeFileExtensions.join("|")})$`,
  "i",
);

export interface FileTypeDescriptor {
  name?: string;
  type?: string;
}

export function normalizedFileContentType(value: string | undefined) {
  return (value ?? "").split(";", 1)[0].trim().toLowerCase();
}

const SANDBOXED_HTML_EXTENSION_RE = new RegExp(`\\.(?:${sandboxedHtmlExtensions.join("|")})$`, "i");

/** An HTML file the server keeps to open only in a frame with no origin
 * (docs/app-platform.md 14); a build without the app platform refuses it. */
export function isSandboxedHtmlFile(file: FileTypeDescriptor) {
  const contentType = normalizedFileContentType(file.type);
  return SANDBOXED_HTML_EXTENSION_RE.test((file.name ?? "").trim())
    && (contentType === sandboxedHtmlContentType || contentType === "" || contentType === "application/octet-stream");
}

/** A file the upload refuses: one that would run as a web page or script. */
export function isActiveContentFile(file: FileTypeDescriptor) {
  if (isSandboxedHtmlFile(file)) return false;
  const contentType = normalizedFileContentType(file.type);
  if (ACTIVE_CONTENT_MIME_TYPES.has(contentType) || contentType.endsWith("+xml")) {
    return true;
  }
  return ACTIVE_CONTENT_EXTENSION_RE.test((file.name ?? "").trim());
}

class UnsafeWorkspaceFileError extends Error {
  readonly code = "unsafe-active-content";

  constructor() {
    super("Active web content files cannot be uploaded.");
    this.name = "UnsafeWorkspaceFileError";
  }
}

export function assertSafeWorkspaceUpload(file: FileTypeDescriptor) {
  if (isActiveContentFile(file)) throw new UnsafeWorkspaceFileError();
}

export function isUnsafeWorkspaceFileError(error: unknown): error is UnsafeWorkspaceFileError {
  return error instanceof UnsafeWorkspaceFileError ||
    (error instanceof Error && error.name === "UnsafeWorkspaceFileError");
}
