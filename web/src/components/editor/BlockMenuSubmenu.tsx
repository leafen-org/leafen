import { Fragment, type ReactNode, type RefObject, useEffect, useLayoutEffect, useRef, useState } from "react";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import type { GraphBlockType } from "@/contentGraph";
import { ArrowsLeftRight, CheckIcon, ChevronRight, ImageAlignIcon, LeftToRightIcon, RightToLeftIcon, TurnIntoIcon } from "@/icons/leafen";
import { shortcutCompactLabel } from "@/lib/keyboardShortcuts";
import { isMenuMoveKey } from "@/lib/menuKeyboard";
import { NativeMenuItem, useNativeMenu, useNativeSubmenu } from "@/lib/nativeMenu";
import { keyCameLast } from "@/lib/lastPress";
import { pointerMoved } from "@/lib/pointerHover";
import { routeOverlayKeyDown } from "@/lib/overlayLayer";
import { mergeProps } from "react-aria";
import { popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import { TEXT_DIRECTIONS, type TextDirection } from "@/lib/textDirection";
import type { BlockPageInAnchor } from "./BlockActionMenu";
import { BlockIcon } from "./BlockIcon";
import { BLOCK_HANDLE_BACKGROUND_COLORS, BLOCK_HANDLE_COLORS } from "./blockHandleModel";
import { blockTurnChoiceLabel } from "./blockItemLabels";
import { blockTurnChoices, type BlockTurnChoice } from "./blockTurnInto";
import { storedEditorColor } from "./colorMemory";
import { TurnRowTip } from "./TurnRowTip";
import styles from "./editorStyles.generated.module.css";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { PhoneMenuHeader } from "./PhoneMenuHeader";
import phoneStyles from "./PhoneMenu.module.css";
import { inlineEndKey } from "@/lib/readingDirection";

export type BlockSubmenu = "turn" | "color" | "direction" | "more" | "duplicate" | "align" | "openWith" | "listFormat" | "language";

/** A list's "목록 형식" or a code block's "언어": the choices, as the block's menu offers them. */
export type BlockListFormat<Format extends string = string> = {
  formats: readonly (readonly [Format, string])[]; active: Format | null; onSelect(format: Format): void;
  /** The row's and the submenu's name, when not "목록 형식" — a quote's "인용 크기". */
  label?: string;
};

/** A plain row of a submenu that runs one action, as an image's "추가 옵션"; one that `opens` another list leads there instead. */
export type BlockSubmenuEntry = { id: string; label: string; icon: ReactNode; run(): void; hint?: string; opens?: BlockSubmenu };
const DIRECTION_ICONS = { auto: ArrowsLeftRight, ltr: LeftToRightIcon, rtl: RightToLeftIcon } as const;
export type BlockSubmenuChoice = { id: string; label: string; group?: string; turn?: BlockTurnChoice; color?: string; direction?: TextDirection;
  checked: boolean; recent?: boolean; backgroundDefault?: boolean; entry?: BlockSubmenuEntry };
type Choice = BlockSubmenuChoice;

/** What a block menu's submenu lists, and its name: the desktop's beside menu and the phone sheet's inner screen alike. */
export type BlockSubmenuSource = {
  activeColor: string; activeType: GraphBlockType | "mixed"; selectionCount: number;
  direction?: { active: TextDirection | null; onSelect(direction: TextDirection): void };
  entries?: BlockSubmenuEntry[]; choices?: BlockTurnChoice[];
  alignment?: { active: "left" | "center" | "right"; onSelect(value: "left" | "center" | "right"): void };
  listFormat?: BlockListFormat; language?: BlockListFormat;
};

export function blockSubmenuChoices(kind: BlockSubmenu, source: BlockSubmenuSource, t: TFunction<"blockHandle">,
  options: { phone: boolean; recent: string | null }): { label: string; items: Choice[] } {
  const { activeColor, activeType, selectionCount, direction, entries, choices, alignment, listFormat, language } = source;
  const { phone, recent } = options;
  const colorName = (color: string) => t(`colors.${color}`, { defaultValue: t("colors.default") });
  const textColorName = (color: string) => t("menu.textColorNamed", { color: colorName(color) });
  const items: Choice[] = kind === "turn" ? (choices ?? blockTurnChoices(selectionCount)).map(turn => ({
    id: turn.key, label: blockTurnChoiceLabel(turn), turn, checked: activeType === turn.type && turn.type !== "column_list" && !turn.placeIn,
  })) : kind === "direction" && direction ? TEXT_DIRECTIONS.map(value => ({
    id: value, label: t(`direction.${value}`), direction: value, checked: direction.active === value,
  })) : kind === "color" ? [
    ...(recent ? [{ id: "recent", color: recent, label: recent.endsWith("_background") ? colorName(recent) : textColorName(recent),
      group: t("colorMenu.recent"), checked: activeColor === recent, recent: true }] : []),
    ...BLOCK_HANDLE_COLORS.map((color, index) => ({ id: `text:${color}`, color, label: textColorName(color),
      group: index === 0 ? t("colorMenu.textColor") : undefined, checked: activeColor === color })),
    { id: "background:default", color: "default", label: t("colorMenu.defaultBackground"), group: t("colorMenu.backgroundColor"),
      checked: false, backgroundDefault: true },
    ...BLOCK_HANDLE_BACKGROUND_COLORS.map(color => ({ id: `background:${color}`, color, label: colorName(color), checked: activeColor === color })),
  ] : kind === "more" || kind === "duplicate" || kind === "openWith" ? (entries ?? []).map(entry => ({ id: entry.id, label: entry.label, entry, checked: false }))
    // "맞춤 ›" lists its three alignments under its name, a 20px mark before
    // each word, no check at the block's own, as Notion's for a picture, a
    // video, a PDF and an embed alike (seen 2026-11-27 at 1440, dpr 1).
    : kind === "align" && alignment ? (["left", "center", "right"] as const).map((value, index) => ({ id: value, label: t(`align.${value}`),
      group: index === 0 && !phone ? t("media.fit") : undefined, checked: alignment.active === value,
      entry: { id: value, label: t(`align.${value}`), icon: <ImageAlignIcon align={value} size="regular" />, run: () => alignment.onSelect(value) } }))
    : (kind === "listFormat" || kind === "language") && (kind === "language" ? language : listFormat) ? (() => {
      const own = (kind === "language" ? language : listFormat)!;
      // A list's format and a quote's size are words alone under the
      // submenu's name, with no check, as Notion's (seen 2026-11-10); a code
      // block's language is checked, under its name "언어" too (Notion's,
      // seen 2026-11-27 in Chrome at 1440 in two tabs).
      const heading = phone ? undefined : kind === "listFormat" ? listFormat?.label ?? t("menu.listFormat")
        : kind === "language" ? t("menu.codeLanguage") : undefined;
      return own.formats.map(([format, marker], index) => ({ id: format, label: marker, group: index === 0 ? heading : undefined,
        checked: kind === "language" && own.active === format, entry: { id: format, label: marker, icon: null, run: () => own.onSelect(format) } }));
    })() : [];
  const label = kind === "listFormat" && listFormat?.label ? listFormat.label
    : t(kind === "turn" ? "menu.turnInto" : kind === "direction" ? "menu.textDirection" : kind === "more" ? "media.moreOptions"
    : kind === "duplicate" ? "menu.duplicate" : kind === "openWith" ? "media.openWith" : kind === "align" ? "media.fit" : kind === "listFormat" ? "menu.listFormat"
    : kind === "language" ? "menu.codeLanguage" : "menu.color");
  return { label, items };
}

/** A submenu row's own mark: a turn's block, a direction's arrows, an entry's icon. */
export function blockSubmenuGlyph(item: Choice, activeType: GraphBlockType | "mixed") {
  // A file row's turns each show the turn mark, as Notion's (seen 2026-11-07: "PDF", "임베드").
  return item.turn && activeType === "file" ? <TurnIntoIcon size="regular" />
    : item.turn ? <BlockIcon type={item.turn.placeIn ? "link_to_page" : item.turn.type} size="regular" />
    : item.direction ? (() => { const Icon = DIRECTION_ICONS[item.direction]; return <Icon size="regular" />; })()
    : item.entry ? item.entry.icon : null;
}

/** A color row's mark: "A" in a text color, a background's fill (none for
 * "기본 배경"), its edge a faint line of its own color, as Notion's. */
export function BlockColorSwatch({ color, className, chosen }: { color: string; className?: string; chosen?: boolean }) {
  const background = color.endsWith("_background");
  return <span className={`${styles.colorSwatch} ${className ?? ""}`} data-color={color} data-chosen={chosen || undefined} aria-hidden="true"
    style={background && color !== "default_background" ? { color: `var(--c-${color.replace(/_background$/u, "")})` } : undefined}>
    {background ? null : "A"}</span>;
}

/** Aria owns focus, hover, press and nested keys. Native choices/checked
 * values and the page destination command stay with the block owner. */
export function BlockMenuSubmenu({ kind, main, mainPlace, triggerRef, menuRef: ref, menuOptions, focusOnOpen, openedByKeys,
  activeColor, activeType, selectionCount, onBack, onClose, onColor, turnInto, onPageIn, direction, entries, choices, alignment, listFormat, language }: {
  kind: BlockSubmenu; main: RefObject<HTMLDivElement | null>; mainPlace?: { left: number; top: number };
  triggerRef: RefObject<HTMLButtonElement | null>; menuRef: RefObject<HTMLDivElement | null>;
  menuOptions: NonNullable<Parameters<typeof useNativeMenu>[0]["menuOptions"]>; focusOnOpen: boolean;
  /** Opened by the keys (→, Enter): its row is ringed, its tip shown, at once, as Notion's (seen 2026-11-01, 2026-11-11). */
  openedByKeys?: boolean;
  activeColor: string; activeType: GraphBlockType | "mixed"; selectionCount: number;
  onBack(): void; onClose(): void; onColor(color: string): void; turnInto(choice: BlockTurnChoice): void;
  onPageIn?: (place: BlockPageInAnchor | null) => void;
  direction?: { active: TextDirection | null; onSelect(direction: TextDirection): void };
  entries?: BlockSubmenuEntry[];
  choices?: BlockTurnChoice[];
  alignment?: { active: "left" | "center" | "right"; onSelect(value: "left" | "center" | "right"): void };
  listFormat?: BlockListFormat;
  language?: BlockListFormat;
}) {
  const { t } = useTranslation("blockHandle");
  const { phone } = useSidebarLayout(true);
  const pageInRef = useRef<HTMLButtonElement>(null);
  const [recent] = useState(() => kind === "color" ? storedEditorColor() : null);
  const { label, items } = blockSubmenuChoices(kind, { activeColor, activeType, selectionCount, direction, entries, choices, alignment, listFormat, language },
    t, { phone, recent });
  // Beside the menu, 4px over its edge, centred on its row and at most 70%
  // of the window tall, 12px from the window's edges, as Notion's block menu
  // submenus open, long or short (seen 2026-11-09, 779-tall window: "전환",
  // "색" 545 high centred on their rows, "스킬" 123; 873 tall: 611).
  const place = usePopoverPlacement(ref, (_size, viewport) => {
    const box = main.current?.getBoundingClientRect(), row = triggerRef.current?.getBoundingClientRect();
    return { anchor: { top: row?.top ?? 0, bottom: row?.bottom ?? 0, left: box?.left ?? 0, right: box?.right ?? 0 },
      placement: "right", gap: -4, options: { cap: Math.round(viewport.height * 0.7) } };
  }, [kind, mainPlace?.left, mainPlace?.top], undefined,
    { kind: "menu", inside: () => [main.current], onEscape: onBack, onOutsidePress: onClose, returnFocusRef: triggerRef,
      // Closed by the pointer moving on to another row's submenu, it leaves
      // the focus there: given back to its own row, it closed the one just
      // opened (2026-11-09: from "색" on to "전환" opened nothing; Notion's
      // opens it).
      shouldRestoreFocus: () => !main.current?.querySelector('[aria-expanded="true"]') });
  function openPageIn() {
    const row = pageInRef.current, menu = ref.current;
    if (!onPageIn || !row || !menu) return false;
    onPageIn({ row: row.getBoundingClientRect(), menu: menu.getBoundingClientRect(), returnFocusRef: pageInRef, onLeaveList: onBack });
    return true;
  }
  // It opens lighting its first row however it was opened, the block's own
  // choice keeping only its check, as Notion's "전환" and "색" (seen
  // 2026-11-14 and again 2026-11-15 at 1440, after the press only: a heading's
  // "전환" opened by the pointer, a click or → lit "텍스트", ↓ then "제목1").
  const picker = useNativeMenu({ items, open: place !== null, menuRef: ref, label, menuOptions,
    focusOnOpen, firstKey: items[0]?.id,
    keyOf: item => item.id, textOf: item => item.label, onAction: item => {
      if (item.id === "align" && kind === "more" && alignment) nestedAlign.state.open("first");
      else if (item.entry) item.entry.run();
      else if (item.turn) { if (!item.turn.placeIn || !openPageIn()) turnInto(item.turn); }
      else if (item.direction) direction?.onSelect(item.direction);
      else if (item.color) onColor(item.color);
    } });
  const alignRef = useRef<HTMLButtonElement>(null), alignMenuRef = useRef<HTMLDivElement>(null);
  const nestedAlign = useNativeSubmenu({ triggerKey: "align", rootTrigger: picker.rootTrigger, parentMenuRef: ref,
    submenuRef: alignMenuRef, triggerRef: alignRef });
  const focused = picker.state.selectionManager.focusedKey;
  const choice = focused === null ? undefined : picker.state.collection.getItem(focused)?.value?.item as Choice | undefined;
  // As the slash menu's: the row a press opens the list on (the block's own
  // type) shows no tip and no ring until the keys or the pointer move; opened
  // by the keys it is ringed with its tip at once, as the keys' row is
  // (Notion, seen 2026-11-01 and 2026-11-11). So is one the pointer opens
  // while a key came after the last press (keyCameLast) — ⌘/, Escape or ↓
  // since the menu's handle was pressed, if it was: the keys lead its look
  // however it opened, and the pointer's rows take the ring as it moves on.
  // "언어" and "목록 형식" start shaded all the same, a key moving in them
  // ringing its row. Notion's (measured 2026-11-27 in Chrome at 1440, dpr 2,
  // in two tabs at different times, after the action only: ⌘/ from a chosen
  // block or from the text, or the handle pressed and ↓, then the pointer
  // resting on "색 ›", "전환 ›" or a database's "복제 ›" ringed its first row,
  // "전환" with its tip, and the row the pointer moved on to in it; the handle
  // pressed alone shaded it, the pointer's rows shaded with the tip; "언어 ›"
  // shaded "ABAP" either way, ↓ then ringing "Agda", and "목록 형식 ›" "기본").
  const keysLead = () => kind !== "language" && kind !== "listFormat" && keyCameLast();
  const [openedLed] = useState(() => openedByKeys || keysLead());
  const [movedBy, setMovedBy] = useState<"keys" | "pointer" | null>(null);
  const moved = movedBy ?? (openedLed ? "keys" : null);
  const pageOwner = useRef(onPageIn); pageOwner.current = onPageIn;
  useEffect(() => () => pageOwner.current?.(null), []);
  useLayoutEffect(() => { if (choice && !choice.turn?.placeIn) onPageIn?.(null); }, [focused]);
  // A phone's "블록 색상" ticks and rings the block's own color, as Notion's;
  // a block of the default text color has neither there (seen 2026-11-14).
  const phoneChosen = (item: Choice) => phone && item.checked && Boolean(item.color) && item.color !== "default" && !item.recent;
  const groups: { label?: string; items: Choice[] }[] = [];
  for (const item of items) {
    if (item.group || !groups.length) groups.push({ label: item.group, items: [] });
    groups.at(-1)!.items.push(item);
  }
  const row = (item: Choice) => (
        <NativeMenuItem key={item.id} state={picker.state} itemKey={item.id} buttonRef={item.id === "align" && kind === "more" ? alignRef : item.turn?.placeIn ? pageInRef : undefined}
          className={phone ? phoneStyles.row : item.color ? styles.colorItem : styles.turnItem} data-block-menu-item role={item.entry && kind !== "align" && kind !== "language" ? "menuitem" : "menuitemradio"}
          aria-checked={item.entry && kind !== "align" && kind !== "language" ? undefined : item.checked} data-submenu-row={item.id}
          data-submenu={item.id === "align" && kind === "more" ? "align" : undefined}
          data-turn-type={item.turn?.key} data-text-direction-choice={item.direction} data-block-color-choice={item.color}
          data-block-color-recent={item.recent || undefined} data-block-color-group={item.backgroundDefault ? "background" : undefined}
          itemOptions={mergeProps(item.id === "align" && kind === "more" ? nestedAlign.submenuTriggerProps : {}, {
            onHoverStart: () => { if (item.turn?.placeIn) openPageIn(); } })}>
          {({ labelProps }) => <>
            {item.color ? <BlockColorSwatch color={item.backgroundDefault ? "default_background" : item.color}
              className={phone ? phoneStyles.swatch : undefined} chosen={phoneChosen(item)} />
              : kind === "duplicate" || kind === "listFormat" ? null : <span className={styles.turnGlyph} aria-hidden="true">
                {blockSubmenuGlyph(item, activeType)}
              </span>}
            <span {...labelProps} className={item.color ? undefined : styles.turnName}>{item.label}</span>
            {item.id === "align" && kind === "more" ? <ChevronRight size="small" aria-hidden="true" /> : null}
            {item.turn?.placeIn ? <ChevronRight className={styles.menuChevron} size="small" aria-hidden="true" />
              : item.checked && !item.color && kind !== "align" || phoneChosen(item) ? <CheckIcon className={phone ? phoneStyles.check : styles.colorCheck} size={phone ? "page" : "small"} aria-hidden="true" /> : null}
            {/* A phone's list shows no keys, as Notion's. */}
            {item.recent && !phone ? <span className={styles.menuShortcut} aria-hidden="true">{shortcutCompactLabel("blockColor")}</span> : null}
            {item.entry?.hint && !phone ? <span className={styles.menuShortcut} aria-hidden="true">{item.entry.hint}</span> : null}
          </>}
        </NativeMenuItem>
  );
  return <>
    <div {...picker.menuProps} ref={ref} className={`${styles.blockMenu} ${styles.blockSubmenu} ${phone ? `${phoneStyles.menu} ${phoneStyles.list}` : ""}`} data-block-submenu={kind}
      style={place ? popoverPlacementStyle(place) : { visibility: "hidden" }}
      data-moved-by={moved ?? undefined}
      onPointerMove={event => { if (pointerMoved(event)) setMovedBy(keysLead() ? "keys" : "pointer"); }}
      onKeyDown={event => {
        if (event.key === "Escape") { routeOverlayKeyDown(event); return; }
        if (isMenuMoveKey(event.key)) setMovedBy("keys");
        if (event.key === inlineEndKey(event.currentTarget) && choice?.id === "align" && kind === "more" && alignment) {
          event.preventDefault(); event.stopPropagation(); nestedAlign.state.open("first"); return;
        }
        if (event.key === inlineEndKey(event.currentTarget) && choice?.turn?.placeIn && openPageIn()) { event.preventDefault(); event.stopPropagation(); return; }
        picker.menuProps.onKeyDown?.(event);
      }}>
      {phone ? <PhoneMenuHeader title={kind === "color" ? t("menu.phoneColorTitle") : label} onClose={onClose} /> : null}
      {/* A phone's groups stand apart on the screen, as Notion's (PhoneMenu.module.css). */}
      {groups.map(group => phone ? <div key={group.label ?? kind} className={phoneStyles.section}>
        {group.label ? <div className={phoneStyles.label}>{group.label}</div> : null}
        <div className={phoneStyles.rows} role={group.label ? "group" : undefined} aria-label={group.label}>{group.items.map(row)}</div>
      </div> : <Fragment key={group.label ?? kind}>
        {group.label ? <div className={styles.menuLabel}>{group.label}</div> : null}
        {group.label ? <div className={styles.colorList} role="group" aria-label={group.label}>{group.items.map(row)}</div> : group.items.map(row)}
      </Fragment>)}
    </div>
    {kind === "more" && alignment && nestedAlign.state.isOpen ? <BlockMenuSubmenu kind="align" main={ref} mainPlace={place ?? undefined}
      triggerRef={alignRef} menuRef={alignMenuRef} activeType={activeType} activeColor={activeColor} selectionCount={selectionCount}
      onBack={nestedAlign.state.close} onClose={onClose} onColor={onColor} turnInto={turnInto} alignment={alignment}
      // Opened by the pointer too, it starts on its first row, as every list beside a block menu's row does.
      menuOptions={nestedAlign.submenuProps} focusOnOpen openedByKeys={nestedAlign.state.focusStrategy !== null} /> : null}
    <TurnRowTip menuRef={ref} choice={choice?.turn} place={place} shown={Boolean(moved)} />
  </>;
}
