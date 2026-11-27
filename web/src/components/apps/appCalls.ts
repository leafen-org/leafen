import type { MutableRefObject } from "react";
import type { TFunction } from "i18next";
import { request, API_ROOT } from "@/apiTransport";
import {
  appPlatformErrorCodes, appPlatformOperations, type AppApprovalGrant, type AppApprovalRequest, type AppGuestLinkMade, type AppGuestReportResult, type AppSession,
  type AppCredentialList, type AppSessionInput, type AppStorageResult,
} from "@/appPlatformWire.generated";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { askAppApproval } from "./AppApprovalDialog";
import { enterAppCredential } from "./AppCredentialDialog";
import { appText } from "@/components/admin/administratorAppsApi";
import { pushRoute, routeInfoFromPath } from "@/lib/router";
import { legacySettingsAddress } from "@/components/settings/settingsWindowModel";
import { requestSettingsWindow } from "@/components/settings/settingsWindowRequest";
import { serverOrigin, serverPath } from "@/lib/serverOrigin";
import type { Session } from "@/types";
import { appCallResult, type AppFrameAnswer, appBridgeRefusal } from "./appFrameBridge";
import { printPdfDocument } from "@/lib/printExportDocument";
import { signInWindowEnded } from "@/lib/signInWindow";
import { APP_TOKEN_REFRESH_BEFORE_MS, openAppSession } from "./appSessions";
import { callsOnCurrentInstall } from "./appInstallMove";
import { saveAppFile, type AppFileSaveTarget } from "./appFileSave";
import { askServerForApp } from "./appAskServer";

/** The code an app sees when the person didn't allow its action, one of the platform's own. */
const APPROVAL_DECLINED = appPlatformErrorCodes.approvalDeclined;

// What Leafen does for an app's frame wherever it is drawn — its own page,
// one of its blocks in a page, beside a page (docs/app-platform.md 4): its
// calls to Leafen with a token the frame never sees, outside sign-ins,
// actions that may need the person's approval, the relay, rooms, its stores,
// an outside link after asking, a Leafen page, a short notice. Moving within
// the app and anything only one place has are that place's to answer.

const API_METHODS = new Set(["GET", "POST", "PATCH", "PUT", "DELETE"]);

/** A call to Leafen for an app's frame, asked again while the server or a limit refuses it for now (appAskServer.ts). */
const askServer = (input: string, init?: RequestInit) => askServerForApp(() => fetch(input, init), init?.signal);

export type AppToken = { token: string; expiresAt: number };

// How a call to an action failed, for the app's screen. What Leafen itself says
// of a run is in the person's language, as a screen may show it whole; a refusal
// is the app's own words, and the rest are for whoever is writing the app.
function actionFailure(status: number, error: { code?: string; message?: string } | undefined, t: TFunction) {
  const code = error?.code ?? "failed";
  const words = code === appPlatformErrorCodes.actionFailed ? t(status === 503 ? "appPlace.actionBusy" : "appPlace.actionBroke")
    : code === appPlatformErrorCodes.actionTimedOut ? t("appPlace.actionPastLimit")
    : code === appPlatformErrorCodes.appStopped ? t("appPlace.stopped")
    : code === appPlatformErrorCodes.approvalInvalid ? t("appPlace.approvalInvalid")
    : error?.message ?? "";
  return `${code}: ${words}`;
}

