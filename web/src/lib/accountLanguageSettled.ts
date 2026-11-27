// Whether this page reads in its signed-in account's language for this load:
// the account's step found it here, or switched to it in place, rather than
// reloading for it (AuthenticatedAccountGates.tsx, i18n applyAccountLanguage).
// What a reload for the language would lose waits for it: a link opened in a
// signed-in browser is taken or said, and leaves the address, only then
// (App.tsx; 2026-11-23: with the account's language changed on another
// device, the first invitation link opened came back from that reload with
// the address already without it, and no card said what became of it).

let settled = false;
const waiting = new Set<() => void>();

/** Resolves once the page reads in its account's language for this load. */
export function accountLanguageSettled(): Promise<void> {
  if (settled) return Promise.resolve();
  return new Promise((resolve) => { waiting.add(resolve); });
}

/** The account's language is here: no reload for it is coming in this load. */
export function settleAccountLanguage() {
  settled = true;
  for (const resolve of waiting) resolve();
  waiting.clear();
}
