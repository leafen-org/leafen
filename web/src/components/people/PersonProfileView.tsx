import { useEffect, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { PropertyTypeIcon } from "@/components/database/PropertyTypeIcon";
import { PageCover } from "@/components/page/PageCover";
import { TopBar } from "@/components/page/TopBar";
import { setDocumentChrome } from "@/lib/documentChrome";
import { canManageNativeWorkspace } from "@/lib/nativeWorkspaceRolePolicy";
import { personLabel } from "@/lib/peopleDirectory";
import {
  personProfileCover, personProfileHref, readPersonProfile, updatePersonAbout, updatePersonCover, type PersonProfileData,
} from "@/lib/peopleProfiles";
import { pushRoute } from "@/lib/router";
import type { Page, Session, Workspace } from "@/types";
import { MembershipTag, PeopleCrumbs } from "./PeopleView";
import { PersonAvatar } from "./PersonAvatar";
import { PersonProfileOwnProperties } from "./PersonProfileOwnProperties";
import { PersonProfileTopBarActions } from "./PersonProfileTopBarActions";
import { PersonProfileWork } from "./PersonProfileWork";
import { SelfMark } from "./SelfMark";
import styles from "./PeopleView.module.css";
import { KeptIconGlyph } from "@/components/teamspace/KeptIconGlyph";
import pageStyles from "@/components/page/PageView.module.css";
import { holdPageScrollHost } from "@/components/shell/pageScrollHost";
import { fieldEnter } from "@/lib/fieldKeyBoundary";
import { ReadFailed } from "@/components/ReadFailed";
import { LeafenApiError } from "@/apiTransport";

/**
 * A member's profile, as Notion's (seen 2026-10-12): a cover, their picture
 * over its foot and their name ("(나)" on one's own); beside their work —
 * "최근 활동" and "〈이름〉 님의 페이지" — "About", which they and the workspace's owner write,
 * "Email", "Membership Type", the properties its owner added to People
 * (PersonProfileOwnProperties), "최고의 공동 작업자" and "팀스페이스". In a
 * narrow window the side comes first. It links to no account history, as
 * Notion's has none (seen 2026-11-15 on one's own profile in two free
 * workspaces): a person's history opens from the members table's "이력" and
 * their name in the audit log. Its bar is their People row's page's
 * (PersonProfileTopBarActions).
 */
export function PersonProfileView({ session, workspace, userId, embedded = false }: {
  session: Session; workspace: Workspace; userId: string;
  /** Shown over the people list, without the window's bar and title. */
  embedded?: boolean;
}) {
  const { t } = useTranslation("peopleProfiles");
  const [profile, setProfile] = useState<PersonProfileData | null>(null);
  const [missing, setMissing] = useState(false);
  // A read that failed otherwise than "not here" says so, with a way to read again.
  const [failed, setFailed] = useState(false);
  const [readTurn, setReadTurn] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    setFailed(false);
    readPersonProfile(session, workspace.id, userId, controller.signal)
      .then((read) => {
        setProfile(read);
        if (!embedded) setDocumentChrome({ title: read.displayName });
      })
      .catch((caught: unknown) => {
        if (controller.signal.aborted) return;
        const status = caught instanceof LeafenApiError ? caught.status : 0;
        if (status === 404 || status === 403 || status === 410) setMissing(true); else setFailed(true);
      });
    return () => controller.abort();
  }, [embedded, readTurn, session, userId, workspace.id]);

  if (missing) {
    return (
      <>
        {embedded ? null : <TopBar title={t("people")} crumbs={<PeopleCrumbs />} />}
        <div className={styles.unavailable} role="status" data-person-profile-missing>{t("profileMissing")}</div>
      </>
    );
  }
  if (failed) {
    return (
      <>
        {embedded ? null : <TopBar title={t("people")} crumbs={<PeopleCrumbs />} />}
        <div className={styles.unavailable}><ReadFailed message={t("profileLoadFailed")} onRetry={() => { setFailed(false); setReadTurn((turn) => turn + 1); }} /></div>
      </>
    );
  }
  if (!profile) return embedded ? null : <TopBar title={t("people")} crumbs={<PeopleCrumbs />} />;
  const person = { userId, name: profile.displayName, avatar: profile.avatar };
  // A restricted member doesn't open People, so their own page's path is its name alone, as Notion's (seen 2026-11-16).
  const restricted = workspace.restricted === true;
  const deactivated = profile.membership === "deactivated";
  const member = !deactivated && profile.membership !== "guest";
  return (
    <>
      {embedded ? null : <TopBar title={profile.displayName} crumbs={<PeopleCrumbs person={person} people={!restricted} />}
        screenActions={profile.rowPageId ? <PersonProfileTopBarActions session={session} rowPageId={profile.rowPageId} title={profile.displayName}
          restricted={restricted} /> : undefined} />}
      <ProfileScroll embedded={embedded}>
        <div className={styles.profile} data-person-profile={userId}>
          <div className={styles.cover} data-person-profile-cover>
            <PageCover page={{ id: `profile:${userId}`, workspaceId: workspace.id, title: profile.displayName,
              cover: profile.cover || personProfileCover(userId), coverPosition: profile.coverPosition } as Page}
            readOnly={!profile.coverEditable} changeLabel={t("changeCover")} onChange={async (change) => {
              try {
                const saved = await updatePersonCover(session, workspace.id, userId, {
                  ...(change.cover !== undefined ? { cover: change.cover } : {}),
                  ...(change.coverPosition !== undefined ? { coverPosition: change.coverPosition } : {}),
                });
                setProfile((current) => current && { ...current, ...saved });
                return true;
              } catch {
                return false;
              }
            }} />
          </div>
          <div className={styles.profileBody}>
            <div className={styles.face}>
              <PersonAvatar userId={userId} avatar={profile.avatar} name={profile.displayName} size={140} />
            </div>
            {/* As Notion's title (seen 2026-11-28 on one's own profile, two tabs): the name in its
                own box with 8px either side and the lists' own mark (SelfMark) set against that box,
                in a row that wraps — so the mark's room is the box's, in every language. */}
            <h1 className={styles.profileName} data-self={profile.self || undefined}>
              <span className={styles.profileNameText}>{profile.displayName}</span>
              {profile.self ? <SelfMark text={t("selfMark")} /> : null}
            </h1>
            {/* Someone no longer in the workspace says so under their name, as Notion's (seen 2026-11-15). */}
            {deactivated ? <div className={styles.profileStatus} data-person-deactivated>{t("deactivatedStatus")}</div> : null}
            <div className={styles.columns}>
              <aside className={styles.side} data-person-profile-side>
                <About session={session} workspace={workspace} profile={profile}
                  onSaved={(about) => setProfile((current) => current && { ...current, about })} />
                <Property type="email" label={t("emailProperty")}>
                  {profile.email ? <a className={styles.emailValue} href={`mailto:${profile.email}`}>{profile.email}</a>
                    : <span className={styles.empty}>{t("empty")}</span>}
                </Property>
                <Property type="select" label={t("membershipType")}>
                  <MembershipTag membership={profile.membership} />
                </Property>
                <PersonProfileOwnProperties session={session} workspace={workspace} userId={userId} />
                <hr className={styles.sideRule} />
                {/* Best collaborators are a member's; a guest's or a deactivated
                    person's side ends on their teamspaces, "없음" where they have
                    none, as Notion's (seen 2026-11-15, a guest and the same
                    person removed, in two tabs). */}
                {member ? <div className={styles.sideGroup} data-person-collaborators>
                  <div className={styles.sideHeading}>{t("collaborators")}</div>
                  {profile.collaborators.length ? profile.collaborators.map((id) => (
                    <button key={id} type="button" className={styles.sideItem} onClick={() => pushRoute(personProfileHref(id))}>
                      <PersonAvatar userId={id} size={20} />
                      <span>{personLabel(id)}</span>
                    </button>
                  )) : <div className={styles.sideEmpty}>{t("noCollaborators")}</div>}
                </div> : null}
                {profile.teamspaces.length || !member ? (
                  <div className={styles.sideGroup} data-person-teamspaces>
                    <div className={styles.sideHeading}>{t("teamspaces")}</div>
                    {profile.teamspaces.map((teamspace) => (
                      <div key={teamspace.id} className={styles.sideItem}>
                        <span className={styles.sideIcon}>
                          <KeptIconGlyph icon={teamspace.icon} fallback={teamspace.name.trim().slice(0, 1).toUpperCase() || "?"} />
                        </span>
                        <span>{teamspace.name}</span>
                      </div>
                    ))}
                    {profile.teamspaces.length ? null : <div className={styles.sideNone}>{t("noTeamspaces")}</div>}
                  </div>
                ) : null}
              </aside>
              <PersonProfileWork profile={profile} />
            </div>
          </div>
        </div>
      </ProfileScroll>
    </>
  );
}

