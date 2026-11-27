import { useTranslation } from "react-i18next";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import type { SidebarMenuRow } from "@/components/sidebar/SidebarActionMenu";
import type { Session, Workspace } from "@/types";
import type { WorkspaceGuestDirectoryItem } from "@/workspaceMembershipWire.generated";
import { WorkspaceGuestPagesButton } from "./WorkspaceGuestPagesMenu";
import styles from "./WorkspaceMemberPhoneList.module.css";
import { styles as overview } from "./workspaceSettingsOverviewStyles";

/**
 * A phone's guests, in its members' shape (WorkspaceMemberPhoneList, as
 * Notion's phone "멤버" lines, seen 2026-11-12 at 402pt): no table and no
 * ticks, a line for each guest — their 28px face, their name in 16px, their
 * address in 14px under it — and at its end their "페이지 N개 ⌄", which
 * opens the pages they can open on a whole screen with what the owner may
 * do with them under those, as a member's role opens its choices. Notion's
 * phone has no guests screen.
 */
export function WorkspaceGuestPhoneList({ session, workspace, guests, busy, error, keyOf, labelOf, actionsFor }: {
  session: Session;
  workspace: Workspace;
  guests: readonly WorkspaceGuestDirectoryItem[];
  busy: boolean;
  error: string;
  keyOf(guest: WorkspaceGuestDirectoryItem): string;
  labelOf(guest: WorkspaceGuestDirectoryItem): string;
  /** The owner's actions on a guest: 멤버로 업그레이드 and 워크스페이스에서 제거. */
  actionsFor?(guest: WorkspaceGuestDirectoryItem): SidebarMenuRow[];
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  return <>
    {error ? <div className={overview.notice} role="alert">{error}</div> : null}
    {guests.length === 0 ? <div className={overview.emptyState}>{t("noSearchResults")}</div> : null}
    <div className={styles.list} role="list" aria-label={t("peopleTabGuests")} aria-busy={busy} data-workspace-guests data-guest-phone-list>
      {guests.map((guest) => {
        const guestKey = keyOf(guest), name = labelOf(guest);
        return (
          <div key={guestKey} role="listitem" className={styles.row} data-workspace-guest={guestKey}>
            <PersonAvatar className={styles.avatar} userId={guest.userId || undefined} name={name} size={28} />
            <span className={styles.text}>
              <span className={styles.nameLine}><strong className={styles.name}>{name}</strong></span>
              <span className={styles.sub}>{guest.userId ? guest.email : t("guestEmailInvite")}</span>
            </span>
            <span className={styles.role}>
              <WorkspaceGuestPagesButton session={session} workspace={workspace} guestKey={guestKey} name={name}
                count={guest.pageCount} disabled={busy} actions={actionsFor?.(guest)} />
            </span>
          </div>
        );
      })}
    </div>
  </>;
}
