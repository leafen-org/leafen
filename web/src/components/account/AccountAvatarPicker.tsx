import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { FocusScope, useDialog } from "react-aria";
import { api } from "@/api";
import { AVATAR_ICON_NAMES } from "@/avatarIcons.generated";
import { AVATAR_ICON_PREFIX, AvatarIconGlyph, avatarIconName, avatarKind } from "@/components/people/PersonAvatar";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { useFitPopoverInWindow } from "@/lib/popoverPlacement";
import type { Session } from "@/types";
import styles from "./AccountAvatarPicker.module.css";

const PHOTO_SIDE = 256;

/** A photo made square and small, as the account keeps it. */
async function shrinkPhoto(file: File): Promise<Blob> {
  const bitmap = await createImageBitmap(file);
  const side = Math.min(bitmap.width, bitmap.height);
  const canvas = document.createElement("canvas");
  canvas.width = PHOTO_SIDE;
  canvas.height = PHOTO_SIDE;
  const context = canvas.getContext("2d");
  if (!context) throw new Error("A canvas is required.");
  context.drawImage(bitmap, (bitmap.width - side) / 2, (bitmap.height - side) / 2, side, side, 0, 0, PHOTO_SIDE, PHOTO_SIDE);
  bitmap.close();
  const encode = (type: string) => new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, type, 0.86));
  const photo = await encode("image/webp") ?? await encode("image/jpeg");
  if (!photo) throw new Error("The photo couldn't be encoded.");
  return photo;
}

/**
 * Where a person picks their picture: a line icon, a photo they upload or
 * the first letter of their name, which a person without a photo shows, as
 * in Notion. Removing a photo goes back to the first letter.
 */
export function AccountAvatarPicker({ session, avatar, onPick, onClose, returnFocusRef, uploadFirst = false }: {
  session: Session;
  /** Opened from the hint's "사진을 추가": the photo chooser opens at once. */
  uploadFirst?: boolean;
  avatar: string;
  onPick: (avatar: string) => void;
  onClose: () => void;
  returnFocusRef: React.RefObject<HTMLElement | null>;
}) {
  const { t } = useTranslation(["workspaceSettingsDialog", "peopleDirectory"]);
  const ref = useRef<HTMLDivElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const upload = useRef<AbortController | null>(null);
  const [uploading, setUploading] = useState(false);
  const [failed, setFailed] = useState(false);
  const layer = useOverlayLayer(true, { kind: "picker", ref, onEscape: onClose, onOutsidePress: onClose, returnFocusRef });
  const { dialogProps } = useDialog({ "aria-label": t("avatarPicker.label") }, ref);
  useFitPopoverInWindow(ref, true);
  useEffect(() => () => upload.current?.abort(), []);
  // eslint-disable-next-line react-hooks/exhaustive-deps -- once, as the picker opens
  useEffect(() => { if (uploadFirst) fileRef.current?.click(); }, []);
  const chosenIcon = avatarIconName(avatar);
  const kind = avatarKind(avatar);

  async function uploadPhoto(file?: File) {
    if (!file || !file.type.startsWith("image/")) return;
    upload.current?.abort();
    const controller = new AbortController();
    upload.current = controller;
    setUploading(true);
    setFailed(false);
    try {
      const answer = await api.uploadAccountPhoto(session, await shrinkPhoto(file), controller.signal);
      if (controller.signal.aborted) return;
      onPick(answer.avatar);
    } catch {
      if (!controller.signal.aborted) setFailed(true);
    } finally {
      if (upload.current === controller) {
        upload.current = null;
        setUploading(false);
      }
    }
  }

  return (
    <div ref={ref} className={styles.picker} {...dialogProps} style={{ zIndex: layer.zIndex }} onKeyDown={layer.onKeyDown}
      data-account-avatar-picker>
      <FocusScope contain autoFocus>
        <div className={styles.head}>
          <span className={styles.title}>{t("avatarPicker.icons")}</span>
          <span className={styles.actions}>
            <button type="button" className={styles.action} disabled={uploading} data-avatar-upload
              onClick={() => fileRef.current?.click()}>
              {uploading ? t("avatarPicker.uploading") : t("avatarPicker.upload")}
            </button>
            {kind === "image" ? (
              <button type="button" className={styles.action} data-avatar-remove onClick={() => onPick("")}>
                {t("avatarPicker.remove")}
              </button>
            ) : kind !== "letter" ? (
              <button type="button" className={styles.action} data-avatar-letter onClick={() => onPick("")}>
                {t("avatarPicker.letter")}
              </button>
            ) : null}
          </span>
          <input ref={fileRef} type="file" accept="image/*" hidden
            onChange={(event) => {
              void uploadPhoto(event.currentTarget.files?.[0]);
              event.currentTarget.value = "";
            }} />
        </div>
        {failed ? <p className={styles.error} role="alert">{t("avatarPicker.uploadFailed")}</p> : null}
        <div className={styles.grid} role="listbox" aria-label={t("avatarPicker.icons")}>
          {AVATAR_ICON_NAMES.map((name) => (
            <button key={name} type="button" role="option" aria-selected={chosenIcon === name}
              className={styles.icon} data-avatar-icon={name} title={t(`peopleDirectory:avatarIcons.${name}`)}
              aria-label={t(`peopleDirectory:avatarIcons.${name}`)} onClick={() => onPick(AVATAR_ICON_PREFIX + name)}>
              <AvatarIconGlyph name={name} size={30} />
            </button>
          ))}
        </div>
      </FocusScope>
    </div>
  );
}
