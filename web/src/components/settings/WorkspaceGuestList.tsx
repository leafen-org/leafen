import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { ReadFailed } from "@/components/ReadFailed";
import { SidebarActionMenu, type SidebarMenuRow } from "@/components/sidebar/SidebarActionMenu";
import { ArrowDown, ArrowUp, DotsHorizontal, HandPalmIcon, UpgradeCircleIcon, UserCirclePlusIcon } from "@/icons/leafen";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import { useAccessRevision } from "@/lib/workspaceChangeCheck";
import type { Session, Workspace } from "@/types";
import type { WorkspaceGuestDirectoryItem, WorkspaceGuestDirectoryResponse } from "@/workspaceMembershipWire.generated";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { WorkspaceGuestPagesButton } from "./WorkspaceGuestPagesMenu";
import { WorkspaceGuestPhoneList } from "./WorkspaceGuestPhoneList";
import guestStyles from "./WorkspaceGuestList.module.css";
import tableStyles from "./WorkspaceMemberTable.module.css";
import { styles } from "./workspaceSettingsOverviewStyles";

function guestLabel(guest: WorkspaceGuestDirectoryItem) {
  return guest.displayName.trim() || guest.email.trim() || guest.userId;
}

/** A guest as the server names them: their account, or "email:" and the address a page was shared with. */
function workspaceGuestKey(guest: WorkspaceGuestDirectoryItem) {
  return guest.userId || `email:${guest.email.trim().toLowerCase()}`;
}

/**
 * 설정 → 사람's guests, as Notion's 게스트 tab (seen 2026-11-09, a Plus
 * workspace): "활성" over a table of 사용자 (sorted, Z to A first) and 사용
 * 권한 — "페이지 N개 ⌄", the pages the guest can open. The owner ticks
 * guests to act on them together at the end of the "활성" line, or opens one
 * guest's "…": 멤버로 업그레이드 (Leafen's member; no plan to upgrade to) and
 * 워크스페이스에서 제거, which asks first and takes every page of the
 * workspace shared with them. A phone lists them as its members
 * (WorkspaceGuestPhoneList).
 */
