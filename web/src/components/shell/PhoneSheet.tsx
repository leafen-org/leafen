import { SwitchMark } from "@/components/Switch";
import { Children, cloneElement, createContext, isValidElement, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState, type CSSProperties, type KeyboardEvent, type PointerEvent, type ReactNode, type Ref } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { CheckIcon, ChevronLeft, iconAtSize, SheetChevronRight } from "@/icons/leafen";
import { appleTouchDevice } from "@/lib/iosViewport";
import { useOpeningPress } from "@/lib/openingPress";
import { topLayerZIndex } from "@/lib/overlayLayer";
import { holdSheetLayer, sheetLayerTop } from "@/lib/sheetLayers";
import frameStyles from "./PhoneSheet.module.css";
import rowStyles from "./PhoneSheetRows.module.css";
import { phoneSheetCloseStyles as closeStyles, PhoneSheetCloseButton } from "./PhoneSheetClose";
import headFit from "./PhoneSheetHeadFit.module.css";
import markStyles from "./PhoneSheetMarks.module.css";
import { useSheetBodyDrag, type SheetLanding } from "./sheetBodyDrag";
import { useSheetDrag } from "./sheetGripDrag";
import parts from "./PhoneSheetRowParts.module.css";
import windowStyles from "./PhoneSheetWindow.module.css";
import { useInSheetWindow } from "./sheetWindow";
import { followSheetFocus } from "./sheetFocusReturn";
import { followKeyboardShortening } from "@/lib/softKeyboard";
import { mergeStyleModules } from "@/lib/styleModules";
import { imeComposing } from "@/lib/fieldKeyBoundary";

// The sheet's frame and its groups and rows, one style object as the
// stylesheet they were split from.
const styles = mergeStyleModules(frameStyles, rowStyles);

/** What a screen inside a sheet says its head is, in place of the sheet's own. */
type SheetHead = { title?: ReactNode; onBack?(): void };

/**
 * How a sheet's rows are drawn: a menu of actions, as Notion's "작업", its
 * icons in the icons' dark and "›" in their second grey; or a database's
 * settings and filters, as Notion's "필터" and "설정" (seen 2026-11-11 at
 * 402pt, light and dark), its icons in the words' colour and "›" in the third
 * grey; or a view's options, "보기 옵션" and "데이터 소스 작업", its icons in
 * the words' colour and "›" in the second grey (seen 2026-11-15: A90 Chrome
 * light rgb(44,44,43) and rgb(142,139,134), iPhone dark rgb(240,239,237)). A
 * sheet opened over another takes the one under it.
 */
type SheetTone = "actions" | "settings" | "views";

/**
 * What a bottom list's rows lead with, which sets their mark's box and colour
 * (PhoneSheetMarks.module.css): a menu's 22px tools, or a list's marks at
 * their own size, by default; a database's properties, a 22px mark in a 26px
 * box; pages and data sources, each mark at its own size (a page's 24px).
 */
type SheetMarks = "properties" | "pages";

const SheetContext = createContext<{ layer: number; tone: SheetTone; marks?: SheetMarks; nameHead(key: symbol, head: SheetHead | null): void } | null>(null);

/**
 * A screen drawn inside a sheet names the sheet's head while it shows, as
 * Notion's "고급 필터" in its "필터 및 정렬" sheet (seen 2026-10-21): its title
 * and its "‹" back; null leaves the sheet's own. Named before the first
 * paint, so the sheet never shows its own title first. A screen opened inside
 * another names it over that one's, which comes back as it goes (Notion's "‹ 속성
 * 유형 변경" inside the view settings' "‹ 속성 편집", seen 2026-11-17). Whether it is
 * drawn inside a sheet: a screen that heads itself elsewhere heads the sheet here.
 */
export function usePhoneSheetHead(head: SheetHead | null) {
  const sheet = useContext(SheetContext);
  const [key] = useState(() => Symbol("sheet head"));
  const back = useRef(head?.onBack);
  back.current = head?.onBack;
  const title = head?.title, shown = head !== null, backs = Boolean(head?.onBack);
  useLayoutEffect(() => {
    sheet?.nameHead(key, shown ? { title, onBack: backs ? () => back.current?.() : undefined } : null);
  }, [sheet, key, shown, title, backs]);
  useLayoutEffect(() => () => sheet?.nameHead(key, null), [sheet, key]);
  return sheet !== null;
}

