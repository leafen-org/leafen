import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { request, LeafenApiError } from "@/apiTransport";
import { isRefusedForNow } from "@/lib/serverBusy";
import { appPlatformErrorCodes, appPlatformOperations, type AppConsent, type AppSession, type UsableAppListResponse } from "@/appPlatformWire.generated";
import { TopBar } from "@/components/page/TopBar";
import { FullscreenIcon } from "@/components/icons";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { appMaker, appText } from "@/components/admin/administratorAppsApi";
import { useProductPages } from "@/components/product/productPagesContext";
import { pushRoute } from "@/lib/router";
import { announceWorkspaceOpen } from "@/lib/workspaceOpenEvents";
import { serverPath } from "@/lib/serverOrigin";
import { appCallAnswerer, type AppToken } from "./appCalls";
import { openAppSession } from "./appSessions";
import { appFrameTheme, bridgeAppFrame } from "./appFrameBridge";
import { useAppFrameReveal } from "./useAppFrameReveal";
import { AppFrameBox, AppWaitingLine } from "./AppFrameBox";
import { AppBarMore } from "./AppBarMore";
import { isServerUnreachable, retryWhenServerMayAnswer } from "@/lib/serverReach";
import { setAppFullWindow } from "@/lib/appFullWindow";
import { AppFrameStopped } from "./AppFrameStopped";
import { AppDataReadsDialog } from "./AppDataReadsDialog";
import { AppRunHistoryDialog } from "./AppRunHistoryDialog";
import { AppConsentDialog } from "./AppConsentDialog";
import { consentToApp } from "./personAppsApi";
import { forgetAppTenancy, useChosenAppTenancy } from "./appTenancyChoice";
import styles from "./AppHost.module.css";
import { AppUnavailableNotice, useAppAvailability } from "./AppUnavailable";
import { resetDocumentChrome, setDocumentChrome } from "@/lib/documentChrome";
import { onAppsChanged } from "@/lib/appsChangedEvents";
import { useAppHomeScreenManifest } from "./useAppHomeScreenManifest";
import { randomUUID } from "@/lib/browserCrypto";
import { fetchRuntimeConfig, subscribeRuntimeConfig } from "@/lib/runtimeConfigService";
import { serverOrigin } from "@/lib/serverOrigin";
import { imeComposing } from "@/lib/fieldKeyBoundary";

// An installed app's screen (docs/app-platform.md 4): Leafen draws the bar
// that names the app and holds the app in a frame with no origin of its own.
// Through the frame's channel (appFrameBridge.ts) Leafen runs what it asks
// (appCalls.ts) and moves within the app at its word.

/** The app's place: its path and the address's query and fragment. */
const appPlace = (path: string) => `${path}${window.location.search}${window.location.hash}`;

// An app in its full window leaves out Leafen's sidebar and page header and
// keeps only Leafen's own bar: the app's name, "App" and the way back
// (docs/app-platform.md 4). The address's query and fragment are the app's,
// so this tab remembers the choice per app instead, through reloads and back
// and forward; an app opened from its home-screen icon starts there.
const fullWindowKey = (slug: string) => `leafen:app-full-window:${slug}`;

function startsInFullWindow(slug: string) {
  try {
    const stored = window.sessionStorage.getItem(fullWindowKey(slug));
    if (stored !== null) return stored === "true";
  } catch {
    // Without the tab's storage it starts as the window was opened.
  }
  return window.matchMedia?.("(display-mode: standalone)").matches ?? false;
}

type Phase = { kind: "loading"; name?: Record<string, string> } | { kind: "missing" } | { kind: "unreachable"; busy?: boolean } | { kind: "ready"; app: AppSession }
  | { kind: "consent"; consent: AppConsent; installId: string; tenancyKey: string };

