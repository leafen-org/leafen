import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { api } from "@/api";
import type { GraphBlock } from "@/contentGraph";
import { fileTransferBounds } from "@/fileTransferWire.generated";
import { isUnsafeWorkspaceFileError } from "@/lib/fileSecurity";
import { uploadWorkspaceFile, type UploadedWorkspaceFile } from "@/lib/storage";
import { blockItemText } from "./blockItemLabels";
import {
  blockUploadErrorMessage,
  blockUploadScope,
  fileBlockType,
  insertedFileBlockType,
  uploadBlockType,
  type BlockUploadProgress,
  type DroppedFileBlockType,
} from "./fileDrop";
import { editorBlockChildren } from "./editorBlockIndex";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { keepUploadedFile } from "./mediaBlockUrl";
import type { Session } from "@/types";

// An image, video, audio, PDF or file block holds an uploaded file as its
// managed content (file ID, name, size, type and digest). Files dropped or
// pasted into the editor first become empty blocks of their type; each new
// block takes its file from here and uploads it with its own progress.

const pendingBlockFiles = new Map<string, { file: File; batch?: BlockFileBatch }>();

function queueBlockFile(blockId: string, file: File, batch?: BlockFileBatch) {
  pendingBlockFiles.set(blockId, { file, batch });
}

/**
 * A change that comes after an earlier one of the page's history: one ⌘Z
 * with the step it gave back before while that is the latest, a step of its
 * own otherwise; it gives back the step it went in (the editor's `follow`).
 */
export type FollowLater = (change: () => void, step: unknown) => unknown;

/**
 * The files one drop or paste brought: as each finishes uploading its block
 * takes it in the same step as the others, so one ⌘Z takes them all back
 * to empty file blocks and the next takes the blocks away, as Notion's
 * (seen 2026-11-15 in two tabs, three files dropped at once).
 */
type BlockFileBatch = { follow: FollowLater; step: unknown };

// However many files a drop brings, they upload a few at a time in the
// order their blocks appear; the others wait their turn.
const BLOCK_UPLOADS_AT_ONCE = 3;
let blockUploadsRunning = 0;
const blockUploadsWaiting: Array<() => void> = [];

/** Waits for a turn to upload; the function it gives back ends the turn. */
function blockUploadTurn(signal: AbortSignal): Promise<() => void> {
  return new Promise((resolve) => {
    const begin = () => {
      blockUploadsRunning += 1;
      let ended = false;
      resolve(() => {
        if (ended) return;
        ended = true;
        blockUploadsRunning -= 1;
        blockUploadsWaiting.shift()?.();
      });
    };
    if (blockUploadsRunning < BLOCK_UPLOADS_AT_ONCE) {
      begin();
      return;
    }
    blockUploadsWaiting.push(begin);
    signal.addEventListener("abort", () => {
      const waiting = blockUploadsWaiting.indexOf(begin);
      if (waiting >= 0) blockUploadsWaiting.splice(waiting, 1);
    }, { once: true });
  });
}

/**
 * A file on its way to a block. It belongs to the tab, not to what shows the
 * block: going to another page leaves it uploading, as Notion's (seen
 * 2026-11-05), and the block takes it when the page shows it again.
 */
export type BlockUploadJob = {
  file: File;
  progress: BlockUploadProgress;
  controller: AbortController;
  /** Where it was started, and what the block held then: a later change there wins. */
  pageId: string;
  workspaceId: string;
  sessionToken: string | undefined;
  /** The sign-in it went up with, which puts it in the block when no screen takes it. */
  session?: Session;
  blockType: string;
  source: string;
  uploaded?: UploadedWorkspaceFile;
  /** The drop or paste it came with, whose files are taken in one step. */
  batch?: BlockFileBatch;
  error?: string;
  /** The refusal can pass later (a storage limit, an unreachable server), unlike a file too large or unsafe. */
  retryable?: boolean;
};

const jobs = new Map<string, BlockUploadJob>();
// How long a finished upload waits for a screen showing its block to take it.
const UNTAKEN_UPLOAD_MS = 1_500;
const jobListeners = new Set<() => void>();

function publishJob(blockId: string, job: BlockUploadJob | null) {
  if (job) jobs.set(blockId, job);
  else jobs.delete(blockId);
  jobListeners.forEach((listener) => listener());
  syncLeaveWarning();
}