// Open sheets, the last on top. An Escape nothing inside them took closes
// the top one, as Notion's sheets do with the focus still on their opener
// (2026-10-27: "작업" and "모든 토론" stayed open under the key); where the
// focus goes as it closes, sheetFocusReturn.ts says.
const openSheets: Array<{ close(): void }> = [];
let listening = false;

function escapeTopSheet(event: globalThis.KeyboardEvent) {
  if (event.key !== "Escape" || event.defaultPrevented || imeComposing(event)) return;
  const top = openSheets.at(-1);
  if (!top) return;
  event.preventDefault();
  top.close();
}

/** Whether a sheet is open: an Escape nothing inside takes is then the top sheet's (escapeTopSheet). */
export const phoneSheetOpen = () => openSheets.length > 0;

function useSheetEscape(onClose: () => void) {
  const close = useRef(onClose);
  close.current = onClose;
  const own = useRef<{ close(): void } | null>(null);
  useEffect(() => {
    const entry = { close: () => close.current() };
    own.current = entry;
    const giveFocusBack = followSheetFocus();
    openSheets.push(entry);
    if (!listening) {
      listening = true;
      document.addEventListener("keydown", escapeTopSheet);
    }
    return () => {
      const at = openSheets.indexOf(entry);
      if (at >= 0) openSheets.splice(at, 1);
      giveFocusBack();
    };
  }, []);
  return {
    // Under another sheet, an Escape is the top one's even with the focus here.
    onTop: () => openSheets.at(-1) === own.current,
    overAnother: () => own.current !== null && openSheets.indexOf(own.current) > 0,
  };
}

/**
 * A sheet's shade, as Notion's (seen 2026-11-14 over a column's menu with
 * "속성 편집", "유형 변경" or "표 집계" › "수" on it, its events logged): a
 * sheet over another closes as the finger lifts from its shade, and the
 * press's click goes where the browser sends it then. An iPhone's or iPad's
 * sends it some 140ms later to what is under the shade by then: a row of the
 * sheet under it, which acts, or that sheet's own shade, which closes it too
 * (402pt: a press beside both closed both, one on the menu left it and its
 * row acted, three sheets closed to the menu). An Android phone's sends it
 * within a few ms to the shade the touch began on, which Notion's still has,
 * so only the top sheet closes (Galaxy A90: the menu stayed, its row did
 * nothing): there the shade closes its sheet on the click. The lowest sheet's
 * shade closes it on the click, keeping the press from the page, as a
 * mouse's press closes the top sheet alone.
 */
function useShadePress(onClose: () => void, overAnother: () => boolean) {
  const closed = useRef(false);
  // The long press that opened the sheet ends on its shade (useOpeningPress).
  const opening = useOpeningPress();
  return {
    onPointerUp(event: PointerEvent<HTMLButtonElement>) {
      if (event.pointerType === "mouse" || !appleTouchDevice() || !overAnother() || opening()) return;
      closed.current = true;
      onClose();
    },
    onClick() {
      if (closed.current) closed.current = false;
      else if (!opening()) onClose();
    },
  };
}

/**
 * The layer a sheet opened from inside another goes over; one opened from a
 * window or panel drawn in a layer goes just over that (2026-11-11: a touch
 * screen's inbox row menu went under the inbox panel beside the sidebar).
 * Shown, it holds that layer, which a window, menu or sheet opened after it
 * goes over (sheetLayers.ts).
 */
function useSheetLayer(zIndex: number | undefined) {
  const parent = useContext(SheetContext);
  const [overLayers] = useState(() => {
    const top = sheetLayerTop(topLayerZIndex() ?? -1);
    return top < 0 ? undefined : top + 1;
  });
  const layer = zIndex ?? (parent ? parent.layer + 1 : overLayers);
  useLayoutEffect(() => layer === undefined ? undefined : holdSheetLayer(layer), [layer]);
  return layer;
}

