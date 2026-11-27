import { closedMentionLabel, isClosedMentionText, isMissingMentionText, mentionClosedSince, namedMentionTitle } from "./closedMentions";
import { keepTitlePieces, type MentionTitlePieces } from "./mentionTitlePieces";
import { markedMentionTitle, onMarkedMentionTitles } from "./markedMentionTitles";
import { dataSourceMentionName, onDataSourceMentionNames } from "./dataSourceMentionNames";
import { i18next } from "@/i18n";
import { NOTION_USER_PREFIX, notionUserPlaceholder } from "./notionUserPlaceholder";

export type PersonDirectoryEntry = {
  userId: string;
  displayName?: string | null;
  email?: string | null;
  avatar?: string | null;
  /** An organization suspended the person; Notion crosses out their picture. */
  suspended?: boolean | null;
  /** When the account came to this server: what tells apart two of one name with no email. */
  joinedAt?: string | null;
  /**
   * A connection's bot a person value names, as Notion's people take an
   * integration's, or an app's own account (the server's lookup says so):
   * named and pictured as a connection, never "〈name〉님", never offered to pick.
   */
  bot?: boolean | null;
  /**
   * Someone invited into a person value before an account of theirs, named
   * by their address, as Notion's invite puts them into the cell at once.
   */
  invitee?: boolean | null;
  /** A former guest People keeps as deactivated: named, never offered to pick, their mention opening nothing. */
  deactivated?: boolean | null;
};

export type PersonValueBounds = {
  maxNodes: number;
  maxDepth: number;
  maxEntries: number;
};

let peopleByUserId = new Map<string, PersonDirectoryEntry>();
// The places that name people redraw when the workspace's people arrive.
let directoryRevision = 0;
// Its changes but the workspace's page titles (setMentionPageTitles): what the whole app is drawn again for.
let appRevision = 0;
const directoryListeners = new Set<() => void>();

function announceDirectory(app = true) {
  directoryRevision += 1;
  if (app) appRevision += 1;
  for (const listener of directoryListeners) listener();
}
onMarkedMentionTitles(() => announceDirectory());
onDataSourceMentionNames(() => announceDirectory());

/** The directory's revision but for the workspace's page titles. */
export function appDirectoryRevision() {
  return appRevision;
}

export function subscribePeopleDirectory(listener: () => void) {
  directoryListeners.add(listener);
  return () => { directoryListeners.delete(listener); };
}

export function peopleDirectoryRevision() {
  return directoryRevision;
}

// A guest's app hears of people as its pages show them: an unknown person
// named on screen is asked for once, in a batch with the others shown at the
// same moment (workspacePeopleDirectory.ts sets the lookup).
let lookupPeople: ((ids: string[]) => void) | null = null;
const askedPeople = new Set<string>();

/**
 * Who a page Leafen makes on its own is by — a repeating template's, a
 * scheduled automation's — named and pictured as Leafen, as Notion shows its
 * own as "Notion" with its mark (seen 2026-11-02). No one signs in as it.
 */
export const LEAFEN_SYSTEM_USER_ID = "leafen-system";
const LEAFEN_SYSTEM_NAME = "Leafen";
const LEAFEN_SYSTEM_AVATAR = "/mark-128.png";
/** Who an anonymous form response is by, "익명" with a masked face, as Notion's (seen 2026-11-05). */
export const LEAFEN_ANONYMOUS_USER_ID = "leafen-anonymous";
const LEAFEN_ANONYMOUS_AVATAR = `data:image/svg+xml,${encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20"><rect width="20" height="20" rx="10" fill="#8e8b86"/><path fill="#fff" d="M4 9h12v1.2H4zM6.2 8.2l.9-3.4h5.8l.9 3.4zM7.2 11.4a2 2 0 1 0 0 4 2 2 0 0 0 1.9-1.3h1.8a2 2 0 1 0 0-1.4H9.1a2 2 0 0 0-1.9-1.3z"/></svg>')}`;
let waitingPeople: string[] = [];
let lookupTimer: ReturnType<typeof setTimeout> | null = null;

