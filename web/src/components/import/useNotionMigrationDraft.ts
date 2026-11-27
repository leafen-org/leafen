import { useCallback, useEffect, useRef, useState } from "react";

import i18next from "i18next";
import { api } from "../../api";
import { durableJobUnreachable } from "../../lib/durableJobPolling";
import { userErrorText } from "../../lib/settingsErrors";
import { newId } from "../../lib/ids";
import type {
  NativeNotionImportJob,
  NativeNotionImportRoot,
  NativeNotionMigration,
  NativeNotionMigrationDatabase,
  NativeNotionMigrationIssue,
  NativeNotionMigrationPlanState,
  NativeNotionMigrationSummary,
} from "../../notionImportWire.generated";
import type { Session } from "../../types";

// "직접 가져오기" as a migration with the "as is" draft (design-first Notion
// migration decision, 3절 원칙 1): the chosen root is scanned a step at a
// time, the scan's draft plan is shown, and approving it saves the draft
// with where it goes and runs it on the import engine. A migration going on
// is picked up again when the screen opens. It follows the plan as it changes,
// as an agent connected to the same migration may change it.

const pollMs = 2000;
// While a stage goes, it is read about once a second, for the names passing
// (NotionMoveLive.tsx); the server notes them in memory, so a read costs little.
const livePollMs = 1000;
// How often the screen sets its own scan going again, which goes on on the
// server between (notion_migration_scan_on.go).
const scanKickMs = 20_000;
// How often the agent's way looks whether the agent has started its migration.
const watchMs = 5000;

/** A read that follows the migration: its state, the finished scan's summary, the plan, the run's import. */
export type NotionMigrationRead = "migration" | "summary" | "plan" | "job";

export type NotionMigrationDraftState = {
  migration: NativeNotionMigration | null;
  /** The finished scan's databases and issues, read a window at a time. */
  summary: NativeNotionMigrationSummary | null;
  /** The migration the summary is of. */
  summaryOf: string;
  plan: NativeNotionMigrationPlanState | null;
  job: NativeNotionImportJob | null;
  busy: boolean;
  error: string;
  /**
   * When each read went unanswered, until that read answers: another read's
   * answer doesn't clear it (2026-11-23: the summary alone refused, the plan
   * read that answered cleared its line 2–3 ms after, and the window waited
   * with no "계획 보기" and no reason). A state, not a failure.
   */
  lostReads: Partial<Record<NotionMigrationRead, number>>;
  /** When a read last answered. */
  answeredAt: number;
  /** Where it goes, picked in the draft; until then the plan's place. */
  place: string | null;
};

const empty: NotionMigrationDraftState = {
  migration: null, summary: null, summaryOf: "", plan: null, job: null, busy: false, error: "", lostReads: {}, answeredAt: 0, place: null,
};

/** The reads lost while the server answers the others; none while it is away. */
function notionMigrationReadsLost(state: NotionMigrationDraftState) {
  const lost = Object.entries(state.lostReads) as [NotionMigrationRead, number][];
  // Nothing answered since a read went unanswered: the server is away, or busy.
  const reconnecting = lost.some(([, since]) => since >= state.answeredAt);
  return { reconnecting, readsLost: reconnecting ? [] : lost.map(([read]) => read) };
}

/** A finished scan's databases and issues, every window of them. */
async function readNotionMigrationScan(session: Session, workspaceId: string, migration: NativeNotionMigration, signal: AbortSignal) {
  const databases: NativeNotionMigrationDatabase[] = [];
  const issues: NativeNotionMigrationIssue[] = [];
  for (const kind of ["databases", "issues"] as const) {
    let cursor = "";
    do {
      const read = await api.notionMigrationScanItems(session, workspaceId, migration.id, kind, cursor, { signal });
      databases.push(...(read.databases ?? []));
      issues.push(...(read.issues ?? []));
      cursor = read.nextCursor ?? "";
    } while (cursor);
  }
  return { counts: migration.counts, pages: [], databases, issues, people: [] } satisfies NativeNotionMigrationSummary;
}