/**
 * A phone's menu as Notion's bottom sheet (seen 2026-10-20 and 2026-10-21 at
 * 375px): it rises from the bottom over a shade, full width, its top
 * corners 12px round on the soft page colour, with a grip (an iPhone's and
 * Android's differ, as Notion's: PhoneSheet.module.css) and a 44px
 * head in three even parts — a dark "‹" back at its left and a blue action
 * ("완료") at its right where it has them, its title (16px/600) cut short in
 * the middle one — then its rows in white 12px-round groups 16px in
 * (`PhoneSheetGroup`, `PhoneSheetRow`). A choice or a short list opens over
 * it as a half sheet, its top 16px under the screen's middle (10px on an
 * iPhone), opening out to a whole sheet when pushed up (`size="half"`);
 * a longer pick, such as a property or a filter's options, as a whole screen
 * of flat white rows under a plain head (`size="screen"`). A whole sheet over
 * another stands at the same top, hiding it but for one more shade (Notion's
 * "필터 없음" › "날짜", "설정" › "필터" › "날짜" and "모든 토론" › "필터", seen
 * 2026-11-17: iPhone 72pt on the screen, Galaxy A90 16 in the page). The shade
 * (useShadePress) and an Escape nothing inside took close it (an Escape the
 * top one of several); some also have a round ✕ at the head's right
 * (`closeButton`).
 */
