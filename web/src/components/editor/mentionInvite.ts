import { useEffect, useState } from "react";
import { request } from "@/apiTransport";
import { PUBLIC_CONTENT_CHANGED_EVENT } from "@/components/page/pageShareEvents";
import { personValue, storePersonValue } from "@/lib/browserStorage";
import { inviteeAddress, personLabel } from "@/lib/peopleDirectory";
import { newId } from "@/lib/ids";
import { publicMutationService } from "@/lib/publicMutationService";
import { userErrorText } from "@/lib/settingsErrors";
import type { ToastAction, ToastMessage } from "@/lib/toastStackView";
import type { TextSpan } from "@/lib/types";
import { invitedPeopleValues } from "@/lib/workspacePeopleDirectory";
import { publicMutationActions, publicMutationPrincipalTypes, publicMutationRoles } from "@/publicMutationWire.generated";
import type { Session } from "@/types";
import { blockItemText } from "./blockItemLabels";

/**
 * Mentioning someone the page doesn't open to, as Notion's (seen 2026-11-07):
 * the "@" menu marks them at the end of their row — a person with a plus
 * while they may be invited, a crossed bell once they won't be told — and
 * picking one asks in the corner whether to invite them, with their face, a
 * line that they won't be told unless invited, and "무시" or "초대",
 * going by itself after five seconds unless pointed at (seen 2026-11-12). It
 * asks once a page and person: closed, ignored or let go, the next mention
 * of them there is the crossed bell. "초대" shares the page with them to edit
 * and says "초대 보냄" with an undo; one invited is no longer counted as
 * asked, so once the undo takes the share back the next mention asks again
 * (seen 2026-11-12). Only someone who may share the page is asked; to anyone
 * else the person is the crossed bell.
 */

export type MentionReach = "invite" | "silent";

type ReachAnswer = { unreachable: string[]; canShare: boolean; canInvite?: boolean; guests?: string[]; restricted?: string[] };

/** A person's mark beside their name in the "@" menu: a guest of the page's workspace or one of its restricted members. */
export type MentionPersonBadge = "guest" | "restricted";

const ASKED_KEY = "mentionInviteAsked";
// The pairs kept, newest last; older ones are let go.
const ASKED_KEPT = 500;
const REACH_KEPT_MS = 30_000;
// The question's own time on screen, Notion's (2026-11-12: gone 5.3s after it showed).
const ASK_SHOWN_MS = 5_000;

const reachCache = new Map<string, { at: number; answer: Promise<ReachAnswer> }>();

function readReach(session: Session, pageId: string, ids: readonly string[]): Promise<ReachAnswer> {
  const key = `${session.user.id}\u0000${pageId}\u0000${[...ids].sort().join(",")}`;
  const kept = reachCache.get(key);
  if (kept && Date.now() - kept.at < REACH_KEPT_MS) return kept.answer;
  const query = new URLSearchParams(ids.map((id) => ["userId", id]));
  const answer = request<ReachAnswer>(`/pages/${encodeURIComponent(pageId)}/mention-reach?${query}`, {}, session);
  reachCache.set(key, { at: Date.now(), answer });
  answer.catch(() => reachCache.delete(key));
  return answer;
}

/** Of these people, those the page doesn't open to; null when that can't be read. */
export async function pageUnreachablePeople(session: Session, pageId: string, ids: readonly string[]): Promise<ReadonlySet<string> | null> {
  if (!pageId || ids.length === 0) return new Set();
  const answer = await readReach(session, pageId, ids.slice(0, 50)).catch(() => null);
  return answer ? new Set(answer.unreachable) : null;
}

/** Forgets what was read of a page and says it was shared otherwise, as the share window does: its top bar's mark and lists follow. */
function sharedOtherwise(pageId: string) {
  for (const key of reachCache.keys()) if (key.split("\u0000")[1] === pageId) reachCache.delete(key);
  window.dispatchEvent(new Event(PUBLIC_CONTENT_CHANGED_EVENT));
}

