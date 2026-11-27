"use client";

import { useEffect, useRef, useState, type CSSProperties } from "react";
import { useTranslation } from "react-i18next";
import { request } from "@/apiTransport";
import { heldSignIn } from "@/lib/authSessionStore";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { motionSafeScrollBehavior } from "@/lib/motion";
import { afterPageShown } from "@/lib/pageSideWork";
import { openPage } from "@/lib/pageLeft";
import { usePagePresenceSnapshot } from "@/lib/pagePresenceStore";
import { pagePresenceColor } from "@/lib/pagePresenceModel";
import { pagePresenceAwarenessLabel, pagePresenceStatusLabel } from "@/lib/pagePresenceView";
import { relativeAgoLabel } from "@/lib/relativeTime";
import { useMinuteNow } from "@/lib/useMinuteNow";
import type { Session } from "@/types";
import styles from "./PageView.module.css";
import { PUBLIC_CONTENT_CHANGED_EVENT } from "./pageShareEvents";

const MAX_FACES = 3;
const NO_ONE: readonly string[] = [];

type PastViewer = { userId: string; label: string; lastViewedAt: string };

/** What was viewed: a row page through its database's page, an inline database opened alone through its page, or a page. */
function viewedTarget(pageId: string, containerPageId: string | undefined, database: string | undefined) {
  if (containerPageId) return { pageId: containerPageId, rowId: pageId };
  return database ? { pageId, containerId: database } : { pageId };
}

/**
 * Others who viewed the page and may still edit it, newest first: before this
 * visit, and those seen leaving it since, as Notion's (seen 2026-11-12: a
 * guest and a limited member who went to another page stayed in the bar 2½
 * minutes on, their cards "마지막으로 본 시간: 1분 전"; one who could only read
 * went within 8 seconds). Someone whose page went behind another tab is still
 * here, out of sight: they go from the faces until they look again (away);
 * one who goes from there, as closing the tab, is gone for this visit.
 */
