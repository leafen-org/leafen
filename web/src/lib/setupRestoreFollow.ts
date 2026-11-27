// Following a first-setup restore still being put together after its
// request was answered (server: setup_restore_apart.go): its progress, until
// it waits for the server to start again or stops with why.
import { api } from "@/api";
import { LeafenApiError } from "@/apiTransport";
import type { AdministratorBackupProgress } from "@/administratorWire.generated";
import type { SetupRestoreProgress } from "./setupRestore";

const FOLLOW_MS = 1_500;
// A server that no longer answers its watch has started again on the
// restore: its new start knows no watch, or doesn't answer for a moment.
const GONE_AFTER = 4;

export async function followSetupRestore(watch: string, onProgress: (progress: AdministratorBackupProgress | null) => void):
  Promise<Pick<SetupRestoreProgress, "failure">> {
  let missed = 0;
  for (;;) {
    await new Promise((resolve) => window.setTimeout(resolve, FOLLOW_MS));
    try {
      const answer = await api.setupRestoreProgress(watch);
      missed = 0;
      if (answer.failure) return { failure: answer.failure };
      if (answer.done) return {};
      if (answer.progress) onProgress(answer.progress);
    } catch (cause) {
      if (cause instanceof LeafenApiError && cause.status === 404) return {};
      missed += 1;
      if (missed >= GONE_AFTER) return {};
    }
  }
}
