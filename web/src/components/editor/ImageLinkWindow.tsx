import { useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { Trash } from "@/icons/leafen";
import { pageHref } from "@/lib/navigation";
import { pageIdFromPageHref } from "@/lib/pageLinks";
import { popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { normalizePastedLink } from "./editorInputModel";
import { SelectionToolbarLinkDialog } from "./SelectionToolbarLinkDialog";
import { SelectionToolbarLinkPicker, type SelectionLinkPages } from "./SelectionToolbarLinkPicker";
import styles from "./editorStyles.generated.module.css";

/**
 * A picture's link, in the text's link window, as Notion's (seen
 * 2026-11-12): ⌘K on the chosen picture, or "이미지에 링크 추가하기" in its
 * menus, opens the 330px "링크 붙여넣기 또는 페이지 검색" over "최근
 * 페이지" in the middle under the picture's block, its top on the block's
 * foot (over the block, its foot on the block's top, where only that has
 * room; moved up into the window as what it finds grows it). A linked
 * picture's opens as a text link's edit window: "페이지 또는 URL" over its
 * address and "링크 제거" — a page's address left out of the field, to be
 * searched again, the page's chip standing there while the field is left. Enter on an address or a page links it; Escape in the field
 * leaves the field, and the next Escape or a press beside closes the window,
 * a changed address left unsaved.
 */
export function ImageLinkWindow({ anchor, link, source, onSave, onClose }: {
  /** The picture's block, whose box the window stands by. */
  anchor: Element;
  /** The picture's link as kept, "" for none. */
  link: string;
  source?: SelectionLinkPages;
  /** Links the picture ("" takes its link off) and closes the window. */
  onSave: (href: string) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation("selectionToolbar");
  const ref = useRef<HTMLDivElement>(null), inputRef = useRef<HTMLInputElement>(null);
  // A page's address is left out of the field: the page shows there instead
  // while the field hasn't the focus (SelectionToolbarLinkPicker).
  const pageLink = pageIdFromPageHref(link) ? link : undefined;
  const shown = pageLink ? "" : link;
  const [value, setValue] = useState(shown);
  const [inField, setInField] = useState(true);
  const box = useRef(popoverRectOf(anchor));
  const place = usePopoverPlacement(ref, () => {
    if (anchor.isConnected) box.current = popoverRectOf(anchor);
    return { anchor: box.current ?? { top: 0, bottom: 0, left: 0, right: 0 }, placement: "bottom", gap: 0, options: { whole: true } };
  }, [anchor], undefined, {
    kind: "picker", onEscape: () => {
      // The window keeps the focus, so the next Escape is its own, the picture staying chosen under it.
      if (inputRef.current && document.activeElement === inputRef.current) ref.current?.focus();
      else onClose();
    },
    onOutsidePress: onClose,
  });
  const address = () => {
    const href = normalizePastedLink(value);
    if (href) onSave(href);
  };
  const editing = Boolean(link);
  const typed = inField && value.trim() !== "" && value !== shown;
  return createPortal(
    <SelectionToolbarLinkDialog dialogRef={ref} inputRef={inputRef} placed={Boolean(place)}
      className={`${editing ? styles.linkEditWindow : styles.tbLinkMenu} ${styles.imageLinkWindow}`} style={{ ...popoverPlacementStyle(place), visibility: place ? "visible" : "hidden" }}>
      {editing ? <div className={styles.linkEditLabel}>{t("linkEditor.pageOrUrl")}</div> : null}
      <div className={editing && !inField ? styles.imageLinkLeft : undefined} style={{ display: "contents" }}
        onFocus={() => setInField(true)} onBlur={() => setInField(false)}>
        <SelectionToolbarLinkPicker inputRef={inputRef} value={value} onValue={setValue} popoverRef={ref} source={source}
          placeholder={editing ? t("linkEditor.editPlaceholder") : undefined} current={editing ? shown : undefined} pageLink={pageLink}
          onPage={(page) => onSave(pageHref(page.id))} onAddress={address} />
      </div>
      {editing && !typed ? <>
        <div className={styles.linkEditDivider} />
        <button type="button" className={styles.linkEditRemove} onClick={() => onSave("")}>
          <Trash size="regular" aria-hidden="true" />{t("linkEditor.removeLink")}
        </button>
      </> : null}
    </SelectionToolbarLinkDialog>, document.body);
}
