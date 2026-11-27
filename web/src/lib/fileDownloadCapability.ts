import {
  fileTransferBounds,
  type FileDownloadCapability,
} from "../fileTransferWire.generated";
import { serverPath } from "./serverOrigin";

type CapabilityLoader = () => Promise<FileDownloadCapability>;

export type FileDownloadCapabilityResolver = {
  clear(): void;
  resolve(): Promise<FileDownloadCapability>;
};

type FileDownloadCapabilityResolverOptions = {
  now?: () => number;
};

function validCapability(
  capability: FileDownloadCapability,
  now: number,
): boolean {
  return (
    /^\/api\/leafen\/v1\/file-capabilities\/[A-Za-z0-9_-]+$/u.test(capability.url)
    && Number.isSafeInteger(capability.expiresAt)
    && capability.expiresAt > now
  );
}

export function assertValidFileDownloadCapability(
  capability: FileDownloadCapability,
  now: number,
): FileDownloadCapability {
  if (!validCapability(capability, now)) {
    throw new RangeError("File download capability is invalid or already expired.");
  }
  return capability;
}

// The capability as its readers use it: its server-relative URL, checked
// above, at the active server, which a packaged client's page isn't on.
function onActiveServer(capability: FileDownloadCapability): FileDownloadCapability {
  return { ...capability, url: serverPath(capability.url) };
}

export function createFileDownloadCapabilityResolver(
  load: CapabilityLoader,
  options: FileDownloadCapabilityResolverOptions = {},
): FileDownloadCapabilityResolver {
  const now = options.now ?? Date.now;
  const refreshWindowMilliseconds = fileTransferBounds.capabilityRefreshSeconds * 1_000;
  let current: FileDownloadCapability | null = null;
  let sameFlight: Promise<FileDownloadCapability> | null = null;

  const resolve = async () => {
    const timestamp = now();
    if (
      current
      && validCapability(current, timestamp)
      && current.expiresAt - timestamp > refreshWindowMilliseconds
    ) {
      return onActiveServer(current);
    }
    if (sameFlight) return sameFlight;
    sameFlight = load().then((next) => {
      current = assertValidFileDownloadCapability(next, now());
      return onActiveServer(current);
    }).finally(() => {
      sameFlight = null;
    });
    return sameFlight;
  };

  return {
    clear() {
      current = null;
    },
    resolve,
  };
}
