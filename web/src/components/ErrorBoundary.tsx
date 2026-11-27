import { Component, type ReactNode } from "react";

import { logProductError } from "@/lib/safeErrorLog";
import { createErrorReference, errorReportUrl } from "@/lib/errorReport";
import { i18next } from "@/i18n";
import { screenCodeStillComing, withLoadDeadline } from "@/lib/screenLoad";
import { reloadWhenAtRest } from "@/lib/reloadAtRest";
import { buildAssetGone, failedBuildAsset } from "@/lib/staleBuild";
import englishLabels from "@/locales/en/errorBoundary.json";

// The boundary also shows when the catalogs could not load; it then reads
// the English source catalog directly.
function errorBoundaryLabels() {
  const text = (key: keyof typeof englishLabels, values: Record<string, string> = {}) => (
    i18next.isInitialized
      ? String(i18next.t(`errorBoundary:${key}`, values))
      : englishLabels[key].replace(/\{\{(\w+)\}\}/gu, (_, name: string) => values[name] ?? "")
  );
  return {
    title: text("title"),
    detail: text("detail"),
    lateTitle: text("lateTitle"),
    lateDetail: text("lateDetail"),
    reference: (reference: string) => text("reference", { reference }),
    reload: text("reload"),
    retry: text("retry"),
    reportIssue: text("reportIssue"),
  };
}

interface ErrorBoundaryProps {
  children: ReactNode;
  /** Where the boundary sits, for the console diagnostic. */
  scope: string;
  /** Root-level recovery reloads the bundle instead of repeating a broken tree. */
  reloadOnRetry?: boolean;
}

interface ErrorBoundaryState {
  error: Error | null;
  reference: string;
  /** The screen's code failed to load and the page is about to load it again. */
  recovering: boolean;
  /** The screen's code failed to load and the server doesn't answer: the page loads again once it does. */
  awaitingServer?: boolean;
}

const ASSET_RECOVERY_STORAGE_KEY = "leafen:chunk-recovery:v1";
const ASSET_RECOVERY_HISTORY_LIMIT = 16;
// A browser that drops a module's request while many tabs load at once can do
// it again on the next load, so one file gets more than one try, further
// apart each time, before the screen says it couldn't be shown.
const ASSET_RECOVERY_WAITS_MS = [0, 2_000, 5_000];
const ASSET_QUESTION_WAIT_MS = 10_000;
// How much longer than its load's own wait a screen's late code is waited for.
const LATE_CODE_GIVE_UP_MS = 120_000;
const inFlightAssetRecoveries = new Map<string, Promise<boolean>>();

/** How long to wait before this file's next try, or null when its tries are used up. */
function claimChunkRecovery(
  storage: Pick<Storage, "getItem" | "setItem">,
  assetPath: string,
): number | null {
  try {
    const parsed = JSON.parse(storage.getItem(ASSET_RECOVERY_STORAGE_KEY) ?? "[]");
    const history = Array.isArray(parsed)
      ? parsed.filter((value): value is string => typeof value === "string")
      : [];
    const wait = ASSET_RECOVERY_WAITS_MS[history.filter((path) => path === assetPath).length];
    if (wait === undefined) return null;
    storage.setItem(
      ASSET_RECOVERY_STORAGE_KEY,
      JSON.stringify([...history, assetPath].slice(-ASSET_RECOVERY_HISTORY_LIMIT)),
    );
    return wait;
  } catch {
    // Without durable same-tab state, reloading could turn recovery into a loop.
    return null;
  }
}

