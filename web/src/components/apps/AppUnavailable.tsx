import { useEffect, useState } from "react";
import { appText } from "@/components/admin/administratorAppsApi";
import { requestSettingsWindow } from "@/components/settings/settingsWindowRequest";
import { useTranslation } from "react-i18next";
import { request } from "@/apiTransport";
import { appPlatformOperations, type AppAvailability } from "@/appPlatformWire.generated";
import { useAppPlatformKnown, useAppPlatformMissing } from "@/lib/appPlatformAvailability";
import { onAppsChanged } from "@/lib/appsChangedEvents";
import { displayDate } from "@/lib/displayDate";
import { pushRoute } from "@/lib/router";
import type { Session } from "@/types";
import { AppPlatformMissingNotice } from "./AppPlatformMissing";
import styles from "./AppUnavailable.module.css";

// The reason last read for a place, shared for a few seconds: a frame that
// found its app stopped says why, and the place that takes over from it a
// moment later starts from the same answer instead of asking from nothing.
const SHARED_MS = 10_000;
const recentAnswers = new Map<string, { answer: AppAvailability; at: number }>();

// A place that names no organization (a page outside one) is answered for
// wherever the person may use the app, as the frame's own place was.
function recentAnswer(key: string, exact: boolean): AppAvailability | undefined {
  const now = Date.now();
  for (const [kept, { answer, at }] of recentAnswers) {
    if (now - at < SHARED_MS && (kept === key || (!exact && kept.startsWith(key)))) return answer;
  }
  return undefined;
}

/**
 * Why the person can't use an app here — not on the server, in its trash,
 * stopped, not given to them, or in another organization — with what to do next: an
 * administrator opens Server › Apps, a member browses the apps to ask for it
 * or asks an administrator (2026-10-16: one sentence named all three reasons
 * for everyone and offered nothing).
 */
