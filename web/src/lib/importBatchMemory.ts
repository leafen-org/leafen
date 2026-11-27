import { serverScopedName } from "./serverOrigin";

// A batch keeps its request and its job until all its files are in, and as
// long as the server keeps finished imports (workspace_import.go): picked
// again after an interruption, the same files go on where they stopped; once
// in, picking them again imports them again, as Notion does. The import
// window reads it to say what a reload or a closed tab left (2026-11-04: a ZIP
// stopped at 381 of 600 by a reload showed nothing of it).
const PREFIX = "leafen:import-batch:";
const BATCH_KEPT_MS = 30 * 24 * 60 * 60 * 1000;

type KeptBatch = {
  run: string;
  at: number;
  workspaceId?: string;
  names?: string[];
  fileCount?: number;
  total?: number;
  jobId?: string;
};

export type KeptImportBatch = {
  key: string;
  names: string[];
  fileCount: number;
  total: number;
  jobId: string;
};

function read(key: string): KeptBatch | null {
  try {
    const kept = JSON.parse(localStorage.getItem(key) ?? "null") as KeptBatch | null;
    return typeof kept?.run === "string" && typeof kept.at === "number" && Date.now() - kept.at < BATCH_KEPT_MS ? kept : null;
  } catch {
    return null;
  }
}

function write(key: string, kept: KeptBatch) {
  try {
    localStorage.setItem(key, JSON.stringify(kept));
  } catch {
    // Kept for this run only.
  }
}

/** Where the batch of these picked files to this place is kept. */
export function importBatchKey(picked: string) {
  return serverScopedName(PREFIX + picked);
}

/** The run a batch's request comes from: the one kept, or a new one. */
export function keepImportBatch(key: string, batch: { workspaceId: string; names: string[]; fileCount: number; total: number }, newRun: () => string): string {
  const kept = read(key);
  const run = kept?.run || newRun();
  write(key, { run, at: Date.now(), ...batch, ...(kept?.jobId ? { jobId: kept.jobId } : {}) });
  return run;
}

/** The server's job for the batch, which goes on with no one there. */
export function keepImportBatchJob(key: string, jobId: string) {
  const kept = read(key);
  if (kept) write(key, { ...kept, jobId });
}

export function finishImportBatch(key: string) {
  try {
    localStorage.removeItem(key);
  } catch {
    // Nothing kept.
  }
}

/** The batches into a workspace this browser hasn't seen all the way in. */
export function keptImportBatches(workspaceId: string): KeptImportBatch[] {
  const found: KeptImportBatch[] = [];
  try {
    for (let index = 0; index < localStorage.length; index += 1) {
      const key = localStorage.key(index);
      if (!key?.startsWith(PREFIX)) continue;
      const picked = key.slice(PREFIX.length).split("@")[0];
      if (importBatchKey(picked) !== key) continue;
      const kept = read(key);
      if (kept?.workspaceId !== workspaceId || !kept.total) continue;
      found.push({
        key, names: kept.names ?? [], fileCount: kept.fileCount ?? 1, total: kept.total,
        jobId: kept.jobId ?? "",
      });
    }
  } catch {
    // Nothing to say without storage.
  }
  return found;
}
