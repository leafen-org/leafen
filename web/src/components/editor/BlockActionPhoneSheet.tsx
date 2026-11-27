import { useLayoutEffect, useState, type ReactNode, type Ref } from "react";
import { useTranslation } from "react-i18next";
import { CheckIcon } from "@/icons/leafen";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow, PhoneSheetSwitch } from "@/components/shell/PhoneSheet";
import { blockSubmenuChoices, blockSubmenuGlyph, type BlockSubmenu, type BlockSubmenuChoice, type BlockSubmenuSource } from "./BlockMenuSubmenu";
import type { BlockTurnChoice } from "./blockTurnInto";
import { storedEditorColor } from "./colorMemory";
import editorStyles from "./editorStyles.generated.module.css";
import styles from "./BlockActionPhoneSheet.module.css";

/** A row of the sheet, from the block menu's own rows. */
export type BlockSheetRow = {
  id: string; label: string; icon?: ReactNode; run(): void;
  /** A list it opens inside the sheet, with "‹" back, as Notion's "전환 ›" and "색 ›". */
  submenu?: BlockSubmenu;
  /** Its switch, where the row turns something on or off. */
  switched?: boolean;
  danger?: boolean; badge?: string; disabled?: boolean;
};

/** A screen of the menu's own, as an image's link field: its name, and what it holds. */
export type BlockSheetPanel = { title: string; onBack(): void; rows: BlockSheetRow[]; children?: ReactNode };

/** A block menu row as BlockActionMenu builds it: a `before` starts a group. */
export type BlockMenuRowSource = { id: string; label: string; run(): void; before?: ReactNode; submenu?: BlockSubmenu;
  attributes?: Record<string, unknown> };

// Rows a phone's sheet leaves out of the menu's: "편집 내용 제안" and "여기에서
// 발표", which no block's sheet has on Notion's phone (seen 2026-11-15 on the
// A90 at 411, the same workspace whose computer menus have both: a line of
// text, a picture, a file, a bookmark, an uploaded video and a map's embed).
const NOT_IN_SHEET = new Set(["back", "suggest", "present"]);

/**
 * The menu's rows in the sheet's groups, parted where the menu draws a line
 * or a heading; a switch row keeps its switch, "삭제" is red and nothing
 * shows its keys.
 */
export function blockSheetGroups(rows: BlockMenuRowSource[], iconOf: (id: string) => ReactNode, beta: string,
  disabled: ReadonlySet<string> = new Set()): BlockSheetRow[][] {
  const groups: BlockSheetRow[][] = [];
  // A row left out that starts a group starts it for the row after it.
  let parted = false;
  for (const row of rows) {
    if (NOT_IN_SHEET.has(row.id)) { parted ||= Boolean(row.before); continue; }
    if (!groups.length || (row.before || parted) && groups.at(-1)!.length) groups.push([]);
    parted = false;
    const checked = row.attributes?.["aria-checked"];
    groups.at(-1)!.push({ id: row.id, label: row.label, run: row.run, submenu: row.submenu, icon: iconOf(row.id), danger: row.id === "delete",
      switched: typeof checked === "boolean" ? checked : undefined, badge: row.id === "present" ? beta : undefined, disabled: disabled.has(row.id) });
  }
  return groups;
}

/**
 * A block's menu on a phone, as Notion's "작업" sheet from the editing tools'
 * "더 보기" (seen 2026-11-11, iOS Safari 402 and Android Chrome 411, light and
 * dark): a grip and the title over the block's kind in grey, then its rows in
 * round groups as the menu parts them — no search, so no keyboard rises, and
 * no keys beside the rows — and who edited it last in a group of its own at
 * the foot. A row with "›" opens its list in the same sheet under that name,
 * "‹" going back. The page's focus leaves as it opens, so the keyboard and the
 * tools go down; the shade closes it.
 */
