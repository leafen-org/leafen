import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { i18next } from "@/i18n";
import { newId } from "@/lib/ids";
import { uploadWorkspaceFile, type UploadProgress } from "@/lib/storage";
import { uploadRefusalReason } from "@/lib/uploadRefusal";

/** A comment holds up to three files, as Notion's comments do (2026-09-30). */
const COMMENT_ATTACHMENT_LIMIT = 3;

export type CommentDraftAttachment = {
  key: string;
  name: string;
  size: number;
  contentType: string;
  /** Set once the file is in the workspace. */
  fileId?: string;
  /** The server hasn't answered its upload for a while; it goes on once it answers (lib/storage.ts). */
  waiting?: boolean;
};

export type CommentAttachmentDraft = ReturnType<typeof useCommentAttachmentDraft>;

/**
 * The files a comment being written holds, as Notion's "파일 첨부": each goes
 * up to the workspace as it is picked, and the comment names the ones that
 * are up. Without a workspace, files can't be attached.
 */
export function useCommentAttachmentDraft({ workspaceId, pageId, held = 0, onError }: {
  workspaceId?: string;
  pageId: string;
  /** Files the comment already holds, as one being edited: they take room. */
  held?: number;
  onError?: (message: string) => void;
}) {
  const [items, setItems] = useState<CommentDraftAttachment[]>([]);
  const count = useRef(0);
  const uploads = useRef(new Map<string, AbortController>());
  const report = useRef(onError);
  report.current = onError;

  useEffect(() => {
    const running = uploads.current;
    return () => {
      for (const controller of running.values()) controller.abort();
      running.clear();
    };
  }, []);

  const drop = useCallback((key: string) => {
    uploads.current.get(key)?.abort();
    uploads.current.delete(key);
    setItems((current) => {
      const next = current.filter((item) => item.key !== key);
      count.current = next.length;
      return next;
    });
  }, []);

  const add = useCallback((files: readonly File[]) => {
    if (!workspaceId) return;
    const room = COMMENT_ATTACHMENT_LIMIT - held - count.current;
    if (files.length > room) report.current?.(i18next.t("commentsPanel:attachments.limit", { count: COMMENT_ATTACHMENT_LIMIT }));
    const picked = files.slice(0, Math.max(0, room)).map((file) => ({
      file,
      item: { key: newId(), name: file.name, size: file.size, contentType: file.type } satisfies CommentDraftAttachment,
    }));
    if (picked.length === 0) return;
    count.current += picked.length;
    setItems((current) => [...current, ...picked.map(({ item }) => item)]);
    for (const { file, item } of picked) {
      const controller = new AbortController();
      uploads.current.set(item.key, controller);
      let waiting = false;
      const onProgress = (progress: UploadProgress) => {
        if (Boolean(progress.waiting) === waiting || controller.signal.aborted) return;
        waiting = Boolean(progress.waiting);
        setItems((current) => current.map((candidate) => candidate.key === item.key ? { ...candidate, waiting } : candidate));
      };
      void uploadWorkspaceFile(file, "comments", { workspaceId, pageId }, { signal: controller.signal, onProgress }).then((uploaded) => {
        if (controller.signal.aborted) return;
        uploads.current.delete(item.key);
        setItems((current) => current.map((candidate) => candidate.key === item.key
          ? { ...candidate, fileId: uploaded.fileId, name: uploaded.name || candidate.name, waiting: false }
          : candidate));
      }, (error: unknown) => {
        if (controller.signal.aborted) return;
        drop(item.key);
        report.current?.(uploadRefusalReason(error) ?? i18next.t("commentsPanel:attachments.uploadFailed", { name: item.name }));
      });
    }
  }, [drop, held, pageId, workspaceId]);

  const clear = useCallback(() => {
    for (const controller of uploads.current.values()) controller.abort();
    uploads.current.clear();
    count.current = 0;
    // Holding none, it stays as it was (useMemo below).
    setItems((current) => current.length ? [] : current);
  }, []);

  // The same object while nothing in it changes: every thread's reply box
  // holds the one draft, and a thread's card is drawn again only when what
  // it is given changes (CommentThreadCard).
  return useMemo(() => ({
    enabled: Boolean(workspaceId),
    items,
    add,
    remove: drop,
    clear,
    /** The workspace files of the files that are up. */
    fileIds: items.flatMap((item) => (item.fileId ? [item.fileId] : [])),
    uploading: items.some((item) => !item.fileId),
    full: held + items.length >= COMMENT_ATTACHMENT_LIMIT,
  }), [add, clear, drop, held, items, workspaceId]);
}
