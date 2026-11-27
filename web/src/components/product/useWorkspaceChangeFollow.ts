import { useEffect, useRef } from "react";
import { api } from "@/api";
import { LeafenApiError } from "@/apiTransport";
import type { Bootstrap, Session } from "@/types";
import { announceAccessChange, onWorkspaceChangeCheck } from "@/lib/workspaceChangeCheck";
import { announceNoticeArrival } from "@/lib/noticeArrivals";
import { announcePeopleChange } from "@/lib/peopleProfiles";
import { afterOwnWrites } from "@/lib/ownWritesInFlight";
import { followListToken, listToken, unfollowListToken } from "@/lib/workspaceListToken";
import { serverWebSocketUrl } from "@/lib/serverOrigin";
import { isLeavingWorkspace } from "@/lib/workspaceLeaving";
import { readSidebarLayoutAgain } from "@/components/sidebar/useSidebarLayout";
import { announceGraphsChanged, graphsDrawnBeside, onGraphsDrawnBesideChange } from "@/lib/sidebarGraphFollow";
import { workspaceChangeDecisions, workspaceChangeReasons } from "@/workspaceMembershipWire.generated";

// How often the app asks, while it is in view, whether the workspace changed.
const WORKSPACE_CHANGE_CHECK_MS = 10_000;
// A closed socket is opened again after a pause twice as long each time, up
// to half a minute, so a server going away isn't asked without end; one that
// stayed open a while starts again from a second.
const SOCKET_RETRY_FIRST_MS = 1_000;
const SOCKET_RETRY_MAX_MS = 30_000;

/**
 * The page list follows the changes other people and other sessions make,
 * as Notion's sidebar does: while the app is in view, and whenever it comes
 * back into view, it asks whether the workspace changed since its list was
 * read, and reads the list again when it did or is no longer open to them.
 * A member's screen in view keeps a socket open to the server, which says
 * when another person's page change or sharing change came, read then within
 * about a second (Notion's: 0.6 to 1 second, seen 2026-10-15); meanwhile the
 * ten-second check rests. The socket also says when a notice came for the
 * person, whose inbox then reads it at once, whatever page is open, when the
 * workspace's People changed, which an open People list then reads again,
 * when their sidebar layout changed on another screen or device or by an
 * agent, which the sidebar then reads again (sidebar/useSidebarLayout.ts),
 * as it does when the screen comes back into view, and when a workspace of
 * theirs was renamed or given another icon, whose name the sidebar head and
 * workspace menu then show, as Notion's do within seconds, and when a page
 * whose content the sidebar draws changed (a database view section's rows,
 * lib/sidebarGraphFollow.ts), which it is told of as the socket opens and as
 * they change, and which is then read again; a guest's says those,
 * and that its own pages changed — a page shared with the guest or taken
 * back, within about a second (workspace_change_socket.go), the rest as its
 * ten-second check would. It is a socket rather than a waiting request
 * because a browser keeps six requests to a plain-HTTP server, which six
 * tabs' waits used up (measured 2026-10-30). An open page whose connection the server closed
 * asks at once: a lowered or removed access shows within about a second.
 * The check says which page changes the list holds, and takes the pages
 * changed since in place of the whole list read again, when they say what
 * changed (useAuthenticatedBootstrap.ts takePageChanges).
 */
