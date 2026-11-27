import { PersonAvatar } from "@/components/people/PersonAvatar";
import { ChevronRight, EnvelopeIcon } from "@/icons/leafen";
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { Trans, useTranslation } from "react-i18next";
import { api, LeafenApiError } from "@/api";
import { accessRequestErrorCodes, type AccessRequest } from "@/accessRequestWire.generated";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session, Workspace } from "@/types";
import { readEveryWindow } from "@/lib/listWindows";
import { NativeTab, useNativeTabs } from "@/lib/nativeTabs";
import { listAppRequests } from "@/components/admin/administratorAppsApi";
import styles from "./SettingsItemRows.module.css";
import tabStyles from "./WorkspaceAnalytics.module.css";
import requestStyles from "./WorkspaceRequests.module.css";
import { styles as settingsStyles } from "./workspaceSettingsOverviewStyles";
import { ListNotice } from "./SettingsRowNotice";
import { displayDateTime } from "@/lib/displayDate";
import { activeDateLocale } from "@/lib/i18n";
import { onNoticeArrival } from "@/lib/noticeArrivals";
import { useAccessRevision } from "@/lib/workspaceChangeCheck";
import { listedPageNameKind } from "../../lib/pageTitle";
import { usePageNames } from "@/lib/pageNames";
import { useProductPages } from "@/components/product/productPagesContext";

type RequestsTab = "all" | "controls";

/** When a request came, as Notion's request rows say it: "오늘, 오후 3:05", "어제, …", else the day and time. */
function requestTime(at: number, t: (key: string, options?: Record<string, unknown>) => string) {
  const date = new Date(at);
  const today = new Date();
  const yesterday = new Date(today.getFullYear(), today.getMonth(), today.getDate() - 1);
  const time = date.toLocaleTimeString(activeDateLocale(), { hour: "numeric", minute: "2-digit" });
  if (date.toDateString() === today.toDateString()) return t("requestTimeToday", { time });
  if (date.toDateString() === yesterday.toDateString()) return t("requestTimeYesterday", { time });
  return displayDateTime(at);
}

/**
 * "요청", as Notion's in the workspace's settings (seen 2026-11-09): the tabs
 * "모든 요청" and "제어". 모든 요청 lists by kind — Leafen has only members'
 * requests, Notion's credits and upgrades having no counterpart here — with
 * how many wait at the right: a guest's for membership, a member's to add
 * someone or a guest, and a page's from people the page may be shared with,
 * each approved or declined, and the person's own still waiting. 제어 holds
 * who may invite whom ("게스트와 멤버 초대하기"), the security settings'
 * own rows (controls). A guest's page request reaches only the inbox in
 * Notion; Leafen lists it here too, for those who may decide it.
 */
