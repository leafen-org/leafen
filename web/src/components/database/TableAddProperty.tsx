import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { placeAtPoint, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import type { PropertyType } from "@/lib/types";
import type { UniqueIdSource } from "@/lib/databaseUniqueIdState";
import { DotsHorizontal, Plus } from "../icons";
import {
  TABLE_PROPERTY_TYPE_MENU_MAX_HEIGHT,
  TablePropertyTypeList,
  useTablePropertyTypeChoice,
} from "./TablePropertyTypeChoices";
import styles from "./TableAddProperty.module.css";
import { PropertyIcon } from "./PropertyIcon";
import { inlineSign, inlineStartX, readsRightToLeft, scrollToInlineEnd } from "@/lib/readingDirection";
import type { IconPickerAnchor } from "../page/AnchoredEmojiPicker";
import { menuIconButtonProps } from "./menuIconButton";
import { PropertyIconPicker, propertyIconPickerAnchor } from "./PropertyIconPicker";
import { focusOpenedField } from "@/components/editor/openedFieldCaret";

// The "+" at the end of a table's header, as Notion's (seen 2026-09-30,
// 2026-11-03): it opens a new header cell with a box over it, 4px into the
// header line — the icon button and the name field, which takes focus — and
// the types to choose right under the box. Choosing one adds the property
// with the name typed, or the type's, and the icon chosen; Enter chooses the
// one marked, which the arrow keys move; the search finds a type by name.
// Escape or a click elsewhere adds nothing. However it closes — Escape, a
// type chosen, a press elsewhere — the focus goes back to "+", with the
// keyboard's ring only when keys closed it: a press leaves none, and the
// next key draws one with no tip. "+ 속성 추가" in words gives way to
// "+ ⋯" as the property is made, and the focus stays on the page then, with
// no ring on "+". As Notion's (seen 2026-11-17 in Chrome, three tabs, no
// script before the action, a letter typed after landing nowhere; from the
// words Notion's went to the page twice, by a press and by Enter, and once
// to a sidebar control a menu had earlier given it back to).

// The new cell scrolls into sight within the table's own sideways scroll,
// as Notion's does, leaving the page where it is: its end 20px in from the
// table's (seen 2026-11-06 at 1280: a new cell's 882..1260), its left 20px
// in where the table reads from the right.
function revealInTable(cell: HTMLElement | null) {
  const scroller = cell?.closest<HTMLElement>("[data-table-scroll]");
  if (!cell || !scroller) return;
  const shown = scroller.getBoundingClientRect();
  const rect = cell.getBoundingClientRect();
  const past = readsRightToLeft(scroller) ? shown.left + 20 - rect.left : rect.right - (shown.right - 20);
  if (past > 0) scroller.scrollLeft += past * inlineSign(scroller);
  else if (rect.left < shown.left) scroller.scrollLeft -= shown.left - rect.left;
  else if (rect.right > shown.right) scroller.scrollLeft += rect.right - shown.right;
}

export function TableAddProperty({ onAdd, omit, idSource, onMore, labeled = false }: {
  onAdd(type: PropertyType, typedName: string, icon?: string): void;
  omit?: readonly PropertyType[];
  idSource?: UniqueIdSource;
  /** With the title its only column, "+ 속성 추가" in words and no "⋯", as Notion's new table (seen 2026-10-24). */
  labeled?: boolean;
  /** Beside "+", Notion's "⋯" shows or hides the view's properties. */
  onMore?: (trigger: HTMLElement) => void;
}) {
  const { t } = useTranslation("tableView");
  const [open, setOpen] = useState(false);
  const cellRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!open) return;
    revealInTable(cellRef.current);
  }, [open]);

  // Once the property made shows as a column, the table rolls on to its end,
  // showing it and "+" after it, as Notion's (seen 2026-11-02, 2026-11-06).
  // "+" takes the focus back where it has come to rest, so a tip the keys
  // bring stays up there, as Notion's after Enter chose (seen 2026-11-17 in
  // two tabs, the table rolled or not), not taken by the roll or by "+"
  // moving out from under the pointer.
  const revealAfterAdd = useRef<number | null>(null);
  const focusAfterAdd = useRef(0);
  const giveFocusBack = () => {
    window.clearTimeout(focusAfterAdd.current);
    focusAfterAdd.current = 0;
    // Not from wherever a press has put it meanwhile, or a window opened.
    const active = document.activeElement;
    if (!active || active === document.body) buttonRef.current?.focus();
  };
  useLayoutEffect(() => {
    const cell = cellRef.current;
    if (revealAfterAdd.current === null || !cell || cell.offsetLeft === revealAfterAdd.current) return;
    revealAfterAdd.current = null;
    const scroller = cell.closest<HTMLElement>("[data-table-scroll]");
    // Again once the new column's width has settled, and the focus a frame
    // after that, once the roll's own scroll has been heard.
    const toEnd = () => { if (scroller) scrollToInlineEnd(scroller); };
    toEnd();
    window.requestAnimationFrame(() => {
      toEnd();
      if (focusAfterAdd.current) window.requestAnimationFrame(giveFocusBack);
    });
  });
  useEffect(() => () => window.clearTimeout(focusAfterAdd.current), []);

  function finish(chosen: TableNewPropertyChoice | null) {
    setOpen(false);
    if (!chosen) {
      window.requestAnimationFrame(() => buttonRef.current?.focus());
      return;
    }
    revealAfterAdd.current = cellRef.current?.offsetLeft ?? null;
    onAdd(chosen.type, chosen.name, chosen.icon);
    if (labeled) return;
    // Where no column comes (a relation's database still to choose, the
    // change refused), "+" takes it back all the same.
    window.clearTimeout(focusAfterAdd.current);
    focusAfterAdd.current = window.setTimeout(giveFocusBack, 400);
  }

  return (
    <div ref={cellRef} className={styles.cell} role="columnheader" data-table-add-property data-open={open || undefined} data-labeled={labeled || undefined}
      aria-label={t("addProperty")}>
      {open ? null : (
        <>
          {/* Their tips go above them, as Notion's (seen 2026-11-17 in two tabs). */}
          <button ref={buttonRef} type="button" className={styles.add} aria-label={t("addProperty")} title={labeled ? undefined : t("addProperty")}
            aria-haspopup="dialog" aria-expanded={false} data-labeled={labeled || undefined} data-tip-side="top" onClick={() => setOpen(true)}>
            <Plus size={labeled ? "small" : "regular"} aria-hidden="true" />{labeled ? <span>{t("addProperty")}</span> : null}
          </button>
          {onMore && !labeled ? (
            <button type="button" className={styles.add} aria-label={t("showOrHideProperties")} title={t("showOrHideProperties")}
              aria-haspopup="dialog" data-table-properties data-tip-side="top" onClick={(event) => onMore(event.currentTarget)}>
              <DotsHorizontal size="regular" aria-hidden="true" />
            </button>
          ) : null}
        </>
      )}
      {open ? (
        <TableNewPropertyBox omit={omit} idSource={idSource} marker="data-table-add-property-menu"
          at={() => { const cell = popoverRectOf(cellRef.current); return cell ? { left: cell.left, right: cell.right, top: cell.top } : null; }}
          element={() => cellRef.current} onFinish={finish} />
      ) : null}
    </div>
  );
}