export function setPeopleLookup(lookup: ((ids: string[]) => void) | null) {
  lookupPeople = lookup;
  askedPeople.clear();
  waitingPeople = [];
  if (lookupTimer !== null) clearTimeout(lookupTimer);
  lookupTimer = null;
}

function askForPerson(id: string) {
  if (!lookupPeople || askedPeople.has(id) || id === LEAFEN_SYSTEM_USER_ID || id === LEAFEN_ANONYMOUS_USER_ID) return;
  askedPeople.add(id);
  waitingPeople.push(id);
  lookupTimer ??= setTimeout(() => {
    const ids = waitingPeople;
    waitingPeople = [];
    lookupTimer = null;
    lookupPeople?.(ids);
  }, 30);
}

/**
 * Adds people a lookup found. The answer leaves a false out (scripts/wire-field-tag.mjs): one named unsaid is no
 * invitee (an account proved the address, people_value_invitees.go) and no former guest (shared again, 2026-11-23).
 */
export function addLookedUpPeople(maxEntries: number, people: PersonDirectoryEntry[]) {
  const next = new Map(peopleByUserId);
  for (const person of people) mergePerson(next, { ...person, suspended: person.suspended === true, bot: person.bot === true,
    invitee: person.invitee === true, deactivated: person.deactivated === true });
  assertDirectorySize(next, maxEntries);
  peopleByUserId = next;
  announceDirectory();
}

/**
 * Asks again for every person the directory knows: a guest's app, which reads
 * no list of people when it comes back, learns so of a name or a picture
 * changed while it was away, as a member's app does from the list it reads
 * again, and of an invitee whose account has proved their address since.
 */
export function askAgainForPeopleShown() {
  for (const person of peopleByUserId.values()) {
    askedPeople.delete(person.userId);
    askForPerson(person.userId);
  }
}

function assertBound(bound: number, label: string) {
  if (!Number.isSafeInteger(bound) || bound < 0) {
    throw new RangeError(`${label} requires a caller-owned nonnegative safe-integer bound.`);
  }
}

function assertInputBound(actual: number, bound: number, label: string) {
  assertBound(bound, label);
  if (actual > bound) throw new RangeError(`${label} exceed their caller-owned bound.`);
}

function mergePerson(map: Map<string, PersonDirectoryEntry>, person: PersonDirectoryEntry | undefined) {
  if (!person) return;
  const userId = person.userId.trim();
  if (!userId) return;
  const existing = map.get(userId);
  map.set(userId, {
    userId,
    displayName: person.displayName?.trim() || existing?.displayName || null,
    email: person.email?.trim() || existing?.email || null,
    // A picture said to be none is none (a lookup says so of one taken
    // away); one not said keeps what was known.
    avatar: typeof person.avatar === "string" ? person.avatar.trim() || null : existing?.avatar || null,
    suspended: person.suspended ?? existing?.suspended ?? null,
    joinedAt: person.joinedAt?.trim() || existing?.joinedAt || null,
    bot: person.bot ?? existing?.bot ?? null,
    invitee: person.invitee ?? existing?.invitee ?? null,
    deactivated: person.deactivated ?? existing?.deactivated ?? null,
  });
}

function assertDirectorySize(map: Map<string, PersonDirectoryEntry>, maxEntries: number) {
  assertBound(maxEntries, "people directory entries");
  if (map.size > maxEntries) {
    throw new RangeError("People directory entries exceed their caller-owned bound.");
  }
}

/** Replace the directory with a workspace's people, as its server lists them. */
export function setWorkspaceDirectoryPeople(maxEntries: number, people: PersonDirectoryEntry[]) {
  assertInputBound(people.length, maxEntries, "workspace people");
  const next = new Map<string, PersonDirectoryEntry>();
  for (const person of people) mergePerson(next, person);
  peopleByUserId = next;
  // People a lookup named and this list doesn't hold — a workspace's guests
  // in a person property — are asked for again as they show.
  askedPeople.clear();
  announceDirectory();
}

