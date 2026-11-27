import { authBounds } from "@/authWire.generated";

/** How many characters a password may have with letters alone (auth_password_rule.go). */
const LONG_ENOUGH = 15;

/**
 * The one rule every new password meets, Notion's (seen 2026-10-27): 15
 * characters, or 8 with both letters and digits — the server's own judgment, said before it refuses.
 */
export function passwordAcceptable(value: string): boolean {
  const count = [...value].length;
  if (count >= LONG_ENOUGH) return true;
  if (count < authBounds.minimumPasswordLength) return false;
  return /\p{L}/u.test(value) && /\p{Nd}/u.test(value);
}