export default function AppHost({ slug, subpath }: { slug: string; subpath: string }) {
  const { t, i18n } = useTranslation(["common", "serverConsole"]);
  const pages = useProductPages();
  const session = pages?.session;
  const organizationId = pages?.organizationId;
  // The organization the person picked the app for in the workspace menu
  // comes first, then the open workspace's (appTenancyChoice.ts).
  const chosenTenancy = useChosenAppTenancy(slug);
  const tenancy = chosenTenancy || (organizationId ? `org:${organizationId}` : "");
  useEffect(() => () => forgetAppTenancy(slug), [slug]);
  const [phase, setPhase] = useState<Phase>({ kind: "loading" });
  const [attempt, setAttempt] = useState(0);
  const [fullWindow, setFullWindow] = useState(() => startsInFullWindow(slug));
  const { setChromeHidden } = useSidebarLayout(true);
  const frameRef = useRef<HTMLIFrameElement>(null);
  // A frame stopped for flooding Leafen with calls, and how often it was opened again.
  const [flooded, setFlooded] = useState(false);
  const [generation, setGeneration] = useState(0);
  const [showingReads, setShowingReads] = useState(false);
  const [showingRuns, setShowingRuns] = useState(false);
  // Agreeing again opens the app afresh.
  const [consented, setConsented] = useState(0);
  const [consentBusy, setConsentBusy] = useState(false);
  const [consentError, setConsentError] = useState("");
  const nonceRef = useRef("");
  const portRef = useRef<MessagePort | null>(null);
  const tokenRef = useRef<AppToken>({ token: "", expiresAt: 0 });
  // Why the app isn't the person's to open here, once the list says so.
  const availability = useAppAvailability(session, { slug }, tenancy, phase.kind === "missing");
  const loadsRef = useRef(0);
  const pathRef = useRef(subpath);
  // When this app began opening: the waiting line keeps one clock across the loading state and the frame.
  const loadingSince = useRef(performance.now());

  // Which app the address names, in the organization picked for it or that
  // of the open workspace when the person may use it there.
  useEffect(() => {
    if (!session) return;
    let live = true;
    loadingSince.current = performance.now();
    setPhase({ kind: "loading" });
    void (async () => {
      try {
        const listed = await request<UsableAppListResponse>(appPlatformOperations.usable.path, { method: appPlatformOperations.usable.method }, session);
        const choices = listed.apps.filter((app) => app.slug === slug);
        const choice = choices.find((app) => app.tenancyKey === tenancy) ?? choices[0];
        if (!choice) {
          if (live) setPhase({ kind: "missing" });
          return;
        }
        // The bar names the app while its frame opens, from the list read first.
        if (live) setPhase({ kind: "loading", name: choice.name });
        try {
          const opened = await openAppSession(session, choice.installId, choice.tenancyKey);
          // A run the next one replaced leaves the frame's nonce and token alone:
          // a stale nonce in the frame's address went unanswered, and the frame
          // showed its own white page for good (desktop app, 2026-11-05).
          if (!live) return;
          tokenRef.current = { token: opened.token ?? "", expiresAt: opened.expiresAt ?? 0 };
          nonceRef.current = randomUUID();
          // A frame opened anew is a first document again, with its channel.
          loadsRef.current = 0;
          setPhase({ kind: "ready", app: opened });
        } catch (failure) {
          // An app the person added themselves whose permissions changed asks them again first.
          if (!(failure instanceof LeafenApiError) || failure.code !== appPlatformErrorCodes.consentNeeded) throw failure;
          const consent = await consentToApp(session, choice.installId, choice.tenancyKey);
          if (live) setPhase({ kind: "consent", consent, installId: choice.installId, tenancyKey: choice.tenancyKey });
        }
      } catch (failure) {
        // A server that didn't answer, or stayed busy past the transport's own
        // tries, says so, with a way to ask again (2026-10-17: it read "앱을
        // 찾을 수 없음"; 2026-11-18: so did a session refused while a slow
        // write held the database).
        if (live) setPhase(isServerUnreachable(failure)
          ? { kind: "unreachable", busy: isRefusedForNow(failure) } : { kind: "missing" });
      }
    })();
    return () => { live = false; };
  }, [tenancy, session, slug, consented, attempt]);

  const app = phase.kind === "ready" ? phase.app : null;
  // The tab says the app it shows, not the page opened before (2026-10-16).
  const appTitle = app ? appText(app.name, i18n.language) : "";
  // A stopped or revoked app's notice keeps the app's name in the tab, as
  // its top bar does, so it reads as the same screen.
  const missingTitle = phase.kind === "missing" && availability?.name ? appText(availability.name, i18n.language) : "";
  useEffect(() => {
    if (appTitle || missingTitle) setDocumentChrome({ title: appTitle || missingTitle });
    else if (phase.kind === "missing" || phase.kind === "unreachable") resetDocumentChrome();
  }, [appTitle, missingTitle, phase.kind]);
  // An app turned back on, granted or installed in the settings window the
  // notice opened is opened at once when the window says so, not after
  // "다시 시도".
  useEffect(() => phase.kind === "missing" ? onAppsChanged(() => setAttempt((value) => value + 1)) : undefined, [phase.kind]);
  // The server's address from outside changed — turned on in the settings
  // window over the app, say: the app opens anew at it, as the addresses it
  // gives people and their programs come from it (homeUrl), not still the
  // http one a note just said to change (2026-11-13).
  const homeOrigin = phase.kind === "ready" ? new URL(phase.app.homeUrl).origin : "";
  useEffect(() => {
    if (!homeOrigin) return undefined;
    let known: string | null = null;
    void fetchRuntimeConfig().then(({ config }) => { known ??= config.publicAddress ?? ""; });
    return subscribeRuntimeConfig((config) => {
      const address = config.publicAddress ?? "";
      const changed = known !== null && address !== known;
      known = address;
      const outward = address ? new URL(address).origin : serverOrigin();
      if (changed && outward !== homeOrigin) setAttempt((value) => value + 1);
    });
  }, [homeOrigin]);
  const name = app ? appText(app.name, i18n.language) : "";
  const maker = appMaker(app?.developer.name ?? "", app?.builtin, i18n.language);
  useAppHomeScreenManifest(app?.manifestUrl);
  const { frameStyle, waiting, onChannelReady, onChannelOpened, onFrameLoad } = useAppFrameReveal(nonceRef.current);
  // An app the server didn't answer for opens by itself once the server may answer again (lib/serverReach).
  useEffect(() => {
    if (phase.kind !== "unreachable") return undefined;
    return retryWhenServerMayAnswer(() => setAttempt((value) => value + 1));
  }, [phase.kind]);
  // The bar's links fold into "⋯" when the bar can't show them beside the whole maker's name
  // (AppBarMore); they come back once the bar is as wide as they needed.
  const barRef = useRef<HTMLDivElement>(null), makerRef = useRef<HTMLSpanElement>(null);
  const [folded, setFolded] = useState(false);
  const unfoldedWidth = useRef(0);
  useLayoutEffect(() => {
    const bar = barRef.current, maker = makerRef.current;
    if (!bar || !maker) return undefined;
    const check = () => {
      if (!folded) {
        const cut = maker.scrollWidth - maker.clientWidth;
        unfoldedWidth.current = bar.scrollWidth + cut;
        if (cut > 0 || bar.scrollWidth > bar.clientWidth) setFolded(true);
      } else if (bar.clientWidth >= unfoldedWidth.current) setFolded(false);
    };
    check();
    const observer = new ResizeObserver(check);
    observer.observe(bar);
    return () => observer.disconnect();
  }, [folded, name, i18n.language, phase.kind, fullWindow]);

  // The hello, the channel and the app's calls.
  useEffect(() => {
    if (!app || !session) return undefined;
    const answer = appCallAnswerer({
      app, session, tokenRef, t, language: i18n.language,
      navigate: (next) => {
        // The path part is what the router compares; the query and fragment ride along.
        pathRef.current = next.split(/[?#]/u)[0];
        pushRoute(`/apps/${slug}${next ? `/${next}` : ""}`);
      },
      notice: (message) => pages?.onStatus?.(message, "default"),
    });
    return bridgeAppFrame({
      frame: frameRef, nonce: nonceRef.current, port: portRef, answer, onReady: onChannelReady, onHello: onChannelOpened, onFlood: () => setFlooded(true),
      // Opening it again finds out why, and says so in place of the frame.
      onGone: () => setAttempt((value) => value + 1),
      context: () => ({ locale: i18n.language, theme: appFrameTheme(), path: appPlace(pathRef.current), homeUrl: app.homeUrl,
        app: { slug: app.slug, version: app.version }, place: "page", ...(app.administrator ? { administrator: true } : {}) }),
    });
    // The channel lives as long as this app's frame.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [app, generation]);

  // Back and forward within the app reach it as a new place.
  useEffect(() => {
    if (subpath === pathRef.current) return;
    pathRef.current = subpath;
    portRef.current?.postMessage({ type: "location", path: appPlace(subpath) });
  }, [subpath]);

  // The full window holds while the app is open; leaving the app gives
  // Leafen's sidebar and the bands over the app back. Laid out with the
  // app's own bar, so no frame shows the full window beside the sidebar.
  const showingFull = fullWindow && phase.kind === "ready";
  useLayoutEffect(() => {
    setChromeHidden(showingFull);
    setAppFullWindow(showingFull);
    return () => { setChromeHidden(false); setAppFullWindow(false); };
  }, [setChromeHidden, showingFull]);
  const chooseFullWindow = (next: boolean) => {
    setFullWindow(next);
    try {
      window.sessionStorage.setItem(fullWindowKey(slug), String(next));
    } catch {
      // The choice holds until the tab reloads.
    }
  };
  // Esc in Leafen's bar ends the full window; inside the app's frame the
  // keys are the app's.
  useEffect(() => {
    if (!showingFull || showingReads || showingRuns) return undefined;
    const leave = (event: KeyboardEvent) => {
      if (event.key !== "Escape" || event.defaultPrevented || imeComposing(event)) return;
      chooseFullWindow(false);
    };
    window.addEventListener("keydown", leave);
    return () => window.removeEventListener("keydown", leave);
    // chooseFullWindow only writes this app's choice.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [showingFull, showingReads, showingRuns, slug]);

  // Opening a stopped frame again starts it with a fresh channel.
  const reopen = () => {
    nonceRef.current = randomUUID();
    loadsRef.current = 0;
    setFlooded(false);
    setGeneration((value) => value + 1);
  };

  // Until the list names the app, the bar stays empty: a page's "새 페이지" read
  // as if a page were opening (2026-11-05).
  if (phase.kind === "loading") {
    const loadingName = phase.name ? appText(phase.name, i18n.language) : "";
    return <><TopBar title={loadingName} crumbs={loadingName ? undefined : <></>} />
      <AppWaitingLine since={loadingSince.current} label={t("appHost.loading")} className={styles.state} /></>;
  }
  if (phase.kind === "consent") {
    const current = phase;
    const agree = async () => {
      if (!session) return;
      setConsentBusy(true);
      setConsentError("");
      try {
        await consentToApp(session, current.installId, current.tenancyKey, current.consent.permissionsHash);
        setConsented((count) => count + 1);
      } catch {
        setConsentError(t("appConsent.failed"));
      } finally {
        setConsentBusy(false);
      }
    };
    return (
      <>
        <TopBar title={appText(current.consent.name, i18n.language)} />
        <AppConsentDialog consent={current.consent} again busy={consentBusy} error={consentError} onAgree={() => void agree()}
          onClose={() => pushRoute("/")} />
      </>
    );
  }
  if (phase.kind === "unreachable") {
    return (
      <><TopBar title={t("appHost.unreachableTitle")} />
        <div className={styles.state} role="alert" data-app-unreachable>
          <p>{phase.busy ? t("appPlace.busy") : t("appHost.unreachable")}</p>
          <button type="button" className={styles.retry} onClick={() => setAttempt((value) => value + 1)}>{t("appHost.retry")}</button>
        </div></>
    );
  }
  if (phase.kind === "missing") {
    const named = availability?.name ? appText(availability.name, i18n.language) : "";
    return (
      <><TopBar title={named || t("appHost.missingTitle")} />
        <div className={styles.state} data-app-missing>
          <AppUnavailableNotice availability={availability} session={session} onRetry={() => setAttempt((value) => value + 1)} />
        </div></>
    );
  }
  const allow = phase.app.device.map((kind) => (kind === "clipboard-write" ? "clipboard-write" : kind)).join("; ");
  return (
    <div className={styles.host} data-app-host={slug} data-app-full-window={showingFull ? "true" : undefined}>
      {showingFull ? null : <TopBar title={name} />}
      {showingFull ? (
        <div className={styles.bar}>
          <span className={styles.title}>{name}</span>
          <span className={styles.badge}>{t("appHost.badge")}</span>
          <button type="button" className={styles.workspaceLink} data-app-full-window-leave
            onClick={() => chooseFullWindow(false)}>{t("appHost.leaveFullWindow")}</button>
        </div>
      ) : (
        <div className={styles.bar} ref={barRef}>
          <span className={styles.badge}>{t("appHost.badge")}</span>
          <span className={styles.name} ref={makerRef}>{maker.builtin ? t("serverConsole:apps.trust.builtin") : t("appHost.by", { developer: maker.developer })}</span>
          {folded ? (
            <AppBarMore className={`${styles.iconButton} ${styles.barMore}`} label={t("appHost.more")} links={[
              { key: "reads", label: t("appHost.reads.open"), onSelect: () => setShowingReads(true) },
              { key: "did", label: t("appHost.did.open"), onSelect: () => setShowingRuns(true) },
              ...(phase.app.workspaceId ? [{ key: "workspace", label: t("appHost.openWorkspace"),
                onSelect: () => announceWorkspaceOpen(phase.app.workspaceId) }] : []),
            ]} />
          ) : <>
          <button type="button" className={styles.workspaceLink} data-app-reads-open onClick={() => setShowingReads(true)}>{t("appHost.reads.open")}</button>
          <button type="button" className={styles.barLink} data-app-did-open onClick={() => setShowingRuns(true)}>{t("appHost.did.open")}</button>
          {phase.app.workspaceId ? (
            <button type="button" className={styles.barLink} data-app-workspace-link
              onClick={() => announceWorkspaceOpen(phase.app.workspaceId)}>{t("appHost.openWorkspace")}</button>
          ) : null}
          </>}
          <button type="button" className={styles.iconButton} data-app-full-window-open aria-label={t("appHost.fullWindow")}
            title={t("appHost.fullWindow")} onClick={() => chooseFullWindow(true)}>
            <FullscreenIcon size="small" aria-hidden="true" />
          </button>
        </div>
      )}
      {showingReads && session ? (
        <AppDataReadsDialog session={session} installId={phase.app.installId} name={name} onClose={() => setShowingReads(false)} />
      ) : null}
      {showingRuns && session ? (
        <AppRunHistoryDialog session={session} name={name} onClose={() => setShowingRuns(false)}
          usable={{ installId: phase.app.installId, tenancyKey: phase.app.tenancyKey, name: phase.app.name, slug: phase.app.slug }} />
      ) : null}
      {flooded ? <AppFrameStopped onReopen={reopen} /> : (
        <AppFrameBox className={styles.frameBox} waiting={waiting} since={loadingSince.current} label={t("appHost.loading")}
          lineClassName={styles.state}>
        <iframe
          key={generation}
          ref={frameRef}
          className={styles.frame}
          title={name}
          style={frameStyle}
          src={`${serverPath(phase.app.frameUrl)}#leafen=${nonceRef.current}`}
          sandbox="allow-scripts allow-forms"
          allow={allow || undefined}
          referrerPolicy="no-referrer"
          // One document, one hello: a frame that loads another page loses its channel.
          onLoad={() => {
            onFrameLoad();
            loadsRef.current += 1;
            if (loadsRef.current > 1) { portRef.current?.close(); portRef.current = null; }
          }}
          data-app-frame
        />
        </AppFrameBox>
      )}
    </div>
  );
}
