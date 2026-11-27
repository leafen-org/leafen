import { useEffect, useRef, useState, type ReactNode } from "react";
import { Trans, useTranslation } from "react-i18next";
import type { AccessRequestOutcome } from "@/lib/accessRequests";
import { SendIcon } from "@/components/icons";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { loadSession } from "@/lib/authSessionStore";
import { resetDocumentChrome } from "@/lib/documentChrome";
import { pushRoute, replaceRoute } from "@/lib/router";
import { onWindowReturn } from "@/lib/windowReturn";
import styles from "./PageNoAccess.module.css";
import { pageMarkSource } from "@/lib/pageMark";

/**
 * A screen that takes the whole window: what is behind it takes no focus and
 * isn't read out, as Notion's has nothing behind it. What opens later over it,
 * such as adding an account, stays reachable, and so does a window already
 * open over it (2026-11-06: a restore took the open page, and the "서버의
 * 데이터가 바뀌었습니다" window over its "not found" took no press).
 */
function WholeWindowScreen({ children, ...data }: { children: ReactNode; [key: `data-${string}`]: unknown }) {
  const screen = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const behind: Element[] = [];
    const over = (element: Element) => element.getAttribute("aria-modal") === "true" || Boolean(element.querySelector("[aria-modal=true]"));
    for (let node: Element | null = screen.current; node?.parentElement; node = node.parentElement) {
      for (const sibling of node.parentElement.children) {
        if (sibling !== node && sibling.tagName !== "SCRIPT" && !sibling.hasAttribute("inert") && !over(sibling)) behind.push(sibling);
      }
    }
    for (const element of behind) element.setAttribute("inert", "");
    return () => { for (const element of behind) element.removeAttribute("inert"); };
  }, []);
  return <div ref={screen} className={styles.screen} data-surface="route-problem" {...data}>{children}</div>;
}

// How often a card whose access was asked for looks whether it came.
const ACCESS_CHECK_MS = 10_000;

// The outline of a page behind the card, as Notion draws it (seen 2026-11-09):
// a title, then three headings each over its lines, each bar's top, width
// and height in a 708px column, fading out down its lower half.
const OUTLINE: ReadonlyArray<readonly [top: number, width: number, height: number]> = [
  [0, 540, 28],
  [79, 120, 22],
  [124, 708, 16], [150, 708, 16], [176, 676, 16], [202, 676, 16], [228, 644, 16], [254, 708, 16],
  [304, 112, 22],
  [347, 708, 16], [373, 708, 16], [399, 708, 16], [425, 692, 16], [451, 692, 16], [477, 692, 16], [503, 708, 16], [529, 708, 16],
  [572, 124, 22],
  [615, 708, 16], [641, 708, 16], [667, 692, 16], [693, 660, 16],
];

/** The faded page behind a screen's card; it takes no part in the page. */
function PageOutline() {
  return (
    <div className={styles.outline} aria-hidden="true">
      {OUTLINE.map(([top, width, height]) => <span key={top} style={{ top, width, height }} />)}
    </div>
  );
}

/** Leafen's mark and name in the window's corner; it opens what they can reach. */
function Brand() {
  const { t } = useTranslation("routeState");
  return (
    <button type="button" className={styles.brand} onClick={() => replaceRoute("/")}>
      <img src={pageMarkSource()} alt="" aria-hidden="true" />{t("noAccessBrand")}
    </button>
  );
}

/**
 * A page someone can't open takes the whole window, as Notion's (seen
 * 2026-11-05, for a guest and a member alike 2026-11-09, with no sidebar): a
 * card to ask for it over a faded page outline, and who they are signed in as. A page of an app's own workspace is asked of nobody: its
 * people are those who may use the app, so the card says who chooses them
 * and opens the app, which lets in one who may (2026-11-08: it offered a
 * request no one receives).
 */
