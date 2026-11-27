import { useEffect, useId, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ImageIcon } from "@/icons/leafen";
import { loadSession } from "@/lib/authSessionStore";
import { createWorkspaceCustomEmoji, type WorkspaceCustomEmoji } from "@/lib/customEmoji";
import { newId } from "@/lib/ids";
import { primaryKeyName } from "@/lib/keyboardShortcuts";
import { useWaitingNote } from "@/lib/pendingChoice";
import { uploadWorkspaceFile, type WorkspaceFileTarget } from "@/lib/storage";
import { uploadRefusalReason } from "@/lib/uploadRefusal";
import { CustomEmojiPreview, emojiNameOfFile } from "./CustomEmojiAddDialog";
import { safeExternalPresentationImage } from "./pagePresentation";
import { useWorkspaceCustomEmojis } from "./useWorkspaceCustomEmojis";
import dialog from "./CustomEmojiAddDialog.module.css";
import styles from "./PageIconPicker.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";
import { focusOpenedField } from "@/components/editor/openedFieldCaret";

type Picked = { file: File; url: string } | { link: string; url: string };

/**
 * The icon picker's "업로드" tab, as Notion's (measured 2026-11-01):
 * "이미지 업로드" with a line offering ⌘+V to paste a picture or a link,
 * "취소"·"저장". A picked or pasted picture shows on white and on black, with
 * "워크스페이스 이모지 라이브러리에 추가" (off to start; on, it asks the
 * emoji's name, the file's to start with) and "뒤로"·"저장". Saved, the
 * picture is the icon — as a new workspace emoji where that was ticked.
 */
