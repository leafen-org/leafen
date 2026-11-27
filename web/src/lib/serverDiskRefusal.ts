import { i18next } from "@/i18n";
import { fileTransferErrorCodes, serverDiskRefusals, type ServerDiskRefusal } from "@/fileTransferWire.generated";
import { productErrorText } from "@/lib/productErrorText";

/**
 * A write the server refused for its disk, said as the server classed it
 * for whoever asked (internal/app/data_disk_refusal.go, 2026-11-27: a
 * hosting platform's 0.5 GB trial volume read "almost full, ask the
 * server's administrator" to the administrator who had just set it up): the
 * disk too full or too small for the write, and what to do — to an
 * administrator of the server, what to do there (on Railway, grow its
 * volume); to anyone else, whom to ask. A refusal that names no cause (a
 * server from before, a job's stored code) reads as a full disk to anyone.
 */
const SAID: Record<ServerDiskRefusal, readonly [cause: string, nextAction: string]> = {
  [serverDiskRefusals.full]: ["meanings.serverDiskAlmostFull", "meanings.askServerAdministratorToFreeSpace"],
  [serverDiskRefusals.small]: ["meanings.serverDiskTooSmall", "meanings.askServerAdministratorForLargerDisk"],
  [serverDiskRefusals.fullAdministrator]: ["meanings.serverDiskAlmostFull", "meanings.freeServerDiskOrMoveDataFolder"],
  [serverDiskRefusals.smallAdministrator]: ["meanings.serverDiskTooSmall", "meanings.moveDataFolderToLargerDisk"],
  [serverDiskRefusals.fullRailway]: ["meanings.railwayVolumeAlmostFull", "meanings.growRailwayVolume"],
  [serverDiskRefusals.smallRailway]: ["meanings.railwayVolumeTooSmall", "meanings.growRailwayVolume"],
};

function refusalCause(error: unknown): ServerDiskRefusal | null {
  const record = error as { code?: unknown; causeKey?: unknown } | null;
  if (record?.code !== fileTransferErrorCodes.serverDiskLow) return null;
  const cause = record.causeKey;
  return typeof cause === "string" && Object.hasOwn(SAID, cause) ? cause as ServerDiskRefusal : serverDiskRefusals.full;
}

/** What the disk refusal was and what to do, or null for any other failure. */
export function serverDiskRefusal(error: unknown): { cause: string; nextAction: string } | null {
  const cause = refusalCause(error);
  if (cause === null) return null;
  const [said, next] = SAID[cause];
  return { cause: productErrorText(said), nextAction: productErrorText(next) };
}

/**
 * The same in one line, where a refusal's reason stands alone (an upload's,
 * a setting's): a full disk, to someone who asks the administrator, in the
 * sentence those places have said it in.
 */
export function serverDiskRefusalText(error: unknown): string | null {
  const cause = refusalCause(error);
  if (cause === null) return null;
  if (cause === serverDiskRefusals.full) return i18next.t("settingsErrors:backend.serverDiskLow");
  const said = serverDiskRefusal(error);
  return said && `${said.cause} ${said.nextAction}`;
}

/** A presentation's meaning with the disk refusal's words, when it is one. */
export function withServerDiskWords<Meaning extends { cause: string; nextAction: string }>(meaning: Meaning, error: unknown): Meaning {
  const said = serverDiskRefusal(error);
  return said ? { ...meaning, ...said } : meaning;
}
