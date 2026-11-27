import { type ClipboardEvent, type FormEvent, type KeyboardEvent, type ReactNode, type RefObject, useRef, useState } from "react";
import { createFocusManager } from "@react-aria/focus";
import { useTranslation } from "react-i18next";
import { FolderIcon } from "@/icons/leafen";
import { SearchClearButton } from "@/components/PageActionSearch";
import { primaryKeyName } from "@/lib/keyboardShortcuts";
import { placeAround, popoverPlacementStyle, type PopoverRect, usePopoverPlacement } from "@/lib/popoverPlacement";
import windowStyles from "@/components/editor/BlockEmptyPopover.module.css";
import styles from "./databaseSheet";
import { focusOpenedField } from "@/components/editor/openedFieldCaret";
import { imeComposing } from "@/lib/fieldKeyBoundary";

type AddTab = "upload" | "link";

/**
 * A files value's "+ 파일 또는 이미지를 추가하세요" window, as Notion's (seen
 * 2026-11-11, 300 by 128): under the editor holding that line, from the line's
 * start, over it where only that side has room; "업로드" and "링크" tabs on a
 * 40px bar, the upload tab's blue "파일 업로드" with "폴더 업로드" beside it and
 * "또는 ⌘+V 단축키로 이미지 링크 붙여넣기" under them, the link tab's field and
 * blue "링크", which takes whatever is typed as the file's address and name.
 * A file or a link pasted while it is open is added. Escape closes it alone,
 * the line that opened it taking the focus back (returnTo) unless the cell
 * says the focus is left on nothing (leavesFocus). On the link tab Tab goes
 * round its own controls — the field, its clearing, "링크", the tabs — as
 * Notion's (2026-11-26, two tabs); on the upload tab it leaves the cell's
 * editor as the list's does (tableCellEditorTab.ts).
 */