export function normalizePersonIds(value: unknown, bounds: PersonValueBounds): string[] {
  assertBound(bounds.maxNodes, "person value nodes");
  assertBound(bounds.maxDepth, "person value depth");
  assertBound(bounds.maxEntries, "people directory entries");

  const ids: string[] = [];
  const people: PersonDirectoryEntry[] = [];
  const stack: Array<{ value: unknown; depth: number }> = [{ value, depth: 0 }];
  let visited = 0;

  while (stack.length) {
    const current = stack.pop();
    if (!current) break;
    visited += 1;
    if (visited > bounds.maxNodes) {
      throw new RangeError("Person value nodes exceed their caller-owned bound.");
    }
    if (current.depth > bounds.maxDepth) {
      throw new RangeError("Person value depth exceeds its caller-owned bound.");
    }
    if (Array.isArray(current.value)) {
      for (let index = current.value.length - 1; index >= 0; index -= 1) {
        stack.push({ value: current.value[index], depth: current.depth + 1 });
      }
      continue;
    }
    if (!current.value) continue;
    if (typeof current.value === "string") {
      const id = current.value.trim();
      if (id) ids.push(id);
      continue;
    }
    if (typeof current.value !== "object") continue;
    const record = current.value as Record<string, unknown>;
    const id = record.id ?? record.userId;
    if (typeof id !== "string" || !id.trim()) continue;
    const userId = id.trim();
    const notion = typeof record.notion === "object" && record.notion
      ? record.notion as Record<string, unknown>
      : undefined;
    const notionPerson = typeof notion?.person === "object" && notion.person
      ? notion.person as Record<string, unknown>
      : undefined;
    people.push({
      userId,
      displayName:
        typeof record.displayName === "string"
          ? record.displayName
          : typeof record.name === "string"
            ? record.name
            : typeof notion?.name === "string"
              ? notion.name
              : null,
      email:
        typeof record.email === "string"
          ? record.email
          : typeof notionPerson?.email === "string"
            ? notionPerson.email
            : null,
      // A Notion integration an import kept (notion_import_people.go).
      bot: record.notionUserType === "bot" || null,
    });
    ids.push(userId);
  }

  const next = new Map(peopleByUserId);
  for (const person of people) mergePerson(next, person);
  assertDirectorySize(next, bounds.maxEntries);
  peopleByUserId = next;
  return ids;
}

function displayName(member: PersonDirectoryEntry | undefined) {
  return member?.displayName?.trim() || member?.email?.trim() || "";
}

// The signed-in person as the session names them, known before any list of
// people comes (a guest's workspace never lists them): their own comment,
// change or archive names them, never "나" in a sentence (2026-10-28: a
// comment just posted read "나님의 댓글").
let signedIn = { id: "", name: "" };

export function setSignedInPerson(id: string, name: string) {
  const next = { id, name: name.trim() };
  if (next.id === signedIn.id && next.name === signedIn.name) return;
  signedIn = next;
  announceDirectory();
}

/** The signed-in person's id, "" before the session says it. */
export function signedInPersonId() {
  return signedIn.id;
}

function knownName(id: string) {
  if (id === LEAFEN_SYSTEM_USER_ID) return LEAFEN_SYSTEM_NAME;
  if (id === LEAFEN_ANONYMOUS_USER_ID) return i18next.t("formView:anonymousRespondent");
  return displayName(peopleByUserId.get(id)) || (id === signedIn.id ? signedIn.name : "");
}

/** An organization role in the reader's words; one this app doesn't name stays as it is. */
export function organizationRoleLabel(role: string) {
  const key = `peopleDirectory:roles.${role}`;
  return i18next.exists(key) ? i18next.t(key) : role;
}

/** An organization membership status in the reader's words. */
export function organizationStatusLabel(status: string) {
  const key = `peopleDirectory:statuses.${status}`;
  return i18next.exists(key) ? i18next.t(key) : status;
}

/** How many people the directory knows, suspended ones too. */
export function directoryPeopleCount() {
  return peopleByUserId.size;
}

/** The people the directory knows, but suspended or deactivated ones, bots and those only invited, by name: whom a picker offers. */
export function directoryPeople(): PersonDirectoryEntry[] {
  return [...peopleByUserId.values()].filter((person) => !person.suspended && !person.deactivated && !person.bot && !person.invitee)
    .sort((left, right) => displayName(left).localeCompare(displayName(right)) || left.userId.localeCompare(right.userId));
}

