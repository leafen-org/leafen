import { now as zonedNow } from "@internationalized/date";
import { localDateTimeValue, localTimeZone } from "@/lib/dateReminders";
import { civilDate } from "@/lib/nativeDate";
import { daysIntoWeek } from "@/lib/displayPreferences";
import { pagePlace, placeText } from "@/lib/pagePath";
import { pageNamesNow } from "@/lib/pageNames";
import { pageArchiveRoot } from "@/lib/pageArchive";
import { inlineDatabasePagesOf } from "./inlineDatabasePages";
import { rowMentionItem } from "./rowMentionItem";
import { type MentionVisit, withMentionLead } from "./mentionLead";
import type { OrganizationProfile, Page } from "@/lib/types";
import type { Page as WirePage } from "@/types";
import { personLabel } from "../database/people";
import { pageIconText } from "../page/PageIcon";
import { blockItemLabels, blockItemText } from "./blockItemLabels";
import {
  type BlockPickerSnapshot,
  collectMentionPages,
  collectMentionPeople,
  mentionSearchRank,
  type MentionItem,
  type MentionTrigger,
  type RowPageMentionCandidate,
} from "./blockPickerModel";
import { localDateForOffset, localIsoDate, reminderMentionChoices } from "./mentionCalendarModel";
import { dateMentionLabel } from "./dateMentions";
import { templateMentionDescription } from "./templateMentions";
import { organizationRoleLabel, organizationStatusLabel, personName } from "@/lib/peopleDirectory";
import { startsInLetters } from "@/lib/menuSearch";
import { pageEditedTime, searchAnswerOrder } from "@/lib/pageSearchRank";

// The "@" menu's items for what is typed, apart from the menu that shows
// them: dates, reminders and a template's mentions, people and the invite
// of a typed name, pages and row pages, and the new page items.

function mentionDateDescription(offsetDays: number) {
  return new Intl.DateTimeFormat(blockItemLabels().dateDisplayLocale, { year: "numeric", month: "long", day: "numeric" })
    .format(localDateForOffset(offsetDays));
}

/**
 * An automation field's own date suggestions, as Notion's "@" menu there
 * offers them (seen 2026-10-25): "오늘 — 2026년 10월 25일", then a reminder
 * "다음주 화요일 오후 3시" — the Tuesday of the week after this one, weeks
 * from Sunday.
 */
export function automationDateSuggestions(now = new Date()): MentionItem[] {
  const today = new Intl.DateTimeFormat(blockItemLabels().dateDisplayLocale, { year: "numeric", month: "long", day: "numeric" })
    .format(localDateForOffset(0));
  const tuesday = new Date(now.getFullYear(), now.getMonth(), now.getDate() + (7 - now.getDay()) + 2, 15, 0, 0, 0);
  return [
    { kind: "date", id: "automation-today", label: blockItemLabels().mentionToday, description: today, icon: "@", date: localIsoDate(0), dashed: true },
    { kind: "date", id: "automation-next-tuesday", label: blockItemText("mention.nextTuesdayAfternoon"), description: "", icon: "@",
      date: localDateTimeValue(tuesday, true), dateTimeZone: localTimeZone(), dateReminder: "0" },
  ];
}

function mentionTimeLabel(date: Date) {
  return new Intl.DateTimeFormat(blockItemLabels().dateDisplayLocale, {
    hour: "numeric",
    minute: "2-digit",
  }).format(date);
}

/** A reminder row's time as Notion's menu writes it: "오전 9시" on the hour (seen 2026-10-28), minutes otherwise. */
function mentionHourLabel(date: Date) {
  return new Intl.DateTimeFormat(blockItemLabels().dateDisplayLocale, {
    hour: "numeric",
    ...(date.getMinutes() ? { minute: "2-digit" as const } : {}),
  }).format(date);
}

function organizationProfileMentionLabel(profile: OrganizationProfile) {
  return profile.displayName?.trim() || profile.email?.trim() || profile.userId?.trim() || blockItemText("person.label");
}