export function appCallAnswerer({ app: opened, session, tokenRef, t, language, navigate, notice, extra, fileSave, place, moveInstall }: {
  app: AppSession;
  session: Session;
  tokenRef: MutableRefObject<AppToken>;
  t: TFunction;
  language: string;
  /** Moving within the app, to a path of its own. */
  navigate(path: string): void;
  notice(message: string): void;
  /** What only this place answers; anything else is unknown. */
  extra?: AppFrameAnswer;
  /** The file a screen that opened it may save. */
  fileSave?: MutableRefObject<AppFileSaveTarget | null>;
  /** Where the frame opened, as a fresh token names it (a file opener's file). */
  place?: () => Omit<AppSessionInput, "tenancyKey">;
  /** For a frame that keeps what it holds: the app's session where it is now, once the install it opened with is gone (appInstallMove.ts). */
  moveInstall?: (from: string) => Promise<AppSession | null>;
}): AppFrameAnswer {
  // The install the calls go to: the one the frame opened with, or the app's current one it moved to.
  let app = opened;
  const name = appText(app.name, language);

  // A fresh token before this one runs out.
  async function token(): Promise<string> {
    const current = tokenRef.current;
    if (!current.token) return "";
    if (current.expiresAt - Date.now() > APP_TOKEN_REFRESH_BEFORE_MS) return current.token;
    const opened = await openAppSession(session, app.installId, app.tenancyKey, place?.());
    tokenRef.current = { token: opened.token ?? "", expiresAt: opened.expiresAt ?? 0 };
    return tokenRef.current.token;
  }

  // People's credentials (the mail decision of 2026-10-07): Leafen asks for
  // them in its own window with the person's own sign-in, never the app's
  // token, and the app learns only which are given.
  async function credentials(method: string, key: string): Promise<unknown> {
    const path = (template: string) => template.replace("{installId}", encodeURIComponent(app.installId)).replace("{key}", encodeURIComponent(key));
    const listed = () => request<AppCredentialList>(path(appPlatformOperations.listCredentials.path), {}, session);
    const answer = (list: AppCredentialList) => ({
      enabled: list.enabled, credentials: list.credentials.map((credential) => ({ key: credential.key, connected: credential.connected })),
    });
    if (method === "credentials") return answer(await listed());
    if (method === "disconnectCredential") {
      await request<unknown>(path(appPlatformOperations.deleteCredential.path), { method: "DELETE" }, session);
      return answer(await listed());
    }
    const list = await listed();
    const credential = list.credentials.find((item) => item.key === key);
    if (!credential) throw appBridgeRefusal("invalidCall");
    if (!list.enabled) throw appBridgeRefusal("credentialsOff");
    await enterAppCredential({
      appName: name, credential, language,
      save: async (values) => {
        await request<AppCredentialList>(path(appPlatformOperations.setCredential.path), { method: "PUT", body: JSON.stringify({ values }) }, session);
      },
    });
    return answer(await listed());
  }

  // Outside sign-ins: the provider's page opens in a window of its own (the
  // app's click carries over to this page), the server keeps the tokens, and
  // the app learns only whether each sign-in is connected.
  async function accounts(method: string, provider: string): Promise<unknown> {
    // Opened before anything is awaited, while the app's click still counts.
    const popup = method === "connectAccount" ? window.open("about:blank", `leafen-sign-in-${provider}`, "popup,width=520,height=720") : null;
    if (method === "connectAccount" && !popup) throw appBridgeRefusal("popupBlocked");
    // An app without data access holds no token: Leafen's window connects for its frame with the
    // person's own sign-in, in the tenancy it opened in, as it relays (appRelayCaller).
    const bearer = await token() || session.token;
    const tenancy = bearer === session.token ? `?tenancy=${encodeURIComponent(app.tenancyKey)}` : "";
    const call = async (verb: string, path: string) => {
      const response = await askServer(serverPath(`${API_ROOT}${path}${tenancy}`), { method: verb, headers: { authorization: `Bearer ${bearer}` } });
      const body = await response.json().catch(() => null) as { error?: { code?: string; message?: string } } | null;
      if (!response.ok) throw new Error(`${body?.error?.code ?? "failed"}: ${body?.error?.message ?? ""}`);
      return body;
    };
    const fill = (path: string) => path.replace("{installId}", encodeURIComponent(app.installId)).replace("{provider}", encodeURIComponent(provider));
    if (method === "connectedAccounts") return call("GET", fill(appPlatformOperations.oauthAccounts.path));
    if (method === "disconnectAccount") return call("DELETE", fill(appPlatformOperations.oauthDisconnect.path));
    if (!popup) throw appBridgeRefusal("popupBlocked");
    // When this provider last connected, read beside the start: a newer time is this sign-in's (unread, none is).
    const connectedAt = (list: unknown) => (list as { accounts?: { key: string; connectedAt: number }[] } | null)?.accounts
      ?.find((account) => account.key === provider)?.connectedAt ?? 0;
    const before = call("GET", fill(appPlatformOperations.oauthAccounts.path)).then(connectedAt, () => Number.POSITIVE_INFINITY);
    try {
      const started = await call("POST", fill(appPlatformOperations.oauthStart.path)) as { authorizeUrl: string };
      popup.location.href = started.authorizeUrl;
    } catch (failure) {
      popup.close();
      throw failure;
    }
    // Until the page it comes back to says so, or the server, or the person
    // leaves the window (lib/signInWindow.ts); a word naming no install is a
    // sign-in whose state had expired, this window's.
    const earlier = await before;
    await signInWindowEnded(popup, {
      matches: (word) => word.type === "leafen:oauth" && (word.install === app.installId && word.provider === provider || !word.install),
      completed: async () => connectedAt(await call("GET", fill(appPlatformOperations.oauthAccounts.path))) > earlier,
    });
    return call("GET", fill(appPlatformOperations.oauthAccounts.path));
  }

  // An action runs on the server; one that needs approval asks the person
  // here, outside the app's frame, and only their own click approves it.
  async function runAction(key: string, input: unknown): Promise<unknown> {
    const fill = (path: string, values: Record<string, string>) =>
      path.replace(/\{(\w+)\}/gu, (_, field: string) => encodeURIComponent(values[field] ?? ""));
    const send = async (approval?: string) => {
      const bearer = await token();
      if (!bearer) throw appBridgeRefusal("noDataAccess");
      const response = await askServer(serverPath(`${API_ROOT}${fill(appPlatformOperations.runAction.path, { installId: app.installId, action: key })}`), {
        method: "POST",
        headers: { authorization: `Bearer ${bearer}`, "content-type": "application/json", ...(approval ? { "x-leafen-approval": approval } : {}) },
        body: JSON.stringify({ input }),
      });
      const body = await response.json().catch(() => null) as { error?: { code?: string; message?: string }; approval?: AppApprovalRequest } | null;
      return { response, body };
    };
    let { response, body } = await send();
    if (response.status === 428 && body?.approval) {
      const asked = body.approval;
      const decided = (verb: "approve" | "decline") =>
        fill(appPlatformOperations[verb].path, { installId: app.installId, approvalId: asked.approvalId });
      const choice = await askAppApproval({
        appName: name, actionName: appText(asked.actionName, language), risk: asked.risk, fields: asked.fields ?? [], allowHour: asked.allowHour,
        externalEffect: asked.externalEffect,
      });
      if (choice === "decline") {
        await request(decided("decline"), { method: "POST" }, session).catch(() => undefined);
        // Said as the platform's other refusals are, "code: sentence", so an
        // app that shows why shows words, not the code (2026-11-05: the
        // Approvals app read "approval_declined" in red).
        throw new Error(`${APPROVAL_DECLINED}: ${t("appHost.approvalDeclined")}`);
      }
      const grant = await request<AppApprovalGrant>(decided("approve"), { method: "POST", body: JSON.stringify({ for: choice }) }, session);
      ({ response, body } = await send(grant.approvalToken));
    }
    if (!response.ok) throw new Error(actionFailure(response.status, body?.error, t));
    return body;
  }

  async function answer(method: string, params: Record<string, unknown>, ended?: AbortSignal): Promise<unknown> {
    if (method === "api") {
      const verb = String(params.method ?? "GET").toUpperCase();
      const path = String(params.path ?? "");
      if (!API_METHODS.has(verb) || !path.startsWith("/") || path.startsWith("//") || path.includes("..") || path.length > 2048) {
        throw appBridgeRefusal("invalidCall");
      }
      const bearer = await token();
      if (!bearer) throw appBridgeRefusal("noDataAccess");
      const response = await askServer(serverPath(`${API_ROOT}${path}`), {
        method: verb,
        headers: { authorization: `Bearer ${bearer}`, ...(params.body == null ? {} : { "content-type": "application/json" }) },
        body: params.body == null ? undefined : JSON.stringify(params.body), // a null body is none, as for a worker's leafen_api
      });
      // A file's bytes, such as GET /files/{id} answers, come as they are,
      // for a screen that opens files; everything else is JSON.
      const contentType = response.headers.get("content-type") ?? "";
      if (response.ok && !/\bjson\b/iu.test(contentType)) {
        return { status: response.status, body: await response.arrayBuffer(), contentType };
      }
      const text = await response.text();
      let body: unknown = null;
      try { body = text ? JSON.parse(text) : null; } catch { body = null; }
      return { status: response.status, body };
    }
    if (method === "saveFile") return saveAppFile(params, fileSave, token);
    if (method === "relay") {
      // Outside addresses the administrator approved, through the server, which fills in secrets.
      // An app without data access holds no token: Leafen's window relays for
      // its frame with the person's own sign-in, in the tenancy it opened in.
      const bearer = await token() || session.token;
      const path = appPlatformOperations.relay.path.replace("{installId}", encodeURIComponent(app.installId));
      const headers = params.headers && typeof params.headers === "object" ? params.headers as Record<string, unknown> : {};
      const response = await askServer(serverPath(`${API_ROOT}${path}${bearer === session.token ? `?tenancy=${encodeURIComponent(app.tenancyKey)}` : ""}`), {
        method: "POST",
        headers: { authorization: `Bearer ${bearer}`, "content-type": "application/json" },
        body: JSON.stringify({
          method: String(params.method ?? "GET"), url: String(params.url ?? ""),
          headers: Object.fromEntries(Object.entries(headers).map(([name, value]) => [name, String(value)])),
          ...(typeof params.body === "string" ? { body: params.body, bodyBase64: params.bodyBase64 === true } : {}),
        }),
      });
      const answer = await response.json().catch(() => null) as { error?: { code?: string; message?: string } } | null;
      if (!response.ok) throw new Error(`${answer?.error?.code ?? "relay_failed"}: ${answer?.error?.message ?? ""}`);
      return answer;
    }
    if (method === "room") {
      // A ticket to one of the app's rooms; the app's frame joins it with y-websocket.
      const bearer = await token();
      if (!bearer) throw appBridgeRefusal("noDataAccess");
      const path = appPlatformOperations.roomTicket.path.replace("{installId}", encodeURIComponent(app.installId))
        .replace("{room}", encodeURIComponent(String(params.name ?? "")));
      const response = await askServer(serverPath(`${API_ROOT}${path}`), { method: "POST", headers: { authorization: `Bearer ${bearer}` } });
      const body = await response.json().catch(() => null) as { error?: { code?: string; message?: string } } | null;
      if (!response.ok) throw new Error(`${body?.error?.code ?? "failed"}: ${body?.error?.message ?? ""}`);
      return body;
    }
    if (method === "guestLink") {
      // A public link to one of the app's guest rooms, when the administrator lets the app take guests.
      const bearer = await token();
      if (!bearer) throw appBridgeRefusal("noDataAccess");
      const response = await askServer(serverPath(`${API_ROOT}${appPlatformOperations.makeGuestLink.path.replace("{installId}", encodeURIComponent(app.installId))}`), {
        method: "POST", headers: { authorization: `Bearer ${bearer}`, "content-type": "application/json" },
        body: JSON.stringify({ room: String(params.room ?? ""), minutes: Number(params.minutes ?? 60) }),
      });
      return appCallResult<AppGuestLinkMade>(response);
    }
    if (method === "reportGuest") {
      // Reports a guest in the app's room by the ID Leafen gave the app with its messages; blocking keeps it out.
      const bearer = await token();
      if (!bearer) throw appBridgeRefusal("noDataAccess");
      const path = appPlatformOperations.reportGuest.path.replace("{installId}", encodeURIComponent(app.installId))
        .replace("{guestId}", encodeURIComponent(String(params.guest ?? "")));
      const response = await askServer(serverPath(`${API_ROOT}${path}`), {
        method: "POST", headers: { authorization: `Bearer ${bearer}`, "content-type": "application/json" },
        body: JSON.stringify({ reason: String(params.reason ?? ""), block: Boolean(params.block) }),
      });
      return appCallResult<AppGuestReportResult>(response);
    }
    if (method === "kv") {
      // The app's temporary store, in the organization the app was opened for.
      const bearer = await token();
      if (!bearer) throw appBridgeRefusal("noDataAccess");
      const response = await askServer(serverPath(`${API_ROOT}${appPlatformOperations.kv.path.replace("{installId}", encodeURIComponent(app.installId))}`), {
        method: "POST", headers: { authorization: `Bearer ${bearer}`, "content-type": "application/json" }, body: JSON.stringify(params),
      });
      const body = await response.json().catch(() => null) as { result?: unknown; error?: { code?: string; message?: string } } | null;
      if (!response.ok) throw new Error(`${body?.error?.code ?? "failed"}: ${body?.error?.message ?? ""}`);
      return body?.result ?? null;
    }
    if (method === "storage") {
      // The person's own small store for this app, in place of the browser storage its frame can't have.
      const bearer = await token();
      if (!bearer) throw appBridgeRefusal("noDataAccess");
      const response = await askServer(serverPath(`${API_ROOT}${appPlatformOperations.storage.path.replace("{installId}", encodeURIComponent(app.installId))}`), {
        method: "POST", headers: { authorization: `Bearer ${bearer}`, "content-type": "application/json" }, body: JSON.stringify(params),
      });
      return appCallResult<AppStorageResult>(response);
    }
    if (method === "action") {
      return runAction(String(params.name ?? ""), params.input ?? {});
    }
    if (method === "connectAccount" || method === "connectedAccounts" || method === "disconnectAccount") {
      return accounts(method, String(params.provider ?? ""));
    }
    if (method === "connectCredential" || method === "credentials" || method === "disconnectCredential") {
      return credentials(method, String(params.key ?? ""));
    }
    if (method === "navigate") {
      navigate(String(params.path ?? "").replace(/^\/+/u, ""));
      return null;
    }
    if (method === "openLink") {
      const url = new URL(String(params.url ?? ""));
      if (url.protocol !== "https:" && url.protocol !== "http:") throw appBridgeRefusal("invalidCall");
      // Leafen's own address isn't a site outside it: one of its screens
      // opens here, anything else it serves in a new tab, without asking
      // (2026-10-16: the drive's "앱 비밀번호 만들기" asked to open "a site
      // outside Leafen" — this server).
      if (new Set([new URL(app.homeUrl).origin, window.location.origin, serverOrigin()]).has(url.origin)) {
        // A setting opens in the settings window over the app, which goes on
        // as it was, not by a trip to the setting's address and back: that
        // opened the app anew each time, and on an iPhone it sometimes stayed
        // at "앱을 여는 중…" with no window (2026-11-13, the vault's "외부 접속 켜기").
        const setting = legacySettingsAddress(url.pathname, url.search);
        if (setting) {
          requestSettingsWindow(setting.state);
          return true;
        }
        const route = routeInfoFromPath(url.pathname).kind;
        // Its own passwords on the apps page are where the app's link lands.
        if (route === "apps" && url.hash === "#app-passwords" && !url.searchParams.has("install")) url.searchParams.set("install", app.installId);
        if (route !== "unknown" && route !== "invalid") pushRoute(`${url.pathname}${url.search}${url.hash}`);
        else window.open(url.href, "_blank", "noopener,noreferrer");
        return true;
      }
      const go = await confirmAction({ message: t("appHost.openLink", { host: url.host }), confirmLabel: t("appHost.openLinkAction") });
      if (go) window.open(url.href, "_blank", "noopener,noreferrer");
      return go;
    }
    if (method === "openPage") {
      // A Leafen page, opened in Leafen itself: the app's screen gives way.
      const pageId = String(params.pageId ?? "");
      // A row of an app's table is a page too, under its own longer ID.
      if (!/^[A-Za-z0-9_-]{1,255}$/u.test(pageId)) throw appBridgeRefusal("invalidCall");
      pushRoute(`/p/${pageId}`);
      return null;
    }
    if (method === "printDocument") {
      // An app's frame can't open a print dialog; Leafen prints its PDF here, for as long as the
      // frame that asked is open, once the person has used the page, so an app can't open one by itself.
      if (!ended) throw appBridgeRefusal("unknownCall");
      if (!(params.bytes instanceof ArrayBuffer)) throw appBridgeRefusal("invalidCall");
      if (navigator.userActivation && !navigator.userActivation.hasBeenActive) throw appBridgeRefusal("needsClick");
      await printPdfDocument(params.bytes, ended).catch(() => { throw appBridgeRefusal("invalidCall"); });
      return null;
    }
    if (method === "notice") {
      notice(String(params.message ?? "").slice(0, 200));
      return null;
    }
    if (extra) return extra(method, params);
    throw appBridgeRefusal("unknownCall");
  }

  // A call that never reached Leafen says so in the person's words, not the browser's.
  const run: AppFrameAnswer = (method, params, ended) => answer(method, params, ended).catch((failure: unknown) => {
    if (failure instanceof TypeError && /fetch|network|load failed/iu.test(failure.message)) {
      throw new Error(`unreachable: ${t("appHost.unreachable")}`);
    }
    throw failure;
  });
  return moveInstall ? callsOnCurrentInstall(run, {
    installId: () => app.installId, move: moveInstall,
    moved: (next) => { app = next; tokenRef.current = { token: next.token ?? "", expiresAt: next.expiresAt ?? 0 }; },
  }) : run;
}
