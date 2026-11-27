import { type RefObject, useEffect, useId, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  ActivityIcon, AddCircleFillIcon, ClockIcon, Search, GalleryIcon, FlagIcon, FoodIcon, LightbulbIcon, NatureIcon, Plus, ShuffleIcon,
  SmileyIcon, SymbolsIcon, TravelIcon,
} from "@/icons/leafen";
import { personValue, storePersonValue } from "@/lib/browserStorage";
import type { WorkspaceCustomEmoji } from "@/lib/customEmoji";
import { CaretPickerCell, useCaretPicker } from "@/lib/caretPicker";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { GridCellTip, IconTipButton } from "@/components/IconTipButton";
import { GROUP_BAR_TIP } from "@/lib/tipPlacement";
import { foldNfcText } from "@/shared/database/natural-order.mjs";
import {
  EMOJI_GROUPS, SKIN_TONE_STORAGE_KEY, emojiMatchRank, emojiName, emojiSearchText, emojiSections, loadEmojiCatalog, loadedEmojiCatalog,
  skinToneFrom, type EmojiCatalog, type EmojiSection, type EmojiSectionKey,
} from "./emojiCatalogModel";
import { emojiUseBonus, frequentEmojis, readEmojiUse, recordEmojiUse } from "./emojiUse";
import { CustomEmojiImage } from "./CustomEmojiImage";
import { useWorkspaceCustomEmojis } from "./useWorkspaceCustomEmojis";
import { EmojiTonePalette, TONE_HANDS } from "./EmojiTonePalette";
import { usePickerCellsDrawn } from "./pickerCellsDrawn";
import topStyles from "./EmojiPickerTop.module.css";
import styles from "./EmojiCatalogPanel.module.css";
import phoneList from "./PhonePickerList.module.css";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import { SearchClearButton } from "@/components/PageActionSearch";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

const MAX_RECENT = 24;
// A filter's matches come in one list by rank, without group names and the
// "+" last, while they and the "+" fill no more than six rows of twelve —
// in a line's ":" menu, a small card of them alone (Notion's, measured and
// its code read 2026-11-02).
const COMPACT_CELLS = 72;

const SECTION_ICONS = {
  recent: ClockIcon, people: SmileyIcon, nature: NatureIcon, food: FoodIcon, activity: ActivityIcon,
  travel: TravelIcon, objects: LightbulbIcon, symbols: SymbolsIcon, flags: FlagIcon,
} as const satisfies Record<Exclude<EmojiSectionKey, "callout">, unknown>;

/**
 * The emoji tab, as Notion's picker (seen 2026-09-30): "필터", a random pick
 * and the skin tone (✋) over the emoji — "최근 사용한 항목", then the groups
 * under their names, twelve to a row — and a bar of the groups at the foot,
 * marking the group in view. A filter keeps the groups holding a match, or
 * says "결과 없음". Each emoji is named in the reader's language; the chosen
 * tone is kept. A line's ":" menu (Notion's, seen 2026-09-30) passes the
 * text typed after ":" as the filter: it has no "필터" row, and the keys
 * typed in the line move through the emoji. Where a workspace emoji can be
 * chosen, its "사용자 지정" group ends the list and the bar ends with it and
 * "추가", as Notion's (seen 2026-10-01); "결과 없음" then offers "이모지 추가".
 * Where one can be added, the group is there even empty, a round "+" first
 * (Notion's, measured 2026-11-01).
 */