export function WorkspaceRequestsSection({ session, workspace, controls, onOpenApps, touchTab = false, onOpenSecurity }: {
  session: Session;
  workspace: Workspace;
  /**
   * The 멤버 page's "요청" tab of a wider touch screen, as Notion's iPad's
   * (seen 2026-11-16 in iPad Safari at 834 and 1210): no tabs of its own, a
   * grey note sending who-may-ask to the security settings, "요청" over the
   * list, and an empty list's words in a ringed box.
   */
  touchTab?: boolean;
  /** Opens the security settings, from the touch tab's note. */
  onOpenSecurity?: () => void;
  /** Who may invite whom, for those who may see the security settings. */
  controls?: ReactNode;
  /**
   * Opens the server's apps, where a server administrator handles the
   * requests to install an app; the waiting ones are counted here so this
   * list doesn't read as empty while they wait (2026-10-16).
   */
  onOpenApps?: () => void;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation("workspaceSettingsDialog");
  // A page the reader's list holds is named as it is there, by its kind when untitled.
  const pages = useProductPages()?.pages;
  const [requests, setRequests] = useState<AccessRequest[] | null>(null);
  const [appRequests, setAppRequests] = useState({ count: 0, more: false });
  const [error, setError] = useState("");
  // A failed read of the list, said in its loading line's place with a way to
  // read it again; an approval or decline that failed is `error`.
  const [readFailed, setReadFailed] = useState("");
  const [busy, setBusy] = useState("");
  const [tab, setTab] = useState<RequestsTab>("all");
  const tabsRef = useRef<HTMLDivElement>(null);
  const typesRef = useRef<HTMLDivElement>(null);
  const tabs = useNativeTabs<RequestsTab>({
    tabs: [{ key: "all", text: t("requestsTabAll") }, ...(controls ? [{ key: "controls" as const, text: t("requestsTabControls") }] : [])],
    selected: tab, onSelect: setTab, label: t("navRequests"), listRef: tabsRef,
  });
  const types = useNativeTabs<"members">({
    tabs: [{ key: "members", text: t("requestsTypeMembers") }],
    selected: "members", onSelect: () => undefined, label: t("requestsTypesLabel"), listRef: typesRef,
  });
  const appsShown = Boolean(onOpenApps);

  useEffect(() => {
    if (!appsShown) return undefined;
    const controller = new AbortController();
    listAppRequests(session, "", controller.signal).then(
      (list) => { if (!controller.signal.aborted) setAppRequests({ count: list.requests.length, more: Boolean(list.nextCursor) }); },
      () => undefined,
    );
    return () => controller.abort();
  }, [appsShown, session]);

  const load = useCallback(async (signal?: AbortSignal) => {
    try {
      const list = await readEveryWindow(async (cursor) => {
        const page = await api.listAccessRequests(session, workspace.id, signal, cursor);
        return { items: page.requests, nextCursor: page.nextCursor };
      }, signal);
      if (!signal?.aborted) { setRequests(list); setReadFailed(""); }
    } catch (failure) {
      if (!signal?.aborted) setReadFailed(settingsErrorMessage(failure, t("requestsLoadFailed")));
    }
  }, [session, t, workspace.id]);
  const readAgain = () => { setReadFailed(""); void load(); };

  useEffect(() => {
    setRequests(null);
    setError("");
    setReadFailed("");
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  // The list follows while it is open, as the members' does (2026-10-27: the
  // people settings' waiting lists stayed as first read): a request decided
  // elsewhere changes who is here, which the change check says, and one that
  // comes reaches the person as a notice.
  const accessRevision = useAccessRevision();
  const seenAccessRevision = useRef(accessRevision);
  useEffect(() => {
    if (seenAccessRevision.current === accessRevision) return undefined;
    seenAccessRevision.current = accessRevision;
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [accessRevision, load]);
  useEffect(() => onNoticeArrival((workspaceId) => {
    if (workspaceId === workspace.id) void load();
  }), [load, workspace.id]);

  async function decide(request: AccessRequest, approve: boolean) {
    if (busy) return;
    setBusy(request.id);
    setError("");
    try {
      await api.decideAccessRequest(session, request.id, approve);
    } catch (failure) {
      if (!(failure instanceof LeafenApiError && failure.code === accessRequestErrorCodes.decided)) {
        setError(settingsErrorMessage(failure, t("requestDecideFailed")));
      }
    }
    await load();
    setBusy("");
  }

  // In Notion's words for each kind (requestsSettingsRedesign.memberRequest.title.*).
  function describe(request: AccessRequest) {
    const page = pageNames.listed(pages?.find(({ id }) => id === request.pageId), request.pageTitle, undefined,
      listedPageNameKind(request));
    if (request.kind === "membership") return t("requestMembershipText", { name: request.requesterName });
    if (request.kind === "member") return t("requestMemberText", { name: request.requesterName, email: request.principalLabel });
    if (request.kind === "guest") {
      return t("requestGuestText", { name: request.requesterName, guest: request.principalLabel, page, role: t(`requestRole_${request.role}`) });
    }
    return t(`requestPage_${request.role}`, { name: request.requesterName, page });
  }

  // Every request listed waits, one's own too.
  const waiting = requests?.length ?? 0;
  const rows = requests?.map((request) => (
    <div key={request.id} className={styles.row} data-access-request={request.kind}>
      <PersonAvatar className={styles.icon} userId={request.requesterId} name={request.requesterName} size={28} />
      <span className={styles.text}>
        {describe(request)}
        {/* Why they ask, when they said (a membership request's "메시지 추가"). */}
        {request.message ? <q className={requestStyles.message} data-access-request-message>{request.message}</q> : null}
        <span>{requestTime(request.createdAt, t)}</span>
      </span>
      {request.mine ? (
        <span className={styles.badge}>{t("requestMine")}</span>
      ) : (
        <span className={styles.actions}>
          <button type="button" className={settingsStyles.primaryButton} disabled={Boolean(busy)}
            data-request-approve onClick={() => void decide(request, true)}>{t("requestApprove")}</button>
          <button type="button" className={settingsStyles.secondaryButton} disabled={Boolean(busy)}
            data-request-decline onClick={() => void decide(request, false)}>{t("requestDecline")}</button>
        </span>
      )}
    </div>
  ));
  const appRow = onOpenApps && appRequests.count ? (
    <button type="button" className={`${styles.row} ${styles.linkRow}`} data-requests-apps onClick={onOpenApps}>
      <span className={styles.text}>
        {t(appRequests.more ? "requestsAppRowMore" : "requestsAppRow", { count: appRequests.count })}
        <span>{t("requestsAppRowDescription")}</span>
      </span>
      <ChevronRight size="small" aria-hidden="true" />
    </button>
  ) : null;
  if (touchTab) return (
    <div className={requestStyles.touchTab} data-workspace-requests="touch">
      {onOpenSecurity ? <div className={requestStyles.securityNote} data-requests-security-note>
        <EnvelopeIcon size="regular" aria-hidden="true" />
        <span><Trans t={t} i18nKey="requestsTouchSecurityNote" components={{ b: <b /> }} /></span>
        <button type="button" onClick={onOpenSecurity} data-requests-security-open>
          <Trans t={t} i18nKey="requestsTouchSecurityOpen" components={{ b: <b /> }} />
        </button>
      </div> : null}
      <div className={requestStyles.touchHeading}>{t("navRequests")}</div>
      {error ? <ListNotice role="alert">{error}</ListNotice> : null}
      {readFailed ? <ListNotice role="alert" onRetry={readAgain} data-requests-read-failed>{readFailed}</ListNotice> : null}
      {requests === null ? (readFailed ? null : <div className={requestStyles.touchEmpty} role="status">{t("requestsLoading")}</div>)
        : requests.length === 0 && !appRequests.count ? <div className={requestStyles.touchEmpty} data-requests-empty>
          <EnvelopeIcon size="regular" aria-hidden="true" />{t("requestsTouchEmpty")}
        </div> : <>{rows}{appRow}</>}
    </div>
  );
  return (
    <div data-workspace-requests>
      <div {...tabs.tabListProps} ref={tabsRef} className={tabStyles.tabs}>
        <NativeTab state={tabs.state} tabKey="all" className={tabStyles.tab} data-requests-tab="all">{t("requestsTabAll")}</NativeTab>
        {controls ? <NativeTab state={tabs.state} tabKey="controls" className={tabStyles.tab} data-requests-tab="controls">
          {t("requestsTabControls")}
        </NativeTab> : null}
      </div>
      {tab === "controls" && controls ? (
        <div role="tabpanel" className={requestStyles.panel} data-requests-controls>
          <div className={requestStyles.heading}>{t("requestsControlsTitle")}</div>
          {controls}
        </div>
      ) : (
        <div role="tabpanel" className={requestStyles.panel}>
          <div className={requestStyles.typeBar}>
            <div {...types.tabListProps} ref={typesRef} className={tabStyles.tabs}>
              <NativeTab state={types.state} tabKey="members" className={tabStyles.tab}>{t("requestsTypeMembers")}</NativeTab>
            </div>
            {requests ? <span className={requestStyles.count} data-requests-count>
              {waiting ? t("requestsPendingCount", { count: waiting }) : t("requestsNothingPending")}
            </span> : null}
          </div>
          {error ? <ListNotice role="alert">{error}</ListNotice> : null}
          {readFailed ? <ListNotice role="alert" onRetry={readAgain} data-requests-read-failed>{readFailed}</ListNotice> : null}
          {requests === null ? (readFailed ? null : (
            <div className={requestStyles.empty} role="status">{t("requestsLoading")}</div>
          )) : requests.length === 0 && !appRequests.count ? (
            <div className={requestStyles.empty} data-requests-empty>{t("requestsEmpty")}</div>
          ) : rows}
          {appRow}
        </div>
      )}
    </div>
  );
}
