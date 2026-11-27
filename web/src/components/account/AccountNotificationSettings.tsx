import { SwitchMark } from "@/components/Switch";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { iosBrowserWithoutPush, pushSettingKeys, readDesktopPushState, turnDesktopPushOff, turnDesktopPushOn, type DesktopPushState, type PushWait } from "@/lib/desktopPush";
import { desktopNoticesHere, phoneNoticesHere } from "@/lib/desktopNotices";
import type { NotificationSettings, NotificationSettingsChange, SlackNotificationState } from "@/notificationSettingsApi";
import type { Session } from "@/types";
import { styles } from "./accountSettingsStyles";
import { productErrorMessage } from "@/lib/productErrorPresentation";
import { useServerAdministrator } from "@/lib/serverAdministrator";
import { useShownChanges } from "@/lib/shownChanges";
import { useSidebarLayout } from "@/components/shell/AppShell";

/**
 * Settings → Notifications: desktop push in this browser (a phone's app
 * refreshes in the background instead, and an iPhone's browser points to
 * Leafen added to the home screen, where web push comes), activity emails
 * and whether they wait until the person has been away, and the page update
 * and workspace digests. Email choices stay off while the server cannot send.
 * The Slack and email switches turn as they are pressed and are put back with
 * why if the server refuses (lib/shownChanges.ts); turning push on or off
 * waits for this browser's permission and subscription, as a device's sign-in.
 * A phone lists the emails before Slack, as Notion's in iPhone Safari (seen
 * 2026-11-14), its groups' headings left out (AccountSettingsPhone.module.css).
 */