function askedPairs(): string[] {
  try {
    const kept = JSON.parse(personValue(ASKED_KEY) ?? "[]") as unknown;
    return Array.isArray(kept) ? kept.filter((value): value is string => typeof value === "string") : [];
  } catch {
    return [];
  }
}

function wasAsked(pageId: string, userId: string) {
  return askedPairs().includes(`${pageId}:${userId}`);
}

function rememberAsked(pageId: string, userId: string, asked = true) {
  const pair = `${pageId}:${userId}`;
  const pairs = askedPairs().filter((kept) => kept !== pair);
  storePersonValue(ASKED_KEY, JSON.stringify(asked ? [...pairs, pair].slice(-ASKED_KEPT) : pairs));
}

/**
 * What the "@" menu marks on each offered person: at the end of their row,
 * whether the page opens to them (none for someone it does); beside their
 * name, "게스트" or "제한됨" as the share window marks them (Notion's, seen
 * 2026-11-07, 2026-11-11).
 */
export function useMentionReach(session: Session | null | undefined, pageId: string, userIds: readonly string[]): {
  reach: ReadonlyMap<string, MentionReach>; badges: ReadonlyMap<string, MentionPersonBadge>;
} {
  const key = userIds.join(",");
  const [read, setRead] = useState<{ key: string; reach: ReadonlyMap<string, MentionReach>; badges: ReadonlyMap<string, MentionPersonBadge> }>(
    { key: "", reach: new Map(), badges: new Map() });
  useEffect(() => {
    if (!session || !pageId || !key) return;
    let current = true;
    const ids = key.split(",");
    void readReach(session, pageId, ids).then((answer) => {
      if (!current) return;
      const reach = new Map<string, MentionReach>();
      for (const id of answer.unreachable) {
        reach.set(id, answer.canShare && !wasAsked(pageId, id) ? "invite" : "silent");
      }
      const badges = new Map<string, MentionPersonBadge>([
        ...(answer.guests ?? []).map((id) => [id, "guest"] as const),
        ...(answer.restricted ?? []).map((id) => [id, "restricted"] as const),
      ]);
      setRead({ key: `${pageId}|${key}`, reach, badges });
    }, () => undefined);
    return () => { current = false; };
  }, [key, pageId, session]);
  return read.key === `${pageId}|${key}` ? read : { reach: new Map(), badges: new Map() };
}

// What each page last answered with no one named, shown until it answers again.
const pageAnswered = new Map<string, { canInvite: boolean; canShare: boolean }>();

/** What the server says of the reader on the page with no one named: what it answered last until it answers again, none before it first does. */
function usePageInvites(session: Session | null | undefined, pageId: string, active: boolean) {
  const key = `${session?.user.id ?? ""}\u0000${pageId}`;
  const [answered, setAnswered] = useState(() => pageAnswered.get(key) ?? null);
  useEffect(() => {
    setAnswered(pageAnswered.get(key) ?? null);
    if (!session || !pageId || !active) return;
    let current = true;
    void readReach(session, pageId, []).then((answer) => {
      const kept = { canInvite: answer.canInvite === true, canShare: answer.canShare === true };
      pageAnswered.set(key, kept);
      if (current) setAnswered(kept);
    }, () => undefined);
    return () => { current = false; };
  }, [active, key, pageId, session]);
  return active ? answered : null;
}

/**
 * Whether the "@" menu offers to invite someone not yet here — "'〈typed〉'
 * 초대…", or "초대" with nothing typed — as the server says for the page
 * (page_mention_reach.go: its workspace's owner, as Notion's).
 */
export function useMentionInviteOffer(session: Session | null | undefined, pageId: string, active: boolean): boolean {
  return usePageInvites(session, pageId, active)?.canInvite === true;
}

/**
 * Whether a person picker with no one left to list offers "사용자 초대 ›",
 * sharing the page with whoever is invited, as the server says for the page:
 * the reader may share it (full access, a guest's too), as Notion's picker
 * (PeoplePickerEmpty).
 */
