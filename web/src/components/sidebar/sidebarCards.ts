import { personValue, storePersonValue } from "@/lib/browserStorage";
import type { Workspace } from "@/types";

/**
 * The sidebar's one card place, over its foot where Notion keeps its "멤버
 * 초대" card (user decision 2026-11-22): one card at a time, the first of the
 * lines below whose turn it is. A card handled — its × or a press on it —
 * leaves the place empty for 24 hours, then the next one comes. A card whose
 * reason went (the outside address turned on, an agent connected) leaves by
 * itself and doesn't come back. Days and times are counted on this browser
 * alone and never sent. A new card is one more line here, named by the user.
 *
 * Guests and an app's workspace have none of them (the place isn't drawn
 * there); a member who adds no one (restricted) has no agents' settings and
 * no invitation either, so none. The GitHub star that came after seven days
 * of use left with the in-app credit (user decision 2026-11-25): its ask is
 * the workspace menu's one line now (WorkspaceMenuGitHubLine.tsx).
 */
export type SidebarCardId = "outside-address" | "connect-agent" | "invite-members";

/** What the place knows; null while it is still read, which keeps every card after it waiting. */
export type SidebarCardFacts = {
  /** Whether this person administers the server. */
  administrator: boolean | null;
  /** Whether the server has an address people reach it at from outside. */
  outsideAddress: boolean | null;
  /** Whether this person has an agent connected, in any workspace. */
  agentConnected: boolean | null;
};

type SidebarCardRule = {
  id: SidebarCardId;
  /** Whether a member of that kind may be shown it. */
  shownTo: (place: { restricted: boolean }) => boolean;
  /** Whether its reason holds now: null while not yet known. */
  due: (facts: SidebarCardFacts) => boolean | null;
  /** Whether its reason went for good: the card is put away then. */
  gone?: (facts: SidebarCardFacts) => boolean;
  /** Where its × puts it away: for the person everywhere, or in that workspace only. */
  away: "person" | "workspace";
  /** A press puts it away too: a one-time ask. */
  pressPutsAway: boolean;
};

/** How long the place stays empty after a card is handled. */
export const CARD_REST_MS = 24 * 60 * 60_000;

const unknownOr = (value: boolean | null, holds: (known: boolean) => boolean) => value === null ? null : holds(value);

/** The lines, in the order they take the place. */
export const SIDEBAR_CARDS: readonly SidebarCardRule[] = [
  // 1. The outside address, for the administrator while there is none: phones, other devices and the chat apps need it.
  { id: "outside-address", shownTo: ({ restricted }) => !restricted,
    due: (facts) => facts.administrator === false ? false : facts.administrator === null ? null : unknownOr(facts.outsideAddress, (on) => !on),
    gone: (facts) => facts.administrator === true && facts.outsideAddress === true, away: "person", pressPutsAway: false },
  // 2. Connecting an agent, for anyone with none connected.
  { id: "connect-agent", shownTo: ({ restricted }) => !restricted,
    due: (facts) => unknownOr(facts.agentConnected, (connected) => !connected),
    gone: (facts) => facts.agentConnected === true, away: "person", pressPutsAway: false },
  // 3. Inviting members, as Notion's (SidebarCardSlot.tsx).
  { id: "invite-members", shownTo: ({ restricted }) => !restricted, due: () => true, away: "workspace", pressPutsAway: false },
];

const REST = "sidebar-card-rest";
// The invitation's own key from before it took a line here, so a card put away stays away.
const awayKey = (id: SidebarCardId) => id === "invite-members" ? "sidebar-invite-card-dismissed" : `sidebar-card-away-${id}`;

function isAway(rule: SidebarCardRule, userId: string, workspaceId: string) {
  return personValue(awayKey(rule.id), rule.away === "workspace" ? { userId, part: workspaceId } : { userId }) === "1";
}

function putAway(rule: SidebarCardRule, userId: string, workspaceId: string) {
  storePersonValue(awayKey(rule.id), "1", rule.away === "workspace" ? { userId, part: workspaceId } : { userId });
}

/** The workspaces the place is drawn in, with what kind of member this person is there. */
export function cardPlace(workspace: Workspace | null): { restricted: boolean } | null {
  if (!workspace || workspace.guest || workspace.appInstallId) return null;
  return { restricted: Boolean(workspace.restricted) };
}

/** The lines that may still come, in order: not put away, shown to this member. */
export function cardsStillToCome(userId: string, workspace: Workspace | null): SidebarCardId[] {
  const place = cardPlace(workspace);
  if (!place || !workspace) return [];
  return SIDEBAR_CARDS.filter((rule) => rule.shownTo(place) && !isAway(rule, userId, workspace.id)).map((rule) => rule.id);
}

/**
 * The card the place shows now, or null: empty while it rests, and while a
 * card ahead of the first due one is still being read.
 */
export function sidebarCardNow(userId: string, workspace: Workspace | null, facts: SidebarCardFacts, now = Date.now()): SidebarCardId | null {
  if (!workspace || Number(personValue(REST, { userId }) ?? 0) > now) return null;
  const coming = new Set(cardsStillToCome(userId, workspace));
  for (const rule of SIDEBAR_CARDS) {
    if (!coming.has(rule.id)) continue;
    const due = rule.due(facts);
    if (due === null) return null;
    if (due) return rule.id;
  }
  return null;
}

/** Puts away, for good, the cards whose reason went. */
export function putAwayGoneCards(userId: string, workspace: Workspace | null, facts: SidebarCardFacts) {
  if (!cardPlace(workspace) || !workspace) return;
  for (const rule of SIDEBAR_CARDS) if (rule.gone?.(facts)) putAway(rule, userId, workspace.id);
}

/** A card handled: its × puts it away, a press too where it is a one-time ask; either rests the place 24 hours. */
export function handleSidebarCard(userId: string, workspace: Workspace, id: SidebarCardId, how: "press" | "dismiss", now = Date.now()) {
  const rule = SIDEBAR_CARDS.find((line) => line.id === id);
  if (!rule) return;
  if (how === "dismiss" || rule.pressPutsAway) putAway(rule, userId, workspace.id);
  storePersonValue(REST, String(now + CARD_REST_MS), { userId });
}