function askBeforeLeaving(event: BeforeUnloadEvent) {
  event.preventDefault();
  event.returnValue = "";
}
let warningOnLeave = false;
/** Closing the tab would lose a file still on its way. */
function syncLeaveWarning() {
  const running = [...jobs.values()].some((job) => !job.uploaded && !job.error);
  if (running === warningOnLeave) return;
  warningOnLeave = running;
  if (running) window.addEventListener("beforeunload", askBeforeLeaving);
  else window.removeEventListener("beforeunload", askBeforeLeaving);
}

export function useBlockUploadJob(blockId: string) {
  return useSyncExternalStore((listener) => {
    jobListeners.add(listener);
    return () => { jobListeners.delete(listener); };
  }, () => jobs.get(blockId) ?? null);
}

/** A job still sending its file. */
export function blockUploadRunning(job: BlockUploadJob | null) {
  return Boolean(job && !job.uploaded && !job.error);
}

/** The block takes its finished file wherever it is shown next, and only if nothing changed it meanwhile. */
export function useTakeFinishedBlockUpload(adapter: NativeEditorGraphAdapter, block: GraphBlock, job: BlockUploadJob | null) {
  const uploaded = job?.uploaded;
  useEffect(() => {
    if (!job || !uploaded) return;
    const target = adapter.graph.blocks.find((candidate) => candidate.id === block.id);
    if (target && !adapter.readOnly && adapter.pageId === job.pageId && adapter.workspaceId === job.workspaceId
      && adapter.session?.token === job.sessionToken && target.type === job.blockType
      && String(target.content.fileId ?? target.content.url ?? "") === job.source) {
      keepUploadedFile(uploaded.fileId, job.file);
      let taken = false;
      const take = () => { taken = true; adapter.replaceBlocks([{ ...target, content: managedMediaContent(uploaded, target) }]); };
      if (job.batch) job.batch.step = job.batch.follow(take, job.batch.step) ?? job.batch.step;
      // An editor gone meanwhile takes it all the same, as a step of its own.
      if (!taken) take();
    }
    publishJob(block.id, null);
    // The file is taken once, whichever draw of the page sees it first.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [uploaded, block.id]);
}

/** A file block takes any file; a media block or an embed, the files whose block it is. */
function mediaFileAccepted(kind: DroppedFileBlockType | "embed", file: File) {
  return kind === "file" || uploadBlockType(file) === kind;
}

// How an image is shown, which it keeps when its file changes.
const IMAGE_PRESENTATION_FIELDS = ["altText", "imageLink"] as const;

/**
 * The content a block takes from its uploaded file: the file itself, the
 * block's caption and, for an image, how the image is shown.
 */
function managedMediaContent(uploaded: UploadedWorkspaceFile, block: GraphBlock) {
  const content: Record<string, unknown> = {
    fileId: uploaded.fileId,
    name: uploaded.name,
    size: uploaded.size ?? 0,
    contentType: uploaded.type || "application/octet-stream",
    sha256: uploaded.sha256 ?? "",
  };
  const caption = block.content.caption;
  if (Array.isArray(caption) && caption.length > 0) content.caption = caption;
  for (const field of ["captionVisible", "width", "sizedWidth", "align", "color"] as const) {
    if (block.content[field] !== undefined) content[field] = block.content[field];
  }
  if (block.type === "image") {
    for (const field of IMAGE_PRESENTATION_FIELDS) {
      if (block.content[field] !== undefined && block.content[field] !== "") content[field] = block.content[field];
    }
  }
  return content;
}

function uploadErrorIdentity(error: unknown, file: File) {
  return isUnsafeWorkspaceFileError(error)
    ? "unsafe"
    : file.size > fileTransferBounds.maxFileBytes ? "too_large" : "unknown";
}

/**
 * Sends a file into a block, which takes it when it is up wherever the page
 * shows it (useTakeFinishedBlockUpload): whether it was sent.
 */
export function sendBlockFile(current: NativeEditorGraphAdapter, blockId: string, kind: DroppedFileBlockType | "embed", file: File,
  endTurn = () => {}, batch?: BlockFileBatch) {
  const fileName = file.name || blockItemText("file.label");
  const before = current.graph.blocks.find(candidate => candidate.id === blockId);
  if (!before || current.readOnly) { endTurn(); return false; }
  jobs.get(blockId)?.controller.abort();
  const controller = new AbortController();
  const running: BlockUploadJob = {
    file, controller, progress: { phase: "preparing", percent: 0, fileName },
    pageId: current.pageId, workspaceId: current.workspaceId, sessionToken: current.session?.token, session: current.session ?? undefined,
    blockType: before.type, source: String(before.content.fileId ?? before.content.url ?? ""), batch,
  };
  const update = (next: Partial<BlockUploadJob>) => {
    if (jobs.get(blockId)?.controller !== controller) return;
    publishJob(blockId, { ...jobs.get(blockId)!, ...next });
  };
  publishJob(blockId, running);
  void uploadWorkspaceFile(file, blockUploadScope(kind === "embed" ? "file" : kind), {
    workspaceId: running.workspaceId, pageId: running.pageId, blockId,
  }, {
    signal: controller.signal,
    onProgress: (next) => update({ progress: { ...next, fileName } }),
  }).then((uploaded) => {
    pendingBlockFiles.delete(blockId);
    update({ uploaded });
    // No screen showing the block took it — the person went to another
    // page — so it goes in the block on the server, where it was sent for
    // (2026-11-07: it waited for the page to show again, and a tab read
    // again meanwhile left the block empty for good).
    window.setTimeout(() => {
      const left = jobs.get(blockId);
      if (left?.controller !== controller || !left.uploaded || !running.session || running.source.trim()) return;
      void api.attachPageBlockFile(running.session, running.pageId, blockId, uploaded.fileId)
        .then(() => keepUploadedFile(uploaded.fileId, file), () => undefined)
        .finally(() => { if (jobs.get(blockId)?.controller === controller) publishJob(blockId, null); });
    }, UNTAKEN_UPLOAD_MS);
  }).catch((failure: unknown) => {
    if (controller.signal.aborted) return;
    pendingBlockFiles.delete(blockId);
    const identity = uploadErrorIdentity(failure, file);
    update({ error: blockUploadErrorMessage(failure, file.name, identity), retryable: identity === "unknown" });
  }).finally(endTurn);
  return true;
}

/**
 * Uploads a chosen, dropped or pasted file into a media block; the block takes
 * it through the editor that is current when the upload ends, so edits made
 * meanwhile are kept.
 */
export function useMediaBlockUpload(
  adapter: NativeEditorGraphAdapter,
  block: GraphBlock,
  kind: DroppedFileBlockType | "embed",
) {
  const latest = useRef({ adapter, block });
  latest.current = { adapter, block };
  const job = useBlockUploadJob(block.id);
  const [localError, setLocalError] = useState("");

  function start(file: File, endTurn = () => {}, batch?: BlockFileBatch) {
    if (sendBlockFile(latest.current.adapter, block.id, kind, file, endTurn, batch)) setLocalError("");
  }

  function upload(file: File | undefined) {
    if (!file) return;
    if (mediaFileAccepted(kind, file)) {
      start(file);
      return;
    }
    // Another kind of file makes the block that file's own and uploads there,
    // as Notion's (seen 2026-11-07: a picture given to an audio block or a
    // PDF block's replacement became a picture, a PDF given to a video block
    // a PDF, a document given to a PDF block an embed of it).
    const { adapter: current } = latest.current;
    const before = current.graph.blocks.find(candidate => candidate.id === block.id);
    if (!before || current.readOnly) return;
    jobs.get(block.id)?.controller.abort();
    setLocalError("");
    queueBlockFile(block.id, file);
    const caption = Array.isArray(before.content.caption) && before.content.caption.length > 0 ? { caption: before.content.caption } : {};
    current.replaceBlocks([{ ...before, type: uploadBlockType(file), content: caption }]);
  }

  // A block made for a dropped or pasted file uploads it when it appears.
  useEffect(() => {
    const pending = pendingBlockFiles.get(block.id);
    if (!pending || jobs.has(block.id)) return;
    const turn = new AbortController();
    void blockUploadTurn(turn.signal).then((end) => start(pending.file, end, pending.batch));
    // The upload belongs to the block, not to each render's editor.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [block.id]);

  const running = blockUploadRunning(job);
  // A refused upload can be sent again as it was — once the organization's
  // storage is freed or its limit raised, the same file goes through.
  const failed = !running && !localError && job?.error && job.retryable ? job : null;
  return { progress: running ? job!.progress : null, file: running ? job!.file : null, error: localError || job?.error || "",
    setError: setLocalError, upload, retry: failed ? () => start(failed.file) : undefined };
}

/** Where files go: after a block (null: the page's end), or before or after the block they were let go over (fileDropMode). */
export type FilePlace = string | null | { id: string; mode: string };

/**
 * Turns dropped or pasted files into blocks at a place, one block per file
 * in order, however many; each new block uploads its own file. Each file's
 * block is Notion's for the way it came (insertedFileBlockType): a PDF a file
 * row, not a PDF block, a dropped Word file an embed of it. Before a block is
 * after the one before it, or first where it stands.
 */
export function insertEditorFiles(adapter: NativeEditorGraphAdapter, place: FilePlace, files: readonly File[],
  how: "dropped" | "pasted" = "dropped", follow?: FollowLater) {
  const ids: string[] = [];
  if (adapter.readOnly) return ids;
  const batch = follow ? { follow, step: null } : undefined;
  const before = place && typeof place === "object" && place.mode === "before"
    ? adapter.graph.blocks.find((block) => block.id === place.id) : undefined;
  const siblings = before ? editorBlockChildren(adapter.graph, before.parentId ?? null) : [];
  const at = before ? siblings.findIndex((block) => block.id === before.id) : -1;
  let previous = place && typeof place === "object" ? (at > 0 ? siblings[at - 1].id : place.id) : place;
  for (const file of files.filter((candidate) => candidate.size > 0)) {
    const type = insertedFileBlockType(file, how);
    const inserted = at === 0 && !ids.length ? adapter.insertChild(before!.parentId ?? null, type, {}, null) : adapter.insertAfter(previous, type, {});
    if (!inserted) break;
    queueBlockFile(inserted.id, file, batch);
    previous = inserted.id;
    ids.push(inserted.id);
  }
  return ids;
}

/** Whether a block holds a file that a file dropped on it takes the place of (dropOnFileBlock). */
export function takesDroppedFile(block: GraphBlock | undefined): block is GraphBlock & { type: "file" | "image" } {
  return !!block && (block.type === "file" || block.type === "image")
    && String(block.content.fileId ?? block.content.url ?? "").trim() !== "";
}

/**
 * Files dropped on a block that holds a file, as Notion's (seen 2026-11-15 by
 * a script's drop in two tabs): a file row takes the first of them as its
 * file, whatever it is, and a picture a first file that is a picture; the
 * rest, and a file that is no picture dropped on one, go nowhere — no block
 * is made. False for any other block, after which the files make blocks.
 */
export function dropOnFileBlock(adapter: NativeEditorGraphAdapter, blockId: string | null, files: readonly File[]) {
  const block = blockId ? adapter.graph.blocks.find((candidate) => candidate.id === blockId) : undefined;
  if (!takesDroppedFile(block)) return false;
  const first = files[0];
  if (first && (block.type === "file" || fileBlockType(first) === "image")) sendBlockFile(adapter, block.id, block.type, first);
  return true;
}

/** The page's history a drop's blocks are made in and chosen by, as a paste's: the editor's controller. */
export type FileInsertionHistory = { run(change: () => void): void; choosePasted(ids: readonly string[]): void; follow: FollowLater };

/**
 * Dropped files become blocks in one step of the page's history, and the
 * blocks they made are chosen with no caret, as a paste's (choosePasted) —
 * as Notion's (seen 2026-11-13 by hand from Finder, a picture alone and five
 * files at once, and 2026-11-15 by a script's drop in two tabs: the new
 * blocks washed blue, ready for Delete, a drag or their menu).
 */
export function useEditorFileInsertion(adapter: NativeEditorGraphAdapter, history?: Partial<FileInsertionHistory>,
  setSelection?: (selection: ReadonlySet<string>) => void) {
  return (place: FilePlace, files: readonly File[]) => {
    let made: readonly string[] = [];
    const insert = () => { made = insertEditorFiles(adapter, place, files, "dropped", history?.follow); };
    if (history?.run) history.run(insert); else insert();
    if (!made.length) return made;
    if (history?.choosePasted) history.choosePasted(made); else setSelection?.(new Set(made));
    return made;
  };
}