export function usePeopleInviteOffer(session: Session | null | undefined, pageId: string, active: boolean): boolean {
  return usePageInvites(session, pageId, active)?.canShare === true;
}

type Notify = (
  message: string, tone?: "default" | "success" | "error", actions?: ToastAction | readonly ToastAction[],
  icon?: ToastMessage["icon"], detail?: string, person?: ToastMessage["person"],
) => void;

/**
 * After a person is mentioned: when the page doesn't open to them and the
 * mentioner may share it, asks once whether to invite them — again only
 * after an invitation was taken back.
 */
export async function offerMentionInvite({ session, pageId, userId, name, notify }: {
  session: Session; pageId: string; userId: string; name: string; notify: Notify;
}) {
  if (!pageId || !userId || userId === session.user.id || wasAsked(pageId, userId)) return;
  const answer = await readReach(session, pageId, [userId]).catch(() => null);
  if (!answer?.canShare || !answer.unreachable.includes(userId) || wasAsked(pageId, userId)) return;
  rememberAsked(pageId, userId);
  const ignore: ToastAction = { label: blockItemText("mention.inviteIgnore"), onClick: () => {} };
  const invite: ToastAction = {
    label: blockItemText("mention.invite"),
    icon: "none",
    autoDismissMs: ASK_SHOWN_MS,
    onClick: () => { rememberAsked(pageId, userId, false); return invitePerson(session, pageId, userId, notify); },
  };
  notify(blockItemText("mention.inviteAsk"), "default", [ignore, invite], "none",
    blockItemText("mention.inviteAskDetail"), { userId, name });
}

async function invitePerson(session: Session, pageId: string, userId: string, notify: Notify) {
  const controller = new AbortController();
  // Someone invited by an address without an account yet is shared with by the address.
  const address = inviteeAddress(userId);
  const principal = address ? { principalType: publicMutationPrincipalTypes.email, principalId: address }
    : { principalType: publicMutationPrincipalTypes.user, principalId: userId };
  try {
    const { state } = await publicMutationService.state(session, pageId, controller.signal);
    const result = await publicMutationService.mutate(session, pageId, {
      requestId: newId(), expectedRevision: state.revision, action: publicMutationActions.grantPermission,
      ...principal, role: publicMutationRoles.edit,
    }, controller.signal);
    sharedOtherwise(pageId);
    const granted = result.state.permissions.find((permission) =>
      permission.principalType === principal.principalType && permission.principalId.toLowerCase() === principal.principalId.toLowerCase());
    const undo: ToastAction = {
      label: blockItemText("undo"),
      icon: "none",
      onClick: async () => {
        if (!granted) return;
        try {
          const latest = await publicMutationService.state(session, pageId, controller.signal);
          await publicMutationService.mutate(session, pageId, {
            requestId: newId(), expectedRevision: latest.state.revision,
            action: publicMutationActions.removePermission, permissionId: granted.id,
          }, controller.signal);
          sharedOtherwise(pageId);
        } catch (error) {
          notify(userErrorText(error, blockItemText("mention.inviteUndoFailed")), "error");
        }
      },
    };
    notify(blockItemText("mention.inviteSent"), "default", undo, undefined, blockItemText("mention.inviteSentDetail"));
  } catch (error) {
    notify(userErrorText(error, blockItemText("mention.inviteFailed")), "error");
  }
}

/**
 * What an "@"'s invite writes in place of the "@" and what was typed after
 * it, as Notion's "초대와 멘션" (seen 2026-11-12: "@〈주소〉" invited became a
 * mention of that person, named by the address until they join): a mention
 * of whom the workspace's person values name for each address
 * (invitedPeopleValues), each followed by a space.
 */
export async function invitedPersonMentions(session: Session, workspaceId: string, emails: readonly string[]): Promise<TextSpan[]> {
  const people = await invitedPeopleValues(session, workspaceId, emails);
  return people.flatMap((userId) => [{ text: `@${personLabel(userId, undefined, false)}`, mention: "person" as const, userId }, { text: " " }]);
}
