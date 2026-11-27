import { createContext, useContext, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { Plus } from "../icons";
import { BoardPersonGroupAdd } from "./BoardPersonGroupAdd";
import styles from "./databaseSheet";
import { RelationGroupAdd } from "./RelationGroupAdd";
import { NewGroupName } from "./TableGroupAddRow";
import type { PropertyCellEnvironment } from "./propertyCellTypes";
import type { ViewGroupAddKind } from "./viewGroupAddKind";

/** Whether a view's "+ 신규 그룹" takes no focus when pressed: a board's (ViewGroupAdd). */
export const GroupAddKeepsFocus = createContext(false);

/**
 * A view's "+ 신규 그룹", by what it adds (viewGroupAddKind): an option's or
 * a text's name asked in a small field under it ("새 그룹", "완료 ↵"), a
 * person picked from the workspace's people, a page from the related
 * database — each opening from the button, as Notion's table, list and board
 * (seen 2026-11-12). `value` is the name, or the person's or page's id.
 * An inert one (the layout builder's board) shows the same button, which
 * opens nothing. The button is one in every view (newGroupButton), as
 * Notion's; the view's own class only places it. A group added, or a window
 * left, gives the focus back to what held it as the window opened, as
 * Notion's (seen 2026-11-12, by keys and by the pointer): a table's button
 * takes the focus when pressed, so it comes back there, its ring after keys;
 * a board's takes none (GroupAddKeepsFocus), so it goes back to the view's
 * tab where that held it, its ring after keys, or stays on the page —
 * and to the button itself where keys opened it.
 */
export function ViewGroupAdd({ kind, property, environment, className, taken = [], has, disabled, inert, marker, onAdd }: {
  kind: ViewGroupAddKind;
  property: { config?: unknown; dataSourceId?: string; databaseId?: string };
  /** What a person's or a page's list reads. */
  environment?: PropertyCellEnvironment;
  /** Where the view puts it. */
  className: string;
  /** Names a new option or text may not take. */
  taken?: readonly string[];
  /** Whether a person or a page has a group already. */
  has(value: string): boolean;
  disabled?: boolean;
  /** Shown but doing nothing when pressed, as Notion's in the layout builder (seen 2026-11-12). */
  inert?: boolean;
  /** The button's own data attribute, by name. */
  marker?: string;
  onAdd(value: string): void;
}) {
  const { t } = useTranslation("boardView");
  const [naming, setNaming] = useState<DOMRect | null>(null);
  const keepsFocus = useContext(GroupAddKeepsFocus);
  const buttonClass = `${styles.newGroupButton} ${className}`;
  if (inert) {
    return <button type="button" className={buttonClass} aria-disabled="true" {...(marker ? { [`data-${marker}`]: "" } : {})}>
      <Plus size="small" aria-hidden="true" />{t("newGroup")}
    </button>;
  }
  if (kind === "person") {
    return environment ? <BoardPersonGroupAdd environment={environment} className={buttonClass} keepsFocus={keepsFocus} has={has} onPick={onAdd} /> : null;
  }
  if (kind === "page") {
    return environment ? <RelationGroupAdd environment={environment} property={property} className={buttonClass} keepsFocus={keepsFocus} has={has} onPick={onAdd} /> : null;
  }
  return (
    <>
      <button type="button" className={buttonClass} disabled={disabled} aria-expanded={Boolean(naming)} {...(marker ? { [`data-${marker}`]: "" } : {})}
        {...(keepsFocus ? { "data-press-keeps-focus": "" } : {})} onClick={(event) => setNaming(event.currentTarget.getBoundingClientRect())}>
        <Plus size="small" aria-hidden="true" />{t("newGroup")}
      </button>
      {naming && createPortal(<NewGroupName anchor={naming} taken={taken} onDone={(name) => { setNaming(null); if (name) onAdd(name); }} />, document.body)}
    </>
  );
}