export function PhoneSheet({ title, label, size = "full", head = true, start, titleFit, titleLarge, headEven, plain, closeButton, tone, sheetRef, zIndex, onClose, onBack, action, foot, children, onKeyDown, data, bodyClassName }: {
  title: ReactNode;
  /** How its rows are drawn (SheetTone); left out, the sheet under it's, or a menu of actions'. */
  tone?: SheetTone;
  /** A head as Notion's "공유 설정" and the sheets it opens draw theirs: on a whole sheet 54px (56 with "‹"), its title a 22px line at the top of the one "‹" and "완료" stand on, or 12px down alone (PhoneSheet.module.css); elsewhere the title on the head's top line, as its other sheets'. */
  headEven?: boolean;
  /** White, its rows flat across it, as Notion's "모든 토론" (seen 2026-10-27). */
  plain?: boolean;
  /** The title 17px/700, as Notion's "작업" sheet (seen 2026-10-27; its other sheets 16px/600). */
  titleLarge?: boolean;
  /** A title past its third: "spills" runs on past it uncut, where Leafen's letters run wider than Notion's (the account sheet, "적용할 페이지 위치"); "shares" shares the head by what each part holds (PhoneSheetHeadFit.module.css). */
  titleFit?: "spills" | "shares";
  /** Whether it draws its head; a screen that heads itself (with its own "‹") says false. */
  head?: boolean;
  /** Its name for assistive technology, when the title is not text alone. */
  label?: string;
  /** "half" for a sheet over another, as Notion's choices and short lists; "screen" for a whole screen's pick. */
  size?: "full" | "half" | "screen";
  /** The head's left side in place of "‹", such as "⋯" or "뒤로가기". */
  start?: ReactNode;
  /** A round ✕ at the head's right (PhoneSheetClose): the share sheets' 32px one, or "small", the discussions' 24px one ("모든 토론", "필터"). */
  closeButton?: boolean | "small";
  sheetRef?: Ref<HTMLDivElement>;
  zIndex?: number;
  onClose(): void;
  /** "‹" back to the sheet it came from. */
  onBack?(): void;
  /** The head's right action, such as "완료"; a disabled one is faint, as Notion's "저장" before there is anything to save. */
  action?: { label: string; onPress(): void; disabled?: boolean };
  /** A line across the sheet's foot that stays while the body scrolls, as Notion's "옮기기" workspace (seen 2026-10-31). */
  foot?: ReactNode;
  children: ReactNode;
  onKeyDown?(event: KeyboardEvent<HTMLDivElement>): void;
  /** The sheet's data attributes, as the popover it stands for had them. */
  data?: Record<`data-${string}`, string | undefined>;
  /** Its body's own layout in place of the rows' padding, as a picker that scrolls itself (PhoneEmojiSheet). */
  bodyClassName?: string;
}) {
  const { t } = useTranslation("common");
  const { onTop, overAnother } = useSheetEscape(onClose);
  const shadePress = useShadePress(onClose, overAnother);
  // A half sheet opens out to a whole one when pushed up, as Notion's (useSheetDrag).
  const [expanded, setExpanded] = useState(false);
  const halfOpen = size === "half" && !expanded;
  // A half sheet let go opens out or stands half way again, where its drag lands (followSheetDrag).
  const settle = size === "half" ? (landing: SheetLanding) => setExpanded(landing === "open") : undefined;
  const dragDown = useSheetDrag(onClose, settle, halfOpen);
  // Inside a wider touch screen's middle window, all but a half sheet is a window of its own there (sheetWindow.ts).
  const windowed = useInSheetWindow() && size !== "half";
  const edgeDrag = size !== "screen" && !halfOpen && !windowed ? dragDown : {};
  // Its rows pull it down too, once they stand at their top (sheetBodyDrag.ts).
  const bodyRef = useRef<HTMLDivElement>(null);
  useSheetBodyDrag(bodyRef, size !== "screen" && !halfOpen && !windowed, onClose, settle);
  // A keyboard that shortens the window leaves a half sheet the height it had without one (PhoneSheet.module.css).
  useLayoutEffect(() => size === "half" && !appleTouchDevice() ? followKeyboardShortening(bodyRef.current?.parentElement) : undefined, [size]);
  // The heads its screens name, the one named last shown (usePhoneSheetHead).
  const [heads, setHeads] = useState<readonly { key: symbol; head: SheetHead }[]>([]);
  const nameHead = useCallback((key: symbol, head: SheetHead | null) => setHeads((named) => {
    const rest = named.filter((entry) => entry.key !== key);
    if (!head) return rest.length === named.length ? named : rest;
    return rest.length === named.length ? [...named, { key, head }] : named.map((entry) => entry.key === key ? { key, head } : entry);
  }), []);
  const inner = heads.at(-1)?.head;
  const layerIndex = useSheetLayer(zIndex);
  const layer: CSSProperties | undefined = layerIndex === undefined ? undefined : { zIndex: layerIndex };
  const shownTitle = inner?.title ?? title, back = inner?.onBack ?? onBack;
  const underTone = useContext(SheetContext)?.tone;
  const rowTone = tone ?? underTone ?? "actions";
  const context = useMemo(() => ({ layer: layerIndex ?? 1000, tone: rowTone, nameHead }), [layerIndex, rowTone, nameHead]);
  // An iPhone's or iPad's sheet stands higher, with a narrower grip, as Notion's there (PhoneSheet.module.css).
  const apple = appleTouchDevice() ? "true" : "false";
  const closes = closeButton && !action;
  const sheet = (
    <SheetContext.Provider value={context}>
      {/* Over a modal window (the share window on a phone) a sheet is the modal's own top layer: its page-wide inert leaves it alone. */}
      <button type="button" className={windowed && size === "full" ? `${styles.shade} ${windowStyles.goesOn}` : styles.shade} style={layer} tabIndex={-1}
        aria-label={t("actions.close")} {...shadePress} data-phone-sheet-shade data-press-keeps-focus
        data-react-aria-top-layer="" data-apple={apple} />
      <div ref={sheetRef} className={windowed ? `${styles.sheet} ${windowStyles.window}` : styles.sheet} style={layer} role="dialog"
        data-react-aria-top-layer="" data-apple={apple} data-sheet-window={windowed ? "true" : undefined}
        aria-label={label ?? (typeof title === "string" ? title : undefined)}
        tabIndex={-1} data-phone-sheet={size} data-expanded={expanded ? "true" : undefined} data-plain={plain ? "true" : undefined}
        data-tone={rowTone} {...data}
        {...(halfOpen ? dragDown : {})}
        // A key in a sheet opened over this one reaches it through React's tree alone: it is that sheet's.
        onKeyDown={onKeyDown ? (event) => {
          if (event.currentTarget.contains(event.target as Node) && (event.key !== "Escape" || onTop())) onKeyDown(event);
        } : undefined}>
        {size !== "screen" && !windowed ? <div className={styles.grip} aria-hidden="true" {...edgeDrag} /> : null}
        {head ? <div className={[styles.head, closes ? closeStyles.head : "", titleFit ? headFit[titleFit] : ""].join(" ")} data-title-fit={titleFit}
          data-title-large={titleLarge ? "true" : undefined} data-close={closes ? "true" : undefined}
          data-even={headEven ? "true" : undefined} data-back={!start && back ? "true" : undefined} {...edgeDrag}>
          <span className={styles.headSide}>
            {start ?? (back ? (
              <button type="button" className={`${styles.headButton} ${styles.headBack}`} aria-label={t("actions.back")} data-phone-sheet-back onClick={back}>
                <ChevronLeft size="regular" aria-hidden="true" />
              </button>
            ) : null)}
          </span>
          <strong className={closes ? `${styles.title} ${closeStyles.title}` : styles.title}>{shownTitle}</strong>
          <span className={`${styles.headSide} ${styles.headEnd}`}>
            {action ? (
              <button type="button" className={styles.headButton} data-phone-sheet-action disabled={action.disabled} onClick={action.onPress}>{action.label}</button>
            ) : closes ? <PhoneSheetCloseButton onClose={onClose} small={closeButton === "small"} /> : null}
          </span>
        </div> : null}
        <div ref={bodyRef} className={[styles.body, bodyClassName, windowed ? windowStyles.rows : ""].filter(Boolean).join(" ")}>{children}</div>
        {foot ? <div className={styles.foot}>{foot}</div> : null}
      </div>
    </SheetContext.Provider>
  );
  return typeof document === "undefined" ? sheet : createPortal(sheet, document.body);
}

