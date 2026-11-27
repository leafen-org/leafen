import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { TopBar } from "@/components/page/TopBar";
import { styles as topBarStyles } from "@/components/page/topBarStyles";
import { ChevronRight } from "@/icons/leafen";
import { setDocumentChrome } from "@/lib/documentChrome";
import { PEOPLE_DIRECTORY_HREF, openPersonProfile, usePeopleProfileSettings, type PersonMembership } from "@/lib/peopleProfiles";
import { databaseViewUrlWithSelection } from "@/components/database/databaseViewModel";
import { pushRoute, useSearchParams } from "@/lib/router";
import type { Session, Workspace } from "@/types";
import { PeopleDirectoryBody, usePeopleDirectoryRead } from "./PeopleDirectoryBody";
import { PeopleNoAccess } from "./PeopleNoAccess";
import { PeopleDatabaseTopBarActions } from "./PeopleDatabaseTopBarActions";
import { avatarKind, PersonAvatar } from "./PersonAvatar";
import { PageIconGlyph, PeopleDatabaseIcon, PERSON_ICON_PREFIX } from "@/components/page/PageIcon";
import { PersonPeek } from "./PersonPeek";
import { PersonProfileView } from "./PersonProfileView";
import pageStyles from "@/components/page/PageView.module.css";
import { holdPageScrollHost } from "@/components/shell/pageScrollHost";
import styles from "./PeopleView.module.css";

/**
 * The workspace's people list and its members' profiles, as Notion's
 * "People" (seen 2026-10-12), where the workspace has them — a guest, or a
 * workspace whose owner turned "사용자 디렉토리" off, has no people list.
 */
export function PeopleView({ session, workspace, userId }: {
  session: Session;
  workspace: Workspace;
  userId?: string;
}) {
  const profiles = usePeopleProfileSettings();
  const { t } = useTranslation("peopleProfiles");
  const open = profiles?.workspaceId === workspace.id && profiles.settings.directory && !workspace.guest;
  if (userId) {
    return open ? <PersonProfileView key={userId} session={session} workspace={workspace} userId={userId} />
      : <><TopBar title={t("people")} /><div className={styles.unavailable} role="status" data-people-unavailable>{t("unavailable")}</div></>;
  }
  // A restricted member doesn't open People's page itself (PeopleNoAccess).
  if (workspace.restricted) return <PeopleNoAccess session={session} workspace={workspace} />;
  return <PeopleDirectoryView session={session} workspace={workspace} directory={open} />;
}

/** The bar's path: "People", where the reader opens it, then the person whose profile is open. */
export function PeopleCrumbs({ person, people = true }: { person?: { userId: string; name: string; avatar: string }; people?: boolean }) {
  const { t } = useTranslation("peopleProfiles");
  return (
    <>
      {people || !person ? <span className={topBarStyles.crumbWrap}>
        <button type="button" className={topBarStyles.crumb} data-people-crumb onClick={() => pushRoute(PEOPLE_DIRECTORY_HREF)}>
          {/* People's own icon 18px, as a page's path draws a page's icon (Notion's, seen 2026-11-19). */}
          <span className={topBarStyles.crumbIcon} data-topbar-crumb-icon aria-hidden="true"><PeopleDatabaseIcon pixels={18} /></span>
          <span className={topBarStyles.crumbLabel}>{t("people")}</span>
        </button>
      </span> : null}
      {person ? (
        <>
          {people ? <span className={topBarStyles.sep}><ChevronRight size="small" aria-hidden="true" /></span> : null}
          <span className={topBarStyles.crumbWrap}>
            <span className={topBarStyles.crumb} data-topbar-current-page>
              {/* Their photo is their page's icon, a page's picture as Notion's (seen 2026-11-16: 16px
                  rounded 4); a person without one keeps their letter. */}
              {avatarKind(person.avatar) === "image"
                ? <span className={topBarStyles.crumbIcon}><PageIconGlyph page={{ icon: PERSON_ICON_PREFIX + person.userId, iconType: "image" }} size={16} /></span>
                : <PersonAvatar userId={person.userId} avatar={person.avatar} name={person.name} size={16} />}
              <span className={topBarStyles.crumbLabel}>{person.name}</span>
            </span>
          </span>
        </>
      ) : null}
    </>
  );
}

/** A membership as the profile's "Membership Type" tag names and colors it. */
export function MembershipTag({ membership }: { membership: PersonMembership }) {
  const { t } = useTranslation("peopleProfiles");
  return <span className={styles.tag} data-membership={membership}>{t(`membership.${membership}`)}</span>;
}

/**
 * The people list, as Notion's "People" (seen 2026-10-12): a database of its
 * own under its title — "Directory View" and "Card View", the reader's own
 * filters, sorts and search, guests and the deactivated left out until the
 * filter is changed — whose person opens their profile the way its view's
 * "페이지 보기 선택" says: beside it at first (`?p=〈person〉&pm=s`, "사이드 보기"
 * in Notion, seen 2026-11-15 in two workspaces), in the middle (`pm=c`) or
 * as its own page as its owner sets it; someone joining or changing, or its
 * owner saving its views, shows without a reload.
 */
function PeopleDirectoryView({ session, workspace, directory }: {
  session: Session;
  workspace: Workspace;
  /** The workspace lists its people here. */
  directory: boolean;
}) {
  const { t } = useTranslation("peopleProfiles");
  const search = useSearchParams();
  const peek = search.get("p");
  const peekMode = search.get("pm") === "c" ? "center" : "side";
  // The address keeps the view it names (`?v=`), as a database page's row does.
  const address = (userId: string | null, rowMode: "side" | "center" = "side") => {
    const url = databaseViewUrlWithSelection(new URL(window.location.href), { rowId: userId, rowMode });
    return `${url.pathname}${url.search}`;
  };
  const read = usePeopleDirectoryRead(session, workspace, directory);
  const database = read.database;

  useEffect(() => {
    setDocumentChrome({ title: t("people") });
  }, [t]);

  return (
    <>
      <TopBar title={t("people")} crumbs={<PeopleCrumbs />} screenActions={database?.updatedAt
        ? <PeopleDatabaseTopBarActions session={session} workspace={workspace} database={database} title={t("people")} /> : undefined} />
      {/* A full-page database's frame, as Notion's People is one (seen
          2026-11-12): its title and table where a database page has them,
          the table running on to the window's edge (PageView.module.css), in
          the page's own scroller as a database page's, so its view reaches
          the window's foot with its sideways bar and its head stays at the
          top as the rows pass (useOwnPageViewFill, useStickyViewHeads). */}
      <div ref={holdPageScrollHost} data-page-scroll data-database-canvas="true" className={`${pageStyles.scroll} nscroll`}>
        <div className={pageStyles.doc} data-kind="database" data-database-page="true" data-has-cover="false"
          data-people-directory>
          <PeopleDirectoryBody session={session} workspace={workspace} directory={directory} read={read}
            onOpenPerson={(userId, mode) => mode === "full" || mode === "new_tab" ? openPersonProfile(userId, mode === "new_tab")
              : pushRoute(address(userId, mode === "center" ? "center" : "side"))} />
        </div>
      </div>
      {peek ? <PersonPeek session={session} workspace={workspace} userId={peek} mode={peekMode} onClose={() => pushRoute(address(null))} /> : null}
    </>
  );
}
