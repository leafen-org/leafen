// A code typed by hand — the device and recovery codes — is digits
// (account_device_codes.go), so a phone opens its number pad: a page can't
// choose a letters keyboard's language, and an iPhone whose keyboard was
// Korean opened it for letters whatever the field asked (2026-11-06 decision).
export const CODE_KEYBOARD = { inputMode: "numeric", autoCapitalize: "off", autoCorrect: "off", spellCheck: false } as const;

// Digits typed on a number pad, which has no hyphen, come in fours as the
// other device shows them; anything else (a code from before the digits)
// stays as typed. The server reads either.
export function inFours(typed: string): string {
  const digits = typed.replace(/[\s-]/gu, "");
  return /^\d+$/u.test(digits) ? (digits.match(/\d{1,4}/gu) ?? []).join("-") : typed;
}

// The setup code is the setup link's digits or the one the server was given
// (LEAFEN_SETUP_CODE), which may hold letters — the operator's own, or one a
// hosting platform made — so its field opens the letters keyboard, which has
// the digits too (2026-11-19: a number pad left a code with letters untypable
// on a phone). Nothing is changed as it is typed; the server reads text typed
// with a Korean keyboard on as the keys pressed (typed_code_keys.go).
export const SETUP_CODE_KEYBOARD = { inputMode: "text", autoCapitalize: "off", autoCorrect: "off", spellCheck: false } as const;
