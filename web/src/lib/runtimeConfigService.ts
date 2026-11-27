import { api } from "@/api";
import { outsideAddress } from "./addressReach";
import { isServerUnreachable } from "./serverReach";
import { DEFAULT_LEGAL_LINKS, type LegalLinks } from "./legalLinks";
import { DISABLED_MAP_TILES, normalizeMapConfig } from "./mapTileConfig";
import { serverOrigin, serverScopedName, setEarlierAddresses, setPublishedAddress } from "./serverOrigin";
import { normalizeSponsorCredits } from "./sponsorCredit";
import {
  runtimeEmailRecoveryAvailable,
  type PublicRuntimeConfig,
  type RuntimeConfigTransport,
} from "./runtimeConfigContract";
import {
  productErrorReceiptPresentation,
  type ProductErrorPresentation,
} from "./productErrorPresentation";

export type RuntimeConfigLoadResult = {
  config: PublicRuntimeConfig;
  degradation: ProductErrorPresentation | null;
  /** The server didn't answer: the config is the last one read, or the safe defaults. */
  unreachable?: boolean;
};

// The last config read is kept, so a start while the server can't be
// reached keeps the server's settings rather than the safe defaults
// (2026-10-16: an offline reload showed a failure about them instead).
// A packaged app keeps each server's apart.
const RUNTIME_CONFIG_KEPT_KEY = "leafen:runtime-config";

function keptRuntimeConfig(): PublicRuntimeConfig | null {
  try {
    const raw = window.localStorage.getItem(serverScopedName(RUNTIME_CONFIG_KEPT_KEY));
    return raw ? normalizeRuntimeConfig(JSON.parse(raw)) : null;
  } catch {
    return null;
  }
}

function keepRuntimeConfig(config: PublicRuntimeConfig) {
  try {
    window.localStorage.setItem(serverScopedName(RUNTIME_CONFIG_KEPT_KEY), JSON.stringify(config));
  } catch {
    // A browser that keeps nothing reads the config again next time.
  }
}

function normalizeLegalLinks(value: unknown): LegalLinks {
  const record = value && typeof value === "object" ? value as Record<string, unknown> : {};
  const safeUrl = (candidate: unknown, fallback: string) => {
    if (typeof candidate !== "string") return fallback;
    try {
      const url = new URL(candidate);
      // Only an https address anyone opens, not this computer's or its
      // network's, as the one table judges it (lib/addressReach.ts).
      if (!outsideAddress(url.toString()) || url.username || url.password) return fallback;
      return url.toString();
    } catch {
      return fallback;
    }
  };
  return {
    sourceUrl: safeUrl(record.sourceUrl, DEFAULT_LEGAL_LINKS.sourceUrl),
  };
}

function fallbackRuntimeConfig(): PublicRuntimeConfig {
  return {
    allowAnonymousBootstrap: false,
    oauthProviders: [],
    notionOAuthConfigured: false,
    signupClosed: false,
    signupContact: "",
    serverName: "",
    serverAlias: "",
    emailRecoveryAvailable: false,
    appHostname: "",
    publicAddress: "",
    appPlatform: false,
    appWorkers: "none",
    appPlatformMissing: "",
    legal: DEFAULT_LEGAL_LINKS,
    map: DISABLED_MAP_TILES,
    sponsorCredits: [],
    unsplashCoverEnabled: false,
    placeSearchEnabled: false,
  };
}