function organizationProfileMentionDescription(profile: OrganizationProfile) {
  const parts = [
    profile.email?.trim(),
    profile.organizationRole
      ? blockItemText("person.roleInOrganization", { role: organizationRoleLabel(profile.organizationRole) })
      : null,
    profile.status && profile.status !== "active" ? organizationStatusLabel(profile.status) : null,
  ].filter(Boolean);
  return parts.join(" - ") || blockItemText("person.organizationMember");
}

export function pageIconSource(page: Page) {
  return page as unknown as Parameters<typeof pageIconText>[0];
}

function pageIcon(page: Page, fallback = "P") {
  return pageIconText(pageIconSource(page), fallback);
}

export function pageTitle(page: Page) {
  return pageNamesNow.title(page);
}

/**
 * How many pages the menu finds: twenty for a bare "@", as Notion's lists
 * (five and "15개 결과 더 보기", seen 2026-11-12), and some more for typed
 * words (Notion's held 21 to 23 pages in three searches that matched more).
 */
const PAGES_FOUND = { recent: 20, searched: 25 };

/**
 * The page links' "N개 결과 더 보기", which shows the pages after the first
 * ones in its place (MentionMenu).
 */
export type MorePagesRow = { kind: "more_pages"; id: string; label: string; count: number };

export type MentionMenuQuery = {
  mode: MentionTrigger;
  /** The one kind the slash menu's person, page or date mention offers. */
  only?: "person" | "page" | "date";
  /** Whether a typed name offers a new page, where the line can make one. */
  newPages: boolean;
  /** Whether a typed name offers to invite it, where the page can be shared. */
  invite: boolean;
  /** Whether the reader is a guest of the page's workspace, whose empty "@" lists no one. */
  guest?: boolean;
  query: string;
  snapshot: BlockPickerSnapshot;
  /** People and row pages a search found for this query. */
  searchedPeople: OrganizationProfile[];
  searchedRows: RowPageMentionCandidate[];
  /** The pages and rows the reader visited, newest first, which lead what is found (mentionLead.ts). */
  visited?: readonly MentionVisit[];
  /** The date the typed text names (`mentionTypedDate`), after "remind " for a reminder, once read. */
  typedDate?: { date: string; withTime: boolean; named: boolean; relative?: boolean; reminder: boolean } | null;
};

const DISTANCE_UNITS = [["Minutes", 1], ["Hours", 60], ["Days", 1440], ["Weeks", 7 * 1440], ["Months", 30 * 1440], ["Years", 365 * 1440]] as const;

/**
 * How far a date that counts from now is off, as Notion's rows say it (seen
 * 2026-11-13): "지금부터 1주 후", "3일 전". A day alone counts days from today,
 * "오늘", "내일" and "어제" for the nearest ("morning", "evening"), a time
 * minutes from now; each in the largest unit it reaches — an hour, a day, a
 * week, 30 days, 365 days — rounded ("in 10 days" 1주, "in 45 days" 2개월,
 * "in 18 months" 1년, "in 24 hours" 24시간, "in 30 minutes" 29분 when the
 * minute has begun).
 */
function mentionDateDistance(typed: { date: string; withTime: boolean }, now: Date) {
  const [year, month, day] = typed.date.slice(0, 10).split("-").map(Number);
  const days = Math.round((new Date(year, month - 1, day).getTime() - new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()) / 86_400_000);
  const labels = blockItemLabels();
  if (!typed.withTime && Math.abs(days) <= 1) return days === 0 ? labels.mentionToday : days > 0 ? labels.mentionTomorrow : labels.mentionYesterday;
  const minutes = typed.withTime ? (new Date(typed.date).getTime() - now.getTime()) / 60_000 : days * 1440;
  const size = Math.abs(minutes);
  const [unit, length] = DISTANCE_UNITS.filter(([, unitLength]) => size >= unitLength).pop() ?? DISTANCE_UNITS[0];
  const distance = blockItemText(`mention.distance${unit}`, { count: Math.max(1, Math.round(size / length)) });
  return blockItemText(minutes > 0 ? "mention.fromNow" : "mention.ago", { distance });
}

