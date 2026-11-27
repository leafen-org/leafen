import { withPersonDistinction } from "@/lib/peopleDirectory";
import { useEffect, useMemo, useRef, useState } from "react";
import { useAttemptNotice } from "@/lib/useNoticeFor";
import { useTranslation } from "react-i18next";

import type { OrganizationSettingsAuditEvent, OrganizationSettingsMember } from "@/organizationWire.generated";
import type { AccountHistoryPolicy, OrganizationAuditEvent, ViewDisplayPolicy } from "@/organizationAuditWire.generated";
import {
  organizationAuditActor, organizationAuditKindLabel, organizationAuditLabel, organizationAuditSubject, organizationAuditTargetName,
  withoutRepeatedKeyCalls,
} from "./organizationAuditModel";
import { AppDateField } from "@/components/database/ScheduleFieldPickers";
import { SettingsSwitchRow } from "./SettingsSwitchRow";
import { EmptyLineReadFailed } from "./SettingsRowNotice";
import { styles } from "./workspaceSettingsOverviewStyles";
import { displayDateTime } from "@/lib/displayDate";
import { AppSelect } from "@/components/AppSelect";
import {
  listAuditEvents, readAccountHistoryPolicy, readViewDisplayPolicy, requestAccountHistory, updateAccountHistoryPolicy,
  updateViewDisplayPolicy, type AuditLogFilter,
} from "@/lib/accountHistory";
import { loadSession } from "@/lib/authSessionStore";
import { enterpriseDownloadText } from "@/lib/enterpriseSettingsAdapter";
import { organizationModule } from "@/lib/organizationModule";
import { userErrorText } from "@/lib/settingsErrors";
import { auditExportFailureText } from "./organizationAuditExportFailure";
import { randomUUID } from "@/lib/browserCrypto";

const NONE: AuditLogFilter = { actor: "", action: "", target: "", since: 0, until: 0 };

let auditLogFocusRequested = false;
/** Shows the log the next time 조직 opens, as 엔터프라이즈 제어's "감사 로그 열기" asks. */
export function requestAuditLogFocus() {
  auditLogFocusRequested = true;
}

/** An event of the log as its names read it. */
function settingsEvent(event: OrganizationAuditEvent): OrganizationSettingsAuditEvent {
  return { ...event, actorId: event.actorId ?? "", occurredAt: new Date(event.occurredAt).toISOString() };
}

/** A day's start or end, in this computer's time, from a date input. */
function dayEdge(value: string, end: boolean) {
  if (!value) return 0;
  const [year, month, day] = value.split("-").map(Number);
  return end ? new Date(year, month - 1, day, 23, 59, 59, 999).getTime() : new Date(year, month - 1, day).getTime();
}

/**
 * The organization's audit log (the audit decision of 2026-10-21 20:05, 2):
 * read by person, period, kind and target on the server, page after page,
 * and exported as it is filtered; a person's name opens their history, a
 * target narrows the log to it. Those who keep the log also say whether
 * full-access holders see others' history within their reach.
 */
