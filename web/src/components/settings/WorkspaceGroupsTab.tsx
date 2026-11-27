import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { ChevronLeft, ChevronRight, GroupPeopleIcon, SortIcon } from "@/icons/leafen";
import { SettingsSearchToggle } from "./SettingsSearchToggle";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import { useMadeWithoutAnswer } from "@/lib/uncertainWrites";
import type { Session, Workspace } from "@/types";
import type { WorkspacePermissionGroup } from "@/workspaceMembershipApi";
import type { WorkspaceMemberDirectoryItem } from "@/workspaceMembershipWire.generated";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { SidebarActionMenu } from "@/components/sidebar/SidebarActionMenu";
import { TeamspaceDialog } from "@/components/TeamspaceDialog";
import type { TeamspaceFromGroup } from "@/components/teamspace/TeamspaceCreatePanel";
import { WorkspaceGroupCreateDialog } from "./WorkspaceGroupCreateDialog";
import { WorkspaceGroupDetailDialog } from "./WorkspaceGroupDetailDialog";
import { WorkspaceGroupIcon } from "./WorkspaceGroupIcon";
import { WorkspaceGroupNameLine } from "./WorkspaceGroupNameMarks";
import { WorkspaceGroupPhoneList } from "./WorkspaceGroupPhoneList";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { WorkspaceGroupTypeFilter, type WorkspaceGroupType } from "./WorkspaceGroupTypeFilter";
import groupStyles from "./WorkspaceGroups.module.css";
import tableStyles from "./WorkspaceMemberTable.module.css";
import { styles } from "./workspaceSettingsOverviewStyles";
import { EmptyLineReadFailed } from "./SettingsRowNotice";

type GroupSort = { key: "name" | "members"; descending: boolean };

/** Notion asks for its groups 20 at a time (its code and screen, 2026-11-11). */
const GROUPS_PER_PAGE = 20;

/**
 * 설정 → 사람 → 그룹, as Notion's (seen 2026-10-12; its table with 21 groups
 * 2026-11-11): a line on what groups are for while there are none, "그룹 유형 ⌄" on the left and a
 * sort (이름, 멤버 수) and a search on the right, then the groups as a table —
 * 그룹 (its name and marks, WorkspaceGroupNameMarks), 그룹 유형, 그룹 소유자
 * (their faces), 멤버 and 연결된 팀스페이스 —
 * newest first, 20 to a page under "‹ › N 페이지", or "아직 그룹이 없습니다"
 * until there is one. Its owner makes one from the tab row's "그룹 생성", and
 * the group made opens to its window at 일반 over the list read again, as
 * Notion's (seen 2026-11-11: "그룹 설정", the focus on its 닫기, and back on
 * "그룹 생성" as it closes); a group opens to its window. A phone lists
 * them in its members' shape (WorkspaceGroupPhoneList).
 */