export function useAppAvailability(session: Session | null | undefined, which: { slug?: string; app?: string }, tenancy: string,
  enabled: boolean): AppAvailability | null | undefined {
  const key = `${session?.user.id ?? ""}\u0000${which.slug ?? ""}\u0000${which.app ?? ""}\u0000${tenancy}`;
  // Undefined until answered; null when the server couldn't say.
  const [availability, setAvailability] = useState<AppAvailability | null | undefined>(() => recentAnswer(key, Boolean(tenancy)));
  // A build without the app platform has no one to ask.
  const platform = useAppPlatformKnown();
  // Asked again when apps change — "다시 시도", or an administrator's change
  // in a window over it — so the reason shown is the one that now holds.
  const [asked, setAsked] = useState(0);
  useEffect(() => enabled ? onAppsChanged(() => setAsked((value) => value + 1)) : undefined, [enabled]);
  useEffect(() => {
    if (!session || !enabled || platform === false) return undefined;
    let live = true;
    const query = new URLSearchParams();
    if (which.slug) query.set("slug", which.slug);
    if (which.app) query.set("app", which.app);
    if (tenancy) query.set("tenancy", tenancy);
    request<AppAvailability>(`${appPlatformOperations.availability.path}?${query}`, { method: appPlatformOperations.availability.method }, session)
      .then((answer) => {
        recentAnswers.set(key, { answer, at: Date.now() });
        if (live) setAvailability(answer);
      }, () => { if (live) setAvailability(null); });
    return () => { live = false; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [asked, enabled, key, platform, session]);
  return platform === false && availability === undefined ? null : availability;
}

/**
 * The app's name where it is placed — a block's mark, a side panel's head —
 * from the list of apps the person may use, or, while they can't use it here
 * (stopped, not given to them), from the reason asked for it: a page with
 * several app blocks said only "앱" on a stopped one (2026-11-15).
 */
export function appPlaceName(usable: { name: Record<string, string> } | undefined, availability: AppAvailability | null | undefined,
  language: string) {
  const name = usable?.name ?? availability?.name;
  return name ? appText(name, language) : "";
}

/** The reason in a sentence, by who reads it. */
function appUnavailableText(t: (key: string, options?: Record<string, string>) => string, availability: AppAvailability | null) {
  if (!availability) return t("appHost.missing");
  const manager = availability.canManage;
  switch (availability.state) {
    case "stopped":
      return t(manager ? "appHost.unavailable.stoppedManager" : "appHost.unavailable.stopped");
    case "revoked":
      return t(manager ? "appHost.unavailable.revokedManager" : "appHost.unavailable.revoked");
    case "notGranted":
      if (availability.canAdd) return t("appHost.unavailable.notGrantedAdd");
      return t(manager ? "appHost.unavailable.notGrantedManager" : "appHost.unavailable.notGranted");
    case "otherOrganization":
      return t("appHost.unavailable.otherOrganization");
    // Removed to the trash, it can still be restored until its date (Server › Apps' row says the same).
    case "trashed":
      return manager ? t("appHost.unavailable.trashedManager", { date: displayDate(availability.purgeAt ?? 0) })
        : t("appHost.unavailable.trashed");
    case "notInstalled":
      if (manager) return t("appHost.unavailable.notInstalledManager");
      return t(availability.installPolicy === "admins" ? "appHost.unavailable.notInstalled" : "appHost.unavailable.notInstalledRequest");
  }
  return t("appHost.missing");
}

/** The reason and what to do next, where the app would be. */
export function AppUnavailableNotice({ availability, onRetry, compact = false, session }: {
  /** Undefined while the reason is being asked: the notice stays quiet until it can say why and offer what to do. */
  availability: AppAvailability | null | undefined;
  /** Asks again, once an administrator turned the app back on. */
  onRetry?: () => void;
  compact?: boolean;
  /** For a phone server's administrator, the way to move it (AppPlatformMissing.tsx). */
  session?: Session | null;
}) {
  const { t } = useTranslation("common");
  // A phone's own server or the App Store edition has no apps at all (AppPlatformMissing.tsx).
  const missing = useAppPlatformMissing();
  if (missing) return <AppPlatformMissingNotice missing={missing} session={session} compact={compact} />;
  // Not a sentence without its buttons while the reason is on its way (2026-11-15: "이 앱이 설치되지
  // 않았거나 멈췄거나…" stood for a moment before the stopped app's notice and its buttons).
  if (availability === undefined) return <div className={compact ? styles.compact : styles.notice} data-app-unavailable="asking" />;
  const openServerApps = availability?.canManage && availability.state !== "otherOrganization";
  const browse = !availability?.canManage && ((availability?.state === "notInstalled" && availability.installPolicy !== "admins")
    || (availability?.state === "notGranted" && Boolean(availability.canAdd)));
  // Asking again is for an app that stopped (an administrator stopped it, or
  // a catalog revoked its version): once it runs again it opens here. Not
  // being given it, or another organization's app, a retry doesn't change
  // (2026-11-06: "다시 시도" under "관리자에게 물어보세요").
  const retry = Boolean(onRetry && (availability?.state === "stopped" || availability?.state === "revoked"));
  return (
    <div className={compact ? styles.compact : styles.notice} data-app-unavailable={availability?.state ?? "unknown"}>
      <span>{appUnavailableText(t, availability)}</span>
      {openServerApps || browse || retry ? (
        <span className={styles.actions}>
          {openServerApps ? (
            <button type="button" className={styles.action} data-app-unavailable-manage
              data-focus-return={`app-settings:${availability?.slug ?? ""}`}
              onClick={() => requestSettingsWindow(availability?.slug ? { section: "server-apps", appRow: availability.slug } : { section: "server-apps" })}>
              {t("appHost.unavailable.openServerApps")}
            </button>
          ) : null}
          {browse ? (
            <button type="button" className={styles.action} data-app-unavailable-browse onClick={() => pushRoute("/apps")}>
              {t("appHost.unavailable.browseApps")}
            </button>
          ) : null}
          {retry ? (
            <button type="button" className={styles.action} data-app-unavailable-retry onClick={onRetry}>{t("appHost.retry")}</button>
          ) : null}
        </span>
      ) : null}
    </div>
  );
}
