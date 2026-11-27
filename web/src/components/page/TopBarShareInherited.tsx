import { Fragment, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { Trans, useTranslation } from "react-i18next";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { ChevronRight, ChevronUpDown, DotsHorizontal, RestoreIcon, SlidersIcon, TableIcon, UnlinkSharingIcon } from "@/icons/leafen";
import { NativeActionMenu, NativeActionMenuItem } from "@/lib/nativeActionMenu";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import type {
  PublicMutationInheritedGroup,
  PublicMutationInheritedPerson,
  PublicMutationRole,
  PublicMutationShareUnlink,
} from "@/publicMutationWire.generated";
import { sharePermissionLabel, TopBarSharePermissionPicker } from "./TopBarSharePermissionPicker";
import { ShareRowTip, ShareRowTipPage } from "./ShareRowTip";
import { SharePersonMark } from "./SharePersonMark";
import { shareParentTitle, sharePrincipalNote, shareUnlinkDefaultsHref } from "./topBarShareModel";
import { styles } from "./topBarStyles";
import inherited from "./TopBarShareInherited.module.css";
import { useMenuButtonTrigger } from "@/lib/menuButtonTrigger";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { sharePhoneStyles as phoneCss } from "./TopBarSharePhone";

/**
 * The people a page inherits from the pages above it, as Notion's share
 * window lists them (seen 2026-10-11): a row per page, "〈page〉에 초대된 사용자 ·
 * 사용자 N명 · 〈role〉", which opens to its people. A person's menu heads with
 * the role they have from that page; choosing another changes it here. A
 * database item's page has one row, named for its database with a table
 * icon, and each person's menu names the page above that gives their role
 * (seen 2026-10-13).
 */
export function TopBarShareInheritedGroups({ groups, disabled, readOnly, pageIcon, onChange, loadPeople }: {
  groups: readonly PublicMutationInheritedGroup[]; disabled: boolean; readOnly: boolean;
  /** The page's own icon, as Notion's row shows it. */
  pageIcon(pageId: string): ReactNode;
  onChange(person: PublicMutationInheritedPerson, role: PublicMutationRole | null): void;
  /** Everyone a page above gives, read as its row opens past the first ones shown. */
  loadPeople?(pageAboveId: string): Promise<PublicMutationInheritedPerson[]>;
}) {
  const { t } = useTranslation("topBar");
  const { phone } = useSidebarLayout(true);
  const [open, setOpen] = useState<ReadonlySet<string>>(new Set());
  const [loaded, setLoaded] = useState<Readonly<Record<string, readonly PublicMutationInheritedPerson[]>>>({});
  const toggle = (group: PublicMutationInheritedGroup) => {
    setOpen(current => {
      const next = new Set(current);
      if (!next.delete(group.pageId)) next.add(group.pageId);
      return next;
    });
    if (loadPeople && !loaded[group.pageId] && group.count > group.people.length) {
      void loadPeople(group.pageId).then(people => setLoaded(current => ({ ...current, [group.pageId]: people })), () => undefined);
    }
  };
  return <>{groups.map(group => {
    const title = shareParentTitle({ title: group.pageTitle, database: group.database }), expanded = open.has(group.pageId);
    // The tips name the page above, with its icon, as Notion's (seen 2026-10-13).
    const icon = group.database ? <TableIcon size="small" aria-hidden="true" /> : pageIcon(group.pageId);
    const page = <ShareRowTipPage icon={icon} />;
    return <Fragment key={group.pageId}>
      {/* A phone's is a person's row, as Notion's phone "공유 설정" (seen
          2026-11-12 at 402pt): the page's icon in a 40px circle, the title
          and "›", and under it how many with their role. */}
      {phone ? <button type="button" className={`${phoneCss.person} ${inherited.phoneGroup}`} aria-expanded={expanded}
        onClick={() => toggle(group)} data-share-row data-share-inherited-group={group.pageId}>
        <span className={`${phoneCss.avatar} ${inherited.phoneGroupIcon}`} aria-hidden="true">{icon}</span>
        <span className={phoneCss.personText}>
          <span className={phoneCss.personName}><span>{t("inheritedGroup", { title })}</span><ChevronRight size="small" aria-hidden="true" /></span>
          <span className={phoneCss.personRole}>{t("inheritedPeople", { count: group.count })} · {sharePermissionLabel(group.role, t)}</span>
        </span>
      </button> : <button type="button" className={`${styles.shareRow} ${inherited.groupRow}`} aria-expanded={expanded}
        onClick={() => toggle(group)} data-share-row data-share-inherited-group={group.pageId}>
        <span className={inherited.groupLead} aria-hidden="true">
          <ChevronRight size="regular" className={inherited.groupChevron} />
          <span className={inherited.groupIcon}>{icon}</span>
        </span>
        <ShareRowTip className={styles.shareText}
          text={<Trans t={t} i18nKey="topBar:rowTipGroup" count={group.count} values={{ title }} components={{ page }} />}>
          <span className={inherited.groupTitle}>{t("inheritedGroup", { title })}</span>
          <span>{t("inheritedPeople", { count: group.count })}</span></ShareRowTip>
        {/* Closed, its role with the up-down mark that opens it, as Notion's (seen 2026-11-06). */}
        {expanded ? null : <span className={`${styles.sharePermission} ${inherited.teamspaceRole}`}>
          {sharePermissionLabel(group.role, t)} <ChevronUpDown size="tiny" aria-hidden="true" /></span>}
      </button>}
      {expanded ? (loaded[group.pageId] ?? group.people).map(person => { const note = sharePrincipalNote({ principalType: person.principalType, label: person.label, email: person.email, principalId: person.principalId }, t);
      return <div key={`${person.principalType}:${person.principalId}`}
        className={`${styles.shareRow} ${inherited.person}`} data-share-row data-share-inherited-person={person.principalType}>
        {person.principalType === "user" || person.userId
          ? <PersonAvatar className={styles.shareAvatar} userId={person.userId || person.principalId} name={person.label} size={32} />
          : <span className={styles.shareAvatar}>{person.label.trim().slice(0, 1).toUpperCase() || "?"}</span>}
        <ShareRowTip className={styles.shareText}
          text={<Trans t={t} i18nKey="topBar:rowTipInherited" values={{ title }} components={{ page }} />}>
          <span className={inherited.name}><span>{person.label}</span>
            <SharePersonMark guest={person.guest} restricted={person.restricted} /></span>
          {note ? <span>{note}</span> : null}</ShareRowTip>
        {/* Its menu says what a change does, as Notion's (seen 2026-11-06). */}
        <TopBarSharePermissionPicker value={person.role} disabled={disabled} readOnly={readOnly} note={t("teamspaceRoleNote")} current={<Trans t={t} i18nKey="topBar:pageAccessVia" values={{ title: person.fromTitle?.trim() || title }} components={{ b: <strong /> }} />}
          onChange={next => { if (next !== person.role && next !== "none") onChange(person, next); }} onRemove={() => onChange(person, null)} />
      </div>; }) : null}
      {expanded && !loaded[group.pageId] && group.count > group.people.length
        ? <p className={inherited.more}>{t("inheritedMore", { count: group.count - group.people.length })}</p> : null}
    </Fragment>;
  })}</>;
}

// A page at its teamspace's top unlinked from the teamspace's default says
// so of the teamspace, as Notion's (seen 2026-11-16; its code, read the same day).
const unlinkedKey = (teamspace: boolean | undefined, database: boolean) => teamspace
  ? database ? "topBar:unlinkedFromTeamspaceDatabase" : "topBar:unlinkedFromTeamspace"
  : database ? "topBar:unlinkedFromDatabase" : "topBar:unlinkedFrom";

/**
 * A page whose sharing is unlinked from its parent's says so at the top of
 * its share window, as Notion's (seen 2026-10-11): "권한 복원 옵션 보기" (⋯)
 * offers "기본값 보기", which opens the parent, and "설정 복원"; since 2026-11-06
 * Notion's has no "무시하기" (×), its mark leads it and the parent is a link.
 * A database's says "이 데이터베이스의 …", as Notion's own words for one do.
 * One at its teamspace's top unlinked from the teamspace's default names the
 * teamspace, whose members in its settings the name and "기본값 보기" open.
 */
export function TopBarShareUnlinkBanner({ unlink, database, disabled, onDefaults, onRestore }: {
  unlink: PublicMutationShareUnlink; database: boolean; disabled: boolean;
  onDefaults(): void; onRestore(): void;
}) {
  const { t } = useTranslation("topBar");
  const trigger = useRef<HTMLButtonElement>(null), menu = useRef<HTMLDivElement>(null);
  const { state, menuProps, buttonProps, autoFocus } = useMenuButtonTrigger(trigger, disabled);
  const placement = usePopoverPlacement(menu, state.isOpen ? (size, viewport) => placeAround(
    popoverRectOf(trigger.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport, { align: "end", gap: 4 },
  ) : null, [state.isOpen], undefined, { kind: "menu", onEscape: state.close, onOutsidePress: state.close,
    inside: () => [trigger.current] });
  return <div className={`${inherited.banner} ${inherited.bannerUnlinked}`} role="note" data-share-unlink-banner>
    <UnlinkSharingIcon size="regular" aria-hidden="true" />
    <span><Trans t={t} i18nKey={unlinkedKey(unlink.teamspace, database)}
      values={{ title: unlink.teamspace ? unlink.parentTitle : shareParentTitle({ title: unlink.parentTitle, database: unlink.parentDatabase }) }}
      components={{ page: <a className={inherited.bannerLink} href={shareUnlinkDefaultsHref(unlink)} /> }} /></span>
    <button {...buttonProps} ref={trigger} aria-label={t("restoreOptions")} title={t("restoreOptions")} data-share-restore-options>
      <DotsHorizontal size="small" aria-hidden="true" />
    </button>
    {state.isOpen ? createPortal(<div ref={menu} className={`${styles.sharePermissionMenu} ${inherited.bannerMenu}`}
      style={{ ...popoverPlacementStyle(placement), position: "fixed", right: "auto", bottom: "auto" }}>
      <NativeActionMenu {...menuProps} aria-label={t("restoreOptions")} autoFocus={autoFocus}>
        <NativeActionMenuItem id="defaults" onClick={() => { state.close(); onDefaults(); }}>
          <SlidersIcon size="regular" aria-hidden="true" />{t("viewDefaults")}</NativeActionMenuItem>
        <NativeActionMenuItem id="restore" disabled={disabled} onClick={() => { state.close(); onRestore(); }}>
          <RestoreIcon size="regular" aria-hidden="true" />{t("restoreSettings")}</NativeActionMenuItem>
      </NativeActionMenu>
    </div>, document.body) : null}
  </div>;
}