export function EmojiCatalogPanel({ offerRandom = true, filter: lineFilter, search, workspaceId, custom, suggested, onPick, onShuffle, onRemove, onClose, caretLayerRef, onLayerZIndexChange }: {
  offerRandom?: boolean;
  /** The random button's own pick, put in without joining the recent ones; whether the picker stays open is its own (PageIconPicker). Without it the random emoji is picked as any. */
  onShuffle?: (emoji: string) => void;
  /** Emoji the thing being given an icon suggests, in a row of its own after "최근 사용한 항목" (a callout's "콜아웃"). */
  suggested?: readonly string[];
  /** The workspace whose emoji use "최근 사용한 항목" and the ranks follow, as Notion's. */
  workspaceId?: string;
  /** The filter a line's ":" menu types, in place of the "필터" row. */
  filter?: string;
  /** The "필터" row's text; `waits` leaves its focus to a tap (a touch card's, TouchPickerCard), as Notion's iPad. */
  search?: { query: string; onQuery(value: string): void; waits?: boolean };
  caretLayerRef?: RefObject<HTMLElement | null>;
  onLayerZIndexChange?: (value: number | undefined) => void;
  /** The workspace's custom emoji, where they can be chosen, and adding one — none from a phone's sheet, as Notion's (seen 2026-11-14).
   * `offeredEmpty` keeps the bar's "사용자 지정" pressable with none, in the bar's grey, as Notion's phone sheet (seen 2026-11-14 at 411). */
  custom?: { workspaceId: string; refreshKey: number; onPick: (emoji: WorkspaceCustomEmoji) => void; onAdd?: (opener: HTMLElement) => void; offeredEmpty?: boolean };
  onPick: (emoji: string) => void;
  onRemove?: () => void;
  onClose: () => void;
}) {
  const { t, i18n } = useTranslation("emojiPicker");
  const panelId = useId();
  const [catalog, setCatalog] = useState<EmojiCatalog | null>(loadedEmojiCatalog);
  const [failed, setFailed] = useState(false);
  const [typedQuery, setQuery] = useState("");
  const inLine = lineFilter !== undefined;
  const query = inLine ? lineFilter : search?.query ?? typedQuery;
  // The person's own skin tone; without storage it lasts while the page is open.
  const [tone, setTone] = useState(() => skinToneFrom(personValue(SKIN_TONE_STORAGE_KEY, { previous: [SKIN_TONE_STORAGE_KEY] })));
  const [tonesOpen, setTonesOpen] = useState(false);
  const [use, setUse] = useState(() => readEmojiUse(workspaceId));
  const recent = useMemo(() => frequentEmojis(use, MAX_RECENT), [use]);
  const [current, setCurrent] = useState<EmojiSectionKey | "custom" | null>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const toneButtonRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (loadedEmojiCatalog()) return undefined;
    let current = true;
    loadEmojiCatalog().then((loaded) => { if (current) setCatalog(loaded); }, () => { if (current) setFailed(true); });
    return () => { current = false; };
  }, []);

  const knownRecent = useMemo(
    () => catalog ? recent.filter((emoji) => catalog.byEmoji.has(emoji)) : [],
    [catalog, recent],
  );
  const sections = useMemo(
    () => catalog ? emojiSections(catalog, query, knownRecent, tone, (emoji) => emojiUseBonus(use, emoji), suggested) : [],
    [catalog, knownRecent, query, tone, use, suggested],
  );
  const flat = useMemo(() => sections.flatMap((section) => section.items), [sections]);
  // How many of the groups' emoji are drawn yet, a step at a time
  // (pickerCellsDrawn.ts): a new filter starts again from its first rows.
  const { drawn, allDrawn, drawAll } = usePickerCellsDrawn(flat.length, query);
  const drawnSections = useMemo(() => {
    if (allDrawn) return sections;
    const shown: EmojiSection[] = [];
    let left = drawn;
    for (const section of sections) {
      if (left <= 0) break;
      shown.push(section.items.length <= left ? section
        : { ...section, items: section.items.slice(0, left), ranks: section.ranks?.slice(0, left) });
      left -= section.items.length;
    }
    return shown;
  }, [sections, drawn, allDrawn]);
  const { items: customEmojis, canAdd } = useWorkspaceCustomEmojis(custom?.workspaceId, custom?.refreshKey ?? 0);
  // A workspace may keep new emoji to its owners; the others see no "add".
  const onAdd = canAdd ? custom?.onAdd : undefined;
  // A workspace emoji is found by its name and the name's words, both read
  // as a filter is ("my_emoji": "my emoji", "my", "emoji", as Notion's),
  // ranked as the others.
  const customMatches = useMemo(() => {
    const filter = foldNfcText(query.trim());
    if (!custom) return [];
    return customEmojis.flatMap((emoji) => {
      const name = emojiSearchText(foldNfcText(emoji.name)) || foldNfcText(emoji.name);
      const words = name.split(" ").filter(Boolean);
      const rank = filter ? emojiMatchRank(undefined, [name, ...words], words, filter) : 0;
      return rank === undefined ? [] : [{ emoji, rank }];
    }).sort((left, right) => right.rank - left.rank);
  }, [custom, customEmojis, query]);
  const showCustom = customMatches.length > 0 || Boolean(onAdd) && !query.trim();
  const matched = flat.length + customMatches.length;
  const together = Boolean(catalog) && Boolean(query.trim()) && matched > 0 && matched + (onAdd ? 1 : 0) <= COMPACT_CELLS;
  const choices = useMemo(() => {
    const items = [
      ...drawnSections.flatMap(section => section.items.map((emoji, at) => ({ id: `${section.key}:${emoji}`, emoji,
        rank: section.ranks?.[at] ?? 0, name: emojiName(catalog?.byEmoji.get(emoji), i18n.language) || emoji }))),
      ...(allDrawn ? customMatches : []).map(({ emoji, rank }) => ({ id: `custom:${emoji.id}`, custom: emoji, rank, name: emoji.name })),
    ];
    return together ? items.sort((left, right) => right.rank - left.rank) : items;
  }, [drawnSections, allDrawn, customMatches, catalog, i18n.language, together]);
  const picker = useCaretPicker({ items: choices, query, id: `${panelId}-grid`, label: t("emoji.gridLabel"),
    listRef: scrollRef, ownerRef: inLine ? undefined : searchRef, layout: "grid", tab: "leave",
    keyOf: item => item.id, textOf: item => item.name,
    onPick: item => { if ("emoji" in item) pick(item.emoji); else custom?.onPick(item.custom); }, onClose });
  // At the caret (":"), a press beside it goes on to the page whole (pressGoesOn): the caret where pressed, and the top
  // bar's "공유" or "⋯" opening at that one press, as Notion's ":" menu (2026-11-26, Chrome, judged after the press).
  const layer = useOverlayLayer(inLine, { kind: "caret-menu", handlesUnder: true, ref: caretLayerRef ?? panelRef, onEscape: onClose, onOutsidePress: onClose,
    onCaretKeyDown: (event) => {
      // Enter on no results does nothing, the line staying as it is, as Notion's (seen 2026-11-05).
      if (fieldEnter(event) && !choices.length) { event.preventDefault(); event.stopPropagation(); return; }
      picker.onCaretKeyDown(event);
    }, inside: picker.inside, restoreFocus: false });
  useEffect(() => { if (inLine) onLayerZIndexChange?.(layer.zIndex); }, [inLine, layer.zIndex, onLayerZIndexChange]);
  // The lit emoji's name under it once the keys or the pointer moved the
  // highlight, as Notion's picker and ":" menu (seen 2026-11-14), in place
  // of the browser's own tip on the emoji under the pointer.
  const litTip = <GridCellTip gridRef={scrollRef} cellKey={picker.tipKey} label={choices[picker.active]?.name} />;
  const shownGroup = current === "custom" && showCustom
    ? "custom"
    : sections.some((section) => section.key === current) ? current : sections[0]?.key;
  const currentGroup = shownGroup === "callout" ? "recent" : shownGroup;
  // "최근" leads the bar even before anything was used, idle then, as Notion's (seen 2026-10-07).
  // A callout's "콜아웃" row has no place in the bar; in view, "최근" stands for it.
  const barKeys: readonly Exclude<EmojiSectionKey, "callout">[] = ["recent", ...EMOJI_GROUPS];

  // Filtering changes visible native groups, without moving the editor caret.
  useEffect(() => {
    if (!inLine) return;
    setCurrent(null);
    scrollRef.current?.scrollTo({ top: 0 });
  }, [inLine, lineFilter]);

  /** Scrolls the list alone, never the page around the picker. */
  function scrollListTo(element: Element | null | undefined, block: "start" | "nearest") {
    const list = scrollRef.current;
    if (!list || !element) return;
    const bounds = list.getBoundingClientRect();
    const box = element.getBoundingClientRect();
    if (block === "start" || box.top < bounds.top) list.scrollTop += box.top - bounds.top;
    else if (box.bottom > bounds.bottom) list.scrollTop += box.bottom - bounds.bottom;
  }

  // A group the bar asked for before it was drawn: shown once it is.
  const pendingGroup = useRef<string | null>(null);
  useLayoutEffect(() => {
    const key = pendingGroup.current;
    const section = key === null ? null : scrollRef.current?.querySelector(`[data-emoji-section="${key}"]`);
    if (!section) return;
    pendingGroup.current = null;
    scrollListTo(section, "start");
  });

  /** Scrolls the list to a group from the bar, drawing the whole list first where it isn't yet. */
  function showGroup(key: EmojiSectionKey | "custom") {
    setCurrent(key);
    const section = scrollRef.current?.querySelector(`[data-emoji-section="${key}"]`);
    if (section || allDrawn) {
      scrollListTo(section, "start");
      return;
    }
    pendingGroup.current = key;
    drawAll();
  }

  /** The group at the list's top edge, which the bar marks as the list scrolls. */
  function followScroll() {
    const list = scrollRef.current;
    if (!list) return;
    const top = list.getBoundingClientRect().top + 1;
    let key: EmojiSectionKey | "custom" | null = null;
    for (const section of list.querySelectorAll<HTMLElement>("[data-emoji-section]")) {
      if (key !== null && section.getBoundingClientRect().top > top) break;
      key = section.dataset.emojiSection as EmojiSectionKey | "custom";
    }
    setCurrent(key);
  }

  function filter(next: string) {
    if (search) search.onQuery(next); else setQuery(next);
    setCurrent(null);
    scrollRef.current?.scrollTo({ top: 0 });
  }

  function pick(emoji: string) {
    // Counted as the emoji itself, whatever its tone.
    setUse(recordEmojiUse(use, catalog?.byEmoji.get(emoji)?.emoji ?? emoji, workspaceId));
    onPick(emoji);
  }

  function chooseTone(next: number) {
    setTone(next);
    storePersonValue(SKIN_TONE_STORAGE_KEY, String(next));
    setTonesOpen(false);
  }

  function sectionLabel(key: EmojiSectionKey) {
    return t(`emoji.groups.${key}`);
  }

  // Each emoji is a grid cell holding no state of its own (`CaretPickerCell`).
  let index = -1;
  const emojiOption = (key: string, emoji: string) => {
    index += 1;
    const name = emojiName(catalog?.byEmoji.get(emoji), i18n.language);
    return (
      <CaretPickerCell key={key} {...picker.cell(key)} className={`${styles.emoji} ${phoneList.cell}`} data-emoji-index={index}
        aria-label={name || emoji}>{emoji}</CaretPickerCell>
    );
  };
  const customOption = (emoji: WorkspaceCustomEmoji) => {
    index += 1;
    return (
      <CaretPickerCell key={emoji.id} {...picker.cell(`custom:${emoji.id}`)}
        className={`${styles.emoji} ${phoneList.cell}`} data-emoji-index={index} aria-label={emoji.name}
        data-custom-emoji={emoji.id}><CustomEmojiImage emoji={emoji} /></CaretPickerCell>
    );
  };
  const addCell = onAdd ? (
    <span className={styles.addCell}>
      {/* Its name in the dark tip over it, as Notion's "+" in "사용자 지정" (seen 2026-11-17 at 1440). */}
      <IconTipButton className={styles.addRing} label={t("custom.add.button")}
        data-emoji-add-cell onClick={(event) => onAdd(event.currentTarget)}>
        <Plus size="small" aria-hidden="true" />
      </IconTipButton>
    </span>
  ) : null;
  const rankedOptions = () => <>
    {choices.map((item) => "emoji" in item ? emojiOption(item.id, item.emoji) : customOption(item.custom))}
    {addCell}
  </>;
  // A line's ":" menu finding nothing is one band, "결과 없음" and, where one
  // may be added, "+ 이모지 추가" at its end, as Notion's (seen 2026-11-12: a
  // 408×40 menu; Enter there does nothing, as here).
  if (inLine && catalog && sections.length === 0 && !showCustom) {
    return (
      <div ref={panelRef} data-icon-panel data-emoji-catalog data-emoji-in-line="true" data-emoji-empty="true">
        <div {...picker.listBoxProps} ref={scrollRef} className={styles.emptyBand}>
          <span className={styles.emptyBandText} role="status">{t("emoji.noMatches")}</span>
          {onAdd ? (
            <button type="button" className={styles.emptyBandAdd} data-emoji-add-empty onClick={(event) => onAdd(event.currentTarget)}>
              <Plus size="small" aria-hidden="true" />{t("custom.add.button")}
            </button>
          ) : null}
        </div>
      </div>
    );
  }
  // A line's ":" menu with a few matches is a small card of them alone.
  if (inLine && together) {
    return (
      <div ref={panelRef} data-icon-panel data-emoji-catalog data-emoji-in-line="true" data-emoji-compact="true">
        <div {...picker.listBoxProps} ref={scrollRef} className={`${styles.grid} ${styles.compactGrid}`}>
          {rankedOptions()}
        </div>
        {litTip}
      </div>
    );
  }
  return (
    <div ref={panelRef} data-icon-panel data-emoji-catalog data-emoji-in-line={inLine ? "true" : undefined}>
      {!inLine && <div className={topStyles.top}>
        <span className={topStyles.searchBox}>
          <Search className={topStyles.searchMark} size="small" aria-hidden="true" />
          <input
            ref={searchRef}
            className={topStyles.search}
            placeholder={t("emoji.searchPlaceholder")}
            value={query}
            aria-label={t("emoji.searchLabel")}
            role="combobox"
            aria-autocomplete="list"
            aria-controls={`${panelId}-grid`}
            aria-expanded="true"
            aria-activedescendant={picker.activeId}
            onChange={(event) => filter(event.target.value)}
            onKeyDown={event => picker.onCaretKeyDown(event.nativeEvent)}
            autoFocus={!search?.waits && fieldFocusOnOpen()}
          />
          {/* Notion's shared "입력 값 지우기", over the filter's end (seen 2026-11-17 at 1440). */}
          <SearchClearButton value={query} inputRef={searchRef} over onClear={() => filter("")} />
        </span>
        {/* The top row's buttons and the groups' bar name themselves in the dark
            tip 6px under them, as Notion's (seen 2026-11-17 at 1440: "랜덤",
            "피부색 선택", "사람", "이모지 추가"), not the browser's title tip;
            the top row's centred on them, the bar's from their start (GROUP_BAR_TIP). */}
        {offerRandom ? (
          <IconTipButton className={`${topStyles.iconButton} ${topStyles.outlined}`} disabled={flat.length === 0} label={t("emoji.randomLabel")}
            placement="bottom" onClick={() => { const emoji = flat[Math.floor(Math.random() * flat.length)]; if (onShuffle) onShuffle(emoji); else pick(emoji); }}>
            <ShuffleIcon size="tiny" aria-hidden="true" />
          </IconTipButton>
        ) : null}
        <span className={topStyles.toneHolder}>
          <IconTipButton ref={toneButtonRef} className={topStyles.iconButton} label={t("emoji.skinTone")}
            placement="bottom" aria-haspopup="listbox" aria-expanded={tonesOpen} data-emoji-skin-tone={tone}
            onClick={() => setTonesOpen((open) => !open)}>
            <span className={topStyles.toneHand}>{TONE_HANDS[tone]}</span>
          </IconTipButton>
          {tonesOpen ? <EmojiTonePalette tone={tone} triggerRef={toneButtonRef} label={t("emoji.skinTone")}
            nameOf={value => t(`emoji.tones.${value}`)} onPick={chooseTone} onClose={() => setTonesOpen(false)} /> : null}
        </span>
        {onRemove ? (
          <button type="button" className={topStyles.remove} onClick={onRemove} aria-label={t("remove.ariaLabel")}>
            {t("remove.label")}
          </button>
        ) : null}
      </div>}
      <div {...picker.listBoxProps} ref={scrollRef} className={`${styles.scroll} ${phoneList.list} nscroll`} onScroll={followScroll}>
        {!catalog ? (
          <div className={styles.empty} role="status">{failed ? t("emoji.loadFailed") : t("emoji.loading")}</div>
        ) : sections.length === 0 && !showCustom ? (
          <div className={styles.noMatches} role="status">
            {t("emoji.noMatches")}
            {onAdd ? (
              <button type="button" className={styles.addEmoji} data-emoji-add-empty onClick={(event) => onAdd(event.currentTarget)}>
                <Plus size="small" aria-hidden="true" />{t("custom.add.button")}
              </button>
            ) : null}
          </div>
        ) : together ? (
          <div className={`${styles.grid} ${styles.rankedGrid} ${phoneList.band}`} data-emoji-ranked>{rankedOptions()}</div>
        ) : drawnSections.map((section) => (
          <div key={section.key} role="group" aria-label={sectionLabel(section.key)} data-emoji-section={section.key} className={phoneList.group}>
            <div className={`${styles.sectionLabel} ${phoneList.label}`}>{sectionLabel(section.key)}</div>
            <div className={`${styles.grid} ${phoneList.band}`}>
              {section.items.map((emoji) => emojiOption(`${section.key}:${emoji}`, emoji))}
            </div>
          </div>
        ))}
        {catalog && showCustom && !together && allDrawn ? (
          <div role="group" aria-label={t("emoji.groups.custom")} data-emoji-section="custom" className={phoneList.group}>
            <div className={`${styles.sectionLabel} ${phoneList.label}`}>{t("emoji.groups.custom")}</div>
            <div className={`${styles.grid} ${phoneList.band}`}>
              {addCell}
              {customMatches.map(({ emoji }) => customOption(emoji))}
            </div>
          </div>
        ) : null}
      </div>
      {litTip}
      {/* The bar stays while the catalog loads and while filtering, groups
          without a match idle, and hides (keeping its room) when nothing matches. */}
      <div className={styles.groups} role="toolbar" aria-label={t("emoji.groupsLabel")}
        data-hidden={catalog && sections.length === 0 && !showCustom ? "true" : undefined}>
        {barKeys.map((key) => {
          const Icon = SECTION_ICONS[key];
          return (
            <IconTipButton key={key} className={styles.groupButton} label={sectionLabel(key)}
              placement={GROUP_BAR_TIP} aria-pressed={key === currentGroup}
              disabled={!sections.some((section) => section.key === key)} data-emoji-group={key}
              onClick={() => showGroup(key)}>
              <Icon size="wide" aria-hidden="true" />
            </IconTipButton>
          );
        })}
        {custom ? (
          <>
            <IconTipButton className={styles.groupButton} label={t("emoji.groups.custom")}
              placement={GROUP_BAR_TIP} aria-pressed={currentGroup === "custom"} disabled={!showCustom && !custom.offeredEmpty}
              data-emoji-group="custom"
              onClick={() => showGroup("custom")}>
              <GalleryIcon size="wide" aria-hidden="true" />
            </IconTipButton>
            {onAdd ? (
              <IconTipButton className={styles.groupButton} label={t("custom.add.button")}
                placement={GROUP_BAR_TIP} data-emoji-group="add" onClick={(event) => onAdd(event.currentTarget)}>
                <AddCircleFillIcon size="wide" aria-hidden="true" />
              </IconTipButton>
            ) : null}
          </>
        ) : null}
      </div>
    </div>
  );
}