/**
 * The profile's own scroll under the bar, as a page's and People's are
 * (holdPageScrollHost): the bar keeps the window's width, its buttons where a
 * page's stand, as Notion's person page (seen 2026-11-16: "⋯" at 1402–1430
 * at 1440, a page's too); the whole area scrolled with the bar in it, 15px in.
 * Over the people list it scrolls with the peek.
 */
function ProfileScroll({ embedded, children }: { embedded: boolean; children: ReactNode }) {
  if (embedded) return <>{children}</>;
  return <div ref={holdPageScrollHost} data-page-scroll className={`${pageStyles.scroll} nscroll`}>{children}</div>;
}

function Property({ type, label, children }: { type: "rich_text" | "email" | "select"; label: string; children: ReactNode }) {
  return (
    <div className={styles.property}>
      <div className={styles.propertyLabel}><PropertyTypeIcon type={type} />{label}</div>
      <div className={styles.propertyValue}>{children}</div>
    </div>
  );
}

/**
 * "About": what the person writes about themselves, which the workspace's
 * owner writes too, as Notion's People lets its owner (seen 2026-11-15 twice).
 */
function About({ session, workspace, profile, onSaved }: {
  session: Session;
  workspace: Workspace;
  profile: PersonProfileData;
  onSaved: (about: string) => void;
}) {
  const { t } = useTranslation("peopleProfiles");
  const [draft, setDraft] = useState<string | null>(null);
  const [error, setError] = useState(false);

  async function save(value: string) {
    setDraft(null);
    if (value.trim() === profile.about) return;
    try {
      const saved = await updatePersonAbout(session, workspace.id, profile.userId, value);
      setError(false);
      onSaved(saved.about);
    } catch {
      setError(true);
    }
  }

  const shown = profile.about ? <span className={styles.aboutText}>{profile.about}</span>
    : <span className={styles.empty}>{t("empty")}</span>;
  return (
    <Property type="rich_text" label={t("about")}>
      {!(profile.self || canManageNativeWorkspace(workspace) && profile.membership !== "deactivated") ? shown : draft === null ? (
        <button type="button" className={styles.aboutButton} data-person-about onClick={() => setDraft(profile.about)}>{shown}</button>
      ) : (
        <textarea className={styles.aboutInput} autoFocus value={draft} maxLength={2000} rows={3} aria-label={t("about")}
          onChange={(event) => setDraft(event.target.value)}
          onBlur={() => void save(draft)}
          onKeyDown={(event) => {
            if (event.key === "Escape") { event.preventDefault(); setDraft(null); }
            if (fieldEnter(event) && !event.shiftKey) { event.preventDefault(); void save(draft); }
          }} />
      )}
      {error ? <div className={styles.aboutError} role="alert">{t("aboutFailed")}</div> : null}
    </Property>
  );
}