export function WorkspaceOrganizationAudit({ organizationId, members = [] }: {
  organizationId: string;
  members?: OrganizationSettingsMember[];
}) {
  const sectionRef = useRef<HTMLElement>(null);
  const focusPending = useRef(auditLogFocusRequested);
  const { t } = useTranslation("workspaceSettingsDialog");
  const [filter, setFilter] = useState<AuditLogFilter>(NONE);
  // The target narrowed to, as the line it was picked on names it.
  const [targetName, setTargetName] = useState("");
  const [dates, setDates] = useState({ since: "", until: "" });
  const [events, setEvents] = useState<OrganizationAuditEvent[]>([]);
  const [cursor, setCursor] = useState("");
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useAttemptNotice<string>(dates);
  // A failed read of the log, said where it shows with a way to read again —
  // the first window in the empty line's place ("" cursor), a further one after
  // the records — never as no matching records; an export's or policy's word is `message`.
  const [readFailed, setReadFailed] = useState<{ text: string; cursor: string }>({ text: "", cursor: "" });
  const [readTurn, setReadTurn] = useState(0);
  const [kinds, setKinds] = useState<Map<string, string>>(new Map());
  const [policy, setPolicy] = useState<AccountHistoryPolicy | null>(null);
  const [display, setDisplay] = useState<ViewDisplayPolicy | null>(null);
  const filterNow = useRef(filter);
  filterNow.current = filter;
  const names = useMemo(() => ({
    ...Object.fromEntries(members.map((member) => [member.userId, withPersonDistinction(member.userId, member.displayName)])),
    // A member's setting names its membership: the person, not the record.
    ...Object.fromEntries(members.map((member) => [member.id, member.displayName || member.email])),
    ...Object.fromEntries(events.flatMap((event) => event.actorApp ? [[event.actorId ?? "", t("appAccountName", { name: event.actorApp })]] : [])),
  }), [events, members, t]);

  // Asked for from 엔터프라이즈 제어: shown once its first records are in, so
  // the window has its full height to bring it up to.
  const firstLoad = useRef(false);
  useEffect(() => {
    auditLogFocusRequested = false;
    if (busy) { firstLoad.current = true; return; }
    if (!focusPending.current || !firstLoad.current) return;
    focusPending.current = false;
    sectionRef.current?.scrollIntoView({ block: "start" });
  }, [busy]);

  // Each kind of event seen once, by its name, for the filter.
  const remember = (list: OrganizationAuditEvent[]) => setKinds((current) => {
    const next = new Map(current);
    for (const event of list) if (!next.has(event.action)) next.set(event.action, organizationAuditKindLabel(settingsEvent(event), t));
    return next;
  });

  useEffect(() => {
    const controller = new AbortController();
    setBusy(true);
    setMessage("");
    setReadFailed({ text: "", cursor: "" });
    listAuditEvents(organizationId, filter, "", controller.signal).then((page) => {
      setEvents(page.events);
      setCursor(page.nextCursor ?? "");
      remember(page.events);
    }, (caught) => {
      if (controller.signal.aborted) return;
      setEvents([]);
      setReadFailed({ text: userErrorText(caught, t("auditLoadFailed")), cursor: "" });
    }).finally(() => { if (!controller.signal.aborted) setBusy(false); });
    return () => controller.abort();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filter, organizationId, readTurn]);

  useEffect(() => {
    const controller = new AbortController();
    readAccountHistoryPolicy(organizationId, controller.signal).then(setPolicy, () => undefined);
    readViewDisplayPolicy(organizationId, controller.signal).then(setDisplay, () => undefined);
    return () => controller.abort();
  }, [organizationId]);

  async function more() {
    if (!cursor || busy) return;
    setBusy(true);
    setReadFailed({ text: "", cursor: "" });
    try {
      const page = await listAuditEvents(organizationId, filter, cursor);
      setEvents((current) => [...current, ...page.events]);
      setCursor(page.nextCursor ?? "");
      remember(page.events);
    } catch (caught) {
      setReadFailed({ text: userErrorText(caught, t("auditLoadFailed")), cursor });
    } finally {
      setBusy(false);
    }
  }

  /**
   * The events written since the list was read, put on top of it, as the
   * export's own "감사 로그 내보내기됨" (2026-11-13: it showed only once the
   * filter changed). Pages read further down stay; a filter changed meanwhile
   * reads its own list.
   */
  async function showNewest(asked: AuditLogFilter) {
    try {
      const page = await listAuditEvents(organizationId, asked, "");
      if (filterNow.current !== asked) return;
      setEvents((current) => {
        const known = new Set(current.map((event) => event.id));
        const fresh = page.events.filter((event) => !known.has(event.id));
        return fresh.length ? [...fresh, ...current] : current;
      });
      remember(page.events);
    } catch {
      // The list as it was; the next read brings it.
    }
  }

  // The export is the organization module's: a core-only build has the log and no export.
  const exportAuditLog = organizationModule()?.exportAuditLog;
  async function exportLog() {
    const session = loadSession();
    if (!session || !exportAuditLog) return;
    const asked = filter;
    setBusy(true);
    setMessage("");
    try {
      // The whole filtered range, with no count limit: the server writes it a
      // part at a time to its last event.
      const download = await exportAuditLog(session, organizationId, {
        mutationId: randomUUID(), format: "csv",
        ...(filter.action ? { action: filter.action } : {}), ...(filter.actor ? { actor: filter.actor } : {}),
        ...(filter.target ? { target: filter.target } : {}), ...(filter.since ? { since: filter.since } : {}),
        ...(filter.until ? { until: filter.until } : {}),
      });
      // Said exported only once it is: a save window closed says nothing.
      const saved = await enterpriseDownloadText(download);
      if (saved === "saved") setMessage(t("auditExported", { count: download.record.eventCount }));
      else if (saved === "failed") setMessage(t("auditExportNotSaved"));
    } catch (caught) {
      setMessage(auditExportFailureText(caught, t));
    } finally {
      // The server writes its line once it served the file, whatever became of it here.
      await showNewest(asked);
      setBusy(false);
    }
  }

  async function setScopedView(scopedView: boolean) {
    if (!policy) return;
    try {
      setPolicy(await updateAccountHistoryPolicy(organizationId, scopedView, policy.revision));
    } catch (caught) {
      setMessage(userErrorText(caught, t("auditPolicyFailed")));
    }
  }

  async function chooseDisplay(next: ViewDisplayPolicy["display"]) {
    if (!display) return;
    try {
      setDisplay(await updateViewDisplayPolicy(organizationId, next, display.revision));
    } catch (caught) {
      setMessage(userErrorText(caught, t("auditPolicyFailed")));
    }
  }

  const setDate = (key: "since" | "until", value: string) => {
    setDates((current) => ({ ...current, [key]: value }));
    setFilter((current) => ({ ...current, [key]: dayEdge(value, key === "until") }));
  };
  const people = [...members].sort((a, b) => a.displayName.localeCompare(b.displayName));
  return (
    <section ref={sectionRef} className={styles.section} role="region" aria-label={t("navAuditLog")} data-audit-log>
      <div className={styles.sectionTitle}>{t("navAuditLog")}</div>
      {/* The log's settings above it, not under a list that grows to hundreds of lines (2026-11-05). */}
      {policy ? (
        <SettingsSwitchRow title={t("auditScopedViewTitle")} description={t("auditScopedView")} checked={policy.scopedView}
          attributes={{ "data-audit-scoped-view": "" }} onChange={(next) => void setScopedView(next)} />
      ) : null}
      {/* Whom page analytics names as having viewed: each person's choice, everyone, or no one (Leafen's own). */}
      {display ? (
        <div className={styles.securityRow} data-audit-view-display={display.display}>
          <div className={styles.securityInstruction}>
            <strong>{t("viewDisplay.title")}</strong>
            <span>{t(`viewDisplay.${display.display}Help`)}</span>
          </div>
          <AppSelect aria-label={t("viewDisplay.title")} look="setting" value={display.display}
            onChange={(event) => void chooseDisplay(event.target.value as ViewDisplayPolicy["display"])}>
            <option value="person">{t("viewDisplay.person")}</option>
            <option value="shown">{t("viewDisplay.shown")}</option>
            <option value="hidden">{t("viewDisplay.hidden")}</option>
          </AppSelect>
        </div>
      ) : null}
      <div className={`${styles.auditTools} ${policy || display ? styles.auditToolsAfterRows : ""}`}>
        <AppSelect value={filter.actor} aria-label={t("auditFilterPerson")} data-audit-actor
          onChange={(event) => setFilter((current) => ({ ...current, actor: event.target.value }))}>
          <option value="">{t("auditEveryone")}</option>
          {filter.actor && !people.some((member) => member.userId === filter.actor)
            ? <option value={filter.actor}>{names[filter.actor] || filter.actor}</option> : null}
          {people.map((member) => <option key={member.userId} value={member.userId}>{withPersonDistinction(member.userId, member.displayName)}</option>)}
        </AppSelect>
        <AppSelect value={filter.action} aria-label={t("ariaFilterAudit")} data-audit-action
          onChange={(event) => setFilter((current) => ({ ...current, action: event.target.value }))}>
          <option value="">{t("auditAllEvents")}</option>
          {[...kinds].sort(([, a], [, b]) => a.localeCompare(b)).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
        </AppSelect>
        <AppDateField label={t("auditFromEmpty")} value={dates.since} emptyLabel={t("auditFromEmpty")} clearLabel={t("auditDateClear")}
          onChange={(day) => setDate("since", day)} />
        <AppDateField label={t("auditToEmpty")} value={dates.until} min={dates.since || undefined} emptyLabel={t("auditToEmpty")} clearLabel={t("auditDateClear")}
          onChange={(day) => setDate("until", day)} />
        {filter.target ? (
          <button type="button" className={styles.auditChip} onClick={() => setFilter((current) => ({ ...current, target: "" }))} data-audit-target>
            {t("auditTargetOnly", { name: targetName })} ×
          </button>
        ) : null}
        {exportAuditLog ? (
          <button type="button" className={styles.secondaryButton} disabled={busy} onClick={() => void exportLog()} data-audit-export>
            {t("auditExport")}
          </button>
        ) : null}
      </div>
      {message ? <div className={styles.noticeText} role="status">{message}</div> : null}
      {events.length ? (
        <div className={styles.auditList}>
          {withoutRepeatedKeyCalls(events).map((raw) => {
            const event = settingsEvent(raw);
            return (
              <div key={event.id} className={styles.auditRow} data-audit-event={event.action}>
                <span className={styles.auditText}>
                  <strong>{organizationAuditLabel(event, t)}</strong>
                  <span>
                    {event.actorId === "scim" ? "SCIM" : event.actorId ? (
                      <button type="button" className={styles.auditLink} data-audit-history={event.actorId}
                        onClick={() => requestAccountHistory({ organizationId, userId: event.actorId, name: names[event.actorId] || event.actorName })}>
                        {organizationAuditActor(event, t, names)}
                      </button>
                    ) : null}
                    {(() => {
                      const target = organizationAuditSubject(event, t, names);
                      return target ? (
                        <>
                          {" · "}
                          <button type="button" className={styles.auditLink} data-audit-narrow={event.targetId}
                            onClick={() => {
                              setTargetName(organizationAuditTargetName(event, t, names));
                              setFilter((current) => ({ ...current, target: event.targetId }));
                            }}>{target}</button>
                        </>
                      ) : null;
                    })()}
                  </span>
                </span>
                <time dateTime={event.occurredAt}>{displayDateTime(event.occurredAt)}</time>
              </div>
            );
          })}
        </div>
      ) : readFailed.text ? null : <div className={styles.emptyState}>{busy ? t("auditLoading") : t("noMatchingAudit")}</div>}
      {readFailed.text ? (
        <EmptyLineReadFailed onRetry={() => {
          if (readFailed.cursor) { void more(); return; }
          // The loading line comes back at the press, as the read goes again.
          setReadFailed({ text: "", cursor: "" }); setBusy(true); setReadTurn((turn) => turn + 1);
        }}
          data-audit-read-failed>{readFailed.text}</EmptyLineReadFailed>
      ) : null}
      {cursor && !readFailed.text ? (
        <button type="button" className={`${styles.secondaryButton} ${styles.auditMore}`} disabled={busy} onClick={() => void more()} data-audit-more>{t("auditMore")}</button>
      ) : null}
    </section>
  );
}
