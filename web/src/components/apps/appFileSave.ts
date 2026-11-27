import type { MutableRefObject } from "react";
import { API_ROOT } from "@/apiTransport";
import { serverPath } from "@/lib/serverOrigin";
import { randomUUID, sha256Hex } from "@/lib/browserCrypto";
import { appBridgeRefusal } from "./appFrameBridge";
import { askServerForApp } from "./appAskServer";

// An app's file saved from its screen (document apps decision of 2026-10-10):
// what a file's window keeps for it, and the save the frame's answerer makes.

// An app's bytes become a file of a workspace through the resumable upload,
// which an app's token may use: prepared, sent in parts with their digests,
// completed.
async function uploadAppFileBytes(bearer: string, workspaceId: string, bytes: ArrayBuffer, name: string, contentType: string):
  Promise<{ id: string } | { code: string }> {
  const call = async <T>(path: string, init: RequestInit): Promise<{ status: number; body: T | null }> => {
    // Asked again while the server or a limit refuses it for now (appAskServer.ts).
    const response = await askServerForApp(() => fetch(serverPath(`${API_ROOT}${path}`), { ...init, headers: { authorization: `Bearer ${bearer}`, ...init.headers } }));
    return { status: response.status, body: await response.json().catch(() => null) as T | null };
  };
  type Failure = { error?: { code?: string } };
  // One request id names the upload from preparing it to completing it.
  const requestId = randomUUID();
  const prepared = await call<{ uploadId: string; partBytes: number; nextPart: number } & Failure>("/files/uploads", {
    method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ requestId, workspaceId, name, size: bytes.byteLength, contentType }),
  });
  if (prepared.status >= 300 || !prepared.body?.uploadId) return { code: prepared.body?.error?.code ?? `http_${prepared.status}` };
  const { uploadId, partBytes, nextPart } = prepared.body;
  for (let offset = 0, part = nextPart; ; offset += partBytes, part += 1) {
    const chunk = bytes.slice(offset, Math.min(offset + partBytes, bytes.byteLength));
    const digest = await sha256Hex(chunk);
    const sent = await call<Failure>(`/files/uploads/${encodeURIComponent(uploadId)}/parts/${part}`, {
      method: "PUT", headers: { "content-type": "application/octet-stream", "x-leafen-part-sha256": digest }, body: chunk,
    });
    if (sent.status >= 300) return { code: sent.body?.error?.code ?? `http_${sent.status}` };
    if (offset + partBytes >= bytes.byteLength) break;
  }
  const completed = await call<{ file?: { id: string } } & Failure>(`/files/uploads/${encodeURIComponent(uploadId)}/complete`, {
    method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ requestId }),
  });
  if (completed.status >= 300 || !completed.body?.file?.id) return { code: completed.body?.error?.code ?? `http_${completed.status}` };
  return { id: completed.body.file.id };
}

/** A newer version a save met, and who saved it. */
export type AppFileConflict = { fileId: string; name: string; size: number; version: string; savedBy?: string };
/** A file kept as a new block beside the one opened. */
export type AppFileCopy = { blockId: string; fileId: string; name: string; size: number; version: string };
/** How the person settles a save that met a newer version, asked by Leafen outside the app. */
export type AppFileConflictChoice = "overwrite" | "copy" | "latest" | "cancel";
/** What a screen tried to save, as a file the person may still download. */
export type AppFileEdits = { bytes: ArrayBuffer; name: string; contentType: string };

/** The file an opener screen may save, kept by the place that opened it. */
export type AppFileSaveTarget = {
  workspaceId: string; pageId: string; blockId: string; name: string; contentType: string;
  /** The version this person opened or last saved. */
  baseVersion: string;
  /** The app has told what it has unsaved (setUnsaved), and it is nothing. */
  readonly nothingToSave: boolean;
  /** The name a copy takes. */
  copyName: string;
  /** The name a copy takes when that one is in use on the page, "{n}" for its number. */
  copyNameNumbered: string;
  choose: (found: AppFileConflict) => Promise<AppFileConflictChoice>;
  /** The copy kept is the file in view from now on. */
  kept: (copy: AppFileCopy) => void;
  /** The save renamed the file in view. */
  renamed: (name: string) => void;
  /** The block took the new file: the file in view from now on. */
  saved: (file: { fileId: string; size: number }) => void;
  /** Opens the latest version in place of this one. */
  reopen: (latest: AppFileConflict) => void;
  /** A save that didn't go through, other than over a newer version: what it tried to save. */
  notSaved: (edits: AppFileEdits) => void;
  /** The app is no longer on the server for this screen, which stays: saving here has ended. */
  appGone: () => void;
};

