import { useLayoutEffect, useId, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Search, ShuffleIcon } from "@/icons/leafen";
import { SearchClearButton } from "@/components/PageActionSearch";
import { type PopoverRect, popoverRectOf } from "@/lib/popoverPlacement";
import { CaretPickerCell, useCaretPicker } from "@/lib/caretPicker";
import { GridCellTip, IconTipButton } from "@/components/IconTipButton";
import type { useBuiltInIconChoice } from "./builtInIconChoice";
import { BuiltInIconGlyph } from "./PageIcon";
import { drawNativeIconsAhead } from "./nativeIconDrawing";
import { filteredNativeIcons, nativeIconCellLabel, nativeIconLabel } from "./nativeIcons";
import { IconColorDot, IconColorPalette, IconColorRow } from "./PageIconColorPalette";
import { usePickerCellsDrawn } from "./pickerCellsDrawn";
import { RECENT_BUILT_IN_ICON_ROWS, readRecentBuiltInIcons } from "./builtInIconRecent";
// The filter row is the emoji tab's, as Notion's two tabs share one (seen 2026-11-06).
import topStyles from "./EmojiPickerTop.module.css";
import colorStyles from "./PageIconColor.module.css";
import styles from "./PageIconPicker.module.css";
import phoneList from "./PhonePickerList.module.css";
import { fieldFocusOnOpen } from "@/lib/screenKind";

type BuiltInIconChoice = ReturnType<typeof useBuiltInIconChoice>;
/** The grid's twelve 32px cells a row (PageIconPicker.module.css .grid; on a phone fewer 44px ones, PhonePickerList, measured once it is drawn). */
const GRID_COLUMNS = 12, CELL_PX = 32;
/**
 * The tab's list, as Notion's (measured 2026-11-16 at 1440): 300px down to
 * the picker's foot, each group 4px in, its name block 33.9px over its first
 * row (6 over a 14.9px name, 9 under it) and 4 under its last.
 */
const LIST_PX = 300, GROUP_HEAD_PX = 33.9, GROUP_END_PX = 4;

/** The icon rows the list shows as it opens, under the recent ones' group: drawn first (pickerCellsDrawn.ts). */
function firstIconRows(recentCount: number, columns: number, filtered: boolean) {
  const recentRows = Math.ceil(recentCount / columns);
  const above = recentRows ? GROUP_HEAD_PX + recentRows * CELL_PX + GROUP_END_PX : 0;
  return Math.ceil((LIST_PX - above - (filtered ? GROUP_END_PX : GROUP_HEAD_PX)) / CELL_PX);
}

// What the tab shows as it opens — its recent icons and its first rows, as
// many as twelve a row show with no recent group above them — is worked out
// ahead, once the page's start has settled (nativeIconDrawing.tsx).
drawNativeIconsAhead(() => [...readRecentBuiltInIcons(),
  ...filteredNativeIcons("").slice(0, firstIconRows(0, GRID_COLUMNS, false) * GRID_COLUMNS)]);

/** Catalog/native values stay here; the public grid owns transient focus,
 * spatial navigation, option press and the real search caret relationship. */