export function AccountNotificationSettings({ session }: { session: Session }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const { phone: phoneScreen, touch } = useSidebarLayout(true);
  const [settings, setSettings] = useState<NotificationSettings | null>(null);
  const [error, setError] = useState<string | null>(null);
  // Push alone waits for its answer: the browser asks for permission and subscribes.
  const [pushBusy, setPushBusy] = useState(false);
  // What it waits on meanwhile: the switch says the server isn't answering only for the server (lib/switchTurns.ts).
  const [pushWait, setPushWait] = useState<PushWait | null>(null);
  const pending = useShownChanges<NotificationSettingsChange & { slack: boolean }>();
  const [push, setPush] = useState<DesktopPushState | null>(null);
  const [slack, setSlack] = useState<SlackNotificationState | null>(null);
  // Only the server's administrator can give it a public address: they hear
  // where, everyone else what is so (2026-11-07: members read an environment
  // variable's name).
  const administrator = useServerAdministrator(session, Boolean(settings && !settings.emailLinks));

  useEffect(() => {
    const controller = new AbortController();
    void api.readSlackNotificationState(session, controller.signal).then((state) => {
      if (!controller.signal.aborted) setSlack(state);
    }).catch(() => {
      if (!controller.signal.aborted) setSlack({ available: false, enabled: false });
    });
    return () => controller.abort();
  }, [session.sessionId, session.token, session.user.id]);

  const slackOn = pending.shown.slack ?? slack?.enabled === true;

  function changeSlack() {
    if (!slack?.available) return;
    const next = !slackOn;
    setError(null);
    void pending.change({ slack: next }, async () => setSlack(await api.saveSlackNotificationState(session, next)), "slack")
      .catch(() => setError(t("notificationsSlackFailed")));
  }

  useEffect(() => {
    let current = true;
    void readDesktopPushState(session).then((state) => {
      if (current) setPush(state);
    }).catch(() => {
      if (current) setPush("off");
    });
    return () => { current = false; };
  }, [session.sessionId, session.token, session.user.id]);

  const phone = phoneNoticesHere();
  const app = phone || desktopNoticesHere();
  const iphone = /iPhone|iPad/u.test(navigator.userAgent);
  const keys = pushSettingKeys();
  const pushTitle = t(keys.title);
  const pushDescription = push === "unsupported" ? t(iosBrowserWithoutPush() ? "notificationsPushHomeScreen" : "notificationsPushUnsupported")
    : push === "denied" ? t(app ? "notificationsAppDenied" : "notificationsPushDenied")
    : phone ? `${t(keys.description)}${iphone ? ` ${t("notificationsRealtimeHomeScreen")}` : ""}`
    : t(keys.description);

  const pushLocked = pushBusy || !push || push === "unsupported" || push === "denied";
  async function changePush() {
    if (pushLocked) return;
    setPushBusy(true);
    setError(null);
    try {
      setPush(await (push === "on" ? turnDesktopPushOff(session, setPushWait) : turnDesktopPushOn(session, setPushWait)));
    } catch (failure) {
      setError(t(failure instanceof DOMException && failure.name === "TimeoutError" ? "notificationsPushNoAnswer" : "notificationsPushFailed"));
    } finally {
      setPushBusy(false);
      setPushWait(null);
    }
  }

  useEffect(() => {
    const controller = new AbortController();
    setError(null);
    void api.readNotificationSettings(session, controller.signal).then((next) => {
      if (!controller.signal.aborted) setSettings(next);
    }).catch((caught) => {
      if (!controller.signal.aborted) setError(productErrorMessage(caught, "accountNotifications.read"));
    });
    return () => controller.abort();
  }, [session.sessionId, session.token, session.user.id, t]);

  function change(next: NotificationSettingsChange) {
    setError(null);
    void pending.change(next, async () => setSettings(await api.updateNotificationSettings(session, next)), "email")
      .catch((caught: unknown) => setError(productErrorMessage(caught, "accountNotifications.update")));
  }

  const shown = settings ? { ...settings, ...pending.shown } : null;
  const emailOff = !settings?.emailAvailable;
  const rows: Array<{
    key: keyof NotificationSettingsChange;
    title: string;
    description: string;
    on: boolean;
    disabled: boolean;
  }> = shown ? [
    {
      key: "emailActivity",
      title: t("notificationsEmailActivity"),
      description: t("notificationsEmailActivityDesc"),
      on: shown.emailActivity,
      disabled: emailOff,
    },
    {
      key: "emailAlways",
      title: t("notificationsEmailAlways"),
      description: t("notificationsEmailAlwaysDesc"),
      on: shown.emailAlways,
      disabled: emailOff || !shown.emailActivity,
    },
    {
      key: "emailPageUpdates",
      title: t("notificationsEmailPageUpdates"),
      description: t("notificationsEmailPageUpdatesDesc"),
      on: shown.emailPageUpdates,
      disabled: emailOff,
    },
    {
      key: "emailDigest",
      title: t("notificationsEmailDigest"),
      description: t("notificationsEmailDigestDesc"),
      on: shown.emailDigest,
      disabled: emailOff,
    },
  ] : [];

  const slackSection = (
    <section className={styles.section} role="region" aria-label={t("notificationsConnectedAppsGroup")}>
      <div className={styles.sectionTitle}>{t("notificationsConnectedAppsGroup")}</div>
      <div className={styles.securityRow} data-notification-setting="slack" data-slack-state={slack ? (slackOn ? "on" : slack.available ? "off" : "unavailable") : undefined}>
        <div className={styles.securityInstruction}>
          <strong>{t("notificationsSlack")}</strong>
          <span>{slack && !slack.available ? t("notificationsSlackUnavailable") : t("notificationsSlackDesc")}</span>
        </div>
        <button
          type="button"
          role="switch"
          aria-checked={slackOn}
          aria-label={t("notificationsSlack")}
          disabled={!slack?.available}
          data-waiting-note={pending.note("slack")}
          onClick={changeSlack}
        >
          <SwitchMark on={slackOn} />
        </button>
      </div>
    </section>
  );
  const emailSection = (
    <section className={styles.section} role="region" aria-label={t("notificationsEmailGroup")}>
      <div className={styles.sectionTitle}>{t("notificationsEmailGroup")}</div>
      {settings && emailOff ? (
        <div className={styles.notice} data-tone="neutral">{t("notificationsEmailUnavailable")}</div>
      ) : null}
      {settings && !emailOff && !settings.emailLinks ? (
        <div className={styles.notice} data-tone="neutral">
          {t(administrator ? "notificationsEmailNoLinksAdministrator" : "notificationsEmailNoLinks")}
        </div>
      ) : null}
      {rows.map((row) => (
        <div key={row.key} className={styles.securityRow} data-notification-setting={row.key}>
          <div className={styles.securityInstruction}>
            <strong>{row.title}</strong><span>{row.description}</span>
          </div>
          <button
            type="button"
            role="switch"
            aria-checked={row.on}
            aria-label={row.title}
            disabled={row.disabled}
            data-waiting-note={pending.note(row.key)}
            onClick={() => change({ [row.key]: !row.on })}
          >
            <SwitchMark on={row.on} />
          </button>
        </div>
      ))}
    </section>
  );

  return (
    // In Notion's groups (seen 2026-10-11): 앱 내 알림 · 연결된 앱 · 이메일 알림;
    // on a touch screen the first is "푸시 알림", as Notion's iPad (seen 2026-11-13).
    <div data-account-notifications aria-busy={pushBusy || !settings}>
      {error ? <div className={styles.notice} role="alert">{error}</div> : null}
      <section className={styles.section} role="region" aria-label={t(touch ? "notificationsPushGroup" : "notificationsInAppGroup")}>
        <div className={styles.sectionTitle}>{t(touch ? "notificationsPushGroup" : "notificationsInAppGroup")}</div>
        <div className={styles.securityRow} data-notification-setting="desktopPush" data-push-state={push ?? undefined}>
          <div className={styles.securityInstruction}>
            <strong>{pushTitle}</strong>
            <span>{pushDescription}</span>
          </div>
          <button
            type="button"
            role="switch"
            aria-checked={push === "on"}
            aria-label={pushTitle}
            disabled={pushLocked}
            // A refusal or a browser without push holds it off, waiting on nothing.
            data-lock={pushBusy ? (pushWait === "server" ? undefined : pushWait ?? undefined) : pushLocked ? "held" : undefined}
            onClick={() => void changePush()}
          >
            <SwitchMark on={push === "on"} />
          </button>
        </div>
      </section>
      {phoneScreen ? null : slackSection}
      {emailSection}
      {phoneScreen ? slackSection : null}
    </div>
  );
}
