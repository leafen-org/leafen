// Restoring a lost server at a new one's first start (server: setup_restore.go):
// the place its backups are in, what that place holds, and the answer to a
// restore asked for.

import type { AdministratorBackupProgress } from "@/administratorWire.generated";

export type SetupRestorePlaceInput = {
  setupCode?: string;
  target: "folder" | "webdav" | "s3";
  folder?: string;
  webdavUrl?: string;
  webdavUsername?: string;
  webdavPassword?: string;
  s3Endpoint?: string;
  s3Region?: string;
  s3Bucket?: string;
  s3Prefix?: string;
  s3AccessKey?: string;
  s3SecretKey?: string;
  s3PathStyle?: boolean;
  /** What opens sealed backups: their recovery code or password. */
  code?: string;
  password?: string;
};

export type SetupRestoreSnapshot = {
  name: string;
  createdAt: string;
  bytes: number;
  files: number;
  sealed: boolean;
  readable: boolean;
};

export type SetupRestorePlace = {
  replica: { present: boolean; owner: string; ownerOrigin: string; lastChangeAt: string; sealed: boolean; readable: boolean };
  snapshots: SetupRestoreSnapshot[];
};

/**
 * signIn: the pass this browser signs in with once, as the restored server's owner, when it starts again.
 * watch and progress: a restore still being put together, followed by its watch (server: setup_restore_apart.go).
 */
export type SetupRestoreResult = { restart: "automatic" | "manual"; signIn?: string; watch?: string; progress?: AdministratorBackupProgress | null };

/** How far a restore followed by its watch has got: its progress, done once it waits for the restart, or why it stopped. */
export type SetupRestoreProgress = {
  progress: AdministratorBackupProgress | null;
  done: boolean;
  failure?: { code: string; message: string; reason?: string; cause?: string };
};

const SIGN_IN_KEY = "leafen:setup-restore-sign-in";

/** Keeps the restore's sign-in pass in this tab until the restored server starts. */
export function keepSetupRestoreSignIn(pass: string | undefined) {
  try {
    if (pass) window.sessionStorage.setItem(SIGN_IN_KEY, pass);
  } catch {
    // Without it the browser signs in from a device, as any other does.
  }
}

/** The kept pass, taken: it is good once. */
export function takeSetupRestoreSignIn(): string {
  try {
    const pass = window.sessionStorage.getItem(SIGN_IN_KEY) ?? "";
    window.sessionStorage.removeItem(SIGN_IN_KEY);
    return pass;
  } catch {
    return "";
  }
}

/** What to restore: the place's replica, or a snapshot by its name. */
export type SetupRestoreChoice = "replica" | string;

/**
 * A restore left to an agent (server: setup_restore_agent.go): the code the
 * agent restores with, the pass this browser signs in with afterwards, and
 * what the screen follows meanwhile. Never the secrets: the person types
 * them here and the screen sends them once.
 */
export type SetupRestoreAgentBegun = { code: string; signIn: string; expiresAt: number };

export type SetupRestoreAgentOutside = "" | "quick" | "tailscale";

export type SetupRestoreAgentSecrets = { webdavPassword?: string; s3AccessKey?: string; s3SecretKey?: string; secret?: string };

export type SetupRestoreAgentState = {
  active: boolean;
  /** Why the last code ended: "expired", "set-up", "closed" or "restored". */
  ended?: string;
  expiresAt?: number;
  outside?: {
    mode: SetupRestoreAgentOutside;
    state: "off" | "starting" | "on" | "retrying";
    address?: string;
    failure?: string;
    tailscaleLogin?: string;
    tailscaleAllow?: string;
    tailscalePublishing?: boolean;
  };
  place?: { target: SetupRestorePlaceInput["target"]; folder?: string; webdavUrl?: string; webdavUsername?: string;
    s3Endpoint?: string; s3Bucket?: string; s3Region?: string; s3Prefix?: string };
  found?: { replica: boolean; snapshots: number; sealed: boolean };
  typed: { webdavPassword: boolean; s3AccessKey: boolean; s3SecretKey: boolean; secret: boolean };
  agentSeen?: boolean;
  running?: boolean;
  /** How far the agent's restore has got, while it goes. */
  progress?: AdministratorBackupProgress | null;
  failure?: string;
  restart?: "automatic" | "manual" | "";
  /** The address agents anywhere reach this server at already. */
  publicAddress?: string;
};

/** Where the agent's restore tools answer, on whichever address the agent reaches. */
export const SETUP_RESTORE_MCP_PATH = "/api/leafen/v1/setup/restore/mcp";
export const SETUP_RESTORE_API_PATH = "/api/leafen/v1/setup/restore/agent";