export function PageIconBuiltInPanel({ choice, query, onQuery, onRemove, onClose, touchCard = false }: {
  choice: BuiltInIconChoice; onRemove?: () => void; onClose(): void;
  query: string; onQuery(value: string): void;
  /**
   * In a touch screen's card (TouchPickerCard) its filter's focus waits for a
   * tap, and no icon is lit until the arrows move one, as Notion's iPad and
   * phones (seen 2026-11-17 in iPhone Safari at 402, dark and light, and iPad
   * Safari at 834: the tab opened, its filter tapped and "A" typed, no cell
   * lit; its emoji tab lights its first, as here); at a desk the first is lit
   * as the tab opens, as Notion's at 1440.
   */
  touchCard?: boolean;
}) {
  const { t } = useTranslation("emojiPicker");
  const id = useId(), listRef = useRef<HTMLDivElement>(null), gridRef = useRef<HTMLDivElement>(null), searchRef = useRef<HTMLInputElement>(null);
  const colorButtonRef = useRef<HTMLButtonElement>(null), rowTriggerRef = useRef<HTMLButtonElement>(null);
  const [colorsOpen, setColorsOpen] = useState(false);
  const [rowAnchor, setRowAnchor] = useState<PopoverRect | null>(null);
  const { color, eachTime, choosing } = choice;
  const filtered = Boolean(query.trim());
  // How many cells a row holds and how tall each is: twelve of 32px, or as many 44px ones as fit a phone's card.
  const [columns, setColumns] = useState(GRID_COLUMNS);
  const [cellPx, setCellPx] = useState(CELL_PX);
  // The icons picked last lead the list, two rows of them, until a filter is typed, as Notion's.
  const [recent] = useState(readRecentBuiltInIcons);
  const recentItems = useMemo(() => filtered ? []
    : recent.slice(0, RECENT_BUILT_IN_ICON_ROWS * columns).map(name => ({ key: `recent:${name}`, name })), [filtered, recent, columns]);
  const items = useMemo(() => filteredNativeIcons(query).map(name => ({ key: name, name })), [query]);
  // The grid draws the rows it shows as the tab opens and the rest a step at
  // a time after that frame, as the emoji tab's (pickerCellsDrawn.ts).
  const { drawn, allDrawn } = usePickerCellsDrawn(items.length, query, firstIconRows(recentItems.length, columns, filtered) * columns);
  const shown = useMemo(() => allDrawn ? items : items.slice(0, drawn), [items, drawn, allDrawn]);
  const choices = useMemo(() => [...recentItems, ...shown], [recentItems, shown]);
  // Each icon a grid cell holding no state of its own (`CaretPickerCell`),
  // its picture drawn again only when the icons or their color change.
  const glyphs = useMemo(() => new Map([...recentItems, ...items].map(({ name }) => [name, <BuiltInIconGlyph icon={{ name, color }} size={24} />])),
    [recentItems, items, color]);
  // The cell pressed, which the row of colors stands under ("choose every time").
  const pressedKey = useRef<string | null>(null);
  const picker = useCaretPicker({ items: choices, query, id, label: t("icons.gridLabel"), listRef, ownerRef: searchRef,
    layout: "grid", tab: "leave", lightFirst: !touchCard,
    keyOf: item => item.key, textOf: item => nativeIconLabel(item.name),
    onPick: item => { pressedKey.current = item.key; choice.pickIcon(item.name); }, onClose });
  useLayoutEffect(() => {
    const grid = gridRef.current;
    const tracks = grid ? getComputedStyle(grid).gridTemplateColumns.split(" ").filter(Boolean) : [];
    if (tracks.length && tracks.length !== columns) setColumns(tracks.length);
    const size = parseFloat(tracks[0] ?? "");
    if (size && size !== cellPx) setCellPx(size);
  }, [columns, cellPx]);
  // Geometry/return target reads the actual native icon row; it never builds
  // another keyboard collection or chooses an option.
  useLayoutEffect(() => {
    const option = choosing ? listRef.current?.querySelector<HTMLButtonElement>(
      `[data-native-icon-key="${CSS.escape(pressedKey.current ?? choosing)}"]`) : null;
    if (option) rowTriggerRef.current = option;
    setRowAnchor(popoverRectOf(option));
  }, [choosing]);
  const cell = ({ key, name }: { key: string; name: string }, index: number) => (
    <CaretPickerCell key={key} {...picker.cell(key)} className={`${styles.emoji} ${phoneList.cell}`} data-emoji-index={index}
      data-native-icon-option={name} data-native-icon-key={key}
      aria-label={nativeIconCellLabel(name, color)}>{glyphs.get(name)}</CaretPickerCell>
  );
  return <div data-icon-panel>
    <div className={topStyles.top}>
      <span className={topStyles.searchBox}>
        <Search className={topStyles.searchMark} size="small" aria-hidden="true" />
        <input ref={searchRef} className={topStyles.search} placeholder={t("emoji.searchPlaceholder")} value={query}
          aria-label={t("icons.searchLabel")} role="combobox" aria-autocomplete="list" aria-controls={id}
          aria-expanded="true" aria-activedescendant={picker.activeId}
          onChange={event => { onQuery(event.target.value); choice.cancel(); }}
          onKeyDown={event => picker.onCaretKeyDown(event.nativeEvent)} autoFocus={!touchCard && fieldFocusOnOpen()} />
        <SearchClearButton value={query} inputRef={searchRef} over onClear={() => { onQuery(""); choice.cancel(); }} />
      </span>
      {/* Random and the color name themselves in the dark tip under them, as the emoji tab's (EmojiCatalogPanel). */}
      <IconTipButton className={`${topStyles.iconButton} ${topStyles.outlined}`} disabled={!items.length} placement="bottom"
        onClick={() => { pressedKey.current = null; choice.shuffleIcon(items[Math.floor(Math.random() * items.length)].name); }} label={t("icons.randomLabel")}>
        <ShuffleIcon size="tiny" aria-hidden="true" />
      </IconTipButton>
      {/* A property's picker has no ● (builtInIconChoice). */}
      {choice.colors ? <div className={colorStyles.iconColorControl}>
        {/* Named with its color, its tip "아이콘 색상 선택" alone, as Notion's (seen 2026-11-17 at 1440). */}
        <IconTipButton ref={colorButtonRef} className={`${topStyles.iconButton} ${topStyles.outlined}`}
          aria-haspopup="dialog" aria-expanded={colorsOpen} placement="bottom" tip={t("icons.colorTip")}
          label={t("icons.colorLabel", { color: eachTime ? t("icons.noColor") : t(`icons.colors.${color}`) })}
          onClick={() => setColorsOpen(value => !value)}>
          <IconColorDot color={eachTime ? null : color} size="tiny" />
        </IconTipButton>
        {colorsOpen && <IconColorPalette choice={choice} triggerRef={colorButtonRef} onClose={() => setColorsOpen(false)} />}
      </div> : null}
      {onRemove && <button type="button" className={styles.remove} onClick={onRemove} aria-label={t("remove.ariaLabel")}>
        {t("remove.label")}</button>}
    </div>
    {/* One list down to the picker's foot, the groups' names scrolling with their icons, as Notion's (seen 2026-11-16). */}
    <div {...picker.listBoxProps} className={`${styles.iconList} ${phoneList.list} nscroll`} ref={listRef} onScroll={choosing ? choice.cancel : undefined}>
      {recentItems.length ? <div role="group" aria-label={t("icons.recentLabel")} className={`${styles.iconGroup} ${phoneList.group}`}>
        <div className={`${styles.sectionLabel} ${phoneList.label}`}>{t("icons.recentLabel")}</div>
        <div className={`${styles.grid} ${phoneList.band}`}>{recentItems.map(cell)}</div>
      </div> : null}
      {items.length ? <div role="group" aria-label={t("icons.sectionLabel")} className={`${styles.iconGroup} ${phoneList.group}`}>
        {filtered ? null : <div className={`${styles.sectionLabel} ${phoneList.label}`}>{t("icons.sectionLabel")}</div>}
        <div className={`${styles.grid} ${phoneList.band}`} ref={gridRef}>
          {shown.map((item, index) => cell(item, recentItems.length + index))}
          {/* The rows not drawn yet keep their room, so the list opens at its full height and its scroll bar stays put. */}
          {allDrawn ? null : <div aria-hidden="true" className={styles.gridRest}
            style={{ height: (Math.ceil(items.length / columns) - Math.ceil(drawn / columns)) * cellPx }} />}
        </div>
      </div> : <div className={styles.iconNoMatches} role="status">{t("emoji.noMatches")}</div>}
    </div>
    {/* The lit icon's name under it once the keys or the pointer moved the highlight, as Notion's (seen 2026-11-14). */}
    <GridCellTip gridRef={listRef} cellKey={picker.tipKey}
      label={choices[picker.active] ? nativeIconLabel(choices[picker.active].name) : undefined} />
    {choosing && rowAnchor && <IconColorRow choice={choice} name={choosing} anchor={rowAnchor} triggerRef={rowTriggerRef} />}
  </div>;
}