/** A head's text button, as a screen's blue "뒤로가기" (seen 2026-10-21). */
export function PhoneSheetHeadButton({ label, onPress, children }: { label: string; onPress(): void; children?: ReactNode }) {
  return (
    <button type="button" className={styles.headButton} aria-label={label} data-phone-sheet-start onClick={onPress}>{children ?? label}</button>
  );
}

/**
 * A short list at the screen's bottom over a shade, as Notion's "정렬 추가"
 * and a filter's "⋯" (seen 2026-10-21): white, square, as tall as its rows,
 * without a head; inside a wider touch screen's middle window, a window of
 * its own there, as Notion's iPad "정렬 추가" (sheetWindow.ts). A setting's
 * choice with a grip is a whole sheet instead (PhoneSheetChoices).
 */
export function PhoneSheetMenu({ label, zIndex, marks, className, onClose, children }: {
  label: string; zIndex?: number;
  marks?: SheetMarks;
  className?: string;
  onClose(): void; children: ReactNode;
}) {
  const { t } = useTranslation("common");
  useSheetEscape(onClose);
  const opening = useOpeningPress();
  const layerIndex = useSheetLayer(zIndex);
  const layer: CSSProperties | undefined = layerIndex === undefined ? undefined : { zIndex: layerIndex };
  const tone = useContext(SheetContext)?.tone ?? "actions";
  const context = useMemo(() => ({ layer: layerIndex ?? 1000, tone, marks, nameHead: () => undefined }), [layerIndex, tone, marks]);
  const windowed = useInSheetWindow();
  // A short list goes over a darker shade than a sheet, as Notion's on both phones (PhoneSheet.module.css).
  const menu = (
    <SheetContext.Provider value={context}>
      <button type="button" className={`${styles.shade} ${styles.menuShade}`} style={layer} tabIndex={-1} aria-label={t("actions.close")}
        onClick={() => { if (!opening()) onClose(); }} data-phone-sheet-shade data-press-keeps-focus data-react-aria-top-layer="" />
      <div className={[styles.menu, className, windowed ? `${windowStyles.window} ${windowStyles.rows}` : ""].filter(Boolean).join(" ")} style={layer}
        role="dialog" aria-label={label} data-phone-sheet-menu data-marks={marks} data-react-aria-top-layer="" data-sheet-window={windowed ? "true" : undefined}
        onKeyDown={(event) => { if (event.key === "Escape") { event.stopPropagation(); onClose(); } }}>
        {children}
      </div>
    </SheetContext.Provider>
  );
  return typeof document === "undefined" ? menu : createPortal(menu, document.body);
}

/**
 * A white 12px-round group of a sheet's rows; `fit`, a menu whose rows take
 * their height from their words, as Notion's "사용 권한 변경" (PhoneSheet.module.css).
 */
export function PhoneSheetGroup({ children, className, label, fit }: { children: ReactNode; className?: string; label?: ReactNode; fit?: boolean }) {
  const group = <div className={`${styles.group} ${className ?? ""}`} data-phone-sheet-group data-rows={fit ? "fit" : undefined}>{children}</div>;
  // A group's grey heading, as "필터 1" over its rule (seen 2026-10-21).
  return label === undefined ? group : (
    <section className={parts.section}>
      <h3 className={parts.groupLabel}>{label}</h3>
      {group}
    </section>
  );
}

/**
 * A row's ✓ drawn in the 18px box Notion's phone sheets draw theirs in
 * (checkmarkSmall, seen 2026-11-15 at 402pt in "N개 보기" and "다음과 같이
 * 표시"), whatever size its screen asked for; a ✓ inside a wrapping element
 * is redrawn there, and any other value is left as it is.
 */
