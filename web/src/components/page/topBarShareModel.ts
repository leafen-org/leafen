import { personDistinction } from "@/lib/peopleDirectory";
import { plainTitleOf } from "@/lib/closedMentions";
import type { TextSpan } from "@/lib/types";
import type { Page } from "@/types";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import type { TFunction } from "i18next";
import { inviteMail } from "@/lib/inviteMail";
import { listNames } from "@/lib/listNames";
import {
  publicMutationPrincipalTypes,
  publicMutationStrings,
  type PublicMutationPrincipalOption,
  type PublicMutationPrincipalType,
  type PublicMutationState,
} from "@/publicMutationWire.generated";
import { outOfTrashIn, pageMapOf } from "@/lib/pageIndex";
import { pageHref } from "@/lib/navigation";
/** Sharing needs a page's identity and presentation, including row pages
 * whose editor projection does not carry generic page content/cursors. */
export type SharePage = { id: string; workspaceId: string; title: string; parentId?: string | null; inTrash?: boolean; kind?: "page" | "database";
  /** A database that is a view block's page, "제목 없음" while untitled (untitledPageDisplayTitle). */
  viewBlock?: boolean;
  /** Its title's spans: a plain title says its mentions by them, and a screen draws its custom emoji (TitleWithCustomEmoji). */
  titleSpans?: readonly TextSpan[];
  /** The page's own icon, shown where it gives sharing to the pages under it. */
  icon?: string; iconType?: Page["iconType"]; hasContent?: boolean };

/** How a change the share window sent ended: done, refused and read back, or neither. */
export type ShareMutationOutcome = "succeeded" | "recovered" | "failed";

/**
 * Whether a change the share window sent took, which decides whether what
 * was typed for it is put away: only when it was done. A refusal read back
 * ("recovered" — an address another site has or the site can't have, the
 * sharing changed elsewhere) left the site as it was, and one the server
 * never answered ("failed") isn't known to have taken; either keeps the
 * field open with what was typed and why under it, to fix and send again.
 */
export function shareChangeTook(outcome: ShareMutationOutcome) {
  return outcome === "succeeded";
}

/** The web link's expiry choices, as Notion's (seen 2026-11-05): none, or an hour, a day or a week from now. */
export type ExpiryChoice = "never" | "1h" | "1d" | "7d";
export type SelectedPrincipal = {
  label: string;
  principalType: PublicMutationPrincipalType;
  principalId: string;
  /** A guest of the workspace, whom sharing invites as an address does. */
  guest?: boolean;
  /** One of the workspace's restricted members. */
  restricted?: boolean;
};

export const expiryChoices: ExpiryChoice[] = ["never", "1h", "1d", "7d"];

/**
 * The property "새 규칙 추가" starts on: the only one it offers, chosen with
 * "규칙 생성하기" ready, and none while it offers more, as Notion's (seen
 * 2026-11-18: in Chrome at 1440 in a Business database, "사람" and "생성자"
 * offered none chosen and the button faint; with "사람" ruled, "생성자" alone
 * came ✓ and the button blue; w1949 saw the same on a Galaxy A90 at 411).
 */
export function soleRuleProperty<T>(properties: readonly T[]): T | null {
  return properties.length === 1 ? properties[0] : null;
}

/** The line under a name in the share window: their address, as Notion's. A
 * person without one has just their name; anything else says what it is. */
export function sharePrincipalNote(principal: { principalType: string; label: string; email: string; principalId?: string | null }, t: (key: string) => string) {
  if (principal.email && principal.email !== principal.label) return principal.email;
  if (principal.principalType !== publicMutationPrincipalTypes.user) return t(`topBar:${principal.principalType}`);
  // A person with no email shown is told apart from another of the same name (personDistinction).
  return principal.principalId ? personDistinction(principal.principalId) ?? "" : "";
}

/** A page's title as plain text (plainTitleOf), or untitled — an untitled
 * database by the name its own page gives it, "새 데이터베이스", as Notion's
 * publish preview names one (seen 2026-11-11). */
export function pageTitle(page: SharePage, untitled: string) {
  return plainTitleOf(page) || (page.kind === "database" ? untitledPageDisplayTitle(page) : untitled);
}

/**
 * What an untitled page's site heads its link previews with, and the link
 * title field's placeholder: Leafen's own line, in every language, from the
 * contract the server's head reads too (publicMutationStrings), as Notion
 * heads an untitled page with a line of its own (seen 2026-11-12).
 */
export const SITE_UNTITLED_HEAD_TITLE: string = publicMutationStrings.untitledHeadTitle;

/**
 * The title a page's site heads its link previews with until it is given a
 * link title (public_link_preview.go publicHeadTitleOrProduct): "〈title〉 |
 * Leafen", as Notion's link title starts out "〈title〉 | Notion" (seen
 * 2026-11-12). An untitled page has none of its own: its field starts empty
 * over SITE_UNTITLED_HEAD_TITLE, which its head says, as Notion's.
 */
export function siteHeadTitle(page: SharePage) {
  const title = plainTitleOf(page);
  return title ? `${title} | Leafen` : "";
}

