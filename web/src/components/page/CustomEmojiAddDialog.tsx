import { useEffect, useId, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ImageIcon, ReplaceIcon } from "@/icons/leafen";
import { loadSession } from "@/lib/authSessionStore";
import { createWorkspaceCustomEmoji, renameWorkspaceCustomEmoji, type WorkspaceCustomEmoji } from "@/lib/customEmoji";
import { useWorkspaceFileUrl } from "@/lib/fileUrls";
import { newId } from "@/lib/ids";
import { useWaitingNote } from "@/lib/pendingChoice";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { PopoverLayer } from "@/lib/popoverLayer";
import { useOptionalProductToast } from "@/lib/productToast";
import { userErrorText, writeUncertainMark } from "@/lib/settingsErrors";
import { uploadWorkspaceFile, type WorkspaceFileTarget } from "@/lib/storage";
import { useSendAgainWhenAnswered } from "@/lib/uncertainWrites";
import { uploadRefusalReason } from "@/lib/uploadRefusal";
import { nativeWorkspaceFileLocator } from "@/lib/workspaceFileTransport";
import styles from "./CustomEmojiAddDialog.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

/** A picked file's name without its extension, as Notion names a new emoji. */
export function emojiNameOfFile(file: File) {
  return Array.from(file.name.replace(/\.[^.]+$/u, "").trim()).slice(0, 100).join("");
}

/** A picked picture under "미리 보기", on white and on black, as Notion's (measured 2026-11-01). */
export function CustomEmojiPreview({ src, large, onReplace, onBroken }: { src: string; large?: boolean; onReplace?: () => void; onBroken?: () => void }) {
  const { t } = useTranslation("emojiPicker");
  return (
    <div className={styles.preview} data-large={large ? "true" : undefined} data-custom-emoji-preview>
      <div className={styles.previewLabel}>{t("custom.add.preview")}</div>
      <div className={styles.previewPair}>
        <img src={src} alt="" onError={onBroken} />
        <img src={src} alt="" />
      </div>
      {onReplace ? (
        <button type="button" className={styles.replace} onClick={onReplace}>
          <ReplaceIcon size="tiny" aria-hidden="true" />{t("custom.add.replace")}
        </button>
      ) : null}
    </div>
  );
}

/**
 * "사용자 지정 이모지 추가", as Notion's (measured 2026-11-01): hung under
 * the "+" or "추가" that opened it (over it where there is no room below), a
 * line saying everyone in the workspace can use it, "이미지 업로드" — the
 * picked picture then shows on the screen's ground first, then the other
 * (white then black in light, black then white in dark), with "바꾸기" — and the
 * emoji's name, its file's name to start with, the caret in it; "취소" at the
 * left, "저장" at the right. Saved, the emoji joins the workspace's own.
 * Given an emoji, it is "사용자 지정 이모지 편집" for it, as settings' "⋯ ›
 * 편집" opens Notion's (seen 2026-11-02): its picture, "바꾸기", its name.
 */