export function BlockActionPhoneSheet({ typeLabel, groups, source, panel, footer, sheetRef, onClose, onColor, turnInto }: {
  typeLabel?: ReactNode;
  groups: BlockSheetRow[][];
  /** What each list the rows open holds. */
  source(kind: BlockSubmenu): BlockSubmenuSource;
  panel?: BlockSheetPanel;
  footer?: ReactNode;
  /** The menu's own element, which its submenu machinery reads while the sheet stands for it. */
  sheetRef?: Ref<HTMLDivElement>;
  onClose(): void;
  onColor(color: string): void;
  turnInto(choice: BlockTurnChoice): void;
}) {
  const { t } = useTranslation("blockHandle");
  const [stack, setStack] = useState<BlockSubmenu[]>([]);
  const [recent] = useState(() => storedEditorColor());
  // Before the sheet notes what had the focus: closed, it gives none back.
  useLayoutEffect(() => {
    const active = document.activeElement;
    if (active instanceof HTMLElement && (active.isContentEditable || active.matches("input, textarea"))) active.blur();
  }, []);
  const kind = stack.at(-1);
  const own = kind ? source(kind) : null;
  const list = kind && own ? blockSubmenuChoices(kind, own, t, { phone: true, recent: kind === "color" ? recent : null }) : null;
  const back = kind ? () => setStack(stack.slice(0, -1)) : panel?.onBack;
  const title = list?.label ?? panel?.title ?? t("menu.phoneTitle");

  const sheetRow = (row: BlockSheetRow) => (
    <PhoneSheetRow key={row.id} icon={row.icon} disabled={row.disabled} danger={row.danger} more={Boolean(row.submenu)}
      label={row.badge ? <>{row.label}<span className={styles.badge}>{row.badge}</span></> : row.label}
      value={row.switched === undefined ? undefined : <PhoneSheetSwitch on={row.switched} />}
      onPress={() => { if (row.submenu) setStack([row.submenu]); else row.run(); }}
      data={{ "data-block-menu-item": "", "data-block-sheet-row": row.id }} />
  );
  function choose(item: BlockSubmenuChoice) {
    if (item.entry?.opens) setStack([...stack, item.entry.opens]);
    else if (item.entry) item.entry.run();
    else if (item.turn) turnInto(item.turn);
    else if (item.direction) own?.direction?.onSelect(item.direction);
    else if (item.color) onColor(item.color);
  }
  const choiceRow = (item: BlockSubmenuChoice) => {
    const glyph = item.color
      ? <span className={`${editorStyles.colorSwatch} ${styles.swatch}`} data-color={item.backgroundDefault ? "default_background" : item.color}
        style={item.color.endsWith("_background") ? { color: `var(--c-${item.color.replace(/_background$/u, "")})` } : undefined}>
        {item.color.endsWith("_background") || item.backgroundDefault ? null : "A"}</span>
      : blockSubmenuGlyph(item, own?.activeType ?? "mixed");
    return (
      <PhoneSheetRow key={item.id} icon={glyph ?? undefined} label={item.label} more={Boolean(item.turn?.placeIn || item.entry?.opens)}
        // A color or an alignment chosen now shows no tick, as the menu's.
        value={item.checked && !item.color && kind !== "align" ? <CheckIcon size="regular" className={styles.check} /> : undefined} onPress={() => choose(item)}
        data={{ "data-block-menu-item": "", "data-block-sheet-choice": item.id, "data-sheet-color": item.color ? "" : undefined,
          "data-turn-type": item.turn?.key, "data-block-color-choice": item.color }} />
    );
  };
  const listGroups: { label?: string; items: BlockSubmenuChoice[] }[] = [];
  for (const item of list?.items ?? []) {
    if (item.group || !listGroups.length) listGroups.push({ label: item.group, items: [] });
    listGroups.at(-1)!.items.push(item);
  }

  return (
    <PhoneSheet title={title} sheetRef={sheetRef} onClose={onClose} onBack={back} data={{ "data-block-phone-actions": kind ?? (panel ? "panel" : "main") }}>
      {list ? listGroups.map((group, index) => (
        <PhoneSheetGroup key={group.label ?? index} label={group.label} className={styles.choices}>{group.items.map(choiceRow)}</PhoneSheetGroup>
      )) : panel ? <>
        {panel.children ? <PhoneSheetGroup className={styles.panel}>{panel.children}</PhoneSheetGroup> : null}
        {panel.rows.length ? <PhoneSheetGroup>{panel.rows.map(sheetRow)}</PhoneSheetGroup> : null}
      </> : <>
        {groups.map((rows, index) => (
          <PhoneSheetGroup key={rows[0]?.id ?? index} label={index === 0 ? typeLabel : undefined}>{rows.map(sheetRow)}</PhoneSheetGroup>
        ))}
        {footer ? <PhoneSheetGroup className={styles.foot}>{footer}</PhoneSheetGroup> : null}
      </>}
    </PhoneSheet>
  );
}
