import { memo, useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { CountChevronDown, PersonCircleIcon, X, XCircleIcon } from "@/icons/leafen";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { personLabel } from "@/components/database/people";
import { useProductPages } from "@/components/product/productPagesContext";
import { usePeopleDirectoryRevision } from "@/lib/workspacePeopleDirectory";
import { popoverPlacementStyle, usePopoverPlacement, type PopoverRect } from "@/lib/popoverPlacement";
import type { WorkspacePerson } from "@/lib/workspacePeopleChoices";
import type { AutomationAction, AutomationValueExpression, EditPropertyAutomationAction } from "@/lib/types";
import { AutomationChoicePopover } from "./AutomationChoicePopover";
import type { AutomationEditorSurface } from "./automationActionModel";
import tipStyles from "@/components/IconTipButton.module.css";
import { useTipPress } from "@/lib/tipTiming";
import styles from "./AutomationPersonValue.module.css";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";
import { type PickerWindowLine, pickerWindowBox, pickerWindowLine, usePickerWindow } from "@/lib/usePickerWindow";

/** Who a person value names on each run rather than by name: who runs it, and who made the page. */
export type AutomationPersonSource = "execution_actor" | "page_creator";
/** A person value's people: someone by their ID, or a source. */
export type AutomationPersonItem = string | { source: AutomationPersonSource };
/** How a person or multi-select value meets what the property holds: in its place, or added to it, taken from it or toggled in it. */
export type AutomationValueMode = "replace" | "add" | "remove" | "toggle";

const SOURCES: readonly AutomationPersonSource[] = ["execution_actor", "page_creator"];
export const AUTOMATION_VALUE_MODES: readonly AutomationValueMode[] = ["replace", "add", "remove", "toggle"];
const itemKey = (item: AutomationPersonItem) => typeof item === "string" ? `person:${item}` : `source:${item.source}`;

/** A person value's people as written, sources among them. */
export function automationPeopleOf(value: AutomationValueExpression): AutomationPersonItem[] {
  if (value.kind !== "literal" || !Array.isArray(value.value)) return [];
  return value.value.flatMap((item): AutomationPersonItem[] => typeof item === "string" ? [item]
    : item && typeof item === "object" && SOURCES.includes((item as { source?: AutomationPersonSource }).source!)
      ? [{ source: (item as { source: AutomationPersonSource }).source }] : []);
}

export function automationValueModeOf(value: AutomationValueExpression): AutomationValueMode {
  return value.kind === "literal" && value.mode ? value.mode : "replace";
}

/** A value written out in its mode: none said for "replace". */
export function automationLiteralInMode(value: unknown, mode: AutomationValueMode): AutomationValueExpression {
  return { kind: "literal", value, ...(mode === "replace" ? {} : { mode }) };
}

/**
 * A list's items with the language's words between them ("A, B 및 C"), as
 * Notion's card sentences list people and options (seen 2026-11-11): a comma
 * kept right after its item, the words' spaces their own (" 및 ").
 */
export function ListedItems({ items }: { items: readonly ReactNode[] }) {
  const { i18n } = useTranslation();
  const pieces: { node: ReactNode; comma?: boolean }[] = [];
  for (const part of new Intl.ListFormat(i18n.language, { type: "conjunction" }).formatToParts(items.map((_, index) => `\u0000${index}`))) {
    if (part.type === "element") { pieces.push({ node: items[Number(part.value.slice(1))] }); continue; }
    const last = pieces.at(-1);
    const comma = /^\s*[,，、]/u.test(part.value);
    if (last && comma) last.comma = true;
    const rest = comma ? part.value.replace(/^\s*[,，、]/u, "") : part.value;
    if (rest) pieces.push({ node: rest });
  }
  return <>{pieces.map((piece, index) => typeof piece.node === "string" ? <span key={index}>{piece.node}</span>
    : <span key={index} className={styles.item}>{piece.node}{piece.comma ? "," : null}</span>)}</>;
}

/** A list row's 14px box in its 20px cell, blue with a white ✓ once ticked (Notion's, seen 2026-11-11). */
export function PickBox({ checked }: { checked: boolean }) {
  return <span className={styles.boxCell} aria-hidden="true">
    <span className={styles.box} data-checked={checked || undefined} />
  </span>;
}

/**
 * "다음으로 교체… ⌄" and its menu — "추가…", "제거…", "토글…" — 180 wide 4px
 * under it, opening with no row lit (Notion's, seen 2026-11-11); its ⌄ the
 * thin one as wide as its ink, 8.64 by 14, 6 after the words, 2 in at each
 * side (93.95 with "다음으로 교체…", 49.06 with "추가…", seen 2026-11-22).
 */
export function ValueModeButton({ mode, onPick }: { mode: AutomationValueMode; onPick(mode: AutomationValueMode): void }) {
  const { t } = useTranslation("databaseView");
  const [open, setOpen] = useState(false);
  const button = useRef<HTMLButtonElement>(null);
  return <>
    <button ref={button} type="button" className={styles.mode} aria-haspopup="menu" aria-expanded={open} data-automation-value-mode={mode}
      onClick={() => setOpen(true)}>{t(`peopleModes.${mode}`)}<CountChevronDown size="tiny" trim aria-hidden="true" /></button>
    {open ? <AutomationChoicePopover label={t(`peopleModes.${mode}`)} width={180} quiet trigger={button.current} onClose={() => setOpen(false)}
      choices={AUTOMATION_VALUE_MODES.map((key) => ({ key, text: t(`peopleModes.${key}`) }))}
      onPick={(key) => onPick(key as AutomationValueMode)} /> : null}
  </>;
}

/** Who runs it as Notion names them: "실행한 모든 사람" in a database's automation, "클릭한 모든 사람" on a button. */
function useSourceName(surface: AutomationEditorSurface | undefined) {
  const { t } = useTranslation("databaseView");
  return useCallback((source: AutomationPersonSource) => source === "execution_actor" && (surface ?? "event_automation") === "event_automation"
    ? t("recipientEventActor") : t(`recipientSource_${source}`), [surface, t]);
}

/** The workspace's people one may pick, loaded as the window opens. */
function useWorkspacePeople() {
  const load = useProductPages()?.workspacePeople;
  const [people, setPeople] = useState<WorkspacePerson[]>([]);
  useEffect(() => {
    if (!load) return;
    const controller = new AbortController();
    load(controller.signal).then((result) => { if (!controller.signal.aborted) setPeople(result); }, () => {});
    return () => controller.abort();
  }, [load]);
  return people;
}

/**
 * A person value in a card's sentence, as Notion's (seen 2026-11-11): each
 * person by a 20px face and name, then who runs it and the page's creator in
 * blue tags, in the language's list ("〈사람〉, [실행한 모든 사람] 및 [페이지
 * 작성자]"); `small` as a line's field shows them, 16px faces in 12px words.
 */
export function PersonValueChips({ items, surface, small = false }: {
  items: readonly AutomationPersonItem[]; surface?: AutomationEditorSurface; small?: boolean;
}) {
  usePeopleDirectoryRevision();
  const sourceName = useSourceName(surface);
  const people = items.filter((item): item is string => typeof item === "string");
  const sources = SOURCES.filter((source) => items.some((item) => typeof item !== "string" && item.source === source));
  const chips: ReactNode[] = [
    ...people.map((id) => {
      const name = personLabel(id, undefined, false) || id;
      return <span key={id} className={styles.person} data-small={small || undefined}>
        <PersonAvatar userId={id} name={name} size={small ? 16 : 20} />{name}</span>;
    }),
    ...sources.map((source) => <span key={source} className={styles.source} data-small={small || undefined}>
      <PersonCircleIcon size="tiny" weight="fill" aria-hidden="true" />{sourceName(source)}</span>),
  ];
  if (small) return <span className={styles.smallChips}>{chips}</span>;
  // The list's words between them as the sentence's words.
  return <ListedItems items={chips} />;
}

/**
 * A person value's card in its tip, as Notion's (seen 2026-11-11): whatever
 * its mode, "담당자 속성을 〈사람〉, 이 자동화를 실행하는 조건을 수행한 사람 및
 * 페이지를 만든 사용자 값으로 설정" — each person by name, who runs it and
 * the page's creator by what they are.
 */
export function usePersonCardWords(surface: AutomationEditorSurface | undefined) {
  const { t, i18n } = useTranslation("databaseView");
  const sourceName = useSourceName(surface);
  return (property: string, items: readonly AutomationPersonItem[]) => {
    const names = [
      ...items.filter((item): item is string => typeof item === "string").map((id) => personLabel(id, undefined, false) || id),
      ...SOURCES.filter((source) => items.some((item) => typeof item !== "string" && item.source === source))
        .map((source) => source === "page_creator" || (surface ?? "event_automation") === "event_automation"
          ? t(`personSourceTips.${source}`) : sourceName(source)),
    ];
    return t("editPropertySentence", { property, value: new Intl.ListFormat(i18n.language, { type: "conjunction" }).format(names) });
  };
}

/**
 * A person property's value, as Notion's window (seen 2026-11-11): a field
 * holding those picked — each person by face and name, who runs it and the
 * page's creator in blue tags, each with "×", "⊗" taking all back — and
 * "한 명 이상의 사람 검색"; under it "다음으로 교체… ⌄" ("추가…", "제거…",
 * "토글…"); then "실행한 모든 사람" and "페이지 작성자", each with its tip
 * beside the window, and the workspace's people by a box ticked once picked.
 * A row picks or takes back; Enter picks the lit one, Backspace takes back the
 * last. A page line's (`line`) has no mode, and those picked leave its list.
 */
export function AutomationPersonValues({ action, surface, line = false, onChange }: {
  action: EditPropertyAutomationAction; surface?: AutomationEditorSurface; line?: boolean;
  onChange(action: AutomationAction): void;
}) {
  const { t } = useTranslation("databaseView");
  const directory = usePeopleDirectoryRevision();
  const people = useWorkspacePeople();
  const sourceName = useSourceName(surface);
  const [query, setQuery] = useState("");
  const [lit, setLit] = useState(0);
  // A source's tip beside the window, for the row the pointer is on; gone as the rows change under it.
  const [tip, setTip] = useState<{ key: string; anchor: PopoverRect; label: string } | null>(null);
  const input = useRef<HTMLInputElement>(null);
  const items = useMemo(() => automationPeopleOf(action.value), [action.value]);
  const mode = automationValueModeOf(action.value);
  const latest = useRef({ action, items, mode });
  latest.current = { action, items, mode };
  const set = (next: AutomationPersonItem[], nextMode = latest.current.mode) => onChange({ ...latest.current.action,
    value: automationLiteralInMode(next, nextMode) });
  const chosenKeys = useMemo(() => new Set(items.map(itemKey)), [items]);
  const toggle = (item: AutomationPersonItem) => {
    const current = latest.current.items;
    set(current.some((candidate) => itemKey(candidate) === itemKey(item))
      ? current.filter((candidate) => itemKey(candidate) !== itemKey(item)) : [...current, item]);
    setQuery("");
    setTip(null);
    input.current?.focus({ preventScroll: true });
  };
  const words = query.trim().toLocaleLowerCase();
  // Each person found by their ID at once, not by a look through all of them.
  const byId = useMemo(() => new Map(people.map((person) => [person.id, person])), [people]);
  const nameOf = useCallback((item: AutomationPersonItem) => typeof item === "string"
    ? byId.get(item)?.name ?? (personLabel(item, undefined, false) || item) : sourceName(item.source),
  // eslint-disable-next-line react-hooks/exhaustive-deps -- a name the directory learns names the row anew
  [byId, sourceName, directory]);
  // A schedule has no one running it and no page to have made: its people are
  // named (as its recipients, AutomationRecipientsField). The rows and their
  // names are read again only as the people, the words or the picks change,
  // not as the highlight moves.
  const rows = useMemo(() => [
    ...(surface === "schedule_automation" ? [] : SOURCE_ITEMS),
    ...people.map((person) => person.id),
  ].filter((item) => (!line || !chosenKeys.has(itemKey(item))) && nameOf(item).toLocaleLowerCase().includes(words)),
  [surface, people, line, chosenKeys, nameOf, words]);
  const rowNames = useMemo(() => rows.map(nameOf), [rows, nameOf]);
  const shownLit = Math.min(lit, Math.max(0, rows.length - 1));
  useEffect(() => { setLit(0); }, [words]);
  const keyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (imeComposing(event)) return;
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      if (rows.length) setLit((shownLit + (event.key === "ArrowDown" ? 1 : rows.length - 1)) % rows.length);
    } else if (fieldEnter(event)) {
      event.preventDefault();
      if (rows[shownLit]) toggle(rows[shownLit]);
    } else if (event.key === "Backspace" && !query && items.length) {
      event.preventDefault();
      set(items.slice(0, -1));
      setTip(null);
    }
  };
  const tipFor = (item: AutomationPersonItem) => typeof item === "string" ? ""
    : item.source === "page_creator" ? t("personSourceTips.page_creator")
    : (surface ?? "event_automation") === "event_automation" ? t("personSourceTips.execution_actor") : "";
  const listRef = useRef<HTMLDivElement>(null);
  const showTip = (row: HTMLElement, item: AutomationPersonItem) => {
    if (tip?.key === itemKey(item)) return;
    const edge = listRef.current?.closest<HTMLElement>("[data-automation-value-popover]")?.getBoundingClientRect();
    const rect = row.getBoundingClientRect();
    const label = tipFor(item);
    setTip(label ? { key: itemKey(item), anchor: { top: rect.top, bottom: rect.bottom, left: rect.left, right: edge?.right ?? rect.right }, label } : null);
  };
  // A row's handlers are the same at each drawing, so a row the highlight leaves alone is not drawn again.
  const now = useRef({ toggle, showTip });
  now.current = { toggle, showTip };
  const pick = useCallback((item: AutomationPersonItem) => now.current.toggle(item), []);
  const hover = useCallback((index: number, row: HTMLElement, item: AutomationPersonItem) => {
    setLit(index);
    now.current.showTip(row, item);
  }, []);
  const listId = `automation-people-${action.id}`;
  // Past 100 rows only those around what the list shows are drawn, as the relation cell's picker draws them (usePickerWindow).
  const drawn = usePickerWindow({ scrollRef: listRef, open: true, count: rows.length, keyAt: (index) => itemKey(rows[index]),
    sizeAt: () => 28, gap: 1, focused: shownLit });
  const drawRow = (lineAt: PickerWindowLine) => {
    const row = <PersonRow key={lineAt.key} item={rows[lineAt.index]} index={lineAt.index} listId={listId} lit={lineAt.index === shownLit}
      chosen={chosenKeys.has(lineAt.key)} name={rowNames[lineAt.index] ?? ""} line={line}
      count={drawn.windowed ? rows.length : undefined} onHover={hover} onPick={pick} />;
    return lineAt.top === undefined ? row : <div key={lineAt.key} {...pickerWindowLine(lineAt, drawn.measure)}>{row}</div>;
  };
  const tipRef = useRef<HTMLSpanElement>(null);
  // A press takes the row's tip, as Notion's every tip goes as the mouse goes down (lib/tipTiming.ts).
  useTipPress(tip !== null, () => setTip(null));
  const tipAnchor = tip?.anchor ?? null;
  const tipPlace = usePopoverPlacement(tipRef, tipAnchor ? () => ({ anchor: tipAnchor, placement: "right", gap: 2, options: {} }) : null,
    [tipAnchor], undefined, false);
  const token = (item: AutomationPersonItem) => {
    const name = nameOf(item);
    const remove = <button type="button" aria-label={t("removeRecipient", { name })} onClick={() => toggle(item)}>
      <X size="micro" aria-hidden="true" /></button>;
    return typeof item === "string"
      ? <span key={itemKey(item)} className={styles.fieldPerson}><PersonAvatar userId={item} name={name} size={16} />{name}{remove}</span>
      : <span key={itemKey(item)} className={styles.fieldSource}><PersonCircleIcon size="tiny" weight="fill" aria-hidden="true" />{name}{remove}</span>;
  };
  return <div className={styles.people} data-automation-people={line ? "line" : ""}>
    <div className={styles.field} onPointerDown={(event) => { if (event.target === event.currentTarget) event.preventDefault(); }}
      onClick={() => input.current?.focus()}>
      <span className={styles.tokens}>
        {items.map(token)}
        <input ref={input} className={styles.search} value={query} aria-label={t("recipientsSearch")} autoFocus={fieldFocusOnOpen()}
          placeholder={items.length ? undefined : t("recipientsSearch")} role="combobox" aria-expanded="true"
          aria-controls={listId} aria-activedescendant={rows[shownLit] ? `${listId}-${shownLit}` : undefined}
          onChange={(event) => { setQuery(event.currentTarget.value); setTip(null); }} onKeyDown={keyDown} />
      </span>
      {items.length ? <button type="button" className={styles.clear} aria-label={t("recipientsClear")} onClick={() => set([])}>
        <XCircleIcon size="small" aria-hidden="true" /></button> : null}
    </div>
    {line ? null : <div className={styles.modeRow}>
      <ValueModeButton mode={mode} onPick={(next) => set(latest.current.items, next)} />
    </div>}
    <div ref={listRef} id={listId} role="listbox" aria-multiselectable="true" aria-label={t("recipientsSearch")}
      className={styles.list} onMouseLeave={() => setTip(null)}>
      {drawn.windowed
        ? <div ref={drawn.linesRef} role="presentation" style={pickerWindowBox(drawn.height)}>{drawn.lines.map(drawRow)}</div>
        : drawn.lines.map(drawRow)}
    </div>
    {tip && typeof document !== "undefined" ? createPortal(<span ref={tipRef} className={tipStyles.tip} role="tooltip"
      style={popoverPlacementStyle(tipPlace)}>{tip.label}</span>, document.body) : null}
  </div>;
}