/** Whether the value names a connection's bot, which shows as an integration does. */
export function personIsBot(id: string) {
  return Boolean(id && peopleByUserId.get(id)?.bot);
}

/** Whether the value names someone invited who has no account yet, named by their address. */
export function personIsInvitee(id: string) {
  return Boolean(id && peopleByUserId.get(id)?.invitee);
}

/** The address someone invited without an account yet is named by; null for anyone else. */
export function inviteeAddress(id: string) {
  const person = id ? peopleByUserId.get(id) : undefined;
  return person?.invitee ? person.email?.trim() || null : null;
}

/** Whether People lists the person as deactivated: a former guest, or someone an organization suspended. */
export function personDeactivated(id: string) {
  const person = id ? peopleByUserId.get(id) : undefined;
  return Boolean(person?.deactivated || person?.suspended);
}

/** Whether an organization suspended the person. */
export function personSuspended(id: string) {
  return Boolean(id && peopleByUserId.get(id)?.suspended);
}

/** The picture the person shows, as the directory knows it. */
export function personAvatarValue(id: string) {
  if (id === LEAFEN_SYSTEM_USER_ID) return LEAFEN_SYSTEM_AVATAR;
  if (id === LEAFEN_ANONYMOUS_USER_ID) return LEAFEN_ANONYMOUS_AVATAR;
  return id ? peopleByUserId.get(id)?.avatar?.trim() ?? "" : "";
}

/** The person's name as the directory knows it, or "" when it doesn't. */
const sameName = (name: string | null | undefined) => name?.trim().toLocaleLowerCase() ?? "";
const joinedDate = (joinedAt: string) => new Date(joinedAt.trim().replace(" ", "T"));
const joinedDay = (joinedAt: string) => joinedDate(joinedAt).toDateString();

/**
 * What tells a person apart from another of the same name where people are
 * picked or named: their email where they have one, else when they came to
 * this server — the day, and the time too when the other came the same day.
 * Null when no one else here has the name (2026-11-05: two email-less
 * "합성 멤버" read alike in the @ menu; Notion's people all have emails).
 */
export function personDistinction(id: string): string | null {
  const person = peopleByUserId.get(id);
  const name = sameName(person?.displayName);
  if (!person || !name) return null;
  const twins = [...peopleByUserId.values()].filter((other) => other.userId !== id && sameName(other.displayName) === name);
  if (twins.length === 0) return null;
  if (person.email) return person.email;
  if (!person.joinedAt || Number.isNaN(joinedDate(person.joinedAt).getTime())) return null;
  const day = joinedDay(person.joinedAt);
  const sameDay = twins.some((other) => other.joinedAt && joinedDay(other.joinedAt) === day);
  const when = new Intl.DateTimeFormat(i18next.language, sameDay
    ? { month: "long", day: "numeric", hour: "numeric", minute: "2-digit" }
    : { year: "numeric", month: "long", day: "numeric" }).format(joinedDate(person.joinedAt));
  return i18next.t("common:personJoined", { when });
}

/** A name in a one-line list of people, with what tells it apart after a dash when another has it. */
export function withPersonDistinction(id: string, label: string) {
  const distinction = personDistinction(id);
  return distinction ? `${label} — ${distinction}` : label;
}

export function personName(id: string) {
  return id ? knownName(id) : "";
}

// Pages' titles as they are now, for their mentions to read.
let pageTitles: ReadonlyMap<string, string> = new Map();

// Page titles kept without telling the places naming them yet (setMentionPageTitles).
let pageTitlesUntold = false;

/**
 * The workspace's pages as they are now, those whose titles hold a custom
 * emoji also in pieces; their mentions follow them. Kept but not told
 * (`tell` false) while a drawing reads them, which tells them once drawn
 * (tellMentionPageTitles).
 */
