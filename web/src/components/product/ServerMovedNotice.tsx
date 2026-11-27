import { type MouseEvent, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { openMovedServer } from "@/lib/profileSwitch";
import { followAcrossTabs, tellTabs } from "@/lib/followAcrossTabs";
import { fetchRuntimeConfig } from "@/lib/runtimeConfigService";
import { isPackagedClient } from "@/lib/packagedClient";
import { serverOrigin, setEarlierAddresses, setPublishedAddress } from "@/lib/serverOrigin";
import { activePair, followPair, openAtPair, pairPageUrl, servesAsPrimary } from "@/lib/standbyPair";
import { onWindowReturn } from "@/lib/windowReturn";
import type { Session } from "@/types";
import type { WorkspaceMoveOut } from "@/workspaceMoveApi";
import styles from "../admin/BackupFailureNotice.module.css";

// A pause is looked at again this often, as it ends by itself.
const PAUSE_CHECK_MS = 15_000;

type Pair = { moved: boolean; movedTo: string; paused: boolean; standbyOf: string; tookOver: string };
// What the tab that asked tells the others: the pair, and the addresses the config named.
type Told = { pair: Pair; addresses: { published: string; earlier: string[] } | null };

function samePair(left: Pair, right: Pair) {
  return left.moved === right.moved && left.movedTo === right.movedTo && left.paused === right.paused
    && left.standbyOf === right.standbyOf && left.tookOver === right.tookOver;
}

/**
 * Where this server's data is, read as the app opens and every little while:
 * one of the person's tabs in sight asks for all of them, which hear its
 * answer, and a tab behind asks nothing (followAcrossTabs; 2026-11-13: every
 * tab, hidden ones too, asked every 15 seconds).
 */
export function useServerPair(): Pair | null {
  const [pair, setPair] = useState<Pair | null>(null);
  useEffect(() => {
    let live = true;
    const topic = `server-pair:${serverOrigin()}`;
    const read = () => fetchRuntimeConfig({ within: PAUSE_CHECK_MS }).then(async ({ config, unreachable }) => {
      // A server whose data moved names no pair any more; the one kept is where it went.
      const other = activePair(config.pairAddress ?? "", !unreachable && config.serverMoved !== true);
      const away = unreachable === true || (config.serverMoved === true && config.movedTo === other);
      const tookOver = other && away && await servesAsPrimary(other) ? other : "";
      const told: Told = {
        pair: { moved: config.serverMoved === true, movedTo: config.movedTo ?? "", paused: config.serverPaused === true,
          standbyOf: config.standbyOf ?? "", tookOver },
        // A tab that only hears keeps the addresses its links are made with as the reader's.
        addresses: unreachable ? null : { published: config.publicAddress ?? "", earlier: config.earlierAddresses ?? [] },
      };
      tellTabs(topic, told);
    });
    const heard = (value: unknown, here: boolean) => {
      const told = value as Told | undefined;
      if (!live || !told?.pair) return;
      if (!here && told.addresses) {
        setPublishedAddress(told.addresses.published);
        setEarlierAddresses(told.addresses.earlier);
      }
      const next = told.pair;
      if (next.tookOver && followPair()) return;
      // The same answer keeps the pair as it was: a new one each look drew
      // the whole signed-in app again every 15 seconds (2026-11-11: 1.3
      // seconds each on a page with a table of 100,000 rows).
      setPair((current) => current && samePair(current, next) ? current : next);
    };
    // Read anew at the start too, unless another tab or this start's own read
    // asked within the wait (2026-11-13: read again 300 ms after the start
    // had): the settings read before signing in can be from before a
    // takeover, and show a band that isn't so any more (2026-11-04).
    const stop = followAcrossTabs(topic, PAUSE_CHECK_MS, read, heard, { now: true });
    return () => { live = false; stop(); };
  }, []);
  return pair;
}

/**
 * Why this server takes no changes, for the sync notice to say the same as the
 * band about those waiting on this device (2026-11-04: the band said they
 * weren't saved while that notice said they would be sent).
 */
export function serverRefusal(pair: Pair | null): "moved" | "paused" | null {
  // A server its standby took over from won't take them when it comes back.
  if (pair?.moved || pair?.tookOver) return "moved";
  return pair?.paused ? "paused" : null;
}

/**
 * The band everyone sees about where this server's data is (continuous
 * replication and standby servers, 2026-10-17): moved to another server
 * restored from the replica, so changes here aren't kept, with that server
 * to open; paused, as a primary that can't reach its backup place while its
 * standby may take over; or a standby, whose own data goes when it takes
 * over. A server that stops answering, or says it moved to its pair's other
 * server, while that one serves as the primary has been taken over: the app
 * moves there, a page shows the way (lib/standbyPair.ts). Below those, the
 * workspace on screen moving or moved to another server on its own, which
 * takes no changes here (settings/WorkspaceMoveSetting.tsx).
 */
export function useServerMovedNotice(session: Session, workspaceId: string | undefined, pair: Pair | null) {
  const { t } = useTranslation("app");
  const [workspaceMove, setWorkspaceMove] = useState<WorkspaceMoveOut | null>(null);

  // Read as the workspace opens and when the window comes back, not on a timer.
  useEffect(() => {
    setWorkspaceMove(null);
    if (!workspaceId) return undefined;
    const controller = new AbortController();
    const look = () => api.workspaceMoveOut(session, workspaceId, controller.signal).then(setWorkspaceMove, () => undefined);
    void look();
    const stopReturn = onWindowReturn(() => void look());
    return () => { controller.abort(); stopReturn(); };
  }, [session, workspaceId]);

  // A page goes on there signed in; the app follows on its own or adds that server.
  const goOnAt = (event: MouseEvent, origin: string, inApp: () => void) => {
    event.preventDefault();
    if (isPackagedClient()) inApp();
    else void openAtPair(origin, session.token);
  };
  if (pair?.tookOver && !pair.moved) {
    const text = t("serverMoved.tookOver", { address: pair.tookOver });
    const there = pairPageUrl(pair.tookOver);
    return (
      <div className={styles.notice} role="alert" data-server-taken-over>
        <span className={styles.text} title={text}>{text}</span>
        <a className={styles.action} href={there} rel="noreferrer"
          onClick={(event) => goOnAt(event, pair.tookOver, () => window.location.assign(there))}>{t("serverMoved.openPair")}</a>
      </div>
    );
  }
  if (pair?.moved) {
    // With no address to open, the band says where to go on (2026-11-11: a
    // pair with no outside address left the old primary saying only "moved").
    const text = t(pair.movedTo ? "serverMoved.text" : "serverMoved.textNoAddress");
    return (
      <div className={styles.notice} role="alert" data-server-moved>
        <span className={styles.text} title={text}>{text}</span>
        {pair.movedTo ? (
          <a className={styles.action} href={pair.movedTo} rel="noreferrer"
            onClick={(event) => goOnAt(event, pair.movedTo, () => openMovedServer(pair.movedTo))}>{t("serverMoved.open")}</a>
        ) : null}
      </div>
    );
  }
  if (pair?.paused) {
    const text = t("serverMoved.paused");
    return (
      <div className={styles.notice} role="alert" data-server-paused>
        <span className={styles.text} title={text}>{text}</span>
      </div>
    );
  }
  if (pair?.standbyOf) {
    const text = /^https?:\/\//u.test(pair.standbyOf)
      ? t("serverMoved.standbyOf", { address: pair.standbyOf }) : t("serverMoved.standby");
    return (
      <div className={styles.suggestion} role="status" data-server-standby>
        <span className={styles.text} title={text}>{text}</span>
      </div>
    );
  }
  if (workspaceMove?.state === "moved" || workspaceMove?.state === "sending") {
    const movedTo = workspaceMove.movedTo ?? "";
    const text = t(workspaceMove.state === "moved" ? "serverMoved.workspace" : "serverMoved.workspaceSending");
    return (
      <div className={styles.notice} role="alert" data-workspace-moved={workspaceMove.state}>
        <span className={styles.text} title={text}>{text}</span>
        {workspaceMove.state === "moved" && /^https?:\/\//u.test(movedTo) ? (
          <a className={styles.action} href={movedTo} rel="noreferrer"
            onClick={(event) => { event.preventDefault(); openMovedServer(movedTo); }}>{t("serverMoved.open")}</a>
        ) : null}
      </div>
    );
  }
  return null;
}