/** Saves what a file opener's screen sent, as the person through the app, with the frame's token. */
export async function saveAppFile(params: Record<string, unknown>, fileSave: MutableRefObject<AppFileSaveTarget | null> | undefined,
  token: () => Promise<string>): Promise<unknown> {
  // As the person through the app: the bytes become a file in the page's
  // workspace, which then takes the block's place if the block still
  // holds the version opened (document apps decision of 2026-10-10, 3).
  // Over a newer one, the person chooses here, outside the app (11).
  const target = fileSave?.current;
  if (!target) throw appBridgeRefusal("unknownCall");
  const bytes = params.bytes;
  if (!(bytes instanceof ArrayBuffer)) throw appBridgeRefusal("invalidCall");
  // An app that tells what it has unsaved and has nothing keeps the version
  // it has: a new version of the same file met whoever had it open with
  // "someone saved first" (2026-11-07). A change of format still saves.
  if (target.nothingToSave && params.name === undefined) return { ok: true, version: target.baseVersion };
  const bearer = await token();
  if (!bearer) throw appBridgeRefusal("noDataAccess");
  const edits = { bytes, name: String(params.name ?? target.name), contentType: String(params.contentType ?? target.contentType) };
  const uploaded = await uploadAppFileBytes(bearer, target.workspaceId, bytes, edits.name, edits.contentType);
  if ("code" in uploaded) {
    target.notSaved(edits);
    return { ok: false, code: uploaded.code };
  }
  const file = uploaded;
  type Answer = { blockId?: string; version?: string; fileId?: string; name?: string; size?: number; error?: { code?: string };
    current?: AppFileConflict } | null;
  const send = async (path: string, method: string, body: unknown) => {
    const response = await askServerForApp(() => fetch(serverPath(`${API_ROOT}/pages/${encodeURIComponent(target.pageId)}/blocks/${encodeURIComponent(target.blockId)}/${path}`), {
      method, headers: { authorization: `Bearer ${bearer}`, "content-type": "application/json" }, body: JSON.stringify(body),
    }));
    return { ok: response.ok, status: response.status, answer: await response.json().catch(() => null) as Answer };
  };
  let base = target.baseVersion;
  for (;;) {
    const saved = await send("file", "PUT", { fileId: file.id, baseVersion: base });
    const answer = saved.answer;
    if (saved.ok) {
      // The place may have drawn again meanwhile; its current target takes the version.
      for (const kept of [target, fileSave?.current]) if (kept && answer?.version) kept.baseVersion = answer.version;
      // A format's change renames the file: its next save keeps the name.
      if (params.name) target.renamed(String(params.name));
      target.saved({ fileId: answer?.fileId ?? file.id, size: bytes.byteLength });
      return { ok: true, version: answer?.version, fileId: answer?.fileId };
    }
    const code = answer?.error?.code ?? `http_${saved.status}`;
    const found = answer?.current;
    if (code !== "LEAFEN_FILE_VERSION_CONFLICT" || !found) {
      target.notSaved(edits);
      return { ok: false, code, conflict: found };
    }
    const choice = await target.choose(found);
    if (choice === "overwrite") { base = found.version; continue; }
    if (choice === "copy") {
      const copied = await send("file-copy", "POST", { fileId: file.id, name: target.copyName, numberedName: target.copyNameNumbered });
      const copy = copied.answer;
      if (!copied.ok || !copy?.blockId || !copy.version) return { ok: false, code: copy?.error?.code ?? `http_${copied.status}`, conflict: found };
      target.kept({ blockId: copy.blockId, fileId: copy.fileId ?? file.id, name: copy.name ?? target.copyName, size: copy.size ?? bytes.byteLength,
        version: copy.version });
      return { ok: true, version: copy.version, fileId: copy.fileId, copied: { blockId: copy.blockId, name: copy.name ?? target.copyName } };
    }
    if (choice === "latest") target.reopen(found);
    return { ok: false, code, conflict: found };
  }
}
