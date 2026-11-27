import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { api } from "../../api";
import { LeafenApiError } from "../../apiTransport";
import { workspaceImportErrorCodes } from "../../workspaceImportWire.generated";
import type { BundleSource } from "../../bundleSourcesApi";
import { newId } from "../../lib/ids";
import { LocalizedError, userErrorText } from "../../lib/settingsErrors";
import { localWorkspaceArchiveImport } from "../../lib/localWorkspaceArchiveImport";
import { nativeArchiveSourceId } from "../../lib/nativeArchiveV2";
import type { NativeNotionMigration } from "../../notionImportWire.generated";
import type { Session } from "../../types";
import { createWorkspaceDeletionDriver } from "../settings/workspaceDeletionDriver";
import { announceWorkspaceListChanged } from "@/lib/workspaceListEvents";

// "다른 도구에서 가져오기" (docs/bundle.md): the tool's app hands Leafen a workspace bundle; Leafen,
// as the person, puts it in a new workspace of its own for the migration —
// the source's earlier one deleted first, as its IDs are the next bundle's —
// and moves it with the migration's plan, run and report. The source is the
// tool's and the bundle's own (its descriptor's source id): the next bundle
// of the same vault or space carries on its migration, another vault starts
// its own. When the run is done, the workspace that held the bundle goes.

const deletion = createWorkspaceDeletionDriver({ deleteWorkspace: api.deleteWorkspace });
const jobPollMs = 1000;
const clearingMs = 120_000;

/**
 * Moves a source to its new place once the place its last bundle had is gone:
 * the run before deletes it as it ends, which takes a while when the server
 * is busy, and the move is refused until then (2026-11-05: the next bundle,
 * handed over right after a run under heavy load, failed).
 */
async function moveWhenCleared<T>(move: () => Promise<T>) {
  const started = Date.now();
  for (let delay = 1000; ; delay = Math.min(delay * 2, 5000)) {
    try {
      return await move();
    } catch (caught) {
      if (!(caught instanceof LeafenApiError) || caught.code !== workspaceImportErrorCodes.stateInvalid
        || Date.now() - started > clearingMs) throw caught;
      await new Promise((resolve) => setTimeout(resolve, delay));
    }
  }
}

/** Deletes the workspace a bundle went into, then has the workspace list read again. */
async function removeBundlePlace(session: Session, id: string, name: string) {
  if (!id || !name) return;
  try {
    await deletion.run({
      session, sessionIdentity: session.user.id, workspaceId: id, confirmationName: name,
      isCurrent: () => true, onProgress: () => undefined,
    });
  } finally {
    announceWorkspaceListChanged();
  }
}

/**
 * The places the person's earlier bundles of these tools went into that
 * nothing reads again go, as a run's end takes its place away: a run that
 * ended done while no window watched it, or a bundle whose move never ran,
 * its window closed first — the next bundle of the tool starts the move again
 * (2026-11-07: the workspace menu kept "Obsidian에서 옮기는 중" from a move left
 * on 11-05, and choosing it failed). A place stays while its bundle is going
 * in, while its migration scans or runs, while an agent has its plan, and
 * after its run stopped, which goes on from it in the import history.
 */
export async function clearLeftBundlePlaces(session: Session, workspaceId: string, tools: ReadonlySet<string>, signal?: AbortSignal) {
  if (!tools.size) return;
  const [sources, migrations] = await Promise.all([
    api.bundleSources(session, workspaceId, "", signal),
    api.notionMigrations(session, workspaceId, { signal }),
  ]);
  const moves = new Map<string, NativeNotionMigration[]>();
  for (const migration of migrations.items) moves.set(migration.connectionId, [...moves.get(migration.connectionId) ?? [], migration]);
  const reading = (migration: NativeNotionMigration) => migration.status === "scanning" || migration.runState === "running"
    || migration.runState === "failed" || (migration.runState === "" && migration.agentHandoff);
  for (const source of sources.items) {
    if (signal?.aborted) return;
    if (!source.tool || !tools.has(source.tool) || !source.stagingWorkspaceName) continue;
    const runs = moves.get(source.connectionId) ?? [];
    const left = runs.length ? !runs.some(reading) : source.status === "ready" || source.status === "failed";
    if (left) await removeBundlePlace(session, source.stagingWorkspaceId, source.stagingWorkspaceName).catch(() => undefined);
  }
}

export type ToolImportPhase =
  | { kind: "app" }
  | { kind: "staging"; step: "place" | "upload" | "import"; percent: number }
  | { kind: "migrate"; source: BundleSource; earlier: NativeNotionMigration | null }
  | { kind: "failed"; message: string };