export function FilesAddWindow({ line, holder, returnTo = line, leavesFocus, ringsFirst = false, oneFile, busy, status, error, onFiles, onLink, onClose }: {
  /** The "+ 파일 또는 이미지를 추가하세요" line it opens from. */
  line: RefObject<HTMLElement | null>;
  /** The editor holding that line, whose foot it hangs from. */
  holder: RefObject<HTMLElement | null>;
  /** What takes the focus back as it closes: the line, or the editor where it opened by itself. */
  returnTo?: RefObject<HTMLElement | null>;
  /** Whether its closing now leaves the focus on nothing rather than on `returnTo`. */
  leavesFocus?: () => boolean;
  /** Whether its first tab shows the keyboard's ring as it opens, a press or not: an empty value's, opened by itself. */
  ringsFirst?: boolean;
  oneFile: boolean;
  busy: boolean;
  /** An upload's progress, shown in the upload tab while it runs. */
  status?: ReactNode;
  error: string;
  onFiles(files: readonly File[]): void;
  onLink(text: string): void;
  onClose(): void;
}) {
  const { t } = useTranslation(["propertyCell", "blockItem"]);
  const [tab, setTab] = useState<AddTab>("upload");
  const [draft, setDraft] = useState("");
  // The first tab's ring from the opening (ringsFirst), until a press inside the window.
  const [openRing, setOpenRing] = useState(ringsFirst);
  const windowRef = useRef<HTMLDivElement>(null);
  const firstFocus = useRef<HTMLButtonElement>(null);
  const fileInput = useRef<HTMLInputElement>(null);
  const folderInput = useRef<HTMLInputElement>(null);
  const field = useRef<HTMLInputElement>(null);
  const anchor: PopoverRect = {
    get top() { return holder.current?.getBoundingClientRect().top ?? 0; },
    get bottom() { return holder.current?.getBoundingClientRect().bottom ?? 0; },
    get left() { return line.current?.getBoundingClientRect().left ?? 0; },
    get right() { return line.current?.getBoundingClientRect().right ?? 0; },
    contextElement: holder.current ?? undefined,
  };
  const place = usePopoverPlacement(windowRef, (size, viewport) => placeAround(anchor, size, viewport, { gap: 0 }), [tab, busy, error],
    undefined, { kind: "picker", onEscape: onClose, onOutsidePress: onClose, inside: () => [line.current], returnFocusRef: returnTo,
      shouldRestoreFocus: leavesFocus && (() => !leavesFocus()), initialFocus: firstFocus });
  const mod = primaryKeyName();

  function choose(next: AddTab) {
    setTab(next);
    if (next === "link") focusOpenedField(() => field.current, { scroll: false });
  }

  // A file pasted goes up; a link pasted outside the link's field is taken as one.
  function paste(event: ClipboardEvent<HTMLDivElement>) {
    const files = Array.from(event.clipboardData.files);
    if (files.length > 0) {
      event.preventDefault();
      onFiles(files);
      return;
    }
    if (event.target === field.current) return;
    const text = event.clipboardData.getData("text/plain").trim();
    if (!text) return;
    event.preventDefault();
    onLink(text);
  }

  function goRound(event: KeyboardEvent<HTMLDivElement>) {
    if (tab !== "link" || event.key !== "Tab" || event.altKey || event.ctrlKey || event.metaKey || imeComposing(event)) return;
    event.preventDefault();
    event.stopPropagation();
    const round = createFocusManager(windowRef);
    if (event.shiftKey) round.focusPrevious({ wrap: true, tabbable: true });
    else round.focusNext({ wrap: true, tabbable: true });
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!draft.trim()) return;
    onLink(draft.trim());
    setDraft("");
  }

  return (
    <div ref={windowRef} className={`${windowStyles.popover} ${styles.filesAddWindow}`} role="dialog" aria-label={t("addFileOrImage")}
      data-files-add-window data-placement={place?.side === "above" ? "above" : "below"} data-keeps-tab={tab === "link" ? "" : undefined}
      style={popoverPlacementStyle(place)} onPaste={paste} onKeyDown={goRound} onPointerDown={() => setOpenRing(false)}>
      <div className={windowStyles.tabs} role="tablist">
        {(["upload", "link"] as const).map((id) => (
          <button key={id} ref={id === "upload" ? firstFocus : undefined} type="button" role="tab" className={windowStyles.tab}
            data-rings-at-open={id === "upload" && openRing ? "" : undefined}
            aria-selected={id === tab} data-files-add-tab={id} onClick={() => choose(id)}>
            {t(id === "upload" ? "filesUploadTab" : "filesLinkTab")}
          </button>
        ))}
      </div>
      {tab === "upload" ? (
        <div className={styles.filesAddUpload} data-files-add-pane="upload">
          <input ref={fileInput} type="file" multiple={!oneFile} hidden tabIndex={-1} aria-hidden="true"
            onChange={(event) => { onFiles(Array.from(event.currentTarget.files ?? [])); event.currentTarget.value = ""; }} />
          <input ref={(input) => { folderInput.current = input; input?.setAttribute("webkitdirectory", ""); }} type="file" multiple hidden
            tabIndex={-1} aria-hidden="true"
            onChange={(event) => { onFiles(Array.from(event.currentTarget.files ?? [])); event.currentTarget.value = ""; }} />
          <div className={styles.filesAddButtons}>
            <button type="button" className={windowStyles.primary} disabled={busy} aria-busy={busy} data-files-add-upload
              onClick={() => fileInput.current?.click()}>{t(busy ? "uploading" : "filesUploadFile")}</button>
            <button type="button" className={styles.filesAddFolder} disabled={busy} aria-label={t("filesUploadFolder")}
              title={t("filesUploadFolder")} onClick={() => folderInput.current?.click()}>
              <FolderIcon size="small" aria-hidden="true" />
            </button>
          </div>
          {status}
          {error ? <div className={windowStyles.error} role="alert">{error}</div> : null}
          <div className={styles.filesAddHint}>{t("filesPasteHint", { mod })}</div>
        </div>
      ) : (
        <form className={windowStyles.body} data-block-empty-pane="link" data-files-add-pane="link" onSubmit={submit} noValidate>
          {/* A typed link clears from the field's end by Notion's shared "입력 값 지우기" (seen 2026-11-16, 2026-11-17). */}
          <div className={windowStyles.fieldWrap}>
            <input ref={field} className={windowStyles.field} value={draft} placeholder={t("filesLinkPlaceholder")}
              aria-label={t("filesLinkPlaceholder")} data-files-add-link data-filled={draft ? "true" : undefined}
              onChange={(event) => setDraft(event.target.value)} />
            <SearchClearButton value={draft} inputRef={field} over onClear={() => setDraft("")} />
          </div>
          <button type="submit" className={windowStyles.primary}>{t("filesLinkSubmit")}</button>
          {error ? <div className={windowStyles.error} role="alert">{error}</div> : null}
        </form>
      )}
    </div>
  );
}
