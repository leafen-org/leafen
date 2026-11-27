import type { TFunction } from "i18next";
import { isDiagnosticReference } from "@/lib/diagnosticTrace";
import { nativeNotionImportErrorCodes, type NativeNotionImportJob } from "@/notionImportWire.generated";

// Why a Notion import or migration stopped, from the code its job kept, so the
// window says what happened and what to do instead of only that it stopped
// (2026-10-16). An unknown code reads as Leafen's own stop.
const reasonKeys: Record<string, string> = {
  LEAFEN_NOTION_IMPORT_PROVIDER_FAILED: "provider",
  LEAFEN_NOTION_IMPORT_PROVIDER_LIMITED: "provider",
  LEAFEN_NOTION_IMPORT_PROVIDER_UNREACHABLE: "provider",
  LEAFEN_NOTION_IMPORT_TOKEN_REJECTED: "tokenRejected",
  LEAFEN_NOTION_IMPORT_CONNECTION_FAILED: "connection",
  LEAFEN_NOTION_IMPORT_CONNECTION_NOT_FOUND: "connection",
  LEAFEN_NOTION_IMPORT_SCOPE_FAILED: "scope",
  LEAFEN_NOTION_IMPORT_FILE_COPY_REQUIRED: "file",
  LEAFEN_NOTION_IMPORT_FILE_PERMANENT: "file",
  LEAFEN_NOTION_MIGRATION_PLAN_CONFLICT: "planChanged",
  LEAFEN_NOTION_MIGRATION_PLAN_BLOCKED: "planBlocked",
  LEAFEN_NOTION_MIGRATION_INVALID: "planBlocked",
  LEAFEN_IMPORTED_PEOPLE_MAPPING_FAILED: "people",
  LEAFEN_NOTION_IMPORT_CREATE_FAILED: "create",
  LEAFEN_NOTION_MIGRATION_FAILED: "create",
};

// What Notion's migration blames on Notion — its answers, its token, the
// connection, what it shares — another tool's reads as Leafen's own stop:
// Leafen read that tool's bundle itself.
const notionSideReasons = new Set(["provider", "tokenRejected", "connection", "scope"]);

// Leafen's own stops: no one foresaw them, so the reference the server
// logged the failure under shows once under what they say, as every job
// card's failure does (productErrorPresentation's referenceShown,
// 2026-11-14). A refusal explained — Notion's, the plan's, a file's — reads
// as its reason alone.
const leafenSideReasons = new Set(["leafen", "create", "people"]);

function failureReason(code: string | undefined, source: string) {
  const reason = reasonKeys[code ?? ""] ?? "leafen";
  return source && notionSideReasons.has(reason) ? "leafen" : reason;
}

/** Another tool's migration (`source`, the tool's name) says its own files
 *  and people where Notion's says Notion's, and Notion's side as Leafen's
 *  own stop; the agent is told the same (notionImportFailureReason). */
export function notionImportFailureText(t: TFunction, code: string | undefined, source = ""): string {
  const reason = failureReason(code, source);
  if (source && (reason === "file" || reason === "people")) return t(`importDialog:tools.failure.${reason}`, { name: source });
  return t(`importDialog:migration.failure.${reason}`);
}

/** A stopped run as its card says it: what happened, and the reference line under it for Leafen's own stop, else "". */
export function notionImportFailure(
  t: TFunction,
  job: Pick<NativeNotionImportJob, "errorCode" | "diagnosticReference"> | null | undefined,
  source = "",
): { text: string; reference: string } {
  const text = notionImportFailureText(t, job?.errorCode, source);
  const reference = job?.diagnosticReference;
  const shown = leafenSideReasons.has(failureReason(job?.errorCode, source)) && isDiagnosticReference(reference);
  return { text, reference: shown ? t("importDialog:archive.diagnosticId", { id: reference }) : "" };
}

// What a request to Notion that failed says, by how it failed (the server's
// notionProviderFailureOf): only no way through to Notion asks to check the
// internet (2026-11-13: Notion asking to wait read as the server off it).
const providerFailureKeys: Record<string, string> = {
  [nativeNotionImportErrorCodes.providerLimited]: "importDialog:notionLimited",
  [nativeNotionImportErrorCodes.providerUnreachable]: "importDialog:notionUnreachable",
  [nativeNotionImportErrorCodes.providerFailed]: "importDialog:notionFailed",
};

/**
 * Why a request to Notion failed: Notion refusing the token (one just typed,
 * or a saved connection's), asking to wait, out of reach or not answering,
 * apart from Leafen's own failure (2026-10-25: a made-up token read only
 * "Notion 연결을 저장하지 못했습니다").
 */
export function notionRequestFailureText(t: TFunction, error: unknown, otherwise: string, saved = false): string {
  if (notionTokenRejected(error)) return t(saved ? "importDialog:savedTokenRejected" : "importDialog:tokenRejected");
  const key = providerFailureKeys[String((error as { code?: unknown } | null)?.code ?? "")];
  return key ? t(key) : otherwise;
}

/** Whether a request to Notion failed because Notion refuses the token, apart from Notion not answering. */
export function notionTokenRejected(error: unknown): boolean {
  return (error as { code?: unknown } | null)?.code === nativeNotionImportErrorCodes.tokenRejected;
}

/**
 * A Notion connection's name as the screens say it: Notion's own (its
 * workspace), or the person's words for a connection that has none yet — the
 * server renames one still called "Leafen Notion import", the name every
 * connection had before 2026-10-26, when Notion answers.
 */
export function notionConnectionName(t: TFunction, label: string | undefined): string {
  const name = label?.trim() ?? "";
  return name && name !== "Leafen Notion import" ? name : t("importDialog:notionConnectionDefault");
}