/** "실행한 모든 사람" and "페이지 작성자", the same two items at each drawing so their rows are drawn again only when they change. */
const SOURCE_ITEMS: readonly AutomationPersonItem[] = SOURCES.map((source) => ({ source }));

/**
 * One row of the people window: drawn again only when what it shows changes
 * — lit, picked, named — not each time the pointer lights another (redraw
 * only what changed, as the relation window's rows, 2026-11-22).
 */
const PersonRow = memo(function PersonRow({ item, index, listId, lit, chosen, name, line, count, onHover, onPick }: {
  item: AutomationPersonItem; index: number; listId: string; lit: boolean; chosen: boolean; name: string; line: boolean;
  /** How many the list holds while it draws only some of them. */
  count?: number;
  onHover(index: number, row: HTMLElement, item: AutomationPersonItem): void; onPick(item: AutomationPersonItem): void;
}) {
  return <div id={`${listId}-${index}`} role="option" aria-selected={chosen} aria-setsize={count} aria-posinset={count === undefined ? undefined : index + 1}
    className={styles.row} data-lit={lit || undefined} data-automation-person={typeof item === "string" ? item : item.source}
    onPointerMove={(event) => onHover(index, event.currentTarget, item)}
    onPointerDown={(event) => event.preventDefault()} onClick={() => onPick(item)}>
    {typeof item === "string" ? <>
      {line ? null : <PickBox checked={chosen} />}
      <span className={styles.who}><PersonAvatar userId={item} name={name} size={20} /><span className={styles.name}>{name}</span></span>
    </> : <><PersonCircleIcon size="regular" weight="fill" aria-hidden="true" /><span className={styles.name}>{name}</span></>}
  </div>;
});
