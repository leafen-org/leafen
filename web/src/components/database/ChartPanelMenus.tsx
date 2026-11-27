import { SwitchInput } from "@/components/Switch";
import { Fragment, createContext, useContext, useEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { CheckIcon, ChevronDown, ChevronRight, SidebarChevronRight, iconAtSize } from "@/icons/leafen";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { placeAround, placeBeside, popoverAnchorOf, popoverBackdropStyle, popoverPlacementStyle, usePopoverPlacement,
  type PopoverRect } from "@/lib/popoverPlacement";
import { PhoneSheet, PhoneSheetMenu } from "../shell/PhoneSheet";
import { PhoneSettingsListContext } from "./PropertySettingRow";
import styles from "./ChartPanelMenus.module.css";
import phoneStyles from "./ChartPhoneMenu.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

/**
 * The pieces a chart's settings are made of, as Notion's chart panel (seen
 * 2026-10-05): a row with an icon, its name, what is set and ›, ⌄ while the
 * menu it opens under or over it is open (seen 2026-11-12), the whole row
 * dim where it can't be opened (seen 2026-11-18); a switch row;
 * and the menus a row opens right under it (a choice list, or a box of its
 * own) or, from a menu's row, beside it, whose › stays. In a phone's
 * settings sheet a row is the sheet's own (ChartPanelMenus.module.css), its
 * mark drawn at 22px and its › at 18px, as Notion's (seen 2026-11-18 on the
 * A90); a row inside one of its menus keeps the menu's sizes. A row
 * without a mark has no place for one, its name 8 in from the row's start,
 * as a number's "형식 › 숫자 형식" and "소수점 이하 자리" (Notion's, read
 * 2026-11-18 in Chrome at 1440 dpr 1: their names 12 in from the menu's
 * start, the menu's 4 and the row's 8), as a switch row without one.
 */
function useSheetRow() {
  return useContext(PhoneSettingsListContext) && !useContext(ChartMenuInside);
}

export function ChartRow({ icon, label, meta, swatch, disabled, closed, drop, marker, expanded, beside, onOpen }: {
  icon?: ReactNode; label: string; meta?: string; disabled?: boolean; marker: string; expanded?: boolean;
  /** A color's 16px swatch, 4 round, before what is set, as a number's chosen "색상" (Notion's, seen 2026-11-18). */
  swatch?: string;
  /**
   * What it sets drops down from it, as a number's "색상" (Notion's, seen
   * 2026-11-18 in Chrome at 1440, two tabs): the sidebar's 16px chevron 4
   * past what is set, › while closed, turning to ⌄ while its menu is open and
   * back however that closes (a letter picked, Escape, a press elsewhere);
   * in a phone's sheet the sheet's own › until open (Notion's there, seen
   * 2026-11-18 on the A90).
   */
  drop?: boolean;
  /** It has nothing to open now: no ›, what is set in the name's color, as a count's "계산" (Notion's, seen 2026-11-18). */
  closed?: boolean;
  /** Its menu opens beside it rather than under it. */
  beside?: boolean;
  onOpen(anchor: PopoverRect): void;
}) {
  const sheet = useSheetRow(), chevron = sheet ? "page" : "small", turning = drop && !sheet, down = !turning && expanded && !beside;
  return (
    <button type="button" className={styles.row} disabled={disabled} data-chart-setting={marker} aria-haspopup="true"
      data-closed={closed ? "true" : undefined} data-drop={drop ? "true" : undefined} aria-expanded={expanded} onClick={(event) => onOpen(popoverAnchorOf(event.currentTarget))}>
      {icon !== undefined && <span className={styles.icon} aria-hidden="true">{sheet ? iconAtSize(icon, "wide") : icon}</span>}
      <span className={styles.label}>{label}</span>
      {swatch ? <i className={styles.swatch} style={{ background: swatch }} aria-hidden="true" /> : null}
      {meta ? <span className={styles.meta}>{meta}</span> : null}
      {closed ? null : turning ? <SidebarChevronRight size="small" className={styles.turn} aria-hidden="true" />
        : down ? <ChevronDown size={chevron} aria-hidden="true" /> : <ChevronRight size={chevron} aria-hidden="true" />}
    </button>
  );
}

export function ChartSwitchRow({ icon, label, checked, disabled, marker, onChange }: {
  icon?: ReactNode; label: string; checked: boolean; disabled?: boolean; marker: string; onChange(checked: boolean): void;
}) {
  const sheet = useSheetRow();
  return (
    <label className={styles.row} data-chart-setting={marker} data-disabled={disabled ? "true" : undefined}>
      {icon !== undefined && <span className={styles.icon} aria-hidden="true">{sheet ? iconAtSize(icon, "wide") : icon}</span>}
      <span className={styles.label}>{label}</span>
      <SwitchInput checked={checked} disabled={disabled}
        onChange={(event) => onChange(event.currentTarget.checked)} />
    </label>
  );
}

/** A text the person types, kept when they press Enter, leave it or close its menu. */
export function ChartTextField({ value, placeholder, label, disabled, multiline, marker, onCommit }: {
  value: string; placeholder?: string; label: string; disabled?: boolean; multiline?: boolean; marker: string;
  onCommit(value: string): void;
}) {
  const typed = useRef(value);
  const latest = useRef({ value, onCommit });
  latest.current = { value, onCommit };
  const commit = (next: string) => { if (next !== latest.current.value) latest.current.onCommit(next); };
  // A new value from elsewhere replaces what was typed; a menu closing with
  // Escape takes the field away before it blurs.
  useEffect(() => { typed.current = value; }, [value]);
  useEffect(() => () => commit(typed.current), []);
  const shared = {
    defaultValue: value, placeholder, "aria-label": label, disabled, className: styles.field, "data-chart-field": marker,
  };
  return multiline
    ? <textarea key={value} {...shared} rows={3} onChange={(event) => { typed.current = event.currentTarget.value; }}
      onBlur={(event) => commit(event.currentTarget.value)} />
    : <input key={value} {...shared} onChange={(event) => { typed.current = event.currentTarget.value; }}
      onBlur={(event) => commit(event.currentTarget.value)}
      onKeyDown={(event) => { if (fieldEnter(event)) { event.preventDefault(); commit(event.currentTarget.value); } }} />;
}

// Whether a menu is drawn inside another chart menu: the outermost alone is drawn on the page's body.
const ChartMenuInside = createContext(false);

// How the menu a row is in closes, for a pick in that row's side list to close it too (ChartChoiceRow); none in a
// menu that stays open (ChartMenuStaysOpen).
const ChartMenuClose = createContext<(() => void) | undefined>(undefined);

/**
 * A menu whose rows' side lists leave it open once a choice is picked in
 * them, as Notion's "스타일 옵션 더 보기", which also holds switches and
 * fields (seen 2026-11-18 in Chrome at 1440 dpr 1: "높이 › 크게" and
 * "격자선 › 세로" picked, the options stayed with the new value).
 */
export function ChartMenuStaysOpen({ children }: { children: ReactNode }) {
  return <ChartMenuClose.Provider value={undefined}>{children}</ChartMenuClose.Provider>;
}

/**
 * A box over the panel, flush under the row that opened it or over it where
 * the window has no room under (or beside a menu's row), closing with Escape
 * or a press elsewhere. As Notion's chart panel sizes them (read 2026-11-18
 * in Chrome at 1440, two tabs, from each menu's own style): 250 wide unless
 * its caller says — a list of properties, sorts, colors or heights — or as
 * wide as what it holds ("content", a number's ten colors), never under 180
 * nor over the window less 24, its end at the row's end; 400 high
 * at most, or 70% of the window's height where `tall` (the sorts with their
 * groups, the colors, the heights, the picture to save), its rows rolling
 * inside. A menu beside a menu's row has its middle on the row's and its
 * edge on the row's own, so it overlaps the menu by the menu's 4px inset,
 * 400 high at most, kept 12 inside the window (Notion's, read 2026-11-18 in
 * Chrome at 1440 dpr 1: "소수점 이하 자리" 200 by 210 with its middle on the
 * row's at 612.2, the style options' "높이" and "격자선" 830..1030 beside a
 * menu from 1026, the number formats 250 by 400 pushed up to end 12 over
 * the window's foot). `under` starts it at the row's start, as a reference line card's
 * style and color choices (seen 2026-11-12); `centered` sets its middle on
 * the row's, as "다른 이름으로 차트 저장…". The outermost is drawn on the
 * page's body, so one opening upward stands over the panel's head and the
 * view's bar instead of being cut where the panel's rows are. In a phone's
 * settings sheet it is a list at the screen's foot, or a whole sheet
 * (`whole`), instead (ChartPhoneMenu).
 */
export function ChartPopover(props: ChartPopoverProps) {
  return useContext(PhoneSettingsListContext) ? <ChartPhoneMenu {...props} /> : <ChartDesktopPopover {...props} />;
}

type ChartPopoverProps = {
  anchor: PopoverRect; beside?: boolean; under?: boolean; centered?: boolean; tall?: boolean; label: string; width?: number | "content";
  className?: string; children: ReactNode; onClose(): void; marker: string;
  /** On a phone a whole sheet under its grip rather than a list at the screen's foot. */
  whole?: boolean;
};

/**
 * A chart setting's menu on a phone, as Notion's (seen 2026-11-18 on a Galaxy
 * A90 in Chrome at 411, light, in two tabs): not a box by its row but a white
 * square list at the screen's foot as tall as its rows, over the sheets'
 * darker shade, the settings sheet dimmed under it (PhoneSheetMenu) — "표시
 * 대상", "그룹화", "범위", "스타일 옵션 더 보기" and the lists opened from
 * them; or a whole sheet with its list straight under the grip on the page's
 * ground (`whole`, as PhoneSheetChoices) — "정렬 기준", "색상", "높이" and
 * "기준선". Their choices are bands across it (ChartPhoneMenu.module.css).
 */
function ChartPhoneMenu({ label, className, children, onClose, marker, whole }: ChartPopoverProps) {
  const body = <ChartMenuInside.Provider value={true}>
    <div className={`${phoneStyles.body} ${className ?? ""}`} data-chart-menu={marker}>
      <ChartMenuClose.Provider value={onClose}>{children}</ChartMenuClose.Provider>
    </div>
  </ChartMenuInside.Provider>;
  return whole ? <PhoneSheet title={label} head={false} bodyClassName={phoneStyles.sheetBody} onClose={onClose}>
    <div className={phoneStyles.ground}><div className={phoneStyles.sheetList}>{body}</div></div>
  </PhoneSheet> : <PhoneSheetMenu label={label} className={phoneStyles.menu} onClose={onClose}>{body}</PhoneSheetMenu>;
}

/** How high a chart menu grows before its rows roll, but where `tall`. */
const CHART_MENU_CAP = 400;

function ChartDesktopPopover({ anchor, beside, under, centered, tall, label, width = 250, className, children, onClose, marker }: ChartPopoverProps) {
  const inside = useContext(ChartMenuInside);
  const ref = useRef<HTMLDivElement>(null);
  const place = usePopoverPlacement(ref, (size, viewport) => beside
    ? placeBeside(anchor, size, viewport, { gap: 0, prefer: "right", cap: CHART_MENU_CAP,
      offsetTop: (anchor.bottom - anchor.top - Math.min(size.height, CHART_MENU_CAP)) / 2 })
    : placeAround(anchor, size, viewport, { gap: 0, align: under ? "start" : centered ? "center" : "end", alignFallback: true,
      cap: tall ? Math.floor(viewport.height * 0.7) : CHART_MENU_CAP }),
  [anchor, beside, under, centered, tall], undefined, { kind: "picker", onEscape: onClose, onOutsidePress: onClose });
  const style: CSSProperties = { width: width === "content" ? "max-content" : width, ...popoverPlacementStyle(place) };
  const box = <ChartMenuInside.Provider value={true}>
    <button type="button" className={styles.backdrop} style={popoverBackdropStyle(place)} tabIndex={-1} aria-label={label} onClick={onClose} />
    <div ref={ref} role="dialog" aria-label={label} className={`${styles.popover} ${className ?? ""}`} style={style}
      data-chart-menu={marker}>
      <ChartMenuClose.Provider value={onClose}>{children}</ChartMenuClose.Provider>
    </div>
  </ChartMenuInside.Provider>;
  return inside ? box : createPortal(box, document.body);
}

export type ChartChoice = {
  value: string; label: string; icon?: ReactNode; disabled?: boolean; next?: boolean;
  /** Shown after the name, as a color theme's swatches. */
  trailing?: ReactNode;
  /** What its row shows once it is chosen, where shorter than its name in the list: a currency's name without its code. */
  short?: string;
  /** A sign before its name, as wide as itself, in the name's color and 8 before it: a rule's ">" (Notion's, seen 2026-11-18). */
  sign?: string;
  /** A line before it, as between a menu's kinds of choices. */
  dividerBefore?: boolean;
};

/** A list its caller lets be searched shows its field from this many choices (Notion's, seen 2026-11-18). */
const CHART_SEARCH_FROM = 7;

/**
 * A list of choices, the one set checked, with Notion's menu keys; with a
 * search, "속성 검색" over it narrows the choices by name as typed, the
 * field keeping the keys (Notion's property menus, seen 2026-11-12) — shown
 * only where the list has seven choices or more, "없음" and "수" counted, as
 * Notion's chart menus (seen 2026-11-18 in Chrome, two tabs at 1440 and
 * 820, properties added one at a time: six choices none, seven a field, in
 * the X and Y axes' "표시 대상", "그룹화" and a number's "표시 대상").
 * Opened, a list without a search lights its first choice whichever is
 * checked, and one with a search lights none, its field taking the keys
 * (Notion's chart menus, seen 2026-11-18 in Chrome at 1440, two tabs at
 * 12:0x and 12:1x, read after each press: a number's "표시 대상" with 점수
 * checked lit 단계, "높이" with 중간 checked lit 작게, the X axis's sort
 * with a descending sort lit 수동, the Y axis's "표시 대상" with 점수 lit 수,
 * "소수점 이하 자리" with 2 lit 기본 설정, the colors with 초록색 lit 자동,
 * the grid lines beside the style options with 둘 다 lit 없음). A list with
 * its own filter, as the number formats' "숫자 형식 필터링...", shows its
 * field whatever its length and lights its first choice while the field
 * keeps the keys (Notion's, seen 2026-11-18 in Chrome at 1440: 상속 lit with
 * 숫자 ✓, the field focused).
 */
export function ChartChoiceList({ label, options, value, onPick, marker, search: searchWords, filter, marked = true, select = false }: {
  label: string; options: readonly ChartChoice[]; value?: string; onPick(value: string): void; marker: string;
  /** The search field's words, for a list that may be searched. */
  search?: string;
  /** Its own filter field's words, and which choices a typed filter keeps. */
  filter?: ChartChoiceFilter;
  /**
   * Whether it ✓s the chosen choice, as a radio of the list: every chart
   * list does but the color themes, plain choices with no ✓ whichever is
   * chosen, each theme's swatches standing for it (Notion's, seen 2026-11-18
   * in Chrome at 1440, two tabs: a column's, a line's and a donut's themes
   * with 자동 and with 초록색 chosen, while their sorts ✓ theirs).
   */
  marked?: boolean;
  /**
   * A select of its own rather than a chart list, as a number rule's signs
   * (Notion's, seen 2026-11-18 in Chrome at 1440 dpr 1, two tabs, ">" and
   * then "≥" chosen): its rows touch, 28 high in a 180 by 176 box, the
   * chosen one in the hover's ground with no ✓, none lit until a key moves
   * in — ↓ lights the first. In a phone's settings sheet, where Notion's
   * has not been seen, it stays a sheet's list: ticked, its sign a space
   * before the name.
   */
  select?: boolean;
}) {
  const sheet = useContext(PhoneSettingsListContext), plain = select && !sheet;
  const ref = useRef<HTMLDivElement>(null), searchRef = useRef<HTMLInputElement>(null);
  const search = filter?.words ?? (options.length >= CHART_SEARCH_FROM ? searchWords : undefined);
  const [query, setQuery] = useState("");
  const needle = query.trim().toLocaleLowerCase();
  const shown = !needle ? options : filter ? options.flatMap((option, at) => {
    const rank = filter.rank(option.value, needle);
    return rank === undefined ? [] : [{ option, rank, at }];
  }).sort((one, other) => one.rank - other.rank || one.at - other.at).map(({ option }) => option)
    : options.filter((option) => option.label.toLocaleLowerCase().includes(needle));
  const menu = useNativeMenu({ items: [...shown], open: true, menuRef: ref, label, keyOf: (option) => option.value,
    textOf: (option) => option.label, onAction: (option) => onPick(option.value), focusOwnerRef: search ? searchRef : undefined,
    disabledKeys: shown.filter((option) => option.disabled).map((option) => option.value),
    firstKey: (search && !filter) || plain ? undefined : shown[0]?.value });
  return <>
    {search && <div className={styles.search}>
      <input {...menu.ownerProps} ref={searchRef} value={query} placeholder={search} aria-label={search} data-chart-search={marker}
        onChange={(event) => setQuery(event.currentTarget.value)} />
    </div>}
    <div {...menu.menuProps} ref={ref} className={styles.list} data-chart-choices={marker} data-select={plain ? "true" : undefined}>
      {shown.map((option, index) => (
        <Fragment key={option.value}>
          {option.dividerBefore && index > 0 && <div className={styles.divider} role="separator" />}
          <NativeMenuItem state={menu.state} itemKey={option.value} className={styles.choice}
            disabled={option.disabled} role={marked ? "menuitemradio" : "menuitem"} aria-checked={marked ? option.value === value : undefined}
            data-chart-choice={option.value} data-chosen={plain && option.value === value ? "true" : undefined}>
            {({ labelProps }) => <>
              {option.icon !== undefined && <span className={styles.icon} aria-hidden="true">{option.icon}</span>}
              {option.sign !== undefined && !sheet && <span className={styles.sign}>{option.sign}</span>}
              <span {...labelProps} className={styles.label}>{option.sign !== undefined && sheet ? `${option.sign} ${option.label}` : option.label}</span>
              {option.trailing}
              {option.next ? <ChevronRight size="small" aria-hidden="true" />
                : marked && !plain && option.value === value ? <CheckIcon size="small" aria-hidden="true" /> : null}
            </>}
          </NativeMenuItem>
        </Fragment>
      ))}
    </div>
  </>;
}

/**
 * A list's own filter: its field's words, and where a choice stands for the
 * lowercased words typed — lower first, the list's order within a rank —
 * or none where it is left out.
 */
export type ChartChoiceFilter = { words: string; rank(value: string, query: string): number | undefined };

/**
 * A choice row inside a menu, whose choices open beside it, 200 wide, as
 * Notion's chart panel opens them beside a menu's row ("소수점 이하 자리",
 * the style options' "높이", "격자선", "축 이름", seen 2026-11-18 in Chrome
 * at 1440 dpr 1) — the number formats 250 with their own filter (`width`,
 * `filter`). A choice picked closes its list and the menu the row is in, as
 * Notion's side lists all close back to the panel (seen 2026-11-18 in Chrome
 * at 1440 dpr 1: a number's "형식 › 숫자 형식" and "› 소수점 이하 자리", as
 * the Y axis's "점수 › 평균" and the X axis's "마감 › 월별" do), but in a
 * menu that stays open (ChartMenuStaysOpen).
 */
export function ChartChoiceRow({ icon, label, value, options, disabled, marker, width = 200, filter, onPick }: {
  icon?: ReactNode; label: string; value: string; options: readonly ChartChoice[]; disabled?: boolean; marker: string;
  width?: number; filter?: ChartChoiceFilter;
  onPick(value: string): void;
}) {
  const [anchor, setAnchor] = useState<PopoverRect | null>(null);
  const closeMenu = useContext(ChartMenuClose);
  const chosen = options.find((option) => option.value === value);
  return <>
    <ChartRow icon={icon} label={label} meta={chosen?.short ?? chosen?.label} disabled={disabled}
      marker={marker} expanded={Boolean(anchor)} beside onOpen={setAnchor} />
    {anchor && (
      <ChartPopover anchor={anchor} beside label={label} width={width} marker={marker} onClose={() => setAnchor(null)}>
        <ChartChoiceList label={label} options={options} value={value} marker={marker} filter={filter}
          onPick={(next) => { setAnchor(null); onPick(next); closeMenu?.(); }} />
      </ChartPopover>
    )}
  </>;
}