function notionMigrationGoing(migration: NativeNotionMigration) {
  return migration.status === "scanning"
    || (migration.status === "scanned" && (migration.runState === "running" || migration.runState === ""));
}

export function useNotionMigrationDraft({ active, connectionId, follow = false, watch = false, pick, roots, session, workspaceId, onResume }: {
  active: boolean;
  connectionId: string;
  /** Reads the plan again while it isn't running, as an agent changes it. */
  follow?: boolean;
  /** Looks again now and then, while the tab shows, for a migration an agent starts. */
  watch?: boolean;
  /** Which migration going on this screen picks up; null until it can tell. */
  pick: ((migration: NativeNotionMigration) => boolean) | null;
  roots: readonly NativeNotionImportRoot[];
  session: Session;
  workspaceId: string;
  /** A migration going on was found; the screen shows it. */
  onResume(migration: NativeNotionMigration): void;
}) {
  const [state, setState] = useState<NotionMigrationDraftState>(empty);
  const migrationId = state.migration?.id ?? "";
  const set = useCallback((next: Partial<NotionMigrationDraftState>) => setState((current) => ({ ...current, ...next })), []);
  const fail = useCallback((error: unknown) => {
    set({ busy: false, lostReads: {}, error: userErrorText(error, i18next.t("importDialog:migration.draftFailed")) });
  }, [set]);

  // Pick up a migration going on: as the screen opens, and on the agent's
  // way as the agent starts one.
  const resumeRef = useRef(onResume);
  resumeRef.current = onResume;
  const [looks, setLooks] = useState(0);
  useEffect(() => {
    if (!active || !watch || migrationId) return;
    const timer = window.setInterval(() => {
      if (document.visibilityState === "visible") setLooks((count) => count + 1);
    }, watchMs);
    return () => window.clearInterval(timer);
  }, [active, migrationId, watch]);
  useEffect(() => {
    if (!active || migrationId || !pick) return;
    const controller = new AbortController();
    void (async () => {
      const list = await api.notionMigrations(session, workspaceId, { signal: controller.signal });
      const found = list.items.find((item) => notionMigrationGoing(item) && pick(item));
      if (!found) return;
      // The list leaves the scan's summary out; the draft needs it.
      const current = await api.notionMigration(session, workspaceId, found.id, { signal: controller.signal });
      if (controller.signal.aborted) return;
      set({ migration: current });
      resumeRef.current(current);
    })().catch(() => undefined);
    return () => controller.abort();
  }, [active, looks, migrationId, pick, session, set, workspaceId]);

  /**
   * Starts a migration of the chosen roots, or gives the one going on for
   * them — or the one shown, when no root was chosen.
   */
  const start = useCallback(async (): Promise<NativeNotionMigration | null> => {
    if (!roots.length || !connectionId) return state.migration;
    const rootKey = (id: string) => id.replaceAll("-", "");
    if (state.migration && state.migration.roots.length === roots.length
      && roots.every((root) => state.migration?.roots.some((candidate) => rootKey(candidate.id) === rootKey(root.id)))) return state.migration;
    set({ busy: true, error: "", plan: null, job: null });
    try {
      const migration = await api.createNotionMigration(session, workspaceId, { connectionId, roots: [...roots] });
      set({ migration, summary: null, summaryOf: "", busy: false, place: null, lostReads: {} });
      return migration;
    } catch (error) {
      fail(error);
      return null;
    }
  }, [connectionId, fail, roots, session, set, state.migration, workspaceId]);

  /** Shows a migration run again from the list of what moved before. */
  const show = useCallback((migration: NativeNotionMigration) => {
    set({ migration, summary: null, summaryOf: "", plan: null, job: null, busy: false, error: "", place: null, lostReads: {} });
  }, [set]);

  /** Picks where the draft goes. */
  const choosePlace = useCallback((pageId: string) => set({ place: pageId }), [set]);

  // One rule for every read that follows the migration, whatever its stage —
  // the scan, the finished scan's summary, the plan and the settings reading
  // an agent runs, the run: a read the server didn't answer (away, starting
  // again, busy: durableJobUnreachable, as a copy's) says so and goes again a
  // moment later; only an answer that ends it — the migration gone, no access
  // — fails the screen (2026-11-23: one read lost while the plan was being
  // agreed stopped the window, the live settings reading too, until it was
  // opened again). Each read is said lost until that read answers, whatever
  // the others answer meanwhile. Returns the timer of the read again, if any.
  const [lostRequests, setLostRequests] = useState(0);
  const lost = useCallback((error: unknown, read: NotionMigrationRead) => {
    if (!durableJobUnreachable(error)) {
      fail(error);
      return 0;
    }
    setState((current) => current.lostReads[read] ? current
      : { ...current, lostReads: { ...current.lostReads, [read]: Date.now() } });
    return window.setTimeout(() => setLostRequests((count) => count + 1), pollMs);
  }, [fail]);
  /** A read answered: it is no longer lost, and the server answers. */
  const answered = useCallback((read: NotionMigrationRead, next: Partial<NotionMigrationDraftState>) => {
    setState((current) => {
      const lostReads = { ...current.lostReads };
      delete lostReads[read];
      return { ...current, ...next, lostReads, answeredAt: Date.now() };
    });
  }, []);
  /** Reads the window doesn't make now aren't waited on. */
  const forget = useCallback((reads: readonly NotionMigrationRead[]) => {
    setState((current) => {
      if (!reads.some((read) => current.lostReads[read])) return current;
      const lostReads = { ...current.lostReads };
      for (const read of reads) delete lostReads[read];
      return { ...current, lostReads };
    });
  }, []);

  // The finished scan is read once for the draft and the plan.
  const summaryOf = state.summaryOf;
  const scanned = state.migration?.status === "scanned";
  const scannedMigration = useRef(state.migration);
  scannedMigration.current = state.migration;
  useEffect(() => {
    const current = scannedMigration.current;
    if (!active || !scanned || !current || summaryOf === current.id) {
      forget(["summary"]);
      return;
    }
    const controller = new AbortController();
    let again = 0;
    readNotionMigrationScan(session, workspaceId, current, controller.signal)
      .then((summary) => { if (!controller.signal.aborted) answered("summary", { summary, summaryOf: current.id }); })
      .catch((error) => { if (!controller.signal.aborted) again = lost(error, "summary"); });
    return () => {
      window.clearTimeout(again);
      controller.abort();
    };
  }, [active, answered, forget, lost, lostRequests, migrationId, scanned, session, summaryOf, workspaceId]);

  // Scan step by step, then read the draft; while it runs, follow it. Each
  // answer that changes the migration starts the next turn.
  const migration = state.migration;
  const planRef = useRef(state.plan);
  planRef.current = state.plan;
  const scanKicked = useRef(0);
  useEffect(() => {
    if (!active || !migration) return;
    const controller = new AbortController();
    let stopped = false;
    let again = 0;
    const signal = controller.signal;
    // The read under way, said lost if the server doesn't answer it.
    let reading: NotionMigrationRead = "migration";
    void (async () => {
      try {
        if (migration.status === "scanning") {
          forget(["plan", "job"]);
          // The scan goes on on the server once set going; the agent sets its
          // own, this screen its own now and then. Either way it is followed
          // about once a second, its names passing.
          if (!migration.agentHandoff && Date.now() - scanKicked.current > scanKickMs) {
            scanKicked.current = Date.now();
            void api.scanNotionMigration(session, workspaceId, migration.id).catch(() => { scanKicked.current = 0; });
          }
          await new Promise((resolve) => window.setTimeout(resolve, livePollMs));
          if (stopped) return;
          const next = await api.notionMigration(session, workspaceId, migration.id, { signal });
          if (!stopped) answered("migration", { migration: next, error: "" });
          return;
        }
        if (migration.status === "scanned" && migration.runState === "") {
          forget(["job"]);
          if (!planRef.current || follow) {
            reading = "plan";
            const plan = await api.notionMigrationPlan(session, workspaceId, migration.id, { signal });
            if (stopped) return;
            answered("plan", { plan });
          }
          if (!follow) return;
          // The settings reading an agent runs in Notion is followed as a stage is.
          await new Promise((resolve) => window.setTimeout(resolve, migration.live?.stage === "formats" ? livePollMs : pollMs));
          if (stopped) return;
          reading = "migration";
          const next = await api.notionMigration(session, workspaceId, migration.id, { signal });
          if (!stopped) answered("migration", { migration: next });
          return;
        }
        forget(["plan"]);
        if (!migration.importJobId) return;
        reading = "job";
        const job = await api.notionImportJob(session, workspaceId, migration.importJobId, { signal });
        if (stopped) return;
        answered("job", { job, error: "" });
        if (migration.runState !== "running") return;
        await new Promise((resolve) => window.setTimeout(resolve, livePollMs));
        if (stopped) return;
        reading = "migration";
        const next = await api.notionMigration(session, workspaceId, migration.id, { signal });
        if (!stopped) answered("migration", { migration: next });
      } catch (error) {
        if (!stopped) again = lost(error, reading);
      }
    })();
    return () => {
      stopped = true;
      window.clearTimeout(again);
      controller.abort();
    };
  }, [active, answered, follow, forget, lost, lostRequests, migration, session, workspaceId]);

  /** Saves the plan, or the draft until there is one, with where it goes —
   * the place picked, else the plan's — and runs it: what an agent saved is
   * what runs. */
  const approve = useCallback(async () => {
    const current = state.migration;
    const draft = state.plan?.plan ?? state.plan?.draft;
    if (!current || !draft || !state.plan) return;
    const destinationPageId = state.place ?? state.plan.plan?.destinationPageId ?? "";
    set({ busy: true, error: "" });
    try {
      const saved = await api.saveNotionMigrationPlan(session, workspaceId, current.id, state.plan.revision,
        { ...draft, destinationPageId });
      if (!saved.check?.ok) {
        set({ busy: false, plan: saved, error: saved.check?.problems.map((problem) => problem.code).join(", ") ?? "" });
        return;
      }
      const running = await api.runNotionMigration(session, workspaceId, current.id, newId(), saved.revision);
      set({ busy: false, plan: saved, migration: running });
    } catch (error) {
      fail(error);
    }
  }, [fail, session, set, state.migration, state.place, state.plan, workspaceId]);

  /** Runs a stopped import again from where it stopped. */
  const retry = useCallback(async () => {
    const current = state.migration;
    if (!current?.importJobId) return;
    set({ busy: true, error: "" });
    try {
      const job = await api.retryNotionImportJob(session, workspaceId, current.importJobId);
      const next = await api.notionMigration(session, workspaceId, current.id);
      set({ busy: false, job, migration: next });
    } catch (error) {
      fail(error);
    }
  }, [fail, session, set, state.migration, workspaceId]);

  /** Stops the import before its pages begin to show; the plan read again
   *  says what running it again redoes and how long, before the press. */
  const cancel = useCallback(async () => {
    const current = state.migration;
    if (!current?.importJobId) return;
    set({ busy: true, error: "" });
    try {
      const job = await api.cancelNotionImportJob(session, workspaceId, current.importJobId);
      const next = await api.notionMigration(session, workspaceId, current.id);
      set({ busy: false, job, migration: next });
      void api.notionMigrationPlan(session, workspaceId, current.id).then((plan) => set({ plan }), () => undefined);
    } catch (error) {
      fail(error);
    }
  }, [fail, session, set, state.migration, workspaceId]);

  /** Runs a canceled import's plan again, on from what it had made. */
  const runAgain = useCallback(async () => {
    const current = state.migration;
    if (!current) return;
    set({ busy: true, error: "" });
    try {
      const plan = await api.notionMigrationPlan(session, workspaceId, current.id);
      const running = await api.runNotionMigration(session, workspaceId, current.id, newId(), plan.revision);
      set({ busy: false, job: null, plan, migration: running });
    } catch (error) {
      fail(error);
    }
  }, [fail, session, set, state.migration, workspaceId]);

  const reset = useCallback(() => setState(empty), []);

  return { ...state, ...notionMigrationReadsLost(state), start, show, choosePlace, approve, retry, cancel, runAgain, reset };
}

export type NotionMigrationDraft = ReturnType<typeof useNotionMigrationDraft>;