function usePastViewers(session: Session, pageId: string, containerPageId: string | undefined, database: string | undefined,
  here: readonly { userId: string; isCurrent: boolean; label: string }[], away: readonly string[]) {
  const [viewers, setViewers] = useState<{ list: PastViewer[]; askedAt: number }>({ list: [], askedAt: 0 });
  const [readings, setReadings] = useState(0);
  const before = useRef(here);
  // Those seen leaving, from when: shown at once, then as the server reads
  // them again — one it no longer names, as only reading it, goes.
  const [left, setLeft] = useState<ReadonlyMap<string, PastViewer & { at: number }>>(() => new Map());
  // Those whose page went out of sight and then went (a tab closed) leave the
  // faces for the rest of this visit, as Notion's (seen 2026-11-06 and
  // 2026-11-12: gone within seconds), until they come back.
  const awayBefore = useRef(away);
  const [closed, setClosed] = useState<ReadonlySet<string>>(() => new Set());
  useEffect(() => {
    setViewers({ list: [], askedAt: 0 });
    before.current = [];
    awayBefore.current = [];
    setLeft(new Map());
    setClosed(new Set());
  }, [pageId]);
  // Leaving the page (another page, closing the tab) tells the server when
  // this reader was last here, which those who open it later see (2026-11-06):
  // closing the tab at once, going to another page once that one is drawn and
  // has read what it shows (pageSideWork.ts), signing out before the sign-in
  // ends (pageLeft.ts). A renewed sign-in is the same reader, still here.
  useEffect(() => {
    const page = openPage(session, viewedTarget(pageId, containerPageId, database));
    window.addEventListener("pagehide", page.left);
    return () => { window.removeEventListener("pagehide", page.left); page.close(); afterPageShown(page.left); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [containerPageId, database, pageId, session.user.id]);
  // Who viewed it before is read once it is drawn and has read the rest.
  useEffect(() => {
    let current = true;
    const stop = afterPageShown(() => {
      const askedAt = Date.now(), held = heldSignIn(session);
      if (!held) return;
      void request<{ viewers: { userId: string; displayName: string | null; lastViewedAt: string }[] }>("/page-past-viewers", {
        method: "POST",
        body: JSON.stringify(viewedTarget(pageId, containerPageId, database)),
      }, held).then((answer) => {
        if (current) {
          setViewers({ askedAt, list: answer.viewers.map((viewer) => ({
            userId: viewer.userId, label: viewer.displayName ?? "", lastViewedAt: viewer.lastViewedAt,
          })) });
        }
      }, () => undefined);
    });
    return () => {
      current = false;
      stop();
    };
    // A renewed session is the same reader.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [containerPageId, database, pageId, readings, session.user.id]);
  // Who may still edit it changes with its sharing: a person made a reader
  // here drops out of the faces at once, as in Notion.
  useEffect(() => {
    const reread = () => setReadings((count) => count + 1);
    window.addEventListener(PUBLIC_CONTENT_CHANGED_EVENT, reread);
    return () => window.removeEventListener(PUBLIC_CONTENT_CHANGED_EVENT, reread);
  }, []);
  useEffect(() => {
    const isHere = (userId: string) => here.some((other) => other.userId === userId);
    const gone = before.current.filter((person) => !person.isCurrent && !isHere(person.userId) && !away.includes(person.userId));
    const vanished = awayBefore.current.filter((userId) => !away.includes(userId) && !isHere(userId));
    before.current = here;
    awayBefore.current = away;
    setClosed((current) => {
      const next = new Set([...current, ...vanished].filter((userId) => !isHere(userId)));
      return next.size === current.size && [...next].every((userId) => current.has(userId)) ? current : next;
    });
    if (gone.length === 0) return;
    const at = Date.now();
    setLeft((current) => new Map([...current, ...gone.map((person) => [person.userId,
      { userId: person.userId, label: person.label, lastViewedAt: new Date(at).toISOString(), at }] as const)]));
    setReadings((count) => count + 1);
  }, [away, here]);
  const shown = new Map(viewers.list.map((viewer) => [viewer.userId, viewer]));
  for (const person of left.values()) {
    const known = shown.get(person.userId);
    if (known) {
      if (Date.parse(known.lastViewedAt) < person.at) shown.set(person.userId, { ...known, lastViewedAt: person.lastViewedAt });
    } else if (viewers.askedAt < person.at) shown.set(person.userId, person);
  }
  return [...shown.values()]
    .filter((viewer) => !away.includes(viewer.userId) && !closed.has(viewer.userId) && !here.some((person) => person.userId === viewer.userId))
    .sort((one, other) => Date.parse(other.lastViewedAt) - Date.parse(one.lastViewedAt));
}

function revealPerson(pageId: string, blockId: string | undefined) {
  if (!blockId) return;
  document.querySelector(`[data-editor-page="${CSS.escape(pageId)}"] [data-block-id="${CSS.escape(blockId)}"]`)
    ?.scrollIntoView({ behavior: motionSafeScrollBehavior(), block: "center", inline: "nearest" });
}

/**
 * The faces in a page's bar, as Notion's (seen 2026-11-05): once anyone else
 * is or has been here, this person's own and the others' with their colour
 * round them; someone here who may only read shows no face of their own, but
 * this person's shows for them (seen 2026-11-12). Resting on someone shows their card under a line saying they are
 * here, and pressing goes to where they are; someone who viewed it before it
 * was opened or left it since is dimmed, their card saying when they last
 * viewed the page (usePastViewers). A touch screen's bar has none,
 * as Notion's phone and iPad (seen 2026-11-12 in iPad Safari 834: none in a
 * page's bar with the sidebar shut, nor in a row's page) — the faces beside
 * the lines stay.
 */
export function PagePresence(props: PagePresenceProps) {
  const { touch } = useSidebarLayout(true);
  return touch ? null : <PresenceFaces {...props} />;
}

type PagePresenceProps = {
  pageId: string;
  /** The database a row page is in: its views are read through it. */
  containerPageId?: string;
  /** An inline database opened alone on the page, whose own viewers these are. */
  database?: string;
  session: Session;
};

function PresenceFaces({ pageId, containerPageId, database, session }: PagePresenceProps) {
  const { t } = useTranslation("topBar");
  const { activeCount, awareness, away = NO_ONE, peers, readers = 0, status } = usePagePresenceSnapshot(pageId);
  const past = usePastViewers(session, pageId, containerPageId, database, peers, away);
  // A dimmed face's "마지막으로 본 시간: N분 전" goes on with the clock.
  useMinuteNow();
  const connected = status === "connected";
  const others = connected ? peers.filter((peer) => !peer.isCurrent) : [];
  // Someone who may only read is here unnamed: only this person's own face
  // shows for them, as Notion's (seen 2026-11-12).
  const readersHere = connected && readers > 0;
  const trouble = status === "auth_lost" || status === "kicked" || status === "disconnected";
  const hadOthers = peers.some((peer) => !peer.isCurrent);
  if (!others.length && !readersHere && !past.length && !(trouble && hadOthers)) return null;

  const self = peers.find((peer) => peer.isCurrent) ?? {
    color: pagePresenceColor(session.user.id), userId: session.user.id, label: session.user.name || session.user.email,
  };
  const faces = [
    { key: self.userId, userId: self.userId, label: self.label, color: undefined, kind: "self" as const, note: undefined, at: undefined },
    ...others.map((peer) => ({
      key: peer.userId, userId: peer.userId, label: peer.label, color: peer.color, kind: "here" as const,
      note: t("presenceHere"), at: undefined,
    })),
    ...past.map((viewer) => ({
      key: viewer.userId, userId: viewer.userId, label: viewer.label, color: undefined, kind: "past" as const,
      note: t("presenceLastSeen", { time: relativeAgoLabel(viewer.lastViewedAt) }), at: viewer.lastViewedAt,
    })),
  ];
  const visible = faces.slice(0, MAX_FACES);
  const hidden = faces.length - visible.length;
  const label = pagePresenceStatusLabel(status, activeCount, t);
  const activity = pagePresenceAwarenessLabel(awareness, t);

  return (
    <div className={styles.presence} data-status={status} data-variant="topbar" data-topbar-presence="true"
      data-testid="topbar-page-presence" aria-label={activity ? `${label}, ${activity}` : label}>
      <div className={styles.presenceStack}>
        {visible.map((face) => {
          const avatar = <PersonAvatar userId={face.userId} name={face.label} size={22} />;
          const look = {
            className: styles.presenceFace, "data-person-card": face.userId, "data-person-card-note": face.note,
            "data-presence-kind": face.kind, style: { "--presence-color": face.color } as CSSProperties,
          };
          return face.kind === "here" ? (
            <button key={face.key} type="button" {...look} aria-label={`${face.label}: ${face.note}`}
              onClick={() => revealPerson(pageId, awareness.find((person) => person.userId === face.userId)?.blockId)}>
              {avatar}
            </button>
          ) : <span key={face.key} {...look}>{avatar}</span>;
        })}
        {hidden > 0 && <span className={`${styles.presenceAvatar} ${styles.presenceMore}`}>+{hidden}</span>}
      </div>
      {trouble && <span className={styles.presenceStatus} aria-hidden="true">{label}</span>}
    </div>
  );
}