/** The page a page's sharing comes from, or left, by its title or else by
 * what an untitled one of its kind is called — a database item's, its
 * database (Notion's banner, seen 2026-11-11). */
export function shareParentTitle(parent: { title: string; database?: boolean }) {
  return parent.title || untitledPageDisplayTitle(parent.database ? "database" : "page");
}

/**
 * Where an unlinked page's "기본값 보기" and the name in its band go: the
 * page above, or, for a page at its teamspace's top, the teamspace's members
 * in its settings, as Notion's (seen 2026-11-16; its code, read the same day).
 */
export function shareUnlinkDefaultsHref(unlink: { parentId: string; teamspace?: boolean }) {
  if (!unlink.teamspace) return pageHref(unlink.parentId);
  const url = new URL(window.location.href);
  url.searchParams.set("teamspace-settings", unlink.parentId);
  url.searchParams.set("teamspace-tab", "members");
  return `${url.pathname}${url.search}${url.hash}`;
}

/** A new site's address: the title's letters and the page's id, as Notion's
 * "Team-notes-40f1…" (seen 2026-11-05), so two pages never ask for
 * the same one (a title of Korean and "page" alone made "page" twice). */
export function defaultSiteSlug(page: SharePage, untitled: string) {
  const idPart = page.id.toLowerCase().replace(/[^a-z0-9]/g, "").slice(0, 16) || "home";
  const titleSlug = siteSlugWords(pageTitle(page, untitled), 60 - idPart.length - 1);
  return `${titleSlug || "page"}-${idPart}`;
}

/** Words as a site's address takes them: small letters and digits, a "-"
 * between, at most `limit` letters (the server's rule, public_visibility.go). */
function siteSlugWords(value: string, limit: number) {
  return value
    .normalize("NFKD")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, limit)
    .replace(/-+$/g, "");
}

/** An address someone typed, as the site will have it ("AB07 Slug" → "ab07-slug"). */
export function normalizeSiteSlug(value: string) {
  return siteSlugWords(value, 60);
}

export function expiryAt(choice: ExpiryChoice, now = Date.now()) {
  if (choice === "never") return "";
  const hours = choice === "1h" ? 1 : choice === "1d" ? 24 : 7 * 24;
  return new Date(now + hours * 60 * 60 * 1000).toISOString();
}

/** People and groups to invite: by name or email, not already shared with,
 * and never the people named in `excludedUserIds` (the person sharing), with
 * `leadUserId` first when offered. */
export function matchingPrincipalOptions(
  options: PublicMutationPrincipalOption[],
  permissions: Array<{ principalType: PublicMutationPrincipalType; principalId: string }>,
  query: string,
  excludedUserIds: string[] = [],
  leadUserId = "",
) {
  const normalized = query.trim().toLocaleLowerCase();
  const leads = (option: PublicMutationPrincipalOption) => option.principalType === publicMutationPrincipalTypes.user && option.principalId === leadUserId;
  return [...options.filter(leads), ...options.filter(option => !leads(option))].filter((option) => (
    (!normalized || `${option.label} ${option.email}`.toLocaleLowerCase().includes(normalized))
    && !(option.principalType === publicMutationPrincipalTypes.user && excludedUserIds.includes(option.principalId))
    && !permissions.some((permission) => (
      permission.principalType === option.principalType && permission.principalId === option.principalId
      // A guest who came in by an address this page is shared with has it already.
      || permission.principalType === publicMutationPrincipalTypes.email && Boolean(option.email)
        && permission.principalId.trim().toLocaleLowerCase() === option.email.toLocaleLowerCase()
    ))
  )).slice(0, 8);
}

/** The field's test of an address, as Notion's: a name, one @ and a dotted
 * domain. "guest-a3" and "guest-a3@ex" invite no one. */
export function isInviteAddress(text: string) {
  return /^[^\s@,]+@[^\s@,.]+(\.[^\s@,.]+)+$/u.test(text);
}

/** Words that match no one offer addresses to choose, as Notion's (seen
 * 2026-11-04): common domains until a domain is begun, then what was typed.
 * Without domains to guess (a phone's invite screen) it is what was typed
 * alone, as Notion's phone offers "✉ z" for a typed "z" (seen 2026-11-16 in
 * a Galaxy A90's Chrome, two tabs). */
export function inviteAddressOptions(draft: string, domains: string[]): PublicMutationPrincipalOption[] {
  const typed = draft.trim();
  if (!typed) return [];
  const name = typed.endsWith("@") ? typed.slice(0, -1) : typed;
  return (name.includes("@") || !domains.length ? [typed] : domains.map(domain => `${name}@${domain}`)).map(address => ({
    principalType: publicMutationPrincipalTypes.email, principalId: address, label: address, email: "",
  }));
}

/** A comma finishes what came before it: addresses become chips, the rest is
 * named as not an address. What follows the last comma is still being typed. */