export function useWorkspaceChangeFollow(
  session: Session,
  workspaceId: string | null,
  refreshToken: string | undefined,
  refresh: () => Promise<unknown>,
  follow?: ListFollow,
) {
  const latest = useRef({ refreshToken, refresh, follow });
  latest.current = { refreshToken, refresh, follow };
  // A list read again is the one followed (lib/workspaceListToken.ts).
  useEffect(() => {
    if (!workspaceId) return;
    followListToken(workspaceId, refreshToken);
    return () => unfollowListToken(workspaceId);
  }, [refreshToken, workspaceId]);
  useEffect(() => {
    if (!workspaceId) return;
    let stopped = false;
    let checking = false;
    // Asked while a check goes on: once it ends it asks once more, for what
    // changed after its question went (2026-11-14: a change said while the
    // check waited for this tab's writes, or while it asked, was dropped,
    // and the socket said nothing more).
    let askAgain = false;
    // When the list has to be read whole, a page made elsewhere shows before
    // it: the pages made since the newest one listed are read at once, one
    // read at a time, again after it for what was said meanwhile
    // (useAuthenticatedBootstrap.ts readNewPages); the whole read follows
    // (2026-11-14: on a NAS a 47,000-page workspace's other screen showed a
    // new page 17–25 s later, Notion's within a second).
    let readingNew = false;
    let readNewAgain = false;
    const readNew = async () => {
      if (stopped || document.visibilityState !== "visible" || !latest.current.follow) return;
      if (readingNew) {
        readNewAgain = true;
        return;
      }
      readingNew = true;
      readNewAgain = false;
      try {
        await afterOwnWrites();
        if (!stopped) await latest.current.follow?.readNewPages(workspaceId);
      } catch {
        // The whole read the check makes follows anyway.
      } finally {
        readingNew = false;
        if (readNewAgain && !stopped) void readNew();
      }
    };
    // What the socket would have said while none listened — since the list
    // shown was read (a 47,000-page list's first part, seconds before it is
    // shown), while the screen was hidden, or while a socket was closed — is
    // asked once: what changed, People, the sidebar layout and the workspaces;
    // and, after a socket that listened, the pages the sidebar draws, read
    // again (as the first opens, they are being read anyway).
    let listenedBefore = false;
    const askMissed = () => {
      void check();
      announcePeopleChange(workspaceId);
      readSidebarLayoutAgain(session, workspaceId);
      readWorkspaces();
      if (listenedBefore) announceGraphsChanged(graphsDrawnBeside());
      listenedBefore = true;
    };
    // A workspace renamed or given another icon by its owner shows in the
    // sidebar head and workspace menu without a reload (workspace_look_follow.go).
    const readWorkspaces = () => {
      if (stopped || document.visibilityState !== "visible") return;
      void latest.current.follow?.readWorkspaces().catch(() => undefined);
    };
    // The socket is open, so the ten-second check needn't ask.
    let socket: WebSocket | null = null;
    let listening = false;
    // The socket is told which pages the sidebar draws, as it opens and as they change.
    const tellDrawn = () => {
      if (listening && socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ graphs: graphsDrawnBeside() }));
    };
    let retryDelay = SOCKET_RETRY_FIRST_MS;
    let retryAt = 0;
    const check = async () => {
      // A workspace this tab is deleting isn't asked about (lib/workspaceLeaving.ts).
      if (stopped || document.visibilityState !== "visible" || isLeavingWorkspace(workspaceId)) return;
      if (checking) {
        askAgain = true;
        return;
      }
      checking = true;
      askAgain = false;
      try {
        // Not before this tab's own changes on their way have landed: read
        // sooner, the list would show them undone (lib/ownWritesInFlight.ts);
        // and one landed with the list it now shows answers with that list's
        // token, which finds nothing new (lib/workspaceListToken.ts).
        await afterOwnWrites();
        const token = listToken(workspaceId) ?? latest.current.refreshToken;
        if (stopped || !token) return;
        const since = latest.current.follow?.pageChangeSeq();
        const result = await api.probeWorkspaceChange(session, workspaceId, token, since ? { pagesChangedSince: since } : {});
        if (stopped) return;
        // The pages changed since the list, when the server could say.
        const caughtUp = result.changes && latest.current.follow
          ? await latest.current.follow.takePageChanges(workspaceId, result.changes, result.workspaceRefreshToken)
          : false;
        if (!stopped && result.decision === workspaceChangeDecisions.bootstrapRequired) {
          if (!caughtUp) {
            if (result.reason === workspaceChangeReasons.contentChanged) void readNew();
            await latest.current.refresh();
          }
          // Beyond content, what the person may do may have changed: the open page is read again,
          // and an open People list, whose members are who may.
          if (result.reason !== workspaceChangeReasons.contentChanged) {
            announceAccessChange();
            announcePeopleChange(workspaceId);
          }
        }
      } catch (cause) {
        // A workspace no longer open to the person, such as a guest's last
        // shared page taken back, is read again: what they had open goes.
        // So is a sign-in that ended elsewhere (an administrator's "로그아웃",
        // 2026-11-05: the open screen stayed until reloaded): reading again
        // finds it ended and shows the sign-in screen.
        // Anything else is asked again at the next check.
        if (!stopped && cause instanceof LeafenApiError && (cause.status === 404 || cause.status === 401)) {
          await latest.current.refresh().catch(() => undefined);
          announceAccessChange();
        }
      } finally {
        checking = false;
        if (askAgain && !stopped) void check();
      }
    };
    const listen = () => {
      if (socket || stopped || document.visibilityState !== "visible" || Date.now() < retryAt || isLeavingWorkspace(workspaceId)) return;
      const opened = new WebSocket(serverWebSocketUrl(
        `/api/leafen/v1/workspaces/${encodeURIComponent(workspaceId)}/change-socket?token=${encodeURIComponent(session.token)}`,
      ));
      socket = opened;
      let openedAt = 0;
      opened.onopen = () => {
        openedAt = Date.now();
        listening = true;
        tellDrawn();
        // It says what changes from its first wait on (workspace_change_socket.go).
        askMissed();
      };
      opened.onmessage = (message) => {
        let said: { changed?: boolean; notified?: boolean; layout?: number; people?: boolean; workspaces?: boolean; graphs?: unknown } = {};
        try { said = JSON.parse(String(message.data)); } catch { return; }
        if (Array.isArray(said.graphs)) announceGraphsChanged(said.graphs);
        if (said.notified) announceNoticeArrival(workspaceId);
        if (said.people) announcePeopleChange(workspaceId);
        if (said.workspaces) readWorkspaces();
        if (typeof said.layout === "number") readSidebarLayoutAgain(session, workspaceId, said.layout);
        if (said.changed) void check();
      };
      opened.onclose = () => {
        if (socket !== opened) return;
        socket = null;
        listening = false;
        if (stopped) return;
        // Meanwhile the ten-second check asks, and the socket opens again later.
        const wait = openedAt && Date.now() - openedAt > SOCKET_RETRY_MAX_MS ? SOCKET_RETRY_FIRST_MS : retryDelay;
        retryDelay = Math.min(wait * 2, SOCKET_RETRY_MAX_MS);
        retryAt = Date.now() + wait;
        window.setTimeout(listen, wait);
      };
    };
    const close = () => {
      const closing = socket;
      socket = null;
      listening = false;
      closing?.close();
    };
    // Without an open socket (none yet, or none opens), the screen asks
    // every ten seconds, reads the sidebar layout again and tries the
    // socket again.
    const timer = window.setInterval(() => {
      if (listening) return;
      void check();
      if (document.visibilityState === "visible") readSidebarLayoutAgain(session, workspaceId);
      listen();
    }, WORKSPACE_CHANGE_CHECK_MS);
    let leftView = false;
    const onInView = () => {
      if (document.visibilityState !== "visible") {
        // A hidden screen holds no socket.
        leftView = true;
        close();
        return;
      }
      // Back in view, it asks once what changed meanwhile and waits again,
      // though a window shown and focused says so twice. A window only
      // brought to the front (resized, or moved to from another) stayed in
      // view, where the waiting or ten-second check followed all along.
      if (!leftView) return;
      leftView = false;
      retryAt = 0;
      // A socket still open asks now; one opening asks as it opens.
      if (listening) askMissed();
      listen();
    };
    // A hidden tab asks when it is back in view (above).
    const stopAsked = onWorkspaceChangeCheck(() => void check());
    const stopDrawn = onGraphsDrawnBesideChange(tellDrawn);
    document.addEventListener("visibilitychange", onInView);
    window.addEventListener("focus", onInView);
    listen();
    return () => {
      stopped = true;
      close();
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", onInView);
      window.removeEventListener("focus", onInView);
      stopAsked();
      stopDrawn();
    };
  }, [session, workspaceId]);
}

/** What the change check follows the page list by (useAuthenticatedBootstrap.ts listFollow). */
export type ListFollow = {
  /** The page list change the list holds changes from. */
  pageChangeSeq(): number | undefined;
  /** Reads the pages made since the newest one listed, ahead of a whole read. */
  readNewPages(workspaceId: string): Promise<unknown>;
  /** Takes the pages changed since the list over it, with the workspace's token then; false when the list has to be read whole. */
  takePageChanges(workspaceId: string, changes: Bootstrap, token: string): Promise<boolean>;
  /** Reads the person's workspaces again — their names, icons, places — over the ones listed. */
  readWorkspaces(): Promise<unknown>;
};