export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = { error: null, reference: "", recovering: false };

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error, reference: createErrorReference(), recovering: failedBuildAsset(error) !== null };
  }

  componentDidCatch(error: Error, _info: { componentStack?: string | null }) {
    logProductError("error", "error-boundary.render", error, {
      diagnosticReference: this.state.reference,
      errorCode: "LEAFEN_RENDER_FAILED",
      causeCode: "LEAFEN_UNKNOWN",
      causeStage: "render",
    });
    this.recoverAsset(error);
    this.recoverLateCode(error);
  }

  // A page reads code it got late from the browser's cache at once, so it
  // loads again when the code is here; a screen beside the person's work waits
  // until that takes nothing from them.
  private recoverLateCode(error: Error) {
    const coming = screenCodeStillComing(error);
    if (!coming || typeof window === "undefined") return;
    // Code that fails at last, or still hasn't come after a while more, isn't
    // waited for: the page loads again for what the server has now, or says
    // plainly it couldn't show the screen (2026-11-06: a tab whose server
    // went to another build meanwhile sat half an hour on "it opens by
    // itself once it arrives").
    let settled = false;
    window.clearTimeout(this.lateCodeGiveUp);
    this.lateCodeGiveUp = window.setTimeout(() => { if (!settled) this.stopWaitingForLateCode(); }, LATE_CODE_GIVE_UP_MS);
    void coming.then(() => {
      settled = true;
      window.clearTimeout(this.lateCodeGiveUp);
      if (this.unmounted) return;
      if (!this.props.reloadOnRetry) {
        reloadWhenAtRest({ shown: true, why: "screen-failed" });
        return;
      }
      let wait: number | null = null;
      try {
        wait = claimChunkRecovery(window.sessionStorage, "late-screen-code");
      } catch {
        wait = null;
      }
      if (wait !== null) window.setTimeout(() => this.reloadApplication(), wait);
      else this.stopWaitingForLateCode();
    }, (cause: unknown) => {
      settled = true;
      window.clearTimeout(this.lateCodeGiveUp);
      if (this.unmounted) return;
      if (cause instanceof Error && failedBuildAsset(cause)) this.recoverAsset(cause);
      else this.stopWaitingForLateCode();
    });
  }

  // A boundary gone (its screen left, or drawn anew) waits for nothing.
  private lateCodeGiveUp: number | undefined;
  private unmounted = false;

  componentWillUnmount() {
    this.unmounted = true;
    window.clearTimeout(this.lateCodeGiveUp);
  }

  private stopWaitingForLateCode() {
    // Only while it still says the code is coming: a screen shown again meanwhile is left alone.
    if (this.unmounted || screenCodeStillComing(this.state.error) === null) return;
    this.setState({ error: new Error("A screen's code didn't arrive."), awaitingServer: false });
    reloadWhenAtRest({ shown: true, why: "screen-code-late" });
  }

  private reloadApplication() {
    window.location.reload();
  }

  private showError() {
    this.setState({ recovering: false });
  }

  // Loading a module again inside this page can't help: the page remembers
  // the failure of every module the file brings with it, while a new load
  // works. So once the server still answers for the file, or has a newer
  // build in its place, the page loads again.
  private recoverAsset(error: Error) {
    const asset = failedBuildAsset(error);
    if (!asset || typeof window === "undefined") return;

    const assetPath = asset.url.pathname;
    const inFlight = inFlightAssetRecoveries.get(assetPath);
    if (inFlight) {
      void inFlight.then((reloading) => { if (!reloading) this.showError(); });
      return;
    }
    let wait: number | null = null;
    try {
      wait = claimChunkRecovery(window.sessionStorage, assetPath);
    } catch {
      wait = null;
    }
    if (wait === null) {
      logProductError("error", "error-boundary.chunk-recovery", error, {
        diagnosticReference: this.state.reference,
        errorCode: "LEAFEN_CHUNK_RECOVERY_FAILED",
        causeCode: "LEAFEN_UNKNOWN",
        causeStage: "recovery-admission",
      });
      this.showError();
      return;
    }
    const recovery = new Promise<void>((resolve) => { setTimeout(resolve, wait); })
      .then(() => withLoadDeadline(buildAssetGone(assetPath), ASSET_QUESTION_WAIT_MS).catch(() => null))
      .then((gone) => {
        if (gone !== null) {
          this.reloadApplication();
          return true;
        }
        logProductError("error", "error-boundary.chunk-recovery", error, {
          diagnosticReference: this.state.reference,
          errorCode: "LEAFEN_CHUNK_RECOVERY_FAILED",
          causeCode: "LEAFEN_UNKNOWN",
          causeStage: "recovery-load",
        });
        this.setState({ recovering: false, awaitingServer: true });
        this.askUntilServerAnswers(assetPath);
        return false;
      })
      .finally(() => {
        if (inFlightAssetRecoveries.get(assetPath) === recovery) {
          inFlightAssetRecoveries.delete(assetPath);
        }
      });
    inFlightAssetRecoveries.set(assetPath, recovery);
  }

  // No answer about the file is no answer from the server (2026-11-05: a reload
  // in an outage right after a new build, whose modules weren't kept yet, stayed
  // on this screen after the server came back). It says so, and goes on asking.
  private askUntilServerAnswers(assetPath: string) {
    const ask = () => {
      void withLoadDeadline(buildAssetGone(assetPath), ASSET_QUESTION_WAIT_MS).catch(() => null).then((gone) => {
        if (gone !== null) this.reloadApplication();
        else window.setTimeout(ask, ASSET_QUESTION_WAIT_MS);
      });
    };
    window.setTimeout(ask, ASSET_QUESTION_WAIT_MS);
  }

  render() {
    if (!this.state.error) return this.props.children;
    if (this.state.recovering) return null;
    const labels = errorBoundaryLabels();
    const late = this.state.awaitingServer === true || screenCodeStillComing(this.state.error) !== null;
    const reportHref = errorReportUrl({
      errorClass: "render-crash",
      pathname: typeof window === "undefined" ? "" : window.location.pathname,
      reference: this.state.reference,
      userAgent: typeof navigator === "undefined" ? "" : navigator.userAgent,
    });
    return (
      <div
        role="alert"
        style={{
          margin: "24px auto",
          maxWidth: 480,
          padding: "16px 20px",
          borderRadius: 8,
          border: "1px solid var(--border-default)",
          color: "var(--text-default)",
          fontSize: 14,
          lineHeight: 1.5,
        }}
      >
        <div style={{ fontWeight: 600, marginBottom: 4 }}>{late ? labels.lateTitle : labels.title}</div>
        <div style={{ opacity: 0.7, marginBottom: 4 }}>
          {late ? labels.lateDetail : labels.detail}
        </div>
        <div style={{ opacity: 0.55, marginBottom: 12, fontSize: 12 }}>
          {labels.reference(this.state.reference)}
        </div>
        <div
          style={{
            display: "flex",
            flexWrap: "wrap",
            gap: 8,
          }}
        >
          <button
            type="button"
            onClick={() => {
              if (this.props.reloadOnRetry) {
                this.reloadApplication();
                return;
              }
              this.setState({ error: null, reference: "", recovering: false, awaitingServer: false });
            }}
            style={{
              padding: "4px 12px",
              borderRadius: 6,
              border: "1px solid var(--border-default)",
              background: "transparent",
              color: "inherit",
              cursor: "pointer",
              fontSize: 13,
            }}
          >
            {this.props.reloadOnRetry
              ? labels.reload
              : labels.retry}
          </button>
          <a
            href={reportHref}
            target="_blank"
            rel="noopener noreferrer"
            style={{
              padding: "4px 12px",
              borderRadius: 6,
              color: "inherit",
              fontSize: 13,
              textDecoration: "underline",
              textUnderlineOffset: 2,
            }}
          >
            {labels.reportIssue}
          </a>
        </div>
      </div>
    );
  }
}
