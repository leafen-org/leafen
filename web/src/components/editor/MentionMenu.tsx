"use client";

import {
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";
import { scheduleOrganizationPeopleTypeahead } from "@/lib/typeaheadSearch";
import { localTimeZone } from "@/lib/dateReminders";
import { searchLetters } from "@/lib/menuSearch";
import { placeAround, popoverPlacementStyle, popoverRectAt, usePopoverPlacement } from "@/lib/popoverPlacement";
import { stopTapAtLayer } from "@/lib/lastPress";
import type { OrganizationProfile } from "@/lib/types";
import { AlarmIcon, ArrowUpRightIcon, BellSlashIcon, ChevronLeft, ChevronRight, ClockIcon, DotsHorizontal, PersonInviteIcon, Plus, UserPlusIcon } from "@/icons/leafen";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { SelfMark } from "@/components/people/SelfMark";
import { AutomationMentionIcon } from "./AutomationMentionIcon";
import { PageIconGlyph } from "../page/PageIcon";
import { MentionPageText } from "./MentionPageText";
import { blockItemLabels, blockItemText } from "./blockItemLabels";
import {
  type BlockPickerSnapshot,
  type MentionItem,
  type MentionTrigger,
  type RowPageMentionCandidate,
} from "./blockPickerModel";
import { automationDateSuggestions, groupMentionItems, mentionGhostRest, mentionGroupLabel, mentionMenuItems, type MorePagesRow, pageIconSource } from "./mentionMenuItems";
import { showMentionGhost } from "./mentionQuery";
import { useMentionVisits } from "./useMentionVisits";
import type { SlashMenuAnchor } from "./SlashMenu";
import styles from "./editorStyles.generated.module.css";
import { CaretPickerOption, useCaretPicker } from "@/lib/caretPicker";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { useMentionInviteOffer, useMentionReach } from "./mentionInvite";
import { mentionOpenKey, showMentionOpenRow } from "./mentionOpenChoice";
import { useProductPagesSelect } from "@/components/product/productPagesStore";
import { SharePersonMark } from "../page/SharePersonMark";
import { mentionPeopleScope, mentionPeopleSeen, rememberMentionPeople } from "@/lib/mentionPeopleSeen";
import type { Session } from "@/types";
import taskStyles from "./TodoTaskMenu.module.css";
import type { TodoMenuTask } from "./todoTask";
import type { TypedMentionDate } from "./mentionTypedDate";
import { usePagePathsAbove, usePathsAboveShown } from "@/lib/usePagePathsAbove";
import { inlineSign } from "@/lib/readingDirection";
import { useEdgeFade } from "@/lib/edgeFade";
import { fieldEnter } from "@/lib/fieldKeyBoundary";
import { phoneListPlace, placePhoneListPanel } from "./mentionPhonePanel";
import panelStyles from "./MentionPhonePanel.module.css";

/** How a row was picked: a to-do's "@" menu assigns or sets the due date unless ⇧↵ asked for the mention alone. */
export type MentionPick = { mentionOnly: boolean };

/**
 * The page links show their first five pages, then "N개 결과 더 보기", which
 * shows the rest in its place, the first of them chosen, as Notion's "@",
 * "[[" and "+" menus (seen 2026-11-12); what is typed folds them again.
 */
const PAGES_AT_FIRST = 5;

/** The typed date parser (mentionTypedDate.ts), once a menu has loaded it. */
let typedDateReader: ((text: string, timeZone: string) => TypedMentionDate | null) | null = null;
const noneInside = () => [];

export function MentionMenu({
  anchor,
  ownerBlockId,
  pageId = "",
  query,
  mode = "mention",
  only,
  newPageFirst = false,
  newPages = false,
  invite = false,
  session,
  onPick,
  onClose,
  snapshot,
  automation,
  comment = false,
  task,
  onActive,
  docked = false,
}: {
  anchor?: SlashMenuAnchor;
  ownerBlockId?: string;
  pageId?: string;
  query: string;
  mode?: MentionTrigger;
  /** "+"'s page links lead with making the page, under no label (seen 2026-11-05). */
  newPageFirst?: boolean;
  /** The one kind the slash menu's person, page or date mention offers. */
  only?: "person" | "page" | "date";
  /** Whether a typed name offers a new page, where the line can make one. */
  newPages?: boolean;
  /** Whether the line may offer to invite someone, which the server then decides (useMentionInviteOffer). */
  invite?: boolean;
  /** Who asks, where the page's people are marked by whether they would hear of being mentioned (mentionInvite.ts). */
  session?: Session | null;
  onPick: (item: MentionItem, how?: MentionPick) => void;
  onClose: () => void;
  snapshot: BlockPickerSnapshot;
  /**
   * An automation message's own mentions (AutomationMessageField.tsx), each
   * under its heading: variables and the trigger page's properties first,
   * then each beside the menu's own dates, people and pages — or alone, a
   * page's properties under a heading that goes back, as Notion's
   * "‹ 페이지 실행의 속성" (seen 2026-10-23).
   */
  automation?: { items: readonly MentionItem[]; alone?: { label: string; onBack: () => void } };
  /**
   * A comment field's: until a word is typed its dates are Notion's two
   * there, "오늘 — 〈날짜〉" and "다음주 화요일 오후 3시" (seen 2026-10-28).
   */
  comment?: boolean;
  /**
   * A to-do's: a date row sets its due date and a person row assigns it, as
   * Notion's (seen 2026-11-09), "마감일 설정" and "담당자 설정" at their groups'
   * ends — "마감일 변경" once it has one, "담당자 추가" once someone is
   * assigned; "⇧↵ 멘션만" along its foot, and ⇧↵ writes the mention alone.
   */
  task?: TodoMenuTask;
  /** The row the keys or pointer are on, for a field that previews it after its "@". */
  onActive?: (item: MentionItem | null) => void;
  /** A page's own words' list (its lines' and title's): on a phone a panel over the keyboard (mentionPhonePanel.ts). */
  docked?: boolean;
}) {
  const { phone } = useSidebarLayout(true);
  // A press outside the list closes it and goes on, as its kind's (caret-menu, overlayKinds.ts): on a desktop the
  // caret to the title or the words pressed, "공유" opening the share window in the same press, as Notion's "@" and
  // "[[" do (2026-11-26 in Chrome at 1440, two tabs at two times, judged by the screen and one letter after the press
  // with nothing run in the page before it); on a phone a finger's tap — the caret to the words tapped, a new line
  // under the body's last, the soft keyboard staying as it was — as Notion's phone web does from a title's list or a
  // line's (seen 2026-11-10 in iPhone Safari and Android Chrome, the keyboard up and down). A tap under what shows,
  // by the browser's own bars, is the browser's: it only lets the keyboard go.
  // The page's own words' panel goes no further (mentionPhonePanel.ts).
  const panel = phone && docked ? panelStyles : null;
  // Which words the people were found for; null before the first answer, so
  // an empty "@" waits for its own instead of taking "none" for the answer.
  const [searchedPeople, setSearchedPeople] = useState<{ key: string | null; people: OrganizationProfile[] }>({ key: null, people: [] });
  const [searchedRows, setSearchedRows] = useState<{ key: string; rows: RowPageMentionCandidate[] }>({ key: "", rows: [] });
  // The pages and rows the reader opened lately, which lead the page links (mentionLead.ts).
  const visited = useMentionVisits(snapshot, !only || only === "page");
  const menuId = useId();
  // The menu's box, placed and shadowed, and the list in it that scrolls.
  const menuRef = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const q = query.trim().toLowerCase();

  // A typed date ("Oct 31", "next Monday 9am", after "remind " a reminder's)
  // is read by a parser that loads the first time one is typed, and from then
  // on as the menu draws, so its rows come with the letter typed ("@오" never
  // shows 오늘's rows first).
  const dateText = useMemo(() => {
    if (mode === "page_link" || (only && only !== "date") || !q) return null;
    const reminder = /^(remind|리마인더?)\s+/u.exec(q);
    const text = reminder ? q.slice(reminder[0].length) : q;
    return text ? { text, reminder: Boolean(reminder) } : null;
  }, [mode, only, q]);
  const [readerLoaded, setReaderLoaded] = useState(Boolean(typedDateReader));
  useEffect(() => {
    if (!dateText || typedDateReader) return;
    let current = true;
    void import("./mentionTypedDate").then(({ readTypedMentionDate }) => {
      typedDateReader = readTypedMentionDate;
      if (current) setReaderLoaded(true);
    }).catch(() => undefined);
    return () => { current = false; };
  }, [dateText]);
  const typedDate = useMemo(() => {
    const read = dateText && readerLoaded && typedDateReader ? typedDateReader(dateText.text, localTimeZone()) : null;
    return read && { ...read, reminder: dateText!.reminder };
  }, [dateText, readerLoaded]);

  const searchesPeople = mode !== "page_link" && (!only || only === "person") && Boolean(snapshot.organizationId);
  // Everyone a search has found in this tab (lib/mentionPeopleSeen.ts): while
  // this menu's search is on its way, those whom the typed word fits stand in,
  // so a name being typed is never "결과 없음" before the server has said so
  // (2026-11-06), nor a new session's first "@" the reader alone (2026-11-07).
  useEffect(() => {
    if (!searchesPeople || !snapshot.organizationId) {
      setSearchedPeople({ key: "", people: [] });
      return;
    }
    return scheduleOrganizationPeopleTypeahead({
      organizationId: snapshot.organizationId,
      query: q,
      limit: 8,
    }, {
      search: snapshot.searchOrganizationPeople,
      onResult: (result) => {
        rememberMentionPeople(snapshot.currentUserId ?? "", mentionPeopleScope(snapshot.organizationId, snapshot.workspaceId), result.people ?? []);
        setSearchedPeople({ key: q, people: result.people ?? [] });
      },
      onError: () => setSearchedPeople({ key: q, people: [] }),
    });
  }, [q, searchesPeople, snapshot.organizationId, snapshot.searchOrganizationPeople, snapshot.workspaceId]);
  const peoplePending = searchesPeople && searchedPeople.key !== q;
  const peopleNow = useMemo(() => {
    if (!peoplePending) return searchedPeople.key === q ? searchedPeople.people : [];
    return mentionPeopleSeen(snapshot.currentUserId ?? "", mentionPeopleScope(snapshot.organizationId, snapshot.workspaceId)).filter((person) => !q
      || [person.displayName, person.email].some((text) => searchLetters(text ?? "").includes(searchLetters(q))));
  }, [peoplePending, q, searchedPeople, snapshot.currentUserId, snapshot.organizationId, snapshot.workspaceId]);

  useEffect(() => {
    const search = snapshot.searchRowPages;
    if ((only && only !== "page") || !search) {
      setSearchedRows({ key: "", rows: [] });
      return;
    }
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      search(q, controller.signal)
        .then((rows) => { if (!controller.signal.aborted) setSearchedRows({ key: q, rows }); })
        .catch(() => { if (!controller.signal.aborted) setSearchedRows({ key: q, rows: [] }); });
    }, 150);
    return () => {
      controller.abort();
      window.clearTimeout(timer);
    };
  }, [mode, only, q, snapshot.searchRowPages]);

  // A page under a row is described by the path the server names (Notion's).
  const pathsRead = usePagePathsAbove([]);
  // The invite of someone not yet here goes where the server offers it (mentionInvite.ts).
  const offersInvite = useMentionInviteOffer(session, pageId, invite && mode === "mention");
  // A guest of the page's workspace, as its list says, is offered no one until a name is typed (mentionMenuItems).
  const guest = useProductPagesSelect((pages) => pages?.workspaces?.find(({ id }) => id === snapshot.workspaceId)?.guest === true);
  const found = useMemo(() => {
    if (automation?.alone) return [...automation.items];
    const own = mentionMenuItems({
      mode, only, newPages, invite: offersInvite, guest, query, snapshot,
      searchedPeople: peopleNow,
      searchedRows: searchedRows.key === q ? searchedRows.rows : [],
      visited, typedDate,
    });
    // Its Tuesday is written "다음주" there, apart from an automation's "다음 주" (2026-10-25).
    if (comment && !q && !automation) return [...automationDateSuggestions().map((item) => item.id === "automation-next-tuesday"
      ? { ...item, label: blockItemText("mention.nextTuesdayAfternoonComment") } : item), ...own.filter((item) => item.kind !== "date")];
    // A comment takes dates but no reminder: "@remind" there finds people
    // and the invite alone, as Notion's comments do (seen 2026-11-08).
    if (!automation) return comment ? own.filter((item) => !(item.kind === "date" && item.dateReminder !== undefined)) : own;
    // Each own kind follows the automation's of its heading, as Notion lists
    // them; its dates are Notion's two there until a word is typed, which
    // finds the page's own but reminders.
    const extra = automation.items.filter((item) => item.kind === "automation");
    const under = (label: string) => extra.filter((item) => item.kind === "automation" && item.groupLabel === label);
    const labels = blockItemLabels();
    const kinds = new Set([labels.groupDate, labels.groupPeople, labels.groupLinkToPage]);
    const ownOf = (label: string) => (groupMentionItems(own).find((group) => group.label === label)?.items.map(({ item }) => item) ?? [])
      .filter((item) => !(item.kind === "date" && item.dateReminder !== undefined));
    return [
      ...extra.filter((item) => item.kind === "automation" && !kinds.has(item.groupLabel)),
      ...under(labels.groupDate), ...(q ? ownOf(labels.groupDate) : automationDateSuggestions()),
      ...under(labels.groupPeople), ...ownOf(labels.groupPeople),
      ...under(labels.groupLinkToPage), ...ownOf(labels.groupLinkToPage),
    ];
  }, [automation, comment, guest, offersInvite, mode, newPages, only, pathsRead, peopleNow, q, query, searchedRows, snapshot, typedDate, visited]);

  const results = useMemo(() => {
    const making = (item: (typeof found)[number]) => item.kind === "sub_page" || item.kind === "add_page";
    return newPageFirst ? [...found.filter(making), ...found.filter((item) => !making(item))] : found;
  }, [found, newPageFirst]);
  const [allPagesFor, setAllPagesFor] = useState<string | null>(null);
  const shown = useMemo<(MentionItem | MorePagesRow)[]>(() => {
    const pages = results.filter((item) => item.kind === "page").length;
    if (allPagesFor === q || pages <= PAGES_AT_FIRST) return results;
    let seen = 0;
    return results.flatMap((item): (MentionItem | MorePagesRow)[] => {
      if (item.kind !== "page") return [item];
      seen += 1;
      if (seen < PAGES_AT_FIRST) return [item];
      if (seen > PAGES_AT_FIRST) return [];
      const count = pages - PAGES_AT_FIRST;
      return [item, { kind: "more_pages", id: "more-pages", count, label: blockItemText("mention.morePages", { count }) }];
    });
  }, [allPagesFor, q, results]);
  usePathsAboveShown(shown.flatMap((item) => item.kind === "page" ? [item.page] : []), snapshot.pagesById);
  // The row chosen once the rest of the pages show: the first of them.
  const chooseAfterMore = useRef<string | null>(null);
  const showAllPages = () => {
    const pages = results.filter((item) => item.kind === "page");
    chooseAfterMore.current = pages[PAGES_AT_FIRST]?.id ?? null;
    setAllPagesFor(q);
  };
  const pick = (item: MentionItem | MorePagesRow, how: MentionPick) => {
    if (item.kind === "more_pages") showAllPages(); else onPick(item, how);
  };
  // A page's mention marks a guest or a restricted member beside their name
  // and, where the page can be shared from it, someone it doesn't open to (mentionInvite.ts).
  const offeredPeople = useMemo(() => mode === "mention"
    ? results.flatMap((item) => item.kind === "person" ? [item.userId] : []).slice(0, 50) : [], [mode, results]);
  const marks = useMentionReach(session, pageId, offeredPeople);
  const reach = invite ? marks.reach : new Map<string, never>();
  // A row the keys chose stays chosen as letters are typed: "@" keeps the row
  // (or its place in its group), "[[" and "+" its place (caretPicker, Notion's). An "@" just
  // typed opens past its dates (mentionOpenChoice.ts); a to-do's and an automation's on their first row.
  const pickerState = useCaretPicker({ items: shown, query, id: menuId, ownerBlockId, onOwnerMismatch: onClose,
    label: blockItemText(mode === "page_link" ? "pageLink.linkToPage" : "mention.label"), keep: mode === "page_link" ? "place" : { groupOf: mentionGroupLabel },
    opening: mode === "mention" && !only && !automation && !task ? { key: (items) => q ? undefined : mentionOpenKey(items), show: showMentionOpenRow } : undefined,
    listRef, keyOf: item => item.id, textOf: item => item.label, onPick: (item) => pick(item, { mentionOnly: false }), onClose, lightFirst: only !== "date" });
  // After the picker's own choice of the first row as the list changes.
  useLayoutEffect(() => {
    const key = chooseAfterMore.current;
    if (key === null || !pickerState.state.collection.getItem(key)) return;
    chooseAfterMore.current = null;
    pickerState.state.selectionManager.setFocusedKey(key);
  }, [pickerState.state.collection, pickerState.state.selectionManager]);
  const focusedKey = pickerState.state.selectionManager.focusedKey;
  const focusedRow = focusedKey === null ? null : pickerState.state.collection.getItem(focusedKey)?.value ?? null;
  const focusedItem = focusedRow?.kind === "more_pages" ? null : focusedRow;
  // ⇧↵ in a to-do picks the row as a mention alone; elsewhere it is the line's.
  const onCaretKeyDown = (event: KeyboardEvent) => {
    if (task && fieldEnter(event) && event.shiftKey && !event.defaultPrevented && focusedRow) {
      event.preventDefault(); event.stopPropagation();
      pick(focusedRow, { mentionOnly: true });
      return;
    }
    pickerState.onCaretKeyDown(event);
  };
  const reportActive = useRef(onActive); reportActive.current = onActive;
  // A to-do's row for someone the page doesn't open to assigns no one, so it previews nothing.
  const unassignable = Boolean(task) && focusedItem?.kind === "person" && reach.has(focusedItem.userId);
  useEffect(() => { reportActive.current?.(unassignable ? null : focusedItem); }, [focusedItem, unassignable]);
  useEffect(() => () => reportActive.current?.(null), []);
  // The faint rest after the "@" and its query, in its pill (mentionGhostRest),
  // drawn with the rows it is read from; an automation's menu and the page links' show none.
  const ghostRest = mode === "mention" && !automation ? mentionGhostRest(query, focusedRow, focusedRow?.id === shown[0]?.id, only) : "";
  useLayoutEffect(() => { showMentionGhost({ query, rest: ghostRest }); }, [query, ghostRest]);
  useEffect(() => () => showMentionGhost(null), []);
  const emptyId = `${menuId}-empty`;
  const groupedResults = useMemo(() => groupMentionItems(shown), [shown]);
  const taskHints: Record<string, string> = task ? {
    [blockItemLabels().groupDate]: blockItemText(task.due ? "mention.changeDueDate" : "mention.setDueDate"),
    [blockItemLabels().groupPeople]: blockItemText(task.assigned ? "mention.addAssignee" : "mention.setAssignee"),
  } : {};
  // 12px under the caret from 22px left of it, or over it where only that has
  // room, as Notion's (seen 2026-11-04); cut on the roomier side where the window ends.
  // A page link's menu starts 10px left of its "[[" or "+" (seen 2026-11-07).
  // It is at most 40% of the window high, as Notion's (seen 2026-11-12: 289.2
  // in a 723 window), and chooses its side by that height however few its rows,
  // keeping it as they change (seen 2026-11-12: under its line with 306 under
  // it, over it with 295, "xqzv" typed or not).
  // On a phone it is the same 330 list (EditorSlashMenu.module.css), at most
  // 30% of the window with its bars at their smallest high, choosing its side
  // by that height in the window's room, under the keyboard where the browser
  // draws the page there, as Notion's (seen 2026-11-13 in iPhone Safari at
  // 402: 226.2 of 754, 12 under its line, its foot under the keyboard;
  // 2026-11-14 in Android Chrome at 411: 251.2 of 837.3, over a line low in
  // its 781 window with the keyboard up or down — popoverPlacement `lineSide`);
  // the page's own words' a panel over the keyboard (mentionPhonePanel.ts).
  const menuPlace = usePopoverPlacement(menuRef, anchor ? (size, viewport) => panel ? placePhoneListPanel(size, viewport) : placeAround(
    popoverRectAt({ top: anchor.top, bottom: anchor.bottom, left: anchor.left - (mode === "page_link" ? 10 : 22) * inlineSign(menuRef.current) }), size, viewport,
    phone ? phoneListPlace() : { gap: 12, cap: window.innerHeight * 0.4, lineSide: window.innerHeight * 0.4, marginY: 0 },
  ) : null, [anchor, mode, phone, panel], undefined, { kind: "caret-menu", onEscape: onClose, onOutsidePress: panel ? () => { stopTapAtLayer(); onClose(); } : onClose,
    onCaretKeyDown, inside: panel ? noneInside : pickerState.inside });
  const menuStyle = useMemo<React.CSSProperties | undefined>(() => anchor ? {
    position: "fixed",
    // 330 wide however few its rows, as Notion's "@", "[[" and "+" (seen
    // 2026-11-07, and 2026-11-12: 330 with "xqzv" typed as without).
    width: panel ? "100%" : 330,
    ...popoverPlacementStyle(menuPlace),
    // The list scrolls inside, fading at its edges as Notion's (useEdgeFade).
    overflowY: undefined,
  } : undefined, [anchor, menuPlace, panel]);
  // Whether its rows scroll: they then keep 307px beside the scroll bar's
  // room, and else take the menu's width (EditorSlashMenu.module.css).
  const [scrolls, setScrolls] = useState(false);
  useLayoutEffect(() => {
    const list = listRef.current;
    setScrolls(Boolean(list && list.scrollHeight > list.clientHeight + 1));
  }, [menuPlace, shown]);
  useEdgeFade(listRef, shown);
  const picker = (
    <>
      <div
        className={`${styles.mentionMenu} ${phone ? panel?.panel ?? styles.mentionMenuPhone : ""}`}
        data-phone-line-panel={panel ? "" : undefined}
        ref={menuRef}
        style={menuStyle}
        data-moved-by={pickerState.moved ?? undefined}
        onMouseDown={(event) => event.preventDefault()}
      ><div
        className={`${styles.mentionMenuList} ${panel?.list ?? ""}`}
        ref={listRef}
        data-scrolls={scrolls ? "true" : undefined}
        data-edge-fade=""
        {...pickerState.listBoxProps}
        tabIndex={-1}
        aria-activedescendant={pickerState.activeId}
        aria-describedby={results.length === 0 ? emptyId : undefined}
      >
        {automation?.alone ? (
          <button type="button" className={styles.mentionBack} data-mention-back onClick={automation.alone.onBack}>
            <ChevronLeft size="small" aria-hidden="true" />{automation.alone.label}
          </button>
        ) : null}
        {results.length === 0 ? (
          <div id={emptyId} className={styles.slashEmpty} role="status" aria-busy={peoplePending || undefined}>
            {peoplePending ? null : query.trim() && only !== "date"
              ? blockItemText("mention.noResultsFor", { query: query.trim() })
              : mode === "page_link"
                ? blockItemText("mention.noPages")
                : blockItemText("mention.noResults")}
          </div>
        ) : groupedResults.map((group) => (
          <div key={group.label} className={`${styles.mentionSection} ${panel?.section ?? ""}`} data-unnamed={mode === "page_link" && group.items[0].item.kind === "sub_page" || undefined}>
            {mode === "page_link" && group.items[0].item.kind === "sub_page" ? null : task && taskHints[group.label] ? (
              <div className={`${styles.mentionSectionLabel} ${taskStyles.sectionLabel}`} aria-hidden="true">
                <span>{group.label}</span><span className={taskStyles.hint}>{taskHints[group.label]}</span></div>
            ) : <div className={`${styles.mentionSectionLabel} ${panel?.groupLabel ?? ""}`} aria-hidden="true">{group.label}</div>}
            {group.items.map(({ item }) => item.kind === "more_pages" ? (
              <CaretPickerOption state={pickerState.state} itemKey={item.id} key={item.id}
                className={`${styles.mentionItem} ${styles.mentionMore} ${panel?.row ?? ""}`} data-mention-more-pages>
                {({ labelProps }) => <>
                  <span className={styles.mentionGlyph} aria-hidden="true"><DotsHorizontal size="regular" /></span>
                  <span className={styles.mentionLine}><span className={styles.slashName} {...labelProps}>
                    <span className={styles.slashNameText}>{item.label}</span></span></span>
                </>}
              </CaretPickerOption>
            ) : (
              <CaretPickerOption
                state={pickerState.state} itemKey={item.id}
                key={item.id}
                className={`${item.kind === "page" && (item.place?.length || item.description)
                  ? `${styles.mentionItem} ${styles.mentionPlaceItem}` : styles.mentionItem} ${panel?.row ?? ""}`}
              >{({ labelProps, placeProps, descriptionProps }) => <>
                {/* A template's mention has no icon, its words at the row's start (Notion's, seen 2026-11-09). */}
                {item.kind === "template" ? null : <span className={styles.mentionGlyph} data-kind={item.kind} aria-hidden="true">
                  {item.kind === "automation" ? <AutomationMentionIcon token={item.token} type={item.tokenType} size="regular" />
                    // A reminder's row shows an alarm clock, a date's a clock (Notion's, seen 2026-11-09).
                    : item.kind === "date" ? item.dateReminder !== undefined ? <AlarmIcon size="regular" /> : <ClockIcon size="regular" />
                    : item.kind === "sub_page" ? <Plus size="regular" />
                    : item.kind === "add_page" ? <ArrowUpRightIcon size="regular" />
                    : item.kind === "invite" ? <PersonInviteIcon size="regular" />
                    // A person shows their own picture, as Notion's "@" menu (seen 2026-11-06).
                    : item.kind === "person" ? <PersonAvatar userId={item.userId} name={item.label} size={20} />
                    : item.kind === "page" ? <PageIconGlyph page={pageIconSource(item.page)} size={18} database={item.dataSource ? "cylinder" : "table"} /> : null}
                </span>}
                {/* A page says where it is on a second line (MentionPageText); a date or a person
                    is one line, a reminder's time after a dash, as Notion's (seen 2026-10-17). */}
                {item.kind === "page" ? (
                  <MentionPageText item={item} searched={Boolean(q)} labelProps={labelProps} placeProps={placeProps} descriptionProps={descriptionProps} />
                ) : (
                  <span className={styles.mentionLine}>
                    <span className={styles.slashName} {...labelProps}><span className={styles.slashNameText}>
                      {(item.kind === "sub_page" || item.kind === "add_page") && item.title ? <NewPageLabel item={item} /> : item.label}
                      {item.kind === "person" && item.self ? <SelfMark /> : null}</span></span>
                    {item.kind === "person" && marks.badges.get(item.userId)
                      ? <SharePersonMark guest={marks.badges.get(item.userId) === "guest"} restricted={marks.badges.get(item.userId) === "restricted"} /> : null}
                    {item.kind === "automation" && item.opens ? <ChevronRight size="small" className={styles.mentionOpens} aria-hidden="true" /> : null}
                    {(item.kind === "date" && (item.dateReminder !== undefined || item.dashed) || item.kind === "template") && item.description
                      ? <span className={`${styles.mentionHint} ${panel?.hint ?? ""}`} {...descriptionProps}>— {item.description}</span> : null}
                    {item.kind === "person" && item.distinction
                      ? <span className={`${styles.mentionHint} ${panel?.hint ?? ""}`} {...descriptionProps}>— {item.distinction}</span> : null}
                    {item.kind === "person" && reach.get(item.userId) ? (
                      <span className={styles.mentionReach} role="img"
                        aria-label={blockItemText(reach.get(item.userId) === "invite" ? "mention.reachInvite" : "mention.reachSilent")}>
                        {reach.get(item.userId) === "invite" ? <UserPlusIcon size="small" aria-hidden="true" /> : <BellSlashIcon size="small" aria-hidden="true" />}
                      </span>
                    ) : null}
                  </span>
                )}
              </>}</CaretPickerOption>
            ))}
          </div>
        ))}
        {task ? <div className={taskStyles.footer} aria-hidden="true">
          <span className={taskStyles.keys}>⇧↵</span>{blockItemText("mention.mentionOnly")}</div> : null}
      </div></div>
    </>
  );
  return typeof document === "undefined" ? picker : createPortal(picker, document.body);
}

/** A new page's row with the words typed in it at 500, as Notion's (seen 2026-11-11). */
function NewPageLabel({ item }: { item: Extract<MentionItem, { kind: "sub_page" | "add_page" }> }) {
  const [before = "", after = ""] = blockItemText(item.kind === "sub_page" ? "mention.subPageNamed" : "mention.addPageNamed", { title: "\u0000" })
    .split("\u0000");
  return <>{before}<span className={styles.mentionTyped}>{item.title}</span>{after}</>;
}
