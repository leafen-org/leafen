import { Component, type ReactNode } from "react";
import { i18next } from "@/i18n";
import { createErrorReference } from "@/lib/errorReport";
import { reloadWhenAtRest } from "@/lib/reloadAtRest";
import { logProductError } from "@/lib/safeErrorLog";
import { isScreenLoadFailure, screenCodeStillComing, withLoadDeadline } from "@/lib/screenLoad";
import { retryWhenServerMayAnswer } from "@/lib/serverReach";
import { buildAssetGone, failedBuildAsset } from "@/lib/staleBuild";
import { ConfirmCard } from "./ConfirmActionDialog";
import styles from "./ScreenLoadBoundary.module.css";

// A failure that may pass at once (a browser that dropped a module's request
// while many tabs loaded) is tried again this soon, when the server has the file.
const FIRST_ASK_MS = 2_000;
const ASK_WAIT_MS = 10_000;

type ScreenLoadBoundaryProps = {
  children: ReactNode;
  /** Draws the screen again, its code loaded anew; `serverHadIt` when the server said it has the file. */
  retry: (serverHadIt: boolean) => void;
  /** The screen is a window: its card is one too, and closes it. */
  onClose?: () => void;
  /** What shows in place of the card, for a small part (null: nothing). */
  standIn?: ReactNode;
  /** This drawing follows a try made because the server had the file. */
  serverHadIt: boolean;
};

type ScreenLoadBoundaryState = {
  failure: Error | null;
  reference: string;
  /** The file belongs to a build the server replaced: the boundary above moves the app to the new one. */
  handOver: boolean;
};

/**
 * Keeps a screen's code that didn't come to the place the screen draws — its
 * window, panel or view — and draws that place again once the code can come:
 * when the server answers again and still has the file, or the late code
 * arrives. The rest of the app goes on meanwhile. A file the server no longer
 * has (a new build went up) goes to the boundary above, which moves the app
 * to the new build (ErrorBoundary); any other failure of the screen too.
 */
export class ScreenLoadBoundary extends Component<ScreenLoadBoundaryProps, ScreenLoadBoundaryState> {
  state: ScreenLoadBoundaryState = { failure: null, reference: "", handOver: false };

  // Gone, or handed its place to a new drawing: it waits for nothing more.
  private over = false;
  private stopWaiting = () => {};

  static getDerivedStateFromError(error: Error): Partial<ScreenLoadBoundaryState> {
    return { failure: error, reference: createErrorReference(), handOver: false };
  }

  componentDidCatch(error: Error) {
    if (!isScreenLoadFailure(error) && !failedBuildAsset(error)) return;
    // Reported as the app's boundary reports a screen that couldn't draw, under the card's reference.
    logProductError("error", "error-boundary.render", error, {
      diagnosticReference: this.state.reference,
      errorCode: "LEAFEN_RENDER_FAILED",
      causeCode: "LEAFEN_UNKNOWN",
      causeStage: "render",
    });
    const coming = screenCodeStillComing(error);
    if (coming) {
      // Late code is drawn the moment it comes; code that fails at last is asked after as any.
      void coming.then(() => this.retry(false), (cause: unknown) => this.waitForServer(cause));
      // A load that neither comes nor fails is tried again as the server may answer.
      this.stopWaiting = retryWhenServerMayAnswer(() => this.retry(false));
      return;
    }
    this.waitForServer(error);
  }

  componentWillUnmount() {
    this.over = true;
    this.stopWaiting();
  }

  private retry(serverHadIt: boolean) {
    if (this.over) return;
    this.over = true;
    this.stopWaiting();
    this.props.retry(serverHadIt);
  }

  private waitForServer(error: unknown) {
    if (this.over) return;
    this.stopWaiting();
    const asset = failedBuildAsset(error);
    // Without the file's name (Safari's message names none) it is tried again as the server may answer.
    if (!asset) {
      this.stopWaiting = retryWhenServerMayAnswer(() => this.retry(false));
      return;
    }
    let asking = false;
    const ask = async () => {
      if (asking || this.over) return;
      asking = true;
      const gone = await withLoadDeadline(buildAssetGone(asset.url.pathname), ASK_WAIT_MS).catch(() => null);
      asking = false;
      if (this.over || gone === null) return;
      if (gone) {
        this.stopWaiting();
        this.setState({ handOver: true });
      } else if (!this.props.serverHadIt) {
        this.retry(true);
      } else {
        // Tried again with the file there and failed again: this page keeps
        // the failure (a browser that remembers a module that failed), and a
        // new load mends it, once that takes nothing from the person.
        this.stopWaiting();
        reloadWhenAtRest({ shown: true, why: "screen-code-failed" });
      }
    };
    const first = window.setTimeout(() => void ask(), FIRST_ASK_MS);
    const stopAsking = retryWhenServerMayAnswer(() => void ask());
    this.stopWaiting = () => {
      window.clearTimeout(first);
      stopAsking();
    };
  }

  render() {
    const { failure, handOver, reference } = this.state;
    if (!failure) return this.props.children;
    if (handOver || !(isScreenLoadFailure(failure) || failedBuildAsset(failure))) throw failure;
    if (this.props.standIn !== undefined) return this.props.standIn;
    const title = i18next.t("errorBoundary:lateTitle");
    const detail = i18next.t("errorBoundary:lateDetail");
    const retry = i18next.t("errorBoundary:retry");
    const { onClose } = this.props;
    if (onClose) {
      return (
        <ConfirmCard message={title} detail={detail} confirmLabel={retry} cancelLabel={i18next.t("common:actions.close")}
          markers={{ dialog: { "data-screen-load-failed": "window" } }}
          onAnswer={(answer) => { if (answer === null) onClose(); else this.retry(false); }} />
      );
    }
    return (
      <div role="alert" className={styles.card} data-screen-load-failed="place" contentEditable={false}>
        <div className={styles.title}>{title}</div>
        <div className={styles.detail}>{detail}</div>
        <div className={styles.reference}>{i18next.t("errorBoundary:reference", { reference })}</div>
        <button type="button" className={styles.retry} onClick={() => this.retry(false)}>{retry}</button>
      </div>
    );
  }
}
