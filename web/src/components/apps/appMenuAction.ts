import type { TFunction } from "i18next";
import { LeafenApiError } from "@/apiTransport";
import { appPlatformErrorCodes, type AppSession, type UsableApp } from "@/appPlatformWire.generated";
import { appText } from "@/components/admin/administratorAppsApi";
import { pushRoute } from "@/lib/router";
import type { Session } from "@/types";
import { appCallAnswerer } from "./appCalls";
import { openAppSession } from "./appSessions";
import { withAppInstalledAgain } from "./useUsableApps";

/** The code an app sees when the person didn't allow its action, one of the platform's own. */
const APPROVAL_DECLINED = appPlatformErrorCodes.approvalDeclined;

const APP_MENU_ACTION_QUIET_MS = 800;

/**
 * Runs one of an app's actions from a line it added to Leafen's own menus —
 * a "/" item, a database row's or a page's "…" menu (docs/app-platform.md
 * 10-1) — as the person, in the app's organization, asking here for the
 * approval it needs, and says how it went. The action's input names where
 * it was chosen.
 */
export async function runAppMenuAction({ session, usable, action, label, input, t, language, notice, running }: {
  session: Session;
  usable: UsableApp;
  action: string;
  /** The line's name, as the person read it. */
  label: string;
  input: Record<string, unknown>;
  t: TFunction;
  language: string;
  notice(message: string, tone: "default" | "success" | "error"): void;
  /** Shows that the line is still going, until the returned call. */
  running?(message: string): () => void;
}): Promise<boolean> {
  const name = appText(usable.name, language);
  // A line that takes a while says so: most end before this shows, and the
  // longest would leave ten silent seconds after the menu closed (2026-11-05).
  let stopRunning: (() => void) | undefined;
  const runningTimer = window.setTimeout(() => {
    const message = t("appPlace.actionRunning", { name: label, app: name });
    if (running) stopRunning = running(message);
    else notice(message, "default");
  }, APP_MENU_ACTION_QUIET_MS);
  const finished = () => { window.clearTimeout(runningTimer); stopRunning?.(); };
  let app: AppSession;
  try {
    app = await withAppInstalledAgain(session, usable, (now) => openAppSession(session, now.installId, now.tenancyKey));
  } catch (failure) {
    finished();
    const code = failure instanceof LeafenApiError ? failure.code : "";
    notice(t(code === appPlatformErrorCodes.consentNeeded ? "appPlace.consent"
      : code === appPlatformErrorCodes.appStopped ? "appPlace.stopped" : "appPlace.failed"), "error");
    return false;
  }
  const tokenRef = { current: { token: app.token ?? "", expiresAt: app.expiresAt ?? 0 } };
  const answer = appCallAnswerer({
    app, session, tokenRef, t, language,
    navigate: (next) => pushRoute(`/apps/${app.slug}${next ? `/${next}` : ""}`),
    notice: (message) => notice(message, "default"),
  });
  try {
    await answer("action", { name: action, input });
    finished();
    notice(t("appPlace.actionDone", { name: label, app: name }), "success");
    return true;
  } catch (failure) {
    finished();
    // The person said no; nothing more to say.
    if (failure instanceof Error && failure.message.startsWith(`${APPROVAL_DECLINED}: `)) return false;
    // The app's own reason for refusing is said with it.
    if (failure instanceof Error && failure.message.startsWith(`${appPlatformErrorCodes.appStopped}: `)) {
      notice(t("appPlace.stopped"), "error");
      return false;
    }
    if (failure instanceof Error && failure.message.startsWith(`${appPlatformErrorCodes.actionTimedOut}: `)) {
      notice(t("appPlace.actionTimedOut", { name: label, app: name }), "error");
      return false;
    }
    const refused = failure instanceof Error && failure.message.startsWith(`${appPlatformErrorCodes.actionRefused}: `)
      ? failure.message.slice(appPlatformErrorCodes.actionRefused.length + 2).trim() : "";
    notice(refused
      ? t("appPlace.actionRefused", { name: label, app: name, reason: refused })
      : t("appPlace.actionFailed", { name: label, app: name }), "error");
    return false;
  }
}