export function setMentionPageTitles(titles: ReadonlyMap<string, string>, pieces: ReadonlyMap<string, MentionTitlePieces> = new Map(), tell = true) {
  if (!keepTitlePieces("page", pieces) && sameTitles(titles, pageTitles)) return;
  pageTitles = titles;
  if (tell) announceDirectory(false);
  else pageTitlesUntold = true;
}

/** The page titles mentions read now (setMentionPageTitles). */
export function mentionPageTitlesNow(): ReadonlyMap<string, string> {
  return pageTitles;
}

/** Tells the places naming pages of the titles kept without telling them. */
export function tellMentionPageTitles() {
  if (!pageTitlesUntold) return;
  pageTitlesUntold = false;
  announceDirectory(false);
}

// Mentioned databases' names as they are now, by their mentions' database IDs.
let databaseTitles: ReadonlyMap<string, string> = new Map();

/** The mentioned databases' names as read now, with pieces as setMentionPageTitles; their mentions follow them. */
export function setMentionDatabaseTitles(titles: ReadonlyMap<string, string>, pieces: ReadonlyMap<string, MentionTitlePieces> = new Map()) {
  if (!keepTitlePieces("database", pieces) && sameTitles(titles, databaseTitles)) return;
  databaseTitles = titles;
  announceDirectory();
}

// Mentioned blocks as read now, by "<pageId>#<blockId>": their page's title and their words.
export type MentionedBlock = { title: string; words: string };
let mentionedBlocks: ReadonlyMap<string, MentionedBlock> = new Map();

/** The mentioned blocks as read now; their mentions follow them. */
export function setMentionedBlocks(blocks: ReadonlyMap<string, MentionedBlock>) {
  if (blocks.size === mentionedBlocks.size && [...blocks].every(([key, block]) => {
    const shown = mentionedBlocks.get(key);
    return shown?.title === block.title && shown.words === block.words;
  })) return;
  mentionedBlocks = blocks;
  announceDirectory();
}

/** The key a mentioned block is read by. */
export function mentionedBlockKey(pageId: string, blockId: string) {
  return `${pageId}#${blockId}`;
}

// What a mention of a block without words was written with through the API, as Notion's API reads one.
const UNWORDED_BLOCK_MENTION = "Untitled";

/**
 * A mention of a block as it reads now, as Notion's draws one (seen
 * 2026-11-11): its page's title, " - " and the block's words now, "제목 없음"
 * for a block without them; the words it was written with until the block
 * is read. One whose page is known by neither stays as it is (a published
 * page's, which comes named so).
 */
function withCurrentBlockMentionName<T extends { pageId?: string; blockId?: string; text?: string; unavailable?: boolean }>(span: T): T {
  if (isMissingMentionText(span.text)) return { ...span, unavailable: true };
  if (span.unavailable || isClosedMentionText(span.text)) return { ...span, unavailable: true, text: closedMentionLabel() };
  const read = span.pageId && span.blockId ? mentionedBlocks.get(mentionedBlockKey(span.pageId, span.blockId)) : undefined;
  const title = (span.pageId ? namedMentionTitle(span.pageId) ?? pageTitles.get(span.pageId) : undefined) ?? read?.title;
  if (title === undefined) return span;
  const written = span.text === UNWORDED_BLOCK_MENTION ? "" : span.text ?? "";
  const words = (read ? read.words : written) || i18next.t("common:labels.untitled");
  const text = `${title || i18next.t("common:labels.newPage")} - ${words}`;
  return text === span.text ? span : { ...span, text };
}

function sameTitles(next: ReadonlyMap<string, string>, shown: ReadonlyMap<string, string>) {
  return next.size === shown.size && [...next].every(([id, title]) => shown.get(id) === title);
}

/**
 * A mention as it reads now: a page's or a database's by its title, which it follows as Notion's
 * does (seen 2026-11-03); a page the reader's list doesn't have by its title as read once it is on
 * screen (markedMentionTitles.ts), the name it was written with until then, and one still closed
 * to the reader reads "사용 권한 없음", or "찾을 수 없음" when nothing answers to it
 * (closedMentions.ts), one closed or gone since its words came too (mentionClosedSince). A person's by withCurrentPersonName.
 */