export function WorkspaceGroupsTab({ session, workspace, members, canManage, createOpen, onCreateOpenChange, onCount }: {
  session: Session;
  workspace: Workspace;
  members: WorkspaceMemberDirectoryItem[];
  canManage: boolean;
  createOpen: boolean;
  onCreateOpenChange(open: boolean): void;
  /** How many groups the list read last holds, for the tab's "그룹 n". */
  onCount?(count: number): void;
}) {
  const { t, i18n } = useTranslation(["workspaceSettingsDialog", "common"]);
  const { phone } = useSidebarLayout(true);
  const [groups, setGroups] = useState<WorkspacePermissionGroup[] | null>(null);
  const [error, setError] = useState("");
  // None chosen until the menu picks one, as Notion's menu opens with none
  // checked (seen 2026-11-11); the list stays in the order of names.
  const [sort, setSort] = useState<GroupSort | null>(null);
  const [sortOpen, setSortOpen] = useState(false);
  const sortButton = useRef<HTMLButtonElement>(null);
  const [types, setTypes] = useState<WorkspaceGroupType[]>([]);
  const [query, setQuery] = useState("");
  const [page, setPage] = useState(0);
  const [openGroupId, setOpenGroupId] = useState<string | null>(null);
  // A teamspace being made from a group: its window stands in the group's place.
  const [teamspaceFrom, setTeamspaceFrom] = useState<TeamspaceFromGroup | null>(null);

  const load = useCallback(async (signal?: AbortSignal) => {
    try {
      const list = await api.listWorkspaceGroups(session, workspace.id, signal);
      if (!signal?.aborted) { setGroups(list.groups); setError(""); }
    } catch (failure) {
      if (!signal?.aborted) setError(settingsErrorMessage(failure, t("groupsLoadFailed")));
    }
  }, [session, t, workspace.id]);

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);
  // A group made or deleted here changes the tab's count at once.
  useEffect(() => {
    if (groups) onCount?.(groups.length);
  }, [groups, onCount]);
  // The group made takes the making window's place with its own.
  const opened = (group: WorkspacePermissionGroup) => { onCreateOpenChange(false); setOpenGroupId(group.id); };
  // A group made without the server's answer and found in the list read
  // again opens as the answer would have (lib/uncertainWrites.ts).
  const makeGroup = useMadeWithoutAnswer({
    items: groups,
    idOf: (group) => group.id,
    readAgain: () => void load(),
    landed: opened,
  });

  const byId = new Map(members.map((member) => [member.userId, member]));
  const normalized = query.trim().toLocaleLowerCase(i18n.language);
  // Unsorted, the newest group comes first, as Notion's list (2026-11-11).
  const shown = (groups ?? [])
    .filter((group) => !normalized || group.name.toLocaleLowerCase(i18n.language).includes(normalized))
    .filter((group) => !types.length || types.includes(workspaceGroupType(group)))
    .sort((a, b) => !sort ? b.createdAt - a.createdAt || a.name.localeCompare(b.name, i18n.language)
      : (sort.descending ? -1 : 1) * (sort.key === "members"
        ? a.memberCount - b.memberCount || a.name.localeCompare(b.name, i18n.language)
        : a.name.localeCompare(b.name, i18n.language)));
  // A search, a filter or a sort starts again from the first page, as Notion's.
  const pageCount = Math.max(1, Math.ceil(shown.length / GROUPS_PER_PAGE));
  const current = Math.min(page, pageCount - 1);
  const pageRows = shown.slice(current * GROUPS_PER_PAGE, (current + 1) * GROUPS_PER_PAGE);
  const search = (next: string) => { setQuery(next); setPage(0); };
  const filter = (next: WorkspaceGroupType[]) => { setTypes(next); setPage(0); };
  // A sort chosen again turns the other way; 멤버 수 starts with the most.
  const chooseSort = (key: GroupSort["key"]) => {
    setSortOpen(false);
    setPage(0);
    setSort((now) => now?.key === key ? { key, descending: !now.descending } : { key, descending: key === "members" });
  };
  // "‹ › N 페이지" under the rows, as Notion's: the arrows faint where there is no page that way.
  const pager = (
    <div className={groupStyles.pager} data-groups-pager>
      <button type="button" className={groupStyles.pagerButton} disabled={current === 0} onClick={() => setPage(current - 1)}
        aria-label={t(current === 0 ? "groupsPagePreviousNone" : "groupsPagePrevious")}>
        <ChevronLeft size="small" aria-hidden="true" />
      </button>
      <button type="button" className={groupStyles.pagerButton} disabled={current >= pageCount - 1} onClick={() => setPage(current + 1)}
        aria-label={t(current >= pageCount - 1 ? "groupsPageNextNone" : "groupsPageNext")}>
        <ChevronRight size="small" aria-hidden="true" />
      </button>
      <span className={groupStyles.pagerLabel}>{t("groupsPage", { page: current + 1 })}</span>
    </div>
  );
  const ownerName = (userId: string) => {
    const member = byId.get(userId);
    return member?.displayName.trim() || member?.email.trim() || userId;
  };

  return (
    <div className={groupStyles.groups} data-workspace-groups>
      {/* The line on what groups are for stands over an empty list only, as Notion's (2026-11-11). */}
      {groups !== null && groups.length === 0 ? <p className={groupStyles.lead}>{t("groupsLead")}</p> : null}
      <div className={groupStyles.tools}>
        <WorkspaceGroupTypeFilter value={types} onChange={filter} />
        <span className={groupStyles.toolButtons}>
          <button ref={sortButton} type="button" className={groupStyles.iconButton} aria-label={t("groupsSort")}
            aria-haspopup="menu" aria-expanded={sortOpen} onClick={() => setSortOpen((current) => !current)}>
            <SortIcon size="regular" aria-hidden="true" />
          </button>
          <SettingsSearchToggle query={query} onQueryChange={search} placeholder={t("groupsSearchPlaceholder")} />
        </span>
      </div>
      {sortOpen ? (
        <SidebarActionMenu button={sortButton} label={t("groupsSort")} width={180} backdropLabel={t("common:actions.close")}
          close={() => setSortOpen(false)}
          rows={(["name", "members"] as const).map((key) => ({
            key, label: t(key === "name" ? "groupsSortName" : "groupsSortMembers"),
            checked: sort?.key === key, run: () => chooseSort(key),
          }))} />
      ) : null}
      {error ? <EmptyLineReadFailed onRetry={() => { setError(""); void load(); }} data-groups-read-failed>{error}</EmptyLineReadFailed>
        : groups === null ? <div className={styles.emptyState} role="status">{t("groupsLoading")}</div> : null}
      {groups !== null && groups.length === 0 ? (
        <div className={groupStyles.empty} data-groups-empty>
          <GroupPeopleIcon size="large" aria-hidden="true" />
          <strong>{t("groupsEmptyTitle")}</strong>
          <span>{t("groupsEmptyText")}</span>
        </div>
      ) : null}
      {groups !== null && groups.length > 0 && shown.length === 0 ? (
        <div className={groupStyles.empty} data-groups-no-match>
          <strong>{t("groupsNoMatchTitle")}</strong>
          <span>{t("groupsNoMatchText")}</span>
        </div>
      ) : null}
      {shown.length && phone ? <>
        <WorkspaceGroupPhoneList groups={pageRows} onOpen={setOpenGroupId} />
        {pager}
      </> : shown.length ? (
        <div className={`${tableStyles.scroller} ${groupStyles.tableScroller}`}>
          <div role="table" className={`${tableStyles.table} ${groupStyles.table}`} aria-label={t("peopleTabGroups")}>
            <div role="row" className={`${tableStyles.row} ${tableStyles.headRow} ${groupStyles.tableRow}`}>
              {(["groupColumnGroup", "groupColumnType", "groupColumnOwners", "groupColumnMembers", "groupColumnTeamspaces"] as const).map((key, index) => (
                <div key={key} role="columnheader" className={`${tableStyles.headCell}${index ? "" : ` ${groupStyles.pinned}`}`}>
                  <span className={groupStyles.headLabel}>{t(key)}</span>
                </div>
              ))}
            </div>
            {pageRows.map((group) => (
              <div key={group.id} role="row" className={`${tableStyles.row} ${groupStyles.tableRow} ${groupStyles.bodyRow}`} data-workspace-group={group.id}
                onClick={() => setOpenGroupId(group.id)}>
                <div role="cell" className={`${tableStyles.userCell} ${groupStyles.pinned}`}>
                  <button type="button" className={groupStyles.nameButton} aria-haspopup="dialog"
                    onClick={(event) => { event.stopPropagation(); setOpenGroupId(group.id); }}>
                    <span className={groupStyles.rowMark}><WorkspaceGroupIcon icon={group.icon} size={20} filled /></span>
                    <WorkspaceGroupNameLine group={group} nameClass={groupStyles.rowName} />
                  </button>
                </div>
                <div role="cell" className={tableStyles.cell}>
                  <span className={groupStyles.typeTag} title={t("groupTypeAdminManagedTooltip")}>{t("groupTypeAdminManaged")}</span>
                </div>
                <div role="cell" className={tableStyles.cell}>
                  {group.owners?.length ? (
                    <span className={groupStyles.ownerFaces} role="img" aria-label={group.owners.map(ownerName).join(", ")}
                      title={group.owners.map(ownerName).join(", ")}>
                      {group.owners.map((owner) => (
                        <PersonAvatar key={owner} userId={owner} name={ownerName(owner)} size={20} className={groupStyles.ownerFace} />
                      ))}
                    </span>
                  ) : <span className={tableStyles.none}>{t("groupOwnersNone")}</span>}
                </div>
                <div role="cell" className={`${tableStyles.cell} ${groupStyles.countCell}`}>{t("groupMemberCount", { count: group.memberCount })}</div>
                <div role="cell" className={`${tableStyles.cell} ${groupStyles.countCell}`}>{t("memberTeamspaceCount", { count: group.teamspaceCount ?? 0 })}</div>
              </div>
            ))}
            {pager}
          </div>
        </div>
      ) : null}
      {openGroupId ? (
        <WorkspaceGroupDetailDialog session={session} workspace={workspace} members={members} groupId={openGroupId}
          canManage={canManage} onClose={() => setOpenGroupId(null)} onChanged={() => void load()}
          onMakeTeamspace={(group) => { setOpenGroupId(null); setTeamspaceFrom(group); }} />
      ) : null}
      {teamspaceFrom ? (
        <TeamspaceDialog session={session} workspace={workspace} fromGroup={teamspaceFrom}
          onClose={() => { setTeamspaceFrom(null); void load(); }} />
      ) : null}
      {createOpen ? (
        <WorkspaceGroupCreateDialog session={session} workspace={workspace} members={members} make={makeGroup}
          onClose={() => onCreateOpenChange(false)}
          onCreated={(group) => { opened(group); void load(); }} />
      ) : null}
    </div>
  );
}

/**
 * Leafen's groups are all made and changed by the workspace's owner — no
 * member makes one, and a group's owners don't manage it here — so each is
 * Notion's "관리자 관리" (a group its admins manage).
 */
function workspaceGroupType(_group: WorkspacePermissionGroup): WorkspaceGroupType {
  return "admin_managed";
}
