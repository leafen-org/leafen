import { personDistinction } from "@/lib/peopleDirectory";
import type { Place } from "@/lib/pagePath";
import type { OrganizationProfile, Page } from "@/lib/types";
import type { Session } from "@/types";
import type { OrganizationPeopleSearchTransport } from "@/lib/typeaheadSearch";
import type { PagePresencePeerView } from "@/lib/pagePresenceView";
import { pageTrashRoot } from "@/lib/pageTrash";
import { pageEditedTime, pageSearchRank, searchAnswerOrder } from "@/lib/pageSearchRank";
import { searchLetters } from "@/lib/menuSearch";
import type { DatabaseSourceTitle } from "@/components/editor/databaseTitles";

export type MentionTrigger = "mention" | "page_link";

export type DatabaseBlockKind = "child_database" | "inline_database";

export type MentionItem =
  | {
      kind: "date";
      id: string;
      label: string;
      description: string;
      icon: string;
      date: string;
      /** Set for @remind choices: where the time is and when to remind. */
      dateTimeZone?: string;
      dateReminder?: string;
      /** Its description shown after a dash on its line, as a reminder's time is ("오늘 — 2026년 10월 25일"). */
      dashed?: boolean;
    }
  | {
      kind: "person";
      id: string;
      label: string;
      description: string;
      icon: string;
      userId: string;
      /** The person themself, their name known: the lists' own mark follows it (SelfMark). */
      self?: boolean;
      /** Another here has the same name: what tells this one apart, after a dash. */
      distinction?: string;
    }
  | {
      kind: "page";
      id: string;
      label: string;
      description: string;
      icon: string;
      pageId: string;
      page: Page;
      /** Where it sits as its second line draws it, a title a piece (PlaceLine); the description says it in words. */
      place?: Place;
      /** A database held in a page's body, which its pick mentions as a database. */
      databaseId?: string;
      /** One source of a database holding several, drawn by a data source's icon; `databaseId` is its mention's address. */
      dataSource?: boolean;
      /** Archived itself or with a page above it, which its row marks (PickerArchivedPage.tsx). */
      archived?: boolean;
    }
  | {
      kind: "template";
      id: string;
      label: string;
      description: string;
      icon: string;
      /** A database template's mention that takes its value when the template is used. */
      template: "today" | "now" | "me";
    }
  | {
      /**
       * An automation message's mention of what its run gives — a variable,
       * the trigger page or one of its properties, who ran it, when — as
       * the formula that reads it (AutomationMessageField.tsx).
       */
      kind: "automation";
      id: string;
      label: string;
      description: string;
      icon: string;
      /** The heading it is listed under, as Notion's "사용자 지정" or "날짜". */
      groupLabel: string;
      token: string;
      /** The property type its icon is. */
      tokenType: string;
      /** The chip's name, where the menu names it otherwise ("페이지 실행의 Name"). */
      chip?: string;
      /** Lists the trigger page's properties in its place, as Notion's "페이지 실행 ›". */
      opens?: boolean;
    }
  | {
      /** A new page named by the query, made as a sub-page of this page. */
      kind: "sub_page";
      id: string;
      label: string;
      description: string;
      icon: string;
      title: string;
    }
  | {
      /** A new page named by the query, put in a page the person picks. */
      kind: "add_page";
      id: string;
      label: string;
      description: string;
      icon: string;
      title: string;
    }
  | {
      /** An invite of the name or email typed, to the page. */
      kind: "invite";
      id: string;
      label: string;
      description: string;
      icon: string;
      name: string;
    };