function normalizeRuntimeConfig(value: unknown): PublicRuntimeConfig {
  const record = value && typeof value === "object" ? value as Record<string, unknown> : {};
  return {
    allowAnonymousBootstrap: record.allowAnonymousBootstrap === true,
    oauthProviders: Array.isArray(record.oauthProviders)
      ? Array.from(new Set(record.oauthProviders
          .filter((provider): provider is string => typeof provider === "string")
          .map((provider) => provider.trim())
          .filter((provider) => /^[a-z][a-z0-9_-]{0,31}$/.test(provider))))
      : [],
    notionOAuthConfigured: record.notionOAuthConfigured === true,
    // Where sign-up is closed, and whom its administrator names to ask.
    signupClosed: record.signupClosed === true,
    signupContact: typeof record.signupContact === "string" ? record.signupContact.slice(0, 300) : "",
    // The server's name and alias, which the app shows where it meets more than one server.
    serverName: typeof record.serverName === "string" ? record.serverName.slice(0, 40) : "",
    serverAlias: typeof record.serverAlias === "string" && /^[a-z]{1,30}$/u.test(record.serverAlias) ? record.serverAlias : "",
    // Its code, which names Leafen's entry in an agent's settings with its name (lib/agentEntryNames.ts).
    serverCode: typeof record.serverCode === "string" && /^[a-z]{3}$/u.test(record.serverCode) ? record.serverCode : "",
    unsplashCoverEnabled: record.unsplashCoverEnabled === true,
    placeSearchEnabled: record.placeSearchEnabled === true,
    emailRecoveryAvailable: runtimeEmailRecoveryAvailable(record),
    // An organization signs in with SAML by a verified domain (CompanySignIn.tsx).
    companySignIn: record.companySignIn === true,
    appHostname: typeof record.appHostname === "string"
      ? record.appHostname.trim().toLowerCase()
      : "",
    // The server's public address — written, or a running tunnel's.
    publicAddress: typeof record.publicAddress === "string" && /^https?:\/\/[^/\s]+$/u.test(record.publicAddress)
      ? record.publicAddress : "",
    // Earlier addresses that forward to this server now (2026-10-20).
    earlierAddresses: Array.isArray(record.earlierAddresses)
      ? record.earlierAddresses.filter((address): address is string => typeof address === "string" && /^https?:\/\/[^/\s]+$/u.test(address)).slice(0, 16)
      : [],
    appPlatform: record.appPlatform === true,
    appWorkers: record.appWorkers === "process" ? record.appWorkers : "none",
    // Why a build has no app platform, which its screens say where apps would be.
    appPlatformMissing: record.appPlatformMissing === "phone" || record.appPlatformMissing === "store" ? record.appPlatformMissing : "",
    // A server whose data moved to another one, and where it went.
    serverMoved: record.serverMoved === true,
    movedTo: typeof record.movedTo === "string" && /^https?:\/\/[^/\s]+$/u.test(record.movedTo) ? record.movedTo : "",
    // A paired primary that can't reach its backup place, and a standby's primary.
    serverPaused: record.serverPaused === true,
    standbyOf: typeof record.standbyOf === "string" ? record.standbyOf.slice(0, 300) : "",
    // The other server of its standby pair, where a client looks once it stops answering.
    pairAddress: typeof record.pairAddress === "string" && /^https?:\/\/[^/\s]+$/u.test(record.pairAddress) ? record.pairAddress : "",
    legal: normalizeLegalLinks(record.legal),
    map: normalizeMapConfig(record.map),
    sponsorCredits: normalizeSponsorCredits(record.sponsorCredits),
  };
}

export const apiRuntimeConfigTransport: RuntimeConfigTransport = () => api.runtimeConfig();

export function createRuntimeConfigLoader(transport: RuntimeConfigTransport, server: () => string = serverOrigin) {
  let runtimeConfigPromise: Promise<PublicRuntimeConfig> | null = null;
  let readFrom = "";
  let askedAt = 0;
  const load = (): Promise<PublicRuntimeConfig> => {
    // The settings are the active server's: a packaged app picks its server
    // after its page starts, and settings read before (the phone's own
    // engine's, 2026-10-26: sign-up open on a server that had closed it) or
    // from the server it left are read again from this one.
    const from = server();
    if (runtimeConfigPromise && readFrom !== from) runtimeConfigPromise = null;
    if (!runtimeConfigPromise) {
      readFrom = from;
      askedAt = Date.now();
      runtimeConfigPromise = transport()
        .then(normalizeRuntimeConfig)
        .then((config) => {
          if (server() !== from) return load();
          setPublishedAddress(config.publicAddress ?? "");
          setEarlierAddresses(config.earlierAddresses ?? []);
          keepRuntimeConfig(config);
          return config;
        })
        .catch((error) => {
          if (runtimeConfigPromise === pending) runtimeConfigPromise = null;
          throw error;
        });
      const pending = runtimeConfigPromise;
    }
    return runtimeConfigPromise;
  };
  // `refresh` reads them anew; `{ within }` anew unless they were asked less
  // than that many ms ago, as a start's own read is (2026-11-13: the server
  // pair's first look read them again 300 ms after the start had).
  return async (refresh: boolean | { within: number } = false) => {
    if (refresh === true || (typeof refresh === "object" && Date.now() - askedAt >= refresh.within)) runtimeConfigPromise = null;
    try {
      return { config: await load(), degradation: null } satisfies RuntimeConfigLoadResult;
    } catch (error) {
      // An unreachable server is an ordinary state, not a failure to show:
      // the settings last read stand in and are read again once it answers.
      if (isServerUnreachable(error)) {
        const kept = keptRuntimeConfig();
        if (kept) {
          setPublishedAddress(kept.publicAddress ?? "");
          setEarlierAddresses(kept.earlierAddresses ?? []);
        }
        return { config: kept ?? fallbackRuntimeConfig(), degradation: null, unreachable: true } satisfies RuntimeConfigLoadResult;
      }
      return {
        config: fallbackRuntimeConfig(),
        degradation: productErrorReceiptPresentation(error, "runtimeConfig.read"),
      } satisfies RuntimeConfigLoadResult;
    }
  };
}

export const fetchRuntimeConfig = createRuntimeConfigLoader(apiRuntimeConfigTransport);

const runtimeConfigListeners = new Set<(config: PublicRuntimeConfig) => void>();
let refreshGeneration = 0;
export function subscribeRuntimeConfig(listener: (config: PublicRuntimeConfig) => void) {
  runtimeConfigListeners.add(listener);
  return () => { runtimeConfigListeners.delete(listener); };
}
export async function refreshRuntimeConfig() {
  const generation = ++refreshGeneration;
  const result = await fetchRuntimeConfig(true);
  if (generation === refreshGeneration) {
    for (const listener of runtimeConfigListeners) listener(result.config);
  }
  return result;
}
