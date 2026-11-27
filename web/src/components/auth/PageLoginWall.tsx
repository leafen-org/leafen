import { Trans } from "react-i18next";
import { WorkspaceIconGlyph, workspaceIconKind } from "@/components/page/PageIcon";
import { nativeWorkspaceFileIdFromLocator } from "@/lib/workspaceFileTransport";
import type { PageWorkspace } from "./usePageLoginWall";
import styles from "./PageLoginWall.module.css";

/** The wall's screen and card, for the sign-in screen to take on at a page's address. */
export const pageWallStyles = { screen: styles.screen, card: styles.card };

/*
 * The sign-in screen at the address of a page someone isn't signed in for,
 * as Notion's (the user's picture of it, signed out, 2026-11-15): a faint
 * page behind, and over it a card headed by the workspace's icon, a heading
 * that the reader is nearly there and a line asking them to sign in to see the
 * page in that workspace, then the server's own ways in (AuthGateScreen).
 */

/** The faint page behind the card: a sidebar's rows and a page's lines, nothing of the page itself. */
export function PageWallBackdrop() {
  return (
    <div className={styles.backdrop} aria-hidden="true" data-page-wall-backdrop>
      <div className={styles.backdropSidebar}>
        {[70, 40, 40, 40, 55, 0, 30, 60, 50, 65, 50, 45, 60].map((width, index) => (
          width ? <span key={index} className={styles.bar} style={{ width: `${width}%` }} /> : <span key={index} />
        ))}
      </div>
      <div className={styles.backdropPage}>
        <span className={styles.bar} style={{ width: 120 }} />
        <div className={styles.backdropLines}>
          {[100, 100, 90, 100, 95, 100, 90, 100, 100].map((width, index) => (
            <span key={index} className={styles.bar} style={{ width: `${width}%` }} />
          ))}
        </div>
      </div>
    </div>
  );
}

/** The card's head: the workspace's icon, or its name's first letter, and what to do. */
export function PageWallHead({ workspace, line }: {
  workspace: PageWorkspace;
  /** What the second line says: the page's workspace, or another step's own words; none for a step with its own title. */
  line: "workspace" | string | null;
}) {
  // A picture uploaded as its icon is one only its people read (the server's page_login_wall.go).
  const icon = workspace.icon && !nativeWorkspaceFileIdFromLocator(workspace.icon) ? workspace.icon : "";
  return (
    <div className={styles.head} data-page-wall-head>
      <span className={styles.icon} aria-hidden="true" data-workspace-icon={workspaceIconKind(icon)}>
        {icon ? <WorkspaceIconGlyph icon={icon} size={36} fallback={workspace.name.slice(0, 1)} /> : workspace.name.slice(0, 1)}
      </span>
      <h1 className={styles.title}>
        <Trans i18nKey="authGate:pageWallTitle" />
        {/* A space too, so the heading reads as two sentences when read aloud. */}
        {line === null ? null : <>{" "}<br /></>}
        {line === "workspace" ? <Trans i18nKey="authGate:pageWall" values={{ workspace: workspace.name }} components={{ name: <strong /> }} /> : line}
      </h1>
    </div>
  );
}
