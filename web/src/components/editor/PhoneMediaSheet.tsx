import { useLayoutEffect, useRef, useState, type ComponentType } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { CameraIcon, ImageIcon, VideoIcon, type LeafenIconProps } from "@/icons/leafen";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { editorBlockChildren } from "./editorBlockIndex";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { insertEditorFiles } from "./mediaBlockUpload";
import styles from "./PhoneMediaSheet.module.css";

/** A way of picking: what files it asks for, and whether the camera takes them. */
type Way = { key: "gallery" | "photo" | "video"; icon: ComponentType<LeafenIconProps>; accept: string; capture?: true; multiple?: true };
const WAYS: readonly Way[] = [
  { key: "gallery", icon: ImageIcon, accept: "image/*,video/*", multiple: true },
  { key: "photo", icon: CameraIcon, accept: "image/*", capture: true },
  { key: "video", icon: VideoIcon, accept: "video/*", capture: true },
];

/**
 * A phone strip's picture, in a line or the title: Notion Android app's
 * "미디어 삽입" sheet (seen 2026-11-21 on the A90 at 411, from a line and from
 * the title), "갤러리 열기", "사진 촬영하기" and "동영상 캡처하기". A browser
 * opens them as a file field: the gallery as the phone's chooser of pictures
 * and videos, the photo and the video straight in the camera (`capture`).
 * The sheet goes as one is pressed, the page's focus and the keyboard with it
 * as the app's; what was picked comes to `onFiles`.
 */
export function PhoneMediaSheet({ onClose, onFiles }: { onClose(): void; onFiles(files: File[]): void }) {
  const { t } = useTranslation("editor");
  const ref = useRef<HTMLDivElement>(null);
  const layer = useOverlayLayer(true, { kind: "dialog", ref, onEscape: onClose });
  useLayoutEffect(() => { (document.activeElement as HTMLElement | null)?.blur?.(); }, []);
  function pick(way: Way) {
    const field = document.createElement("input");
    field.type = "file";
    field.accept = way.accept;
    field.multiple = Boolean(way.multiple);
    if (way.capture) field.setAttribute("capture", "environment");
    field.addEventListener("change", () => { const files = [...field.files ?? []]; if (files.length) onFiles(files); }, { once: true });
    field.click();
    onClose();
  }
  return createPortal(<div className={styles.layer} style={{ zIndex: layer.zIndex }}>
    <button type="button" tabIndex={-1} className={styles.shade} aria-label={t("phoneToolbar.media.close")} onClick={onClose} />
    {/* Its own sheet, not a window the phone draws as one (phoneWindowSheet.ts). */}
    <div ref={ref} className={styles.sheet} role="dialog" aria-modal="true" aria-labelledby="phone-media-title" data-phone-media-sheet data-phone-sheet="">
      <div id="phone-media-title" className={styles.title}>{t("phoneToolbar.media.title")}</div>
      <div className={styles.rows} role="menu">
        {WAYS.map(way => <button type="button" key={way.key} role="menuitem" className={styles.row} data-phone-media={way.key} onClick={() => pick(way)}>
          <way.icon size="large" aria-hidden="true" /><span>{t(`phoneToolbar.media.${way.key}`)}</span>
        </button>)}
      </div>
    </div>
  </div>, document.body);
}

/** Where a sheet's files go: after a line (`empty`: in its place), or at the page's start from its title (no line). */
type MediaPlace = { blockId: string | null; empty: boolean };

/**
 * The media sheet of a phone strip's picture (PhoneMediaSheet): its files
 * become blocks as pasted files do (insertEditorFiles), each uploading its
 * own — after the line, taking an empty line's place, or from the title
 * first on the page. The page as it is then (`latest`) takes them: the
 * picker may stay open a while.
 */
export function usePhoneMedia(controller: { run(change: () => void): void }, latest: { readonly current: NativeEditorGraphAdapter }) {
  const [place, setPlace] = useState<MediaPlace | null>(null);
  function put(at: MediaPlace, files: File[]) {
    const adapter = latest.current;
    if (adapter.readOnly || !adapter.isCurrent()) return;
    controller.run(() => {
      const first = at.blockId === null ? editorBlockChildren(adapter.graph, null)[0] : undefined;
      const ids = insertEditorFiles(adapter, at.blockId ?? (first ? { id: first.id, mode: "before" } : null), files, "pasted");
      if (ids.length && at.empty && at.blockId) adapter.remove([at.blockId]);
    });
  }
  return {
    open: (blockId: string | null, empty: boolean) => setPlace({ blockId, empty }),
    sheet: place ? <PhoneMediaSheet onClose={() => setPlace(null)} onFiles={files => put(place, files)} /> : null,
  };
}