function tickAtSheetSize(value: ReactNode): ReactNode {
  if (!isValidElement<{ children?: ReactNode; size?: string }>(value)) return value;
  if (value.type === CheckIcon) return cloneElement(value, { size: "page" });
  if (typeof value.type !== "string" || value.props.children === undefined) return value;
  return cloneElement(value, undefined, Children.map(value.props.children, tickAtSheetSize));
}

/**
 * A sheet's 45px row: its icon drawn at 22px (a bottom list of pages' at
 * its own size, PhoneSheetMenu's marks), its label, a grey value and
 * an 18px "›" where it opens more, each drawn at its size rather than blown
 * up (iconAtSize), their lines as thin as Notion's; an adding row ("+ 새 템플릿") is gray throughout (`muted`), a
 * removing one red (`danger`); a row without an icon may still start where
 * the others' labels do (`indent`), and one may say more under its label.
 * A row that ticks starts with Notion's box (`checked`), a choice under
 * another's 8px further in (`sub`).
 */
export function PhoneSheetRow({ icon, label, value, more = true, muted, danger, indent, small, checked, sub, description, disabled, ownMark, onPress, data }: {
  icon?: ReactNode;
  /** Its icon at its own size, 2px into a 26px box, the label at 64: a view in a source's list (PhoneSheet.module.css). */
  ownMark?: boolean;
  label: ReactNode;
  /** A 16px label, as Notion's discussion filter and a filter value's short choices (measured 2026-11-17 on iPhone Safari at 402 and Galaxy Chrome at 411; its path sheet keeps the sheets' 17px on both). */
  small?: boolean;
  /** Its tick, as Notion's discussion filter (seen 2026-10-27); "mixed" where only some under it are. */
  checked?: boolean | "mixed";
  sub?: boolean;
  value?: ReactNode;
  more?: boolean;
  muted?: boolean;
  danger?: boolean;
  indent?: boolean;
  description?: ReactNode;
  disabled?: boolean;
  onPress(): void;
  data?: Record<`data-${string}`, string | undefined>;
}) {
  // A list of pages draws each mark at the size its row gave it (a page's 24px, "더 보기"'s 22); others at 22.
  const ownSize = useContext(SheetContext)?.marks === "pages";
  return (
    <button type="button" className={styles.row} disabled={disabled} data-muted={muted ? "true" : undefined} data-danger={danger ? "true" : undefined}
      role={checked === undefined ? undefined : "checkbox"} aria-checked={checked} data-sub={sub ? "true" : undefined}
      data-small={small ? "true" : undefined} data-described={description ? "true" : undefined} data-marked={icon || indent ? "true" : undefined} data-own-mark={ownMark ? "true" : undefined}
      data-bare={icon || indent || checked !== undefined ? undefined : "true"} onClick={onPress} {...data}>
      {checked !== undefined ? (
        <span className={parts.rowCheck} data-on={checked === false ? undefined : String(checked)} aria-hidden="true">
          {checked === true ? <CheckIcon size="small" /> : null}
        </span>
      ) : null}
      {icon || indent ? <span className={`${styles.rowIcon} ${markStyles.mark}`} aria-hidden="true">{ownSize || ownMark ? icon : iconAtSize(icon, "wide")}</span> : null}
      {description ? (
        <span className={parts.rowText}>
          <span className={styles.rowLabel}>{label}</span>
          <span className={parts.rowDescription}>{description}</span>
        </span>
      ) : <span className={styles.rowLabel}>{label}</span>}
      {value !== undefined ? <span className={parts.rowValue}>{tickAtSheetSize(value)}</span> : null}
      {more ? <SheetChevronRight size="page" className={parts.rowMore} aria-hidden="true" /> : null}
    </button>
  );
}

/** A sheet row's switch, as Notion's phone "페이지 잠금" (seen 2026-10-27): 51×31, blue when on. */
/** A sheet row's switch: the app's one switch, in its phone size. */
export function PhoneSheetSwitch({ on }: { on: boolean }) {
  return <SwitchMark on={on} />;
}

// The sheet's classes and its rows' parts, for screens that lay their own
// rows in a sheet.
export const phoneSheetStyles: Readonly<Record<string, string>> = { ...styles, ...parts };
