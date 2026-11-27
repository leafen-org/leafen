// What the startup recovery screen asks of the desktop or phone app it runs
// in (desktop/startup_recovery.go): the report it copies or saves, the data
// folder it opens, and the last backup it restores while the app's own
// server is off.

async function call<T>(name: string, ...args: unknown[]): Promise<T> {
  const { Call } = await import("@wailsio/runtime");
  return await Call.ByName(`main.StartupRecovery.${name}`, ...args) as T;
}

/** The app's version and system, its data folder, why its server didn't start, its last backup and its log's end. */
export const startupReport = () => call<string>("Report");

/** Whether this device shows a folder in its file manager (a computer, not a phone). */
export const canOpenDataFolder = () => call<boolean>("CanOpenDataFolder").catch(() => false);

export const openDataFolder = () => call<void>("OpenDataFolder");

/**
 * Restores the server's last backup and starts the server on it: "restored",
 * or "needs-secret" for a sealed backup that wants its recovery code or
 * password. Rejects with why it couldn't be restored.
 */
export const restoreLastBackup = (secret: string) => call<"restored" | "needs-secret">("RestoreLastBackup", secret);