export function WorkspaceGuestList({ session, workspace, query, canManage, onCount, onUpgrade }: {
  session: Session;
  workspace: Workspace;
  query: string;
  /** The owner manages the guests: ticks, "…" and the actions on them. */
  canManage: boolean;
  onCount(count: number | null): void;
  /** Makes these guests members; missing while members can't be added. */
  onUpgrade?(guests: WorkspaceGuestDirectoryItem[]): void;
}) {
  const { t, i18n } = useTranslation(["workspaceSettingsDialog", "common"]);
  const { phone } = useSidebarLayout(true);
  const [state, setState] = useState<{ key: string; directory: WorkspaceGuestDirectoryResponse | null; error: string }>(
    { key: "", directory: null, error: "" });
  const [attempt, setAttempt] = useState(0);
  const [descending, setDescending] = useState(true);
  const [selected, setSelected] = useState<ReadonlySet<string>>(() => new Set());
  const [menuFor, setMenuFor] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState("");
  const menuTrigger = useRef<HTMLButtonElement>(null);
  const key = `${session.user.id}:${session.sessionId ?? ""}:${workspace.id}`;
  // Every window of the guests, each shown as it comes, read again when who
  // is here changes (the workspace's change check).
  const accessRevision = useAccessRevision();
  useEffect(() => {
    const controller = new AbortController();
    void (async () => {
      let guests: WorkspaceGuestDirectoryItem[] = [];
      let cursor = "";
      do {
        const read = await api.listWorkspaceGuests(session, workspace.id, cursor, controller.signal);
        if (controller.signal.aborted) return;
        guests = [...guests, ...read.guests];
        setState({ key, directory: { ...read, guests }, error: "" });
        cursor = read.nextCursor ?? "";
      } while (cursor);
    })().catch((error) => {
      if (!controller.signal.aborted) setState({ key, directory: null, error: settingsErrorMessage(error, t("guestsLoadFailed")) });
    });
    return () => controller.abort();
  }, [accessRevision, attempt, key, session, t, workspace.id]);
  const current = state.key === key ? state : { directory: null, error: "" };
  const guests = useMemo(() => current.directory?.guests ?? [], [current.directory]);
  useEffect(() => onCount(current.directory ? current.directory.total : null), [current.directory, onCount]);
  // Ticks hold only guests still listed.
  useEffect(() => {
    setSelected((ticked) => {
      const listed = new Set(guests.map(workspaceGuestKey));
      const kept = [...ticked].filter((guest) => listed.has(guest));
      return kept.length === ticked.size ? ticked : new Set(kept);
    });
  }, [guests]);
  const normalized = query.trim().toLowerCase();
  const shown = useMemo(() => {
    const collator = new Intl.Collator(i18n.language, { sensitivity: "base", numeric: true });
    const found = normalized
      ? guests.filter((guest) => [guest.displayName, guest.email].join(" ").toLowerCase().includes(normalized))
      : guests;
    return [...found].sort((left, right) => (descending ? -1 : 1) * collator.compare(guestLabel(left), guestLabel(right)));
  }, [descending, guests, i18n.language, normalized]);

  async function remove(chosen: WorkspaceGuestDirectoryItem[]) {
    if (chosen.length === 0 || !await confirmAction({
      message: chosen.length === 1 ? t("guestRemoveConfirmOne", { name: guestLabel(chosen[0]) })
        : t("guestRemoveConfirmMany", { count: chosen.length }),
      detail: t("guestRemoveConfirmDetail"), confirmLabel: t("guestRemoveAction"), danger: true,
    })) return;
    setBusy(true);
    setActionError("");
    try {
      await api.removeWorkspaceGuests(session, workspace.id, chosen.map(workspaceGuestKey));
      setSelected(new Set());
      setAttempt((value) => value + 1);
    } catch (error) {
      setActionError(settingsErrorMessage(error, t("guestRemoveFailed")));
    } finally {
      setBusy(false);
    }
  }

  if (current.error) {
    return (
      <div className={styles.notice}>
        <ReadFailed message={current.error}
          onRetry={() => { setState({ key, directory: null, error: "" }); setAttempt((value) => value + 1); }} />
      </div>
    );
  }
  if (!current.directory) return <div className={styles.emptyState}>{t("loadingGuests")}</div>;
  if (guests.length === 0) {
    return (
      <div className={guestStyles.empty} data-workspace-guests-empty>
        <UserCirclePlusIcon aria-hidden="true" />
        <span>{t("noGuests")}</span>
      </div>
    );
  }
  const ticked = shown.filter((guest) => selected.has(workspaceGuestKey(guest)));
  const allTicked = shown.length > 0 && ticked.length === shown.length;
  const tick = (which: WorkspaceGuestDirectoryItem[], on: boolean) => setSelected((previous) => {
    const next = new Set(previous);
    for (const guest of which) {
      if (on) next.add(workspaceGuestKey(guest));
      else next.delete(workspaceGuestKey(guest));
    }
    return next;
  });
  const menuGuest = menuFor ? guests.find((guest) => workspaceGuestKey(guest) === menuFor) : undefined;
  const guestActions = (guest: WorkspaceGuestDirectoryItem): SidebarMenuRow[] => [
    ...(onUpgrade ? [{ key: "upgrade", label: t("guestUpgradeToMember"), title: t("guestUpgradeTooltip"),
      icon: <UpgradeCircleIcon size="regular" aria-hidden="true" />, run: () => { setMenuFor(null); onUpgrade([guest]); } }] : []),
    { key: "remove", label: t("memberRemoveFromWorkspace"), danger: true, separatorBefore: Boolean(onUpgrade),
      icon: <HandPalmIcon size="regular" aria-hidden="true" />, run: () => { setMenuFor(null); void remove([guest]); } },
  ];
  const menuRows = menuGuest ? guestActions(menuGuest) : [];
  if (phone) {
    return <WorkspaceGuestPhoneList session={session} workspace={workspace} guests={shown} busy={busy} error={actionError}
      keyOf={workspaceGuestKey} labelOf={guestLabel} actionsFor={canManage ? guestActions : undefined} />;
  }

  return (
    <div className={guestStyles.guests} data-workspace-guests aria-busy={busy}>
      <div className={guestStyles.activeLine}>
        <span>{t("guestsActive")}</span>
        {canManage && ticked.length > 0 ? (
          <span className={guestStyles.bulk} data-workspace-guests-bulk>
            {onUpgrade ? (
              <button type="button" className={guestStyles.bulkButton} disabled={busy} onClick={() => onUpgrade(ticked)}>
                <UpgradeCircleIcon size="small" aria-hidden="true" />{t("guestBulkUpgrade", { count: ticked.length })}
              </button>
            ) : null}
            <button type="button" className={guestStyles.bulkButton} data-danger="true" disabled={busy} onClick={() => void remove(ticked)}>
              <HandPalmIcon size="small" aria-hidden="true" />{t("memberRemoveFromWorkspace")}
            </button>
          </span>
        ) : null}
      </div>
      {actionError ? <div className={styles.notice} role="alert">{actionError}</div> : null}
      <div className={tableStyles.scroller}>
        <div role="table" className={tableStyles.table} aria-label={t("peopleTabGuests")}>
          <div role="row" className={`${guestStyles.row} ${guestStyles.head}`} data-manage={canManage ? "true" : "false"}>
            {canManage ? (
              <div role="columnheader" className={guestStyles.checkCell}>
                <input type="checkbox" className={guestStyles.checkbox} checked={allTicked} aria-label={t("guestSelectAll")}
                  ref={(input) => { if (input) input.indeterminate = ticked.length > 0 && !allTicked; }}
                  onChange={(event) => tick(shown, event.currentTarget.checked)} />
              </div>
            ) : null}
            <div role="columnheader" className={tableStyles.headCell} aria-sort={descending ? "descending" : "ascending"}>
              <button type="button" className={tableStyles.sortButton} onClick={() => setDescending((value) => !value)}>
                <span>{t("memberColumnUser")}</span>
                {descending ? <ArrowDown size="tiny" aria-hidden="true" /> : <ArrowUp size="tiny" aria-hidden="true" />}
              </button>
            </div>
            <div role="columnheader" className={tableStyles.headCell}><span className={`${tableStyles.headLabel} ${guestStyles.accessHead}`}>{t("guestColumnAccess")}</span></div>
            {canManage ? <div role="columnheader" className={tableStyles.headCell} aria-label={t("guestColumnActions")} /> : null}
          </div>
          {shown.length === 0 ? <div role="row" className={tableStyles.emptyRow}><span role="cell">{t("noSearchResults")}</span></div> : null}
          {shown.map((guest) => {
            const label = guestLabel(guest);
            const guestKey = workspaceGuestKey(guest);
            const isTicked = selected.has(guestKey);
            // A ticked guest is acted on from the "활성" line; their own controls dim, as Notion's.
            const dimmed = busy || isTicked;
            return (
              <div key={guestKey} role="row" className={guestStyles.row} data-manage={canManage ? "true" : "false"}
                data-workspace-guest={guestKey}>
                {canManage ? (
                  <div role="cell" className={guestStyles.checkCell}>
                    <input type="checkbox" className={guestStyles.checkbox} checked={isTicked} aria-label={t("guestSelect", { name: label })}
                      onChange={(event) => tick([guest], event.currentTarget.checked)} />
                  </div>
                ) : null}
                <div role="cell" className={tableStyles.userCell}>
                  <PersonAvatar className={tableStyles.avatar} userId={guest.userId || undefined} name={label} size={28} />
                  <span className={styles.memberText}>
                    <strong>{label}</strong>
                    <span className={tableStyles.userSub}>{guest.userId ? guest.email : t("guestEmailInvite")}</span>
                  </span>
                </div>
                <div role="cell" className={tableStyles.cell}>
                  <WorkspaceGuestPagesButton session={session} workspace={workspace} guestKey={guestKey} name={label}
                    count={guest.pageCount} disabled={dimmed} />
                </div>
                {canManage ? (
                  <div role="cell" className={guestStyles.endCell}>
                    <button type="button" className={guestStyles.moreButton} aria-label={t("guestActions", { name: label })}
                      aria-haspopup="menu" aria-expanded={menuFor === guestKey} disabled={dimmed} data-workspace-guest-actions
                      onClick={(event) => { menuTrigger.current = event.currentTarget; setMenuFor(menuFor === guestKey ? null : guestKey); }}>
                      <DotsHorizontal size="regular" aria-hidden="true" />
                    </button>
                  </div>
                ) : null}
              </div>
            );
          })}
        </div>
      </div>
      {menuGuest ? (
        <SidebarActionMenu button={menuTrigger} rows={menuRows} width={320} backdropLabel={t("common:actions.close")}
          label={t("guestActions", { name: guestLabel(menuGuest) })} close={() => setMenuFor(null)}
          data={{ "data-workspace-guest-menu": "" }} />
      ) : null}
    </div>
  );
}
