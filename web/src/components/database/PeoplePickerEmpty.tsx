import { useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { ChevronRight, UserPlusIcon } from "@/icons/leafen";
import { popoverRectOf } from "@/lib/popoverPlacement";
import type { SearchOrganizationPeopleInput, SearchOrganizationPeopleResult } from "@/lib/typeaheadSearch";
import type { Session } from "@/types";
import { InviteAndMentionPanel } from "../page/InviteAndMentionPanel";
import { usePeopleInviteOffer } from "../editor/mentionInvite";
import { isInviteAddress } from "../page/topBarShareModel";
import { useProductPagesSelect } from "../product/productPagesStore";
import { INVITED_PERSON_STATUS } from "@/lib/organizationPeopleSearch";
import { loadSession } from "@/lib/authSessionStore";
import styles from "./databaseSheet";

/**
 * A person picker with no one left to list, as Notion's property, to-do and
 * board pickers (seen 2026-11-12 at 1440, dark). To whoever may share the page
 * — its full access, a guest's too, as the server says (usePeopleInviteOffer;
 * Notion's, seen 2026-11-24 in two tabs: the owner, a member and a guest with
 * full access had it, a member and a guest with edit access had none) — its
 * "사람" group says in faint gray that nothing in the workspace matched, and
 * under a line its "더 보기" group offers "사용자 초대 ›" — '”〈주소〉” 초대'
 * once an address is typed — which opens "초대와 멘션" by the row, the picker
 * staying open under it (InviteAndMentionPanel), the address ready as a chip.
 * To anyone else, before the server first answers, and in a board's "새 그룹"
 * list, the picker's own line ("원하는 만큼 선택", "1개 선택", or none) stands
 * over "결과 없음". Notion's line names no workspace to a guest; this one names
 * the workspace the reader is in.
 */
export function PeoplePickerEmpty({ query, label, invitePageId, session, admit, onInvited }: {
  query: string;
  /** The picker's line over its people, kept where no invite is offered. */
  label?: ReactNode;
  /** The page shared with whoever is invited; none offers no invite. */
  invitePageId?: string;
  session?: Session | null;
  /** Whether the picker takes these addresses (InviteAndMentionPanel's `admit`). */
  admit?(emails: string[], signal: AbortSignal): Promise<boolean>;
  /** The addresses invited, for the picker to choose whoever now has an account. */
  onInvited?(emails: string[]): void;
}) {
  const { t } = useTranslation("propertyCell");
  const rowRef = useRef<HTMLButtonElement>(null);
  const [inviting, setInviting] = useState<string | null>(null);
  // "초대와 멘션" closes back onto this row ("←", Escape, or sent): the row
  // keeps the focus, nothing ringed until a key, a letter typed going to no
  // field, as Notion's (Measured 2026-11-16 after the press only (no script in the page before it), Chrome 1440,
  // "←" and Escape each in two tabs 15 minutes apart; InviteAndMentionPanel's restoreFocus). Sent, the
  // picker stays open with the people invited in it and its search emptied,
  // and the row goes with the focus it had: the next key goes to no field,
  // as Notion's (seen 2026-11-15 twice, other tabs a quarter hour apart: after
  // a successful invite its picker keeps the invitee's chip and the focus is
  // the page's — a letter typed went nowhere).
  // The reader's workspace by name.
  const workspace = useProductPagesSelect((pages) => pages?.workspaces?.find((candidate) => candidate.id === pages.workspaceId)?.name ?? "");
  const offered = usePeopleInviteOffer(session, invitePageId ?? "", Boolean(invitePageId && session));
  if (!invitePageId || !session || !offered) {
    return <>{label}<div className={styles.peopleEmpty} role="status">{t("noResults")}</div></>;
  }
  const address = query.trim();
  const invite = isInviteAddress(address) ? address : "";
  return (
    <>
      <div className={styles.peopleGroup}>
        <div className={styles.peopleGroupName}>{t("peopleGroup")}</div>
        <div className={styles.peopleNoMatch} role="status"><span>{t("noMatchesIn", { workspace })}</span></div>
      </div>
      <div className={styles.peopleGroup}>
        <div className={styles.peopleGroupName}>{t("moreGroup")}</div>
        <button ref={rowRef} type="button" className={styles.peopleInvite} aria-haspopup="dialog" aria-expanded={inviting !== null}
          onClick={() => setInviting(invite)}>
          <UserPlusIcon size="regular" aria-hidden="true" />
          <span>{invite ? t("inviteAddress", { address: invite }) : t("invitePeople")}</span>
          <ChevronRight size="small" aria-hidden="true" />
        </button>
      </div>
      {inviting !== null ? <InviteAndMentionPanel session={session} pageId={invitePageId} invite={inviting}
        anchor={() => popoverRectOf(rowRef.current)} admit={admit} onBack={() => setInviting(null)}
        onClose={() => setInviting(null)} onInvited={onInvited} /> : null}
    </>
  );
}

/**
 * The session a person picker invites with: its page editor's, else — a row
 * page's properties or a database opened as a page, which no page editor
 * holds — the signed-in one, when it is the reader the picker is for. Without
 * it a full row page's picker offered no invite, only "결과 없음", where its
 * side peek's and a table cell's offered one (2026-11-14).
 */
export function pickerSession(editorSession: Session | null | undefined, readerId: string | null | undefined): Session | null {
  if (editorSession) return editorSession;
  const signedIn = loadSession();
  return signedIn && readerId && signedIn.user.id === readerId ? signedIn : null;
}

/**
 * Whoever of the addresses invited has an account in the organization now —
 * someone with none yet is chosen once they sign up — for the picker to
 * choose, as Notion's invite mentions them.
 */
export async function findInvitedPeople(
  search: (input: SearchOrganizationPeopleInput) => Promise<SearchOrganizationPeopleResult>,
  organizationId: string | undefined, emails: readonly string[],
): Promise<string[]> {
  if (!organizationId) return [];
  const found: string[] = [];
  for (const email of emails) {
    const result = await search({ organizationId, query: email, limit: 5 }).catch(() => null);
    // Someone only invited, named by the address, has no account yet.
    const person = result?.people?.find((profile) => profile.status !== INVITED_PERSON_STATUS
      && profile.email?.trim().toLowerCase() === email.toLowerCase());
    if (person?.userId && !found.includes(person.userId)) found.push(person.userId);
  }
  return found;
}