export function finishInviteDraft(draft: string) {
  const parts = draft.split(",");
  const typing = (parts.pop() ?? "").trimStart();
  const finished = parts.map(part => part.trim()).filter(Boolean);
  return { typing, addresses: finished.filter(isInviteAddress), invalid: finished.filter(part => !isInviteAddress(part)) };
}

/** Whether a share names the person reading: by account, or by the address they were invited at. */
export function shareIsYou(
  share: { principalType: string; principalId: string },
  user: { id: string; email?: string | null },
) {
  return share.principalType === publicMutationPrincipalTypes.user ? share.principalId === user.id
    : share.principalType === publicMutationPrincipalTypes.email && share.principalId.toLowerCase() === user.email?.toLowerCase();
}

export function principalForInvite(
  draft: string,
  selected: SelectedPrincipal | null,
) {
  const label = draft.trim().replace(/\s+/g, " ");
  if (selected && selected.label.toLocaleLowerCase() === label.toLocaleLowerCase()) return selected;
  return {
    label,
    principalType: publicMutationPrincipalTypes.email,
    principalId: label,
  } satisfies SelectedPrincipal;
}

/**
 * An address typed in the share field names the person the window offers
 * under it, a member or a guest, as choosing their row does — Notion's
 * "공유하기" with a known person's whole address typed shares with them, not
 * with the address (seen 2026-11-16 in Chrome at 1440, two tabs at two
 * times: the toast named the person and the access list had their row).
 * An address no one offered is invited as an address.
 */
export function principalForTypedAddress(address: string, options: readonly PublicMutationPrincipalOption[]): SelectedPrincipal {
  const typed = address.trim().toLocaleLowerCase();
  const known = options.find(option => option.principalType === publicMutationPrincipalTypes.user
    && option.email.trim().toLocaleLowerCase() === typed);
  return known ? { label: known.label, principalType: known.principalType, principalId: known.principalId, guest: known.guest, restricted: known.restricted }
    : principalForInvite(address, null);
}

/**
 * What sharing says once it is done, as Notion's toasts (seen 2026-11-11):
 * "〈이름〉님에게 초대 보냄" when everyone shared with is an address or a guest,
 * else "〈이름〉님과 공유" — with more than one, the first and how many
 * others, "〈이름〉님 외 2명에게 초대 보냄" (Notion's inviteResultsToast
 * messages, 2026-11-16). Where an address's invitation mail went without
 * its link, or didn't go — no mail from this server, or held back by the
 * limits (lib/inviteMail.ts) — a line under it says so and where the link
 * is: its row's "초대 링크 복사", as the row itself says; its "초대했습니다"
 * names them in Notion's list form (listNames).
 */
export function shareDoneNotice(shared: readonly SelectedPrincipal[], state: PublicMutationState | null, t: TFunction):
  { message: string; detail?: string } {
  const labels = shared.map(principal => principal.label), name = labels[0] ?? "", others = labels.length - 1;
  const invited = shared.every(principal => principal.principalType === publicMutationPrincipalTypes.email || principal.guest);
  const fates = shared.flatMap(principal => state?.permissions.filter(permission => permission.signupToken
    && permission.principalType === principal.principalType
    && permission.principalId.toLocaleLowerCase() === principal.principalId.toLocaleLowerCase())
    .map(permission => inviteMail(Boolean(state?.emailSends), permission.inviteMail ?? "")) ?? []);
  // The one that most needs the link handed on is said.
  const mail = (["notSent", "limited", "failed", "sentWithoutLink"] as const).find(fate => fates.includes(fate)) ?? "sent";
  if (mail !== "sent" && mail !== "sentWithoutLink") return { message: t("topBar:toast.invitedNoMail", { name: listNames(labels, t) }),
    detail: mail === "notSent" ? t("topBar:toast.invitedNoMailDetail")
      : mail === "limited" ? t("topBar:toast.inviteMailLimitedDetail") : t("topBar:toast.inviteMailFailedDetail") };
  const message = others > 0
    ? t(invited ? "topBar:toast.inviteSentToMore" : "topBar:toast.sharedWithMore", { name, count: others })
    : t(invited ? "topBar:toast.inviteSentTo" : "topBar:toast.sharedWith", { name });
  return mail === "sentWithoutLink" ? { message, detail: t("topBar:toast.inviteMailNoLinkDetail") } : { message };
}

export function siteNavigationCandidates(root: SharePage, pages: SharePage[], bound: number) {
  const pagesById = pageMapOf(pages), outOfTrash = outOfTrashIn(pages);
  return pages.filter((candidate) => {
    // The site's first page is its title, not a shortcut (Notion's picker lists only the pages under it, seen 2026-11-05);
    // none in the trash or under a page in it (outOfTrashIn).
    if (candidate.id === root.id || candidate.workspaceId !== root.workspaceId || !outOfTrash(candidate)) return false;
    let current: SharePage | undefined = candidate;
    const visited = new Set<string>();
    while (current && !visited.has(current.id)) {
      if (current.id === root.id) return true;
      visited.add(current.id);
      current = current.parentId ? pagesById.get(current.parentId) : undefined;
    }
    return false;
  }).slice(0, bound);
}