export function CustomEmojiAddDialog({ anchor, uploadTarget, editing, onAdded, onClose }: {
  anchor: HTMLElement;
  uploadTarget: WorkspaceFileTarget;
  editing?: WorkspaceCustomEmoji;
  onAdded: (emoji: WorkspaceCustomEmoji) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation("emojiPicker");
  const toast = useOptionalProductToast();
  const nameId = useId();
  const [file, setFile] = useState<File | null>(null);
  const [preview, setPreview] = useState("");
  const [name, setName] = useState(editing?.name ?? "");
  const current = useWorkspaceFileUrl(editing ? nativeWorkspaceFileLocator(editing.fileId) : "", ["data:image/"]);
  const shown = preview || current || "";
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  // A file no picture can be drawn from isn't kept as an emoji (seen 2026-11-05).
  const [broken, setBroken] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const nameRef = useRef<HTMLInputElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const abortRef = useRef<AbortController | null>(null);
  const sendAgain = useSendAgainWhenAnswered();
  // The name a file gave, replaced by the next file's unless it was edited.
  const fileNameRef = useRef("");
  // Closed, the focus goes back to what opened it: a settings row's "⋯",
  // whose menu row "편집" is gone by then (2026-11-07: Enter's save left it on
  // the next row's "⋯").
  const anchorRef = useRef(anchor);
  anchorRef.current = anchor;
  const place = usePopoverPlacement(ref, (size, viewport) => placeAround(popoverRectOf(anchor)!, size, viewport,
    { gap: 0, minHeight: 0 }), [anchor], undefined,
    { kind: "picker", onEscape: onClose, onOutsidePress: onClose, inside: () => [anchor], initialFocus: nameRef, returnFocusRef: anchorRef });

  useEffect(() => () => abortRef.current?.abort(), []);
  useEffect(() => {
    if (!file) {
      setPreview("");
      return;
    }
    const url = URL.createObjectURL(file);
    setPreview(url);
    return () => URL.revokeObjectURL(url);
  }, [file]);

  function chooseFile(next: File) {
    const fileName = emojiNameOfFile(next);
    const lastFileName = fileNameRef.current;
    setFile(next);
    setName((current) => !current.trim() || current === lastFileName ? fileName : current);
    fileNameRef.current = fileName;
    setError("");
    setBroken(false);
  }

  const making = useRef<{ file: File; name: string; fileId: string; requestId: string } | null>(null);
  // Something to save: a new emoji's picture and name, or an emoji's changed
  // picture or name — Notion's "저장" stays dim until then (seen 2026-11-07).
  const changed = Boolean(name.trim()) && (editing ? Boolean(file) || name.trim() !== editing.name : Boolean(file));
  // Its file goes up and the emoji is made as other writes are: ten seconds
  // without the server's answer, the window says so (lib/storage.ts waits).
  const waitingNote = useWaitingNote(saving);

  async function save() {
    const session = loadSession();
    const workspaceId = uploadTarget.workspaceId;
    const trimmed = name.trim();
    if (!changed || !session || !workspaceId || saving || broken) return;
    const controller = new AbortController();
    abortRef.current = controller;
    setSaving(true);
    setError("");
    try {
      // The same emoji asked for again (a make that went without the server's
      // answer, then pressed again) goes with the same request and file, so
      // the server, which names it by its request, makes it once
      // (lib/uncertainWrites.ts; 2026-11-06).
      const again = !editing && file && making.current?.file === file && making.current.name === trimmed ? making.current : null;
      const uploaded = again ? { fileId: again.fileId }
        : file ? await uploadWorkspaceFile(file, "icons", uploadTarget, { signal: controller.signal }) : null;
      if (!editing && file && uploaded) making.current = { file, name: trimmed, fileId: uploaded.fileId, requestId: again?.requestId ?? newId() };
      const emoji = editing ? await renameWorkspaceCustomEmoji(session, workspaceId, editing.id, {
        baseRevision: editing.revision,
        name: Array.from(trimmed).slice(0, 100).join(""),
        ...(uploaded ? { fileId: uploaded.fileId } : {}),
        signal: controller.signal,
      }) : await createWorkspaceCustomEmoji(session, workspaceId, {
        fileId: uploaded!.fileId,
        name: Array.from(trimmed).slice(0, 100).join(""),
        requestId: making.current!.requestId,
        signal: controller.signal,
      });
      if (controller.signal.aborted) return;
      // Said wherever it was added from, as Notion's (seen 2026-11-07).
      toast?.notify(t(editing ? "custom.edited" : "custom.added", { name: emoji.name }));
      onAdded(emoji);
    } catch (error) {
      if (controller.signal.aborted) return;
      // A new emoji's make that went unanswered is sent again once the
      // server answers, and the window ends as answered; meanwhile it says so.
      const waiting = !editing && sendAgain(error, () => save());
      setError(waiting ? userErrorText(error, t("custom.uploadFailed")) : uploadRefusalReason(error) ?? t("custom.uploadFailed"));
      setSaving(false);
    }
  }

  return (
    <PopoverLayer><div ref={ref} className={styles.dialog} role="dialog" aria-label={t(editing ? "custom.edit.title" : "custom.add.title")} data-custom-emoji-add
      style={popoverPlacementStyle(place)}
      onKeyDown={(event) => {
        if (event.key !== "Escape") return;
        event.preventDefault();
        event.stopPropagation();
        onClose();
      }}>
      <div className={styles.title}>{t(editing ? "custom.edit.title" : "custom.add.title")}</div>
      <p className={styles.hint}>{t("custom.add.hint")}</p>
      {shown ? (
        <CustomEmojiPreview src={shown} onReplace={() => fileRef.current?.click()} onBroken={() => {
          setBroken(true);
          setError(t("custom.add.unreadable"));
        }} />
      ) : (
        <div className={styles.drop}>
          <button type="button" className={styles.upload} onClick={() => fileRef.current?.click()} data-custom-emoji-upload>
            <ImageIcon size="regular" aria-hidden="true" />{t("custom.add.upload")}
          </button>
        </div>
      )}
      <input ref={fileRef} type="file" accept="image/*" hidden data-custom-emoji-file
        onChange={(event) => {
          const next = event.target.files?.[0];
          if (next?.type.startsWith("image/")) chooseFile(next);
          event.target.value = "";
        }} />
      <label className={styles.nameLabel} htmlFor={nameId}>{t("custom.add.name")}</label>
      <input ref={nameRef} id={nameId} className={styles.field} value={name} placeholder={t("custom.add.namePlaceholder")}
        maxLength={100} autoFocus
        onChange={(event) => setName(event.target.value)}
        onKeyDown={(event) => {
          if (!fieldEnter(event)) return;
          event.preventDefault();
          void save();
        }} />
      {error ? <div className={styles.error} role="alert" {...writeUncertainMark(error)}>{error}</div>
        : waitingNote ? <div className={styles.error} role="status" data-write-uncertain="">{waitingNote}</div> : null}
      <div className={styles.actions}>
        <button type="button" className={styles.cancel} onClick={onClose}>{t("custom.add.cancel")}</button>
        <button type="button" className={styles.save} disabled={!changed || saving || broken} aria-busy={saving}
          onClick={() => void save()}>{t("custom.add.save")}</button>
      </div>
    </div></PopoverLayer>
  );
}