/**
 * A typed date's rows: the date as a mention reads, then a reminder on it while
 * it is still ahead — a time later than now, a day after today; with "remind "
 * the reminder alone. A day's reminder names it in full, but a day of next
 * week past tomorrow as the date row does ("다음주 화요일" for "next tuesday" or "nov 17",
 * "2026년 11월 15일" for "sunday" this week, seen 2026-11-09), a time's as the
 * date row does; a day named in words with no reminder row names it in full on the
 * date row instead (Notion's, seen 2026-11-05/06: "Dec 25 3pm", "tomorrow";
 * "4am" in the afternoon, "yesterday 4pm" with no reminder; "monday" →
 * "월요일 — 2026년 11월 2일", "Nov 1" bare), and so does one gone this week
 * however typed ("nov 10" and "11/10" on a Friday "화요일 — 2026년 11월
 * 10일", "nov 3" "지난주 화요일" bare; seen 2026-11-13); seen 2026-11-09 at 3:40 in the
 * morning: "오늘" and "today" one row, "오늘 — 2026년 11월 9일", with no
 * reminder though nine was still to come; "4pm" with one). A date that
 * counts from now or a part of a day says how far off it is on its reminder,
 * and on its date row once gone ("next week" → "다음주 금요일", its reminder
 * "지금부터 1주 후"; "last week" "지난주 금요일 — 1주 전"; "evening" "오늘 —
 * 오늘"; seen 2026-11-13).
 */
function typedDateItems(typed: NonNullable<MentionMenuQuery["typedDate"]>, now = new Date()): (MentionItem & { kind: "date" })[] {
  const label = dateMentionLabel(typed.date, now, "relative");
  const ahead = typed.withTime ? new Date(typed.date).getTime() > now.getTime() : typed.date > civilDate(now).toString();
  const zone = typed.withTime ? { dateTimeZone: localTimeZone() } : {};
  const monday = (value: Date) => new Date(value.getFullYear(), value.getMonth(), value.getDate() - daysIntoWeek(value, 1)).getTime();
  const [year, month, day] = typed.date.slice(0, 10).split("-").map(Number);
  // Tomorrow is "내일", not a day of next week, even on a Sunday: its
  // reminder names it in full (Notion's, seen 2026-11-15, a Sunday: "@내일"
  // and "@nov 16" "2026년 11월 16일", "@nov 17" "다음주 화요일").
  const tomorrow = new Date(year, month - 1, day).getTime() === new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1).getTime();
  const nextWeek = !tomorrow && Math.round((monday(new Date(year, month - 1, day)) - monday(now)) / 86_400_000) === 7;
  // A date that counts from now says how far off it is instead (mentionDateDistance).
  const distance = typed.relative ? mentionDateDistance(typed, now) : "";
  const reminder = { kind: "date" as const, id: "typed-reminder", label: blockItemText("mention.getReminder"),
    description: distance || (typed.withTime || nextWeek ? label : dateMentionLabel(typed.date, undefined, "full")),
    icon: "@", date: typed.date, ...zone, dateReminder: "0" };
  if (typed.reminder) return [reminder];
  const thisWeek = monday(new Date(year, month - 1, day)) === monday(now);
  const description = ahead ? "" : distance || (!typed.withTime && (typed.named || thisWeek) ? dateMentionLabel(typed.date, now, "full") : "");
  const date = { kind: "date" as const, id: "typed-date", label, description, ...(description ? { dashed: true } : {}),
    icon: "@", date: typed.date, ...zone };
  return ahead ? [date, reminder] : [date];
}

/**
 * The faint words after an "@" and what is typed, as Notion's (seen
 * 2026-11-13, in a page's lines and a comment's): with nothing typed, the
 * chosen row's — a person's name, a reminder's hint at what to type, none
 * for "초대" (seen 2026-11-24, the owner's "@" opened on it); once
 * something is typed, the rest of the first row's words while it is the one
 * chosen and they begin with what is typed, letter case and all ("@오" + "늘
 * 오전 9:00", "@한지" + "테스트페이지", "@win" + " lees", "@Win" none), but
 * never a reminder's, an invite's or a new page's. The pill shows them only
 * after an "@" that begins its line (mentionQuery).
 */
