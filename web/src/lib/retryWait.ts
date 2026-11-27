import type { TFunction } from "i18next";
import { i18next } from "@/i18n";

// A wait this short is "a moment", as the screens said every wait before.
const MOMENT_SECONDS = 10;

/**
 * "Try again in about …" for the wait a refusal named (the server's
 * Retry-After, in seconds, as the API error carries it), or "" where it named
 * none or only a moment: a limit that holds for an hour says an hour, not
 * "a moment" (2026-11-23: joining by invitation over its limit said to wait
 * a moment, and a person pressed again and again for an hour). Seconds are
 * said by the ten, a minute and more by the minute, an hour and more by the
 * hour — the screen said it once, and the wait runs on from then.
 */
export function retryWaitText(seconds: unknown, t: TFunction = i18next.t): string {
  if (typeof seconds !== "number" || !Number.isFinite(seconds) || seconds <= MOMENT_SECONDS) return "";
  if (seconds < 50) return String(t("productErrors:meanings.tryAgainInSeconds", { count: Math.ceil(seconds / 10) * 10 }));
  const minutes = Math.max(1, Math.round(seconds / 60));
  if (minutes < 60) return String(t("productErrors:meanings.tryAgainInMinutes", { count: minutes }));
  return String(t("productErrors:meanings.tryAgainInHours", { count: Math.round(seconds / 3600) }));
}

/**
 * A limit's refusal that named a wait, said as one sentence's two halves:
 * what was refused, without "for a moment" ("요청이 너무 많아 제한되었습니다."
 * — the wait says how long), and the wait; null where it named none or only a
 * moment, and the screen says its "a moment" line as before. The one rule
 * for every screen's limit line (productErrorPresentation, settingsErrors).
 */
export function limitWithWait(seconds: unknown, t: TFunction = i18next.t): { cause: string; wait: string } | null {
  const wait = retryWaitText(seconds, t);
  return wait ? { cause: String(t("productErrors:meanings.tooManyRequestsLimited")), wait } : null;
}
