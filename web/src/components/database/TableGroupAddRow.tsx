import { forwardRef, useRef, useState, type MouseEvent, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { EnterSign } from "@/components/EnterSign";
import { FilterIcon } from "@/icons/leafen";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { Plus } from "../icons";
import styles from "./databaseSheet";
import { POPOVER_MARGIN } from "@/lib/popoverPlacement";

/**
 * A grouped table's "New page" under one group, which makes a row in it, or
 * its "+ 신규 그룹" under the last group (`groupAdd`, ViewGroupAdd), as
 * Notion's (seen 2026-10-06).
 */
export const TableGroupAddRow = forwardRef<HTMLDivElement, {
  index: number;
  /** "subitemAdd" is an open row's "+ 새 하위 항목", indented under it. */
  kind: "add" | "newGroup" | "subitemAdd";
  label: string;
  indent?: number;
  /** "+ 신규 그룹" (ViewGroupAdd), given the class that places it. */
  groupAdd?(className: string): ReactNode;
  onAdd(): void;
  /** An empty group of a filtered table: "필터 편집하기" before it, as Notion's (seen 2026-11-04). */
  onEditFilters?(trigger: HTMLElement): void;
  /** A group of a database gone to the trash: nowhere to add, its filters still (Notion's sprint board, seen 2026-11-05). */
  filtersOnly?: boolean;
}>(function TableGroupAddRow({ index, kind, label, indent, groupAdd, onAdd, onEditFilters, filtersOnly }, ref) {
  const { t } = useTranslation("boardView");
  return (
    <div ref={ref} data-index={index} className={styles.newRowRail} role="row"
      data-table-group-new-row={kind === "add" ? "true" : undefined} data-table-new-group={kind === "newGroup" ? "true" : undefined}
      data-table-new-subitem={kind === "subitemAdd" ? "true" : undefined}
      style={{ gridTemplateColumns: "var(--table-gutter-width) minmax(0, 1fr)" }}>
      <div className={styles.newRowGutterCell} role="gridcell" aria-hidden="true" />
      <div className={styles.newRowCell} role="gridcell">
        {onEditFilters ? <button type="button" className={styles.newRow} data-table-group-edit-filters onClick={(event) => onEditFilters(event.currentTarget)}>
          <FilterIcon size="small" aria-hidden="true" /> {t("editFilters")}</button> : null}
        {filtersOnly ? null : kind === "newGroup" ? groupAdd?.(styles.tableNewGroup) : <button type="button" className={styles.newRow}
          style={indent ? { marginInlineStart: indent } : undefined} onMouseDown={keepFocusAtPress} onClick={() => onAdd()}>
          <Plus size="small" aria-hidden="true" /> {label}</button>}
      </div>
    </div>
  );
});

/**
 * A mouse press on "+ 새 페이지" leaves the focus where it was: the press
 * then gives it to the field that holds what is typed for the new title
 * (TableView), with no move to the button first, which made two focus moves
 * of the one press.
 */
export function keepFocusAtPress(event: MouseEvent<HTMLElement>) {
  if (event.button === 0) event.preventDefault();
}

// The name field's size, as its stylesheet's, to keep it inside the window.
const NEW_GROUP_NAME_WIDTH = 300;
const NEW_GROUP_NAME_HEIGHT = 36;

/**
 * The new group's name, asked under the button that adds it, its left on the
 * button's — over the button, its foot on the button's top, where it would
 * come within 12px of the window's foot (Notion's table, seen 2026-11-12:
 * under a button ending 673 in a 723 window, over one ending 679). A name a
 * group has already is refused with "그룹이 이미 존재합니다." and the name
 * stays to change, as Notion's board says it (seen 2026-10-30); answered,
 * the focus goes back where it was when the notice opened (the notice's own
 * return): the field after Enter in it, "완료 ↵" after a press of it. Notion's,
 * measured 2026-11-16 in Chrome at 1440 in two tabs 7 minutes apart with no
 * script in the page before the action — after "확인", a typed "z" went into
 * the field after Enter and nowhere after the press, the focus on "완료". Added or
 * left with Escape, it gives the focus back to what held it as it opened
 * (ViewGroupAdd).
 */
export function NewGroupName({ anchor, taken = [], onDone }: {
  anchor: DOMRect; taken?: readonly string[]; onDone(name: string | null): void;
}) {
  const { t } = useTranslation("tableView");
  const [name, setName] = useState("");
  const [refusing, setRefusing] = useState(false);
  const boxRef = useRef<HTMLFormElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const { zIndex } = useOverlayLayer(true, {
    kind: "picker", ref: boxRef, initialFocus: inputRef,
    onEscape: refusing ? undefined : () => onDone(null), onOutsidePress: refusing ? undefined : () => onDone(null),
  });
  const submit = () => {
    const value = name.trim();
    if (!value || !taken.includes(value)) return onDone(value || null);
    setRefusing(true);
    void confirmAction({ message: t("groupExists"), confirmLabel: t("groupExistsOk"), cancelLabel: null })
      .then(() => setRefusing(false));
  };
  return (
    <form ref={boxRef} className={styles.newGroupName} data-table-new-group-name
      style={{ zIndex, top: anchor.bottom + NEW_GROUP_NAME_HEIGHT > window.innerHeight - POPOVER_MARGIN ? anchor.top - NEW_GROUP_NAME_HEIGHT : anchor.bottom, left: Math.max(POPOVER_MARGIN, Math.min(anchor.left, window.innerWidth - NEW_GROUP_NAME_WIDTH - POPOVER_MARGIN)) }}
      onSubmit={(event) => { event.preventDefault(); submit(); }}>
      <input ref={inputRef} value={name} placeholder={t("newGroupName")} aria-label={t("newGroupName")}
        onChange={(event) => setName(event.target.value)} />
      <button type="submit">{t("newGroupDone")} <EnterSign /></button>
    </form>
  );
}

/** The names of a grouping property's options, which a new group may not take. */
export function groupOptionNames(property: { config?: unknown } | null | undefined): string[] {
  const options = (property?.config as { options?: unknown } | undefined)?.options;
  return Array.isArray(options)
    ? options.map((option) => (option as { name?: unknown })?.name).filter((name): name is string => typeof name === "string")
    : [];
}