export function PageIconUploadPanel({ uploadTarget, canUpload, onPickImage, onPickCustomEmoji, onAddedToLibrary, onCancel }: {
  uploadTarget?: WorkspaceFileTarget;
  canUpload: boolean;
  onPickImage: (value: string) => void;
  onPickCustomEmoji?: (emoji: WorkspaceCustomEmoji) => void;
  onAddedToLibrary: (emoji: WorkspaceCustomEmoji) => void;
  onCancel: () => void;
}) {
  const { t } = useTranslation("emojiPicker");
  const checkId = useId();
  const nameId = useId();
  const [picked, setPicked] = useState<Picked | null>(null);
  const [toLibrary, setToLibrary] = useState(false);
  const [name, setName] = useState("");
  const [saving, setSaving] = useState(false);
  // Ten seconds without the server's answer, the panel says so (lib/storage.ts waits).
  const waitingNote = useWaitingNote(saving);
  const [error, setError] = useState("");
  const fileRef = useRef<HTMLInputElement>(null);
  const nameRef = useRef<HTMLInputElement>(null);
  const abortRef = useRef<AbortController | null>(null);
  const workspaceId = canUpload ? uploadTarget?.workspaceId : undefined;
  const { canAdd } = useWorkspaceCustomEmojis(workspaceId, 0);
  const offerLibrary = Boolean(workspaceId && canAdd && onPickCustomEmoji);
  const mod = primaryKeyName();

  useEffect(() => () => abortRef.current?.abort(), []);
  useEffect(() => () => { if (picked && "file" in picked) URL.revokeObjectURL(picked.url); }, [picked]);

  function pickFile(file?: File | null) {
    if (!canUpload || !file?.type.startsWith("image/")) return false;
    setPicked({ file, url: URL.createObjectURL(file) });
    setName(emojiNameOfFile(file));
    setError("");
    return true;
  }

  // ⌘V anywhere in the tab but a text field takes a picture or a link.
  useEffect(() => {
    if (picked) return;
    const onPaste = (event: ClipboardEvent) => {
      const target = event.target;
      if (target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement) return;
      const file = Array.from(event.clipboardData?.files ?? []).find((item) => item.type.startsWith("image/"));
      if (pickFile(file)) {
        event.preventDefault();
        return;
      }
      const link = safeExternalPresentationImage(event.clipboardData?.getData("text/plain").trim() ?? "");
      if (!link) return;
      event.preventDefault();
      setPicked({ link, url: link });
      setName("");
      setError("");
    };
    document.addEventListener("paste", onPaste);
    return () => document.removeEventListener("paste", onPaste);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [picked, canUpload]);

  function back() {
    abortRef.current?.abort();
    setPicked(null);
    setToLibrary(false);
    setName("");
    setSaving(false);
    setError("");
  }

  async function save() {
    if (!picked || saving) return;
    if (!toLibrary && "link" in picked) {
      onPickImage(picked.link);
      return;
    }
    const session = loadSession();
    const trimmed = name.trim();
    if (!uploadTarget || !workspaceId || !session || (toLibrary && !trimmed)) return;
    const controller = new AbortController();
    abortRef.current = controller;
    setSaving(true);
    setError("");
    try {
      // A link joins the library as its picture, fetched here.
      const file = "file" in picked ? picked.file : await fetch(picked.link, { signal: controller.signal })
        .then((response) => response.ok ? response.blob() : Promise.reject(new Error("fetch failed")))
        .then((blob) => new File([blob], "emoji", { type: blob.type }));
      if (!file.type.startsWith("image/")) throw new Error("not an image");
      const uploaded = await uploadWorkspaceFile(file, "icons", uploadTarget, { signal: controller.signal });
      if (controller.signal.aborted) return;
      if (!toLibrary) {
        onPickImage(uploaded.fileId);
        return;
      }
      const emoji = await createWorkspaceCustomEmoji(session, workspaceId, {
        fileId: uploaded.fileId,
        name: Array.from(trimmed).slice(0, 100).join(""),
        requestId: newId(),
        signal: controller.signal,
      });
      if (controller.signal.aborted) return;
      onAddedToLibrary(emoji);
      onPickCustomEmoji?.(emoji);
    } catch (error) {
      if (controller.signal.aborted) return;
      setError(uploadRefusalReason(error) ?? t("custom.uploadFailed"));
      setSaving(false);
    }
  }

  const fileInput = canUpload ? (
    <input ref={fileRef} type="file" accept="image/*" hidden tabIndex={-1} aria-hidden="true"
      onChange={(event) => {
        pickFile(event.currentTarget.files?.[0]);
        event.currentTarget.value = "";
      }} />
  ) : null;

  if (!picked) return (
    <div className={styles.uploadPanel} data-icon-panel>
      {canUpload ? (
        <div className={dialog.drop}>
          <button type="button" className={dialog.upload} onClick={() => fileRef.current?.click()} data-icon-upload>
            <ImageIcon size="regular" aria-hidden="true" />{t("custom.add.upload")}
          </button>
        </div>
      ) : null}
      {fileInput}
      <div className={styles.uploadHint}>{t(canUpload ? "custom.pasteHint" : "custom.pasteLinkHint", { mod })}</div>
      <div className={dialog.actions}>
        <button type="button" className={dialog.cancel} onClick={onCancel}>{t("custom.add.cancel")}</button>
        <button type="button" className={dialog.save} disabled>{t("custom.add.save")}</button>
      </div>
    </div>
  );

  return (
    <div className={styles.uploadPanel} data-icon-panel>
      <CustomEmojiPreview src={picked.url} large />
      {offerLibrary ? (
        <label className={styles.libraryCheck} htmlFor={checkId}>
          <input id={checkId} type="checkbox" checked={toLibrary} disabled={saving}
            onChange={(event) => {
              setToLibrary(event.target.checked);
              if (event.target.checked) focusOpenedField(() => nameRef.current);
            }} />
          {t("custom.addToLibrary")}
        </label>
      ) : null}
      {toLibrary ? (
        <>
          <label className={dialog.nameLabel} htmlFor={nameId}>{t("custom.add.name")}</label>
          <input ref={nameRef} id={nameId} className={dialog.field} value={name} placeholder={t("custom.add.namePlaceholder")}
            maxLength={100} onChange={(event) => setName(event.target.value)}
            onKeyDown={(event) => {
              if (!fieldEnter(event)) return;
              event.preventDefault();
              void save();
            }} />
        </>
      ) : null}
      {error ? <div className={dialog.error} role="alert">{error}</div>
        : waitingNote ? <div className={dialog.error} role="status" data-write-uncertain="">{waitingNote}</div> : null}
      <div className={dialog.actions}>
        <button type="button" className={dialog.cancel} onClick={back}>{t("custom.back")}</button>
        <button type="button" className={dialog.save} disabled={saving || (toLibrary && !name.trim())} aria-busy={saving}
          onClick={() => void save()}>{t("custom.add.save")}</button>
      </div>
    </div>
  );
}