export type TableNewPropertyChoice = { type: PropertyType; name: string; icon?: string };

/**
 * A new column's box, as Notion's (seen 2026-11-03 at 1280px, 2026-11-06,
 * 2026-11-14): over the new column's head from its start, 4px into the
 * header line, the icon button and the name field, which takes focus, and
 * the types 4px under it from 11px before it; held 12px in from the
 * window's end, which moves both back over the column before it (a box at
 * 1024 for a column at 1043), and moved up where the window ends. The
 * header's "+" opens it over its cell, a column's "왼쪽에 삽입" and
 * "오른쪽에 삽입" over the empty column made at that edge of the column
 * (tableNewColumn.ts). Choosing a type finishes it
 * with the name typed and the icon chosen; Enter chooses the one marked,
 * which the arrow keys move; the search finds a type by name. Escape or a
 * press elsewhere finishes it with nothing.
 */
export function TableNewPropertyBox({ at, element, omit, idSource, marker, onFinish }: {
  /** The new column's start, its far side and the header's top, where it opens. */
  at(): { left: number; right: number; top: number } | null;
  /** What it opens from, which a press on stays inside and whose reading direction it follows. */
  element(): HTMLElement | null;
  omit?: readonly PropertyType[];
  idSource?: UniqueIdSource;
  marker: "data-table-add-property-menu" | "data-table-insert-property";
  onFinish(chosen: TableNewPropertyChoice | null): void;
}) {
  const { t } = useTranslation(["tableView", "emojiPicker"]);
  const [name, setName] = useState("");
  const [icon, setIcon] = useState<string | null>(null);
  const [iconAnchor, setIconAnchor] = useState<IconPickerAnchor | null>(null);
  const choice = useTablePropertyTypeChoice(omit, idSource);
  const menuRef = useRef<HTMLDivElement>(null);
  const nameRef = useRef<HTMLInputElement>(null);
  const place = usePopoverPlacement(menuRef, (size, viewport) => {
    const edge = at() ?? { left: 0, right: 0, top: 0 };
    return placeAtPoint({ x: inlineStartX(edge, -11, element()), y: edge.top + 4 }, size, viewport, { cap: TABLE_PROPERTY_TYPE_MENU_MAX_HEIGHT + 32 });
  }, [], undefined, { kind: "picker", restoreFocus: false, onEscape: () => finish(null), onOutsidePress: () => finish(null),
    inside: () => [element(), document.querySelector<HTMLElement>("[data-new-property-icon-picker]")] });

  function finish(type: PropertyType | null) {
    setIconAnchor(null);
    onFinish(type ? { type, name, ...(icon ? { icon } : {}) } : null);
  }

  function closeIconPicker() {
    setIconAnchor(null);
    focusOpenedField(() => nameRef.current);
  }

  if (typeof document === "undefined") return null;
  return <>
    {createPortal(
      <div ref={menuRef} className={styles.panel} role="dialog" aria-label={t("newPropertyType")} {...{ [marker]: "" }}
        style={popoverPlacementStyle(place)}>
        <div className={styles.nameBox} data-new-property-name-box>
          <button type="button" className={styles.iconButton}
            {...menuIconButtonProps(t("emojiPicker:dialog.label"), iconAnchor !== null)} data-new-property-icon-button
            onClick={(event) => {
              setIconAnchor(propertyIconPickerAnchor(event.currentTarget));
            }}>
            {/* The new property's icon, the icon palette's smiley until one is
                picked, drawn as a property's in a 20px place: Notion's 16px box
                at 1.2, 19.2 in the 24px button, a gray one in the button's
                icoSec (seen 2026-11-17 at 1440 in two tabs: "emoji_gray.svg"
                and a picked "anchor_gray.svg" alike). */}
            <PropertyIcon property={{ type: "rich_text", config: { icon: icon ?? "emoji:gray" } }} size="regular" />
          </button>
          {/* The name field takes focus as it opens, before anything more is typed. */}
          <input ref={nameRef} autoFocus className={styles.name} value={name} placeholder={t("propertyNamePlaceholder")}
            aria-label={t("propertyNamePlaceholder")} onChange={(event) => setName(event.target.value)}
            onKeyDown={(event) => choice.onFieldKeyDown(event, finish, "name", name)} />
        </div>
        <div className={styles.typeMenu}>
          <TablePropertyTypeList choice={choice} onFinish={finish} />
        </div>
      </div>,
      document.body,
    )}
    {iconAnchor ? (
      <PropertyIconPicker anchor={iconAnchor} marker="new-property-icon-picker" onClose={closeIconPicker}
        picker={{
          label: t("emojiPicker:dialog.label"),
          onPickBuiltInIcon: (picked) => {
            setIcon(picked);
            closeIconPicker();
          },
          onRemove: () => {
            setIcon(null);
            closeIconPicker();
          },
        }} />
    ) : null}
  </>;
}