export function withCurrentMentionName<T extends { mention?: string; userId?: string; pageId?: string; blockId?: string; databaseId?: string; text?: string; unavailable?: boolean }>(span: T): T {
  if (span.mention !== "page" && span.mention !== "database") return withCurrentPersonName(span);
  const closedSince = mentionClosedSince(span, pageTitles);
  if (closedSince || (span.mention === "page" && span.blockId)) return closedSince ?? withCurrentBlockMentionName(span);
  const title = span.mention === "page" && span.pageId ? namedMentionTitle(span.pageId) ?? pageTitles.get(span.pageId) ?? markedMentionTitle(span.pageId)
    : span.mention === "database" && span.databaseId ? databaseTitles.get(span.databaseId) ?? dataSourceMentionName(span.databaseId) : undefined;
  if (title) return title === span.text && !span.unavailable ? span : { ...span, text: title, unavailable: undefined };
  if (isMissingMentionText(span.text)) return { ...span, unavailable: true }; // One nothing answers to keeps its marks, its drawing says so by (richtext.ts).
  if (span.unavailable || isClosedMentionText(span.text)) return { ...span, unavailable: true, text: closedMentionLabel() };
  return span;
}

/**
 * A mention on a published page as it reads now: one of a block comes named
 * by the page it is published on (public_content_mentions.go); any other as
 * withCurrentMentionName reads it.
 */
export function withCurrentPublishedMentionName<T extends { mention?: string; userId?: string; pageId?: string; blockId?: string; databaseId?: string; text?: string; unavailable?: boolean }>(span: T): T {
  return span.mention === "page" && span.blockId ? span : withCurrentMentionName(span);
}

/**
 * A person mention as it reads now (2026-10-20): "@" and the person's name, which follows the person's renames, as
 * Notion's mention does; the name it was written with stands for a person the directory doesn't know — a deleted
 * account, someone the reader can't see, a Notion person not mapped yet — who is asked for once.
 */
export function withCurrentPersonName<T extends { mention?: string; userId?: string; text?: string }>(span: T): T {
  const id = span.mention === "person" ? span.userId?.trim() ?? "" : "";
  if (!id || id.startsWith(NOTION_USER_PREFIX)) return span;
  const name = personName(id);
  if (!name) {
    askForPerson(id);
    return span;
  }
  return `@${name}` === span.text ? span : { ...span, text: `@${name}` };
}

/** What a person not known is called (a deleted account, a removed member). */
export function unknownPersonLabel() {
  return i18next.t("peopleDirectory:unknown");
}

export function personLabel(id: string, currentUserId?: string, ownSuffix = true) {
  if (!id) return "";
  const label = knownName(id);
  if (label)
    return ownSuffix && currentUserId && id === currentUserId
      ? `${label} (${i18next.t("peopleDirectory:youSuffix")})`
      : label;
  if (currentUserId && id === currentUserId) return i18next.t("peopleDirectory:you");
  if (id === "local-user") return i18next.t("peopleDirectory:you");
  askForPerson(id); // A Notion person kept by ID reads by the name a migration kept, once asked.
  const importedPlaceholder = notionUserPlaceholder(id);
  if (importedPlaceholder) return importedPlaceholder;
  // A removed guest/former member can leave a stable UUID on old records.
  // Showing fragments such as "User 8f31...b921" leaks implementation detail
  // and looks like a corrupt identity; one not known is "알 수 없는 사용자",
  // as every other screen names them — not "게스트", which they weren't
  // (2026-11-06: a deleted editor read "게스트" on the home's recent cards).
  return unknownPersonLabel();
}

/** Who last edited, as a page menu's foot names them: by name, as Notion's
 * does even for the reader (seen 2026-10-08), else as actorLabel would. */
export function editorName(id: string | null | undefined, currentUserId: string) {
  return (id ? personName(id) : "") || actorLabel(id, currentUserId);
}

/** Who did something, as a sentence names them: the name alone, without the
 * " (나)" lists add (2026-10-28: "뷰 관리자 (나)님이 보관함"). */
export function actorLabel(id?: string | null, currentUserId?: string) {
  return id ? personLabel(id, currentUserId, false) : i18next.t("peopleDirectory:you");
}