export function useToolImport({ session, workspaceId, label, tool }: {
  session: Session;
  workspaceId: string;
  /** The tool's name the source keeps, the same in every language. */
  label: string;
  /** The app's tool, "<app id>:<key>", the sources are found by. */
  tool: string;
}) {
  const { t } = useTranslation("importDialog");
  const [phase, setPhase] = useState<ToolImportPhase>({ kind: "app" });
  // The sources the person moved from this tool into the workspace before.
  const [earlier, setEarlier] = useState<BundleSource[] | undefined>(undefined);
  // That read failing is said with a way to read again: the app is not told
  // there were none (it would move them anew beside the earlier copy).
  const [earlierFailed, setEarlierFailed] = useState(false);
  const [earlierTurn, setEarlierTurn] = useState(0);
  const live = useRef(true);
  useEffect(() => {
    live.current = true;
    const controller = new AbortController();
    setEarlierFailed(false);
    api.bundleSources(session, workspaceId, tool, controller.signal)
      .then((list) => setEarlier(list.items))
      .catch(() => { if (!controller.signal.aborted) setEarlierFailed(true); });
    return () => { live.current = false; controller.abort(); };
  }, [earlierTurn, session, tool, workspaceId]);
  const readEarlierAgain = useCallback(() => setEarlierTurn((turn) => turn + 1), []);

  const removeWorkspace = useCallback((id: string, name: string) => removeBundlePlace(session, id, name), [session]);

  /** Takes the app's bundle: a place for it, the source, the bundle in, then the migration. */
  const handOff = useCallback(async (bundle: Blob) => {
    if (phase.kind !== "app") throw new Error("busy: Leafen is already taking a bundle.");
    // A workspace made for the bundle that no source holds yet goes if the hand-off fails.
    let unbound: { id: string; name: string } | null = null;
    try {
      setPhase({ kind: "staging", step: "place", percent: 0 });
      const file = new File([bundle], `${label}.leafen.zip`, { type: "application/vnd.leafen.archive+zip" });
      const origin = await nativeArchiveSourceId(file).catch((caught: unknown) => {
        throw new LocalizedError(caught instanceof Error ? t("tools.bundleRefusedWhy", { detail: caught.message.replace(/\.$/u, "") }) : t("tools.bundleRefused"));
      });
      let known = earlier?.find((item) => item.origin === origin) ?? null;
      if (known) {
        known = await api.bundleSource(session, workspaceId, known.connectionId);
        if (known.stagingWorkspaceName) await removeWorkspace(known.stagingWorkspaceId, known.stagingWorkspaceName);
      }
      const name = t("tools.stagingName", { name: label });
      const place = await api.createWorkspace(session, { requestId: newId(), name, skipDefaultPages: true });
      unbound = { id: place.id, name };
      const source = known
        ? await moveWhenCleared(() => api.moveBundleSource(session, workspaceId, known.connectionId, place.id))
        : await api.createBundleSource(session, workspaceId, { requestId: newId(), stagingWorkspaceId: place.id, label, tool, origin });
      unbound = null;
      setPhase({ kind: "staging", step: "upload", percent: 0 });
      let job = await localWorkspaceArchiveImport.run({
        file, requestId: newId(), session, workspaceId: place.id,
        onProgress: (progress) => {
          if (live.current) setPhase({ kind: "staging", step: "upload", percent: (progress.receivedBytes / Math.max(progress.archiveBytes, 1)) * 100 });
        },
      });
      while (job.status !== "completed") {
        // Leafen says where and what the bundle got wrong, for the app that made it.
        if (job.status === "failed" || job.status === "cancelled") {
          throw new Error(job.errorDetail ? t("tools.bundleRefusedWhy", { detail: job.errorDetail }) : t("tools.bundleRefused"));
        }
        if (live.current) setPhase({ kind: "staging", step: "import", percent: (job.processedItems / Math.max(job.totalItems, 1)) * 100 });
        await new Promise((resolve) => setTimeout(resolve, jobPollMs));
        job = await api.workspaceImportJob(session, place.id, job.id);
      }
      // A source moved before goes on with the migration it had.
      let before: NativeNotionMigration | null = null;
      if (known) {
        const migrations = await api.notionMigrations(session, workspaceId);
        before = migrations.items.find((item) => item.connectionId === source.connectionId) ?? null;
      }
      if (live.current) setPhase({ kind: "migrate", source: { ...source, stagingWorkspaceName: name }, earlier: before });
    } catch (caught) {
      if (unbound) await removeWorkspace(unbound.id, unbound.name).catch(() => undefined);
      if (live.current) setPhase({ kind: "failed", message: userErrorText(caught, t("tools.bundleRefused")) });
      throw caught;
    }
  }, [earlier, label, phase.kind, removeWorkspace, session, t, tool, workspaceId]);

  /** The run is done: the workspace that held the bundle goes. */
  const finish = useCallback(async () => {
    if (phase.kind !== "migrate") return;
    await removeWorkspace(phase.source.stagingWorkspaceId, phase.source.stagingWorkspaceName ?? "").catch(() => undefined);
  }, [phase, removeWorkspace]);

  const origins = (earlier ?? []).flatMap((item) => item.origin ? [item.origin] : []);
  return { phase, earlier: origins, ready: earlier !== undefined, earlierFailed, readEarlierAgain, handOff, finish };
}
