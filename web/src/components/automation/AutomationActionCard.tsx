import { useEffect, useId, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { combine } from "@atlaskit/pragmatic-drag-and-drop/combine";
import { draggableByPointer, dropTargetByPointer } from "@/lib/pointerDrag";
import { ArrowDown, ArrowUp, Copy, DotsHorizontal, DragHandleIcon, Pencil, Trash } from "@/icons/leafen";
import { AddActionAboveIcon, AddActionBelowIcon, FoldCardIcon, UnfoldCardIcon } from "@/icons/leafen/automationIcons";
import { IconTipButton, useHoverTip } from "@/components/IconTipButton";
import tipStyles from "@/components/IconTipButton.module.css";
import { AutomationCardMenu, AutomationActionTypeMenu, type AutomationMenuChoice } from "./AutomationActionMenus";
import type { AutomationActionType } from "./automationActionModel";
import styles from "./automationActionEditor.module.css";

/** A card's words as its tip says them: its pieces a space apart, a comma kept to the word before it
 * (a status set to one value reads as one sentence). */
export function automationCardWords(label: Element | null | undefined) {
  if (!label) return "";
  // A sentence that says it otherwise in its tip, as a person value's (AutomationPersonValue).
  const own = label.querySelector<HTMLElement>("[data-card-words]")?.dataset.cardWords;
  if (own) return own;
  const parts: string[] = [];
  const walker = document.createTreeWalker(label, NodeFilter.SHOW_TEXT);
  while (walker.nextNode()) {
    const text = walker.currentNode.textContent?.trim();
    if (text) parts.push(text);
  }
  return parts.join(" ").replace(/ ,/gu, ",");
}

/** Handles and menus edit action order; an editor identity fences sibling editors.
 * A heading reads as a sentence around its chip, as Notion's card heads do
 * ("[데이터 소스] 에 페이지 추가"); without one the chip leads the title. A
 * card that is its sentence alone ("Stage 속성을 [Won] 값으로 설정") opens
 * where it is set when pressed; one with fields folds them with its fold mark,
 * arrows into a bar while open and out of a box while folded, as Notion's
 * (seen 2026-10-24; its marks 2026-11-15). */
export function AutomationActionCard({ id, owner, index, count, title, icon, prefix, suffix, heading, sentence, tip: ownTip, problemTip, onOpen, onEdit, types, menu, children,
  fixed = false, onMove, onRemove, onCopy, onAdd }: {
  id: string; owner: string; index: number; count: number; title: string; prefix?: ReactNode; children: ReactNode;
  /** Its fields always open, its head words alone with no fold mark: "페이지 아이콘 설정"'s (Notion's, seen 2026-11-21). */
  fixed?: boolean;
  /** What follows the heading's words ("열기 [중앙에서 보기 ⌄] 에서"). */
  suffix?: ReactNode;
  /** The action's mark, first after the handle as Notion's. */
  icon?: ReactNode;
  heading?: { before: string; after: string };
  /** The card's whole sentence, pressed to set it. */
  sentence?: ReactNode;
  /** What its tip says in place of its words: a deleted property's what to do (Notion's, seen 2026-11-11). */
  tip?: string;
  /**
   * What a red card lacks, tipped under its head's row wherever the pointer is
   * on the head but its "⋯" — its mark, its words, its chips — centred on the
   * card, 300 wide (Notion's, seen 2026-11-21 in a database button's editor
   * and a database automation's: "아이콘 선택", the destination's and the
   * general sentence alike, a chip's too). A sentence card's mark tips it
   * itself, the card's words tipping under the whole card.
   */
  problemTip?: string;
  onOpen?(): void;
  /** What its "⋯"'s "편집" does where pressing the card does otherwise: nothing for a deleted property's (Notion's, seen 2026-11-11). */
  onEdit?(): void;
  types: { type: AutomationActionType; label: string }[];
  /** The action list's own page for a type that asks more first ("속성 편집..."'s properties). */
  menu?: (type: AutomationActionType, after: boolean, controls: { close(): void; back(): void }) => ReactNode | undefined;
  /** Without one the menu has no "복제": a notification, one to an automation or a button (Notion's, seen 2026-11-08). */
  onMove(sourceId: string, targetId: string, after: boolean): void; onRemove(): void; onCopy?(): void;
  onAdd(type: AutomationActionType, after: boolean): void;
}) {
  const { t } = useTranslation("databaseView");
  const [menuOpen, setMenuOpen] = useState(false), [adding, setAdding] = useState<boolean | null>(null);
  const [collapsed, setCollapsed] = useState(false), [dropAfter, setDropAfter] = useState<boolean | null>(null);
  const card = useRef<HTMLElement>(null), handle = useRef<HTMLButtonElement>(null), more = useRef<HTMLButtonElement>(null);
  const fieldsId = useId();
  // A sentence card's words in a tip under it while a mouse is on it, but not on its "⋯" (Notion's, seen 2026-11-11);
  // a card with a head and fields has none.
  const tip = useHoverTip();
  const showTip = () => {
    if (sentence && card.current) tip.enter(card.current, ownTip ?? automationCardWords(card.current.querySelector("[data-automation-action-open]")));
  };
  const latest = useRef(onMove); latest.current = onMove;
  // The grip is another element once there is somewhere to move it.
  const movable = count > 1;
  useEffect(() => {
    if (!card.current || !handle.current) return;
    // A card being moved folds, as Notion's does (seen 2026-11-03); a mouse moves it by the pointer's events (pointerDrag).
    return combine(draggableByPointer({ element: handle.current, getInitialData: () => ({ kind: "automation-action", owner, id }),
      onDragStart: () => setCollapsed(true) }),
      dropTargetByPointer({ element: card.current,
        canDrop: ({ source }) => source.data.kind === "automation-action" && source.data.owner === owner && source.data.id !== id,
        getData: ({ input, element }) => ({ after: input.clientY > element.getBoundingClientRect().top + element.getBoundingClientRect().height / 2 }),
        onDragEnter: ({ self }) => setDropAfter(self.data.after === true),
        onDrag: ({ self }) => setDropAfter(self.data.after === true), onDragLeave: () => setDropAfter(null),
        onDrop: ({ source, self }) => { setDropAfter(null); latest.current(String(source.data.id), id, self.data.after === true); },
      }));
  }, [id, owner, movable]);
  // Notion's (seen 2026-11-08 in its dialog, 2026-10-24 at 375px): ✎ 편집 for a sentence card, ↑ 위로 옮기기 ·
  // ↓ 아래로 이동; a line, ↱ 위에 · ↳ 아래에 작업 추가; a line, ⧉ 복제 and 삭제 (red on a phone).
  const choices: AutomationMenuChoice[] = [
    ...(sentence && onOpen ? [{ key: "edit", label: t("edit"), icon: <Pencil size="regular" />, run: onEdit ?? onOpen }] : []),
    ...(index ? [{ key: "up", label: t("moveActionUp"), icon: <ArrowUp size="regular" />, run: () => onMove(id, "previous", false) }] : []),
    ...(index < count - 1 ? [{ key: "down", label: t("moveActionDown"), icon: <ArrowDown size="regular" />, run: () => onMove(id, "next", true) }] : []),
    { key: "above", label: t("addActionAbove"), icon: <AddActionAboveIcon size="regular" />, run: () => setAdding(false), rule: true },
    { key: "below", label: t("addActionBelow"), icon: <AddActionBelowIcon size="regular" />, run: () => setAdding(true) },
    ...(onCopy ? [{ key: "copy", label: t("duplicateAction"), icon: <Copy size="regular" />, run: onCopy, rule: true }] : []),
    { key: "delete", label: t("deleteAction"), icon: <Trash size="regular" />, run: onRemove, danger: true, rule: !onCopy },
  ];
  // The heading's words collapse the card; the chip between them chooses.
  const before = heading?.before ?? "", after = heading ? heading.after : title;
  // A card whose heading holds all it asks, as an open action's, is one line with nothing to fold.
  const bodyless = children === null || children === undefined || children === false;
  const still = bodyless || fixed;
  // A red card's head tips what it lacks (problemTip); a sentence card's mark does.
  const headTip = sentence ? undefined : problemTip;
  const toggle = (text: string, lead = false) => <button type="button" className={styles.actionTitle} data-lead={lead || undefined} data-head-word
    aria-label={heading ? title : undefined}
    aria-controls={fieldsId} aria-expanded={!collapsed} onClick={() => setCollapsed(value => !value)}>{text}</button>;
  const moveKeys = (event: KeyboardEvent<HTMLButtonElement>) => {
    if ((event.key === "ArrowUp" && index > 0) || (event.key === "ArrowDown" && index < count - 1)) {
      event.preventDefault(); onMove(id, event.key === "ArrowUp" ? "previous" : "next", event.key === "ArrowDown");
    }
  };
  // With two or more actions the grip takes the mark's place as the pointer is on the card, "끌어서 N단계 이동" under it
  // (Notion's, seen 2026-11-11); one alone has nowhere to go.
  const grip = movable
    ? <IconTipButton ref={handle} className={styles.actionHandle} label={t("moveAction", { number: index + 1 })} placement="bottom"
      tip={<>{t("moveActionTip.drag")} <span className={tipStyles.tipNote}>{t("moveActionTip.step", { number: index + 1 })}</span></>}
      onKeyDown={moveKeys}><DragHandleIcon size="regular" aria-hidden="true" /></IconTipButton>
    : <button ref={handle} type="button" className={styles.actionHandle} aria-label={t("moveAction", { number: index + 1 })}
      onKeyDown={moveKeys}><DragHandleIcon size="small" /></button>;
  return <section ref={card} className={styles.card} data-automation-action={id} data-sentence={sentence ? "true" : undefined}
    data-movable={movable || undefined} data-drop={dropAfter === null ? undefined : dropAfter ? "after" : "before"}
    onPointerEnter={event => { if (event.pointerType === "mouse") showTip(); }} onPointerLeave={tip.leave} onPointerDown={tip.leave}>
    <header data-icon-tip={headTip} data-tip-under={headTip ? "[data-automation-action-icon]" : undefined} data-tip-wide={headTip ? "" : undefined}>
      {grip}
      {icon}
      {sentence ? <button type="button" className={styles.actionSentence} aria-label={title} data-automation-action-open
        onClick={onOpen}>{sentence}</button> : <>
        {/* Words before the chip fold the card only where it has something to fold ("보관 [⌄]" has not). */}
        {before ? after || still ? <span className={styles.actionTitleText} data-head-word>{before}</span> : toggle(before, true) : null}
        {prefix}
        {after ? still ? <span className={styles.actionTitleText} data-head-word>{after}</span> : toggle(after, Boolean(suffix)) : null}
        {suffix}
        {still ? null : <button type="button" className={styles.actionFold} aria-label={t(collapsed ? "actionExpand" : "actionCollapse", { number: index + 1 })}
          aria-controls={fieldsId} aria-expanded={!collapsed} data-automation-action-fold onClick={() => setCollapsed(value => !value)}>
          {collapsed ? <UnfoldCardIcon size="small" aria-hidden="true" /> : <FoldCardIcon size="small" aria-hidden="true" />}
        </button>}
      </>}
      {/* Its "⋯" tips nothing: the head's tip goes as the pointer comes onto it (Notion's, seen 2026-11-21). */}
      <button ref={more} type="button" aria-label={t("actionMenuFor", { number: index + 1 })} aria-haspopup="menu" aria-expanded={menuOpen}
        data-icon-tip={headTip ? "" : undefined}
        onPointerEnter={tip.leave} onPointerLeave={event => { if (event.pointerType === "mouse") showTip(); }}
        onClick={() => setMenuOpen(value => !value)}><DotsHorizontal size="regular" /></button>
    </header>
    {tip.tip}
    {menuOpen ? <AutomationCardMenu choices={choices} trigger={more} width={180} align="center" onClose={() => setMenuOpen(false)} /> : null}
    {adding !== null ? <AutomationActionTypeMenu types={types} trigger={more} onClose={() => setAdding(null)}
      page={menu ? (type, controls) => menu(type, adding, controls) : undefined} onAdd={type => onAdd(type, adding)} /> : null}
    {sentence || bodyless ? children : <div id={fieldsId} className={styles.actionFields} hidden={collapsed && !fixed}>{children}</div>}
  </section>;
}