export function mentionGhostRest(query: string, item: MentionItem | MorePagesRow | null, first: boolean, only?: MentionMenuQuery["only"]) {
  // "날짜 또는 리마인더"'s dates say what to type, after what is typed too,
  // while no row is lit (Notion's, seen 2026-11-15: "@", "@내일", "@abc";
  // ↓ lighting "내일" took it away).
  if (only === "date" && !item) return blockItemText("mention.dateHint");
  if (!item) return "";
  const reminder = item.kind === "date" && item.dateReminder !== undefined;
  const words = item.label;
  if (!query) return reminder ? blockItemText("mention.commentHint") : item.kind === "invite" ? "" : words;
  if (!first || reminder || !["date", "person", "page"].includes(item.kind)) return "";
  return words.startsWith(query) ? words.slice(query.length) : "";
}

export function mentionMenuItems({
  mode, only, newPages, invite, guest, query, snapshot, searchedPeople, searchedRows, visited = [], typedDate,
}: MentionMenuQuery): MentionItem[] {
  const q = query.trim().toLowerCase();
  // "@지금" is today at this minute in the person's time zone, as Notion's
  // ("오늘 오전 11:53", seen 2026-10-26); it is found by its words alone.
  const now = zonedNow(localTimeZone());
  const pad = (value: number) => String(value).padStart(2, "0");
  // An empty "@" offers today alone, as Notion's (seen 2026-10-10). A typed
  // word that starts a day's name reads as that day, its rows those of the day
  // typed in full — today's first, then tomorrow's ("tmr" too), then
  // yesterday's: "to" gives "오늘 — 2026년 11월 9일" alone, "내" "내일" and its
  // reminder. One that starts "지금" or "now" gives this minute. The date a
  // word stands for isn't searched: "@11월" and "@11월 10일" offer no date
  // (Notion's, seen 2026-11-09). Words are begun letter by letter, so a
  // syllable being typed finds them ("ㄴ" 내일, "ㅈ" 지금; seen 2026-11-12).
  const days = [
    { offset: 0, words: [blockItemLabels().mentionToday, "today", "오늘"] },
    { offset: 1, words: [blockItemLabels().mentionTomorrow, "tomorrow", "tmr", "내일"] },
    { offset: -1, words: [blockItemLabels().mentionYesterday, "yesterday", "어제"] },
  ];
  const startsWord = (words: readonly string[]) => words.some((word) => startsInLetters(word, q));
  // "remi", "리마인더" and "알림" offer tomorrow's reminder, before a day or a
  // weekday the text begins: "@ㅇ" and "@알" offer it alone (Notion's, seen
  // 2026-11-12; "@알람" nothing).
  const remindWord = Boolean(q) && startsWord(["remind", "리마인더", "알림"]);
  // A half of the day begun before a day's name is the parser's: "@오" is
  // 오전, today at 9:00, before 오늘 (mentionTypedDate, seen 2026-11-13).
  const day = q && !remindWord && !startsWord(["오전", "오후"]) ? days.find(({ words }) => startsWord(words)) : undefined;
  const thisMinute: MentionItem & { kind: "date" } = {
    kind: "date",
    id: "now",
    label: blockItemText("mention.todayAt", { time: mentionTimeLabel(new Date(2000, 0, 1, now.hour, now.minute)) }),
    description: "",
    icon: "@",
    date: `${now.year}-${pad(now.month)}-${pad(now.day)}T${pad(now.hour)}:${pad(now.minute)}`,
    dateTimeZone: localTimeZone(),
  };
  const dates: (MentionItem & { kind: "date" })[] = mode === "page_link" ? [] : !q
    ? [{ kind: "date", id: "today", label: blockItemLabels().mentionToday, description: mentionDateDescription(0), icon: "@", date: localIsoDate(0) }]
    : [
        ...(day ? typedDateItems({ date: localIsoDate(day.offset), withTime: false, named: true, reminder: false }) : []),
        ...(startsWord(["now", "지금"]) ? [thisMinute] : []),
      ];
  // A date its parser reads takes the place of the words' rows, as Notion's "@"
  // takes a date in any format.
  if (mode !== "page_link" && typedDate && !remindWord) dates.splice(0, dates.length, ...typedDateItems(typedDate));
  else if (mode !== "page_link" && remindWord) {
    dates.push({ kind: "date", id: "remind-word", label: blockItemText("mention.getReminder"),
      description: mentionDateDescription(1), icon: "@", date: localIsoDate(1), dateReminder: "0" });
  }
  const currentUserId = snapshot.currentUserId || "local-user";
  const currentUserLabel = personLabel(currentUserId, snapshot.currentUserId, false);
  // A guest's empty "@" lists no one, its dates, pages and new pages alone, as
  // Notion's (seen 2026-11-24 in two tabs: a guest with edit or full access in
  // a page's lines, with comment access in its comment box); a typed name
  // finds people as anyone's does (the start of the owner's address found them).
  const people = mode === "page_link" || guest && !q && !only ? [] : collectMentionPeople({
    collaboratorDescription: blockItemText("person.collaborator"),
    currentUserId,
    currentUserLabel,
    currentUserNamed: Boolean(personName(currentUserId)),
    organizationProfiles: snapshot.organizationProfiles,
    pagePresencePeers: snapshot.pagePresencePeers,
    profileDescription: organizationProfileMentionDescription,
    profileLabel: organizationProfileMentionLabel,
    query: q,
    searchedPeople,
  });
  // The databases the listed pages hold in their bodies are found among the
  // pages, with a table's icon and the path to the page holding them, and
  // mentioned as databases, as Notion's "@" menu offers them (seen 2026-11-10)
  // — but one of several sources without a name of its own, which the menu
  // offers by its sources alone (seen 2026-11-24 in two tabs; the rows below).
  const databases = inlineDatabasePagesOf(snapshot.inlineDatabases?.filter((database) => database.title || (database.sourceTitles?.length ?? 0) < 2),
    snapshot.pagesById);
  const placeOf = (page: Page) => pagePlace(page as unknown as WirePage, snapshot.pagesById as unknown as Record<string, WirePage>);
  const found = q ? PAGES_FOUND.searched : PAGES_FOUND.recent;
  const pageCandidates = collectMentionPages({
    describe: (page) => placeText(placeOf(page)),
    label: pageTitle,
    // All of them, cut once the rows found are among them.
    limit: q ? Number.POSITIVE_INFINITY : found,
    pagesById: databases.size ? { ...snapshot.pagesById, ...Object.fromEntries(databases) } : snapshot.pagesById,
    query: q,
  });
  const pageItem = (page: Page, title = pageTitle(page), description = placeText(placeOf(page))): MentionItem => ({
    kind: "page",
    id: `page:${page.id}`,
    label: title,
    description,
    place: placeOf(page),
    icon: pageIcon(page),
    pageId: page.id,
    page,
    // An archived page and those under it stay found, marked (Notion's, seen 2026-11-19).
    ...(pageArchiveRoot(page, (id) => snapshot.pagesById[id]) ? { archived: true } : {}),
    // A full-page database is mentioned as a database too, by its own
    // database, as Notion's "@" makes it (seen 2026-11-10).
    ...(databases.has(page.id) ? { databaseId: page.id }
      : page.kind === "database" && page.databaseContainerId ? { databaseId: page.databaseContainerId } : {}),
  });
  const pages = pageCandidates.map(({ page, title, description }) => pageItem(page, title, description));
  // Database row pages and sources go among the workspace pages by how well
  // their names answer and then by their last edit (searchAnswerOrder),
  // under their database: a page holding only some of the words typed no
  // longer pushes a row holding them all out of the menu, as Notion's ranks
  // them together (seen 2026-11-24 in two tabs).
  const listedRows = searchedRows.filter((row) => row.dataSourceId || !snapshot.pagesById[row.databaseId ?? row.rowId]);
  const rowItem = (row: RowPageMentionCandidate) => rowMentionItem(row, snapshot.pagesById, pageTitle, pageIcon);
  const rows = listedRows.map(rowItem);
  const edited = q ? [...pages.map((item) => item.kind === "page" ? pageEditedTime(item.page) : 0), ...listedRows.map((row) => row.editedAt ?? 0)] : [];
  const ranked = [...pages, ...rows].map((item, index) => ({ item, index, rank: mentionSearchRank(item.label, q), edited: edited[index] ?? 0 }));
  // The pages and rows the reader opened lately whose titles answer lead them (mentionLead.ts).
  const searchedRow = (rowId: string) => listedRows.find((row) => row.rowId === rowId && !row.databaseId && !row.dataSourceId);
  pages.splice(0, pages.length, ...withMentionLead(ranked.sort((left, right) => searchAnswerOrder(left, right) || left.index - right.index)
    .map(({ item }) => item), visited, query, (visit) => visit.page ? pageItem(visit.page) : rowItem(searchedRow(visit.row.rowId) ?? visit.row)));
  pages.splice(found);
  // An empty "@" offers one reminder, "리마인더 받기 — 내일 오전 9시", as Notion's (seen 2026-10-10);
  // so does "날짜 또는 리마인더"'s list of dates alone (seen 2026-11-15).
  const tomorrowReminder = reminderMentionChoices().find(({ id }) => id === "remind-tomorrow");
  const emptyReminder: MentionItem[] = mode === "page_link" || only && only !== "date" || q || !tomorrowReminder ? [] : [{
    kind: "date",
    id: "remind-tomorrow",
    label: blockItemText("mention.getReminder"),
    description: blockItemText("mention.tomorrowAt", { time: mentionHourLabel(tomorrowReminder.at) }),
    icon: "@",
    date: localDateTimeValue(tomorrowReminder.at, true),
    dateTimeZone: localTimeZone(),
    dateReminder: "0",
  }];
  // A database template's mentions that take their value when it is used, as
  // Notion's (seen 2026-11-09): "지금 — 복제 일시" and "오늘 — 복제하는 날짜"
  // head the dates and "나" the people, none with an icon. A typed word finds
  // each by the start of its own name alone: "@now", "@today" and "@me" in a
  // Korean page offer none of them.
  const templates: MentionItem[] = mode === "page_link" || !snapshot.templateMentions ? [] : (["now", "today", "me"] as const)
    .map((template) => ({
      kind: "template" as const,
      id: `template:${template}`,
      label: blockItemText(`mention.template.${template}`),
      description: templateMentionDescription(template),
      icon: "",
      template,
    }))
    .filter(({ label }) => !q || startsInLetters(label, q));
  const templateDates = templates.filter((item) => item.kind === "template" && item.template !== "me");
  const templateMe = templates.filter((item) => item.kind === "template" && item.template === "me");
  // As Notion's "@" menu (seen 2026-10-01), a typed name ends the menu with
  // its "새 페이지" group, whether or not a page has that name: a sub-page of
  // this page, then a page added to a page the person picks. The "[[" page
  // link menu ends with the same group.
  // An empty "@" ends with the same group, unnamed: "새 하위 페이지 추가" and
  // "새 페이지 추가" make an untitled page (seen 2026-10-10).
  const addItem: MentionItem[] = newPages && (q || (!only && mode === "mention"))
    ? [{
        kind: "sub_page",
        id: `sub:${q}`,
        label: q ? blockItemText("mention.subPageNamed", { title: query.trim() }) : blockItemText("mention.addSubPage"),
        description: "",
        icon: "+",
        title: query.trim(),
      }, {
        kind: "add_page",
        id: `add:${q}`,
        label: q ? blockItemText("mention.addPageNamed", { title: query.trim() }) : blockItemText("mention.addPage"),
        description: "",
        icon: "↗",
        title: query.trim(),
      }]
    : [];
  // As Notion's "@" menu (seen 2026-10-01), a typed name ends the people
  // with an invite of it, after anyone it names.
  const inviteItem: MentionItem[] = mode === "mention" && invite && q
    ? [{
        kind: "invite",
        id: `invite:${q}`,
        label: blockItemText("mention.inviteNamed", { name: query.trim() }),
        description: "",
        icon: "+",
        name: query.trim(),
      }]
    : [];
  // The people end with an invite: of a typed name, or "초대" with none
  // typed — in the person mention's own list (seen 2026-11-05) and in the
  // "@" menu (seen 2026-11-07).
  const plainInvite: MentionItem[] = mode === "mention" && invite && !q
    ? [{ kind: "invite", id: "invite:", label: blockItemText("mention.invite"), description: "", icon: "+", name: "" }] : [];
  // "날짜 또는 리마인더" lists the "@" menu's dates alone, by the same words:
  // "오늘" and tomorrow's reminder, "내일" and its reminder, nothing for
  // "abc", whose empty list says "결과 없음" alone (Notion's, seen 2026-11-15
  // after "+" › "날짜 또는 리마인더"; MentionMenu).
  if (only === "date") return [...dates, ...emptyReminder];
  if (only === "person") return [...people, ...inviteItem, ...plainInvite];
  if (only) return [...people, ...pages].filter((item) => item.kind === only);
  // Notion's "@" menu keeps people and their invite before page links where
  // someone is found. An invite naming no one follows the pages found, the
  // first page chosen, whatever is typed — an address's letters alone too
  // ("@vexq", "@kribun@zqzq.zq", in a page's lines and a database's text
  // cells alike; seen 2026-11-24 in two tabs twenty minutes apart, where
  // 2026-11-09 had put the invite first for those letters) — and stands
  // alone where no page is found ("@pulmvy").
  // An empty "@" keeps its people's place for "초대" alone where no one else
  // is here, as Notion's (seen 2026-11-14 in a workspace of one).
  const named = people.length > 0 || !q ? [...templateMe, ...people, ...inviteItem, ...plainInvite, ...pages]
    : [...templateMe, ...pages, ...inviteItem];
  // Two or more letters that find someone put the people, with their
  // invite, above the dates, the pages after: "@ju", "@jun", "@june l" list
  // june lee before 2027년 6월 1일; one letter keeps the dates first ("@j",
  // "@w", "@s"), as does a date that finds no one ("@w8", "@june 5")
  // (Notion's, seen 2026-11-13).
  if (people.length > 0 && [...q].length >= 2) {
    return [...templateMe, ...people, ...inviteItem, ...plainInvite, ...templateDates, ...dates, ...emptyReminder, ...pages, ...addItem];
  }
  return [...templateDates, ...dates, ...emptyReminder, ...named, ...addItem];
}

/** The group a row of the menu is listed under, by its label. */
export function mentionGroupLabel(item: MentionItem | MorePagesRow) {
  return item.kind === "automation" ? item.groupLabel
    : item.kind === "date" || item.kind === "template" && item.template !== "me"
    ? blockItemLabels().groupDate
    : item.kind === "person" || item.kind === "invite" || item.kind === "template"
      ? blockItemLabels().groupPeople
      : item.kind === "sub_page" || item.kind === "add_page"
        ? blockItemLabels().groupNewPage
        : blockItemLabels().groupLinkToPage;
}

/** The items under their group's label, as the menu shows them, each with its place in the list. */
export function groupMentionItems<T extends MentionItem | MorePagesRow>(results: readonly T[]) {
  const groups: Array<{
    label: string;
    items: Array<{ item: T; index: number }>;
  }> = [];
  for (const [index, item] of results.entries()) {
    const label = mentionGroupLabel(item);
    const group = groups.find((candidate) => candidate.label === label);
    if (group) group.items.push({ item, index });
    else groups.push({ label, items: [{ item, index }] });
  }
  return groups;
}