/** A database row page the reader may open, found for the @ menu. */
export type RowPageMentionCandidate = {
  rowId: string;
  title: string;
  icon: string;
  iconType: string;
  containerPageId: string;
  containerTitle: string;
  /** Its inline database, named in its path; none for a full-page database. */
  inlineDatabase?: boolean;
  databaseTitle?: string;
  /** What it is called while it has no title (pageDisplayTitle). */
  untitledName?: string;
  /** The database the row holds in its body, found by its name (`title`); `rowTitle` names the row. */
  databaseId?: string;
  rowTitle?: string;
  /** Its row is archived, itself or with a page above it (database_row_page_mention_search.go). */
  archived?: boolean;
  /** The workspace's People itself, `databaseId` its database, in no page list (database_row_page_mention_people.go). */
  peopleDatabase?: boolean;
  /**
   * One source of a database holding several, named by `title`, under the
   * database's page (`containerPageId`) and, inline, its own name or its
   * sources' names (database_source_mention_search.go).
   */
  dataSourceId?: string;
  sourceTitles?: DatabaseSourceTitle[];
  /** When it was edited last, in milliseconds, which orders those answering as well (searchAnswerOrder). */
  editedAt?: number;
};

/** A database a listed page holds in its body, which the @ menu offers beside the pages. */
export type MentionInlineDatabase = {
  containerId: string;
  pageId: string;
  title: string;
  icon?: string;
  iconType?: string;
  /** A view block shows another's database, none of its own. */
  viewBlock?: boolean;
  /** Its sources' names while it holds several, which name it while it has no name of its own. */
  sourceTitles?: readonly DatabaseSourceTitle[];
};

export type BlockPickerSnapshot = {
  currentUserId: string;
  organizationId?: string;
  /** The workspace whose people its search finds, among the organization's. */
  workspaceId?: string;
  organizationProfiles: readonly OrganizationProfile[];
  pagePresencePeers: readonly PagePresencePeerView[];
  pagesById: Readonly<Record<string, Page>>;
  searchOrganizationPeople: OrganizationPeopleSearchTransport;
  // Database row pages are not workspace pages, so the menu asks for them.
  searchRowPages?: (query: string, signal: AbortSignal) => Promise<RowPageMentionCandidate[]>;
  /** The databases the pages hold in their bodies, as the sidebar lists them. */
  inlineDatabases?: readonly MentionInlineDatabase[];
  /** A database template's body, whose @ menu offers "when duplicated" mentions. */
  templateMentions?: boolean;
  /** Who reads, whose visits lead what the menu finds (mentionLead.ts). */
  session?: Session;
};

export type InlineMenuAnchor = Pick<DOMRect, "left" | "top" | "bottom">;

/** How well a page or person answers an inline menu's search, by its name alone (pageSearchRank). */
export function mentionSearchRank(label: string, query: string) {
  if (!query) return 0;
  return pageSearchRank(label.toLowerCase(), query);
}

/**
 * A person is found by their name holding what was typed as typed, as
 * Notion's "@" menu finds people (seen 2026-11-06: "ee" and "june l" find
 * "june lee"; "lee june", "june  lee" and "게스트" find no one) — not by the
 * words one at a time, as pages are — and letter by letter (searchLetters):
 * "ㅇ" finds a name holding "음" (seen 2026-11-12); pages go by syllables.
 */
function personSearchRank(label: string, query: string) {
  if (!query) return 0;
  const name = searchLetters(label), typed = searchLetters(query);
  if (name === typed) return 0;
  if (name.startsWith(typed)) return 1;
  return name.includes(typed) ? 2 : Number.POSITIVE_INFINITY;
}

type MentionPerson = {
  description: string;
  id: string;
  icon: string;
  kind: "person";
  label: string;
  userId: string;
  self?: boolean;
  /** Another here has the same name: what tells this one apart (personDistinction). */
  distinction?: string;
};