export function PageNoAccess({ pageId, workspaceName, appSlug, account, onAddAccount, onRequest, onCheckAccess }: {
  pageId: string;
  workspaceName: string;
  /** The app's address when the page is in an app's own workspace. */
  appSlug?: string;
  account: string;
  onAddAccount?: () => void;
  onRequest: (pageId: string) => Promise<AccessRequestOutcome>;
  /** Looks whether the page has opened to them; if it has, it replaces this screen. */
  onCheckAccess?: () => Promise<unknown>;
}) {
  const { t } = useTranslation("routeState");
  const [outcome, setOutcome] = useState<AccessRequestOutcome | "busy" | "">("");
  const requested = outcome === "requested";
  // A page they lost keeps no name in the window.
  useEffect(() => { resetDocumentChrome(); }, []);
  // Access may come while the card is up: coming back to the window looks, and
  // so does waiting once it was asked for.
  useEffect(() => {
    if (!onCheckAccess) return;
    const check = () => { void onCheckAccess().catch(() => undefined); };
    const stopReturn = onWindowReturn(check);
    const timer = requested ? window.setInterval(() => {
      if (document.visibilityState === "visible") check();
    }, ACCESS_CHECK_MS) : undefined;
    return () => { stopReturn(); window.clearInterval(timer); };
  }, [onCheckAccess, requested]);
  return (
    <WholeWindowScreen data-page-no-access="">
      <PageOutline />
      <Brand />
      {requested ? (
        // Once asked, the card says it was sent and offers the way back, as Notion's (seen 2026-11-09).
        <div className={styles.card} role="status" data-route-access-request-outcome="requested">
          <span className={styles.sent} aria-hidden="true"><SendIcon size="card" /></span>
          <h1>{t("accessRequestSentTitle")}</h1>
          <p>{t("accessRequestSentBody")}</p>
          <button type="button" className={`${styles.request} ${styles.back}`} onClick={() => replaceRoute("/")}>{t("noAccessBackToContent")}</button>
        </div>
      ) : <div className={styles.card} role="status">
        <span className={styles.mark} aria-hidden="true">{workspaceName.trim().slice(0, 1)}</span>
        {/* The workspace's name alone is bold, as Notion's title (seen 2026-11-13: 600 over 400). */}
        <h1 className={styles.noAccessTitle}><Trans t={t} i18nKey="noAccessTitle" values={{ workspace: workspaceName }}
          components={{ name: <span className={styles.workspaceName} /> }} /></h1>
        {appSlug ? <>
          <p>{t("noAccessAppBody")}</p>
          <button type="button" className={styles.request} data-route-open-app={appSlug} onClick={() => pushRoute(`/apps/${appSlug}`)}>
            {t("noAccessOpenApp")}
          </button>
        </> : <>
          <p>{t("noAccessBody")}</p>
          <button type="button" className={styles.request} data-route-access-request disabled={outcome === "busy"}
            onClick={() => {
              setOutcome("busy");
              void onRequest(pageId).then(setOutcome);
            }}>
            {t("requestAccess")}
          </button>
          {outcome && outcome !== "busy" ? <p className={styles.outcome} data-route-access-request-outcome={outcome}>{t(`accessRequest_${outcome}`)}</p> : null}
        </>}
      </div>}
      <SignedInAs account={account} onAddAccount={onAddAccount} />
    </WholeWindowScreen>
  );
}

/** The window a page's screen takes while what it is — closed to the reader, or gone — is read: its outline alone. */
export function PageProblemPending() {
  return <WholeWindowScreen data-page-problem-pending=""><PageOutline /><Brand /></WholeWindowScreen>;
}

function SignedInAs({ account, onAddAccount }: { account: string; onAddAccount?: () => void }) {
  const { t } = useTranslation("routeState");
  // The signed-in person's face as everywhere else, their photo or their
  // name's first letter, beside their address, as Notion's (seen 2026-11-15).
  const user = account ? loadSession()?.user : undefined;
  return (
    <p className={styles.account}>
      {account ? <PersonAvatar className={styles.accountMark} userId={user?.id} name={user?.name || account} size={20} /> : null}
      {account ? <Trans t={t} i18nKey="noAccessAccount" values={{ account }}
        components={{ name: <span className={styles.accountName} /> }} /> : null}
      {account && onAddAccount ? " · " : null}
      {onAddAccount ? <button type="button" onClick={onAddAccount}>{t("noAccessAddAccount")}</button> : null}
    </p>
  );
}

/**
 * A page that isn't there, or isn't there for this person, takes the whole
 * window too, as Notion's (seen 2026-11-05): nothing moves them elsewhere, and
 * the way back opens what they can still reach, or the offer to make a
 * workspace when they have none. An open tab shows it as soon as its workspace
 * is deleted.
 */
export function PageNotFound({ account, onAddAccount }: { account: string; onAddAccount?: () => void }) {
  const { t } = useTranslation("routeState");
  useEffect(() => { resetDocumentChrome(); }, []);
  return (
    <WholeWindowScreen data-page-not-found="">
      <Brand />
      <div className={styles.notFound} role="status">
        <h1>{t("pageNotFoundTitle")}</h1>
        <p>{t("pageNotFoundBody")}</p>
        <button type="button" onClick={() => replaceRoute("/")}>{t("pageNotFoundBack")}</button>
      </div>
      <SignedInAs account={account} onAddAccount={onAddAccount} />
    </WholeWindowScreen>
  );
}
