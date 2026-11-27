// An account made a moment ago on this screen (the first administrator's
// setup, a name-only invitation) has no language yet: its account steps open
// on the language at once instead of reading the account first, which drew a
// blank moment between the name and the language (2026-11-06). One made with
// the language chosen on the first screen (lib/firstRunLanguage.ts) takes it
// without asking.
const fresh = new Map<string, string>();

/** Notes that this account was made just now, here, with the language chosen before it, if any. */
export function markFreshAccount(userId: string, language = ""): void {
  fresh.set(userId, language);
}

/**
 * Whether this account was made just now, here — the language chosen before
 * it, or "" — or null when it wasn't; asked once.
 */
export function takeFreshAccount(userId: string): string | null {
  const language = fresh.get(userId);
  fresh.delete(userId);
  return language ?? null;
}