export function collectMentionPeople({
  collaboratorDescription,
  currentUserId,
  currentUserLabel,
  currentUserNamed,
  organizationProfiles,
  pagePresencePeers,
  profileDescription,
  profileLabel,
  query,
  searchedPeople,
}: {
  collaboratorDescription: string;
  currentUserId: string;
  /** What the person themself is called: their name, or "나" while it is not known. */
  currentUserLabel: string;
  /** Their name is known: their row reads it with the lists' own mark after it. */
  currentUserNamed?: boolean;
  organizationProfiles: readonly OrganizationProfile[];
  pagePresencePeers: readonly PagePresencePeerView[];
  profileDescription: (profile: OrganizationProfile) => string;
  profileLabel: (profile: OrganizationProfile) => string;
  query: string;
  searchedPeople: readonly OrganizationProfile[];
}): MentionPerson[] {
  const q = query.trim().toLowerCase();
  const profileCandidates = new Map<string, {
    description: string;
    label: string;
    source: "organization" | "page";
    userId: string;
  }>();
  for (const peer of pagePresencePeers) {
    const userId = peer.userId.trim();
    if (peer.isCurrent || !userId || userId === currentUserId || profileCandidates.has(userId)) {
      continue;
    }
    profileCandidates.set(userId, {
      description: collaboratorDescription,
      label: peer.label.trim() || userId,
      source: "page",
      userId,
    });
  }
  for (const profile of [...searchedPeople, ...organizationProfiles]) {
    const userId = profile.userId?.trim();
    if (!userId || userId === currentUserId) continue;
    const existing = profileCandidates.get(userId);
    if (existing?.source === "organization") continue;
    profileCandidates.set(userId, {
      description: profileDescription(profile),
      label: profileLabel(profile),
      source: "organization",
      userId,
    });
  }
  const otherPeople = Array.from(profileCandidates.values())
    .map((profile, index) => ({
      ...profile,
      index,
      rank: personSearchRank(profile.label, q),
    }))
    .filter((candidate) => Number.isFinite(candidate.rank))
    .sort((left, right) => left.rank - right.rank ||
      left.label.localeCompare(right.label) || left.index - right.index)
    .slice(0, 8)
    .map(({ description, label, userId }) => {
      const distinction = personDistinction(userId);
      return {
        description,
        icon: label.slice(0, 1).toUpperCase(),
        id: `person:${userId}`,
        kind: "person" as const,
        label,
        userId,
        ...(distinction ? { distinction } : {}),
      };
    });
  // The person themself is found only by what is typed, never listed before
  // a letter is, as Notion's "@" (seen 2026-11-14 and again 2026-11-15 at
  // 1440: an empty "@" lists the others and "초대"; the first letter of the
  // person's own name then lists them with "(나)"). Their row is their name
  // and the mark is its own span after it, as Notion's "〈이름〉" and grey "(나)"
  // (seen 2026-11-27 in two tabs); a mention of them keeps the name alone.
  const currentRank = !q ? Number.POSITIVE_INFINITY : Math.min(
    personSearchRank(currentUserLabel, q),
    ["you", "나", "본인"].some((alias) => searchLetters(alias).includes(searchLetters(q)))
      ? 1
      : Number.POSITIVE_INFINITY,
  );
  const current = Number.isFinite(currentRank) ? [{
    description: collaboratorDescription,
    icon: currentUserLabel.slice(0, 1).toUpperCase(),
    id: `person:${currentUserId}`,
    kind: "person" as const,
    label: currentUserLabel,
    userId: currentUserId,
    ...(currentUserNamed ? { self: true } : {}),
  }] : [];
  return [...current, ...otherPeople];
}

export function collectMentionPages({ describe, label, limit, pagesById, query }: {
  describe: (page: Page) => string;
  label: (page: Page) => string;
  /** How many of the best it keeps. */
  limit: number;
  pagesById: Readonly<Record<string, Page>>;
  query: string;
}) {
  const q = query.trim().toLowerCase();
  // A page under a page in the trash is in the trash too. Those a search
  // finds go in its order (searchAnswerOrder); with nothing typed, by title.
  return Object.values(pagesById)
    .filter((page) => !pageTrashRoot(page, (id) => pagesById[id]))
    .map((page, index) => {
      const title = label(page);
      const description = describe(page);
      return { page, title, description, index, rank: mentionSearchRank(title, q), edited: 0 };
    })
    .filter((candidate) => Number.isFinite(candidate.rank))
    .map((candidate) => q ? { ...candidate, edited: pageEditedTime(candidate.page) } : candidate)
    .sort((left, right) => searchAnswerOrder(left, right) ||
      (q ? 0 : left.title.localeCompare(right.title)) || left.index - right.index)
    .slice(0, limit);
}
