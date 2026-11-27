import { useEffect, useRef } from "react";
import { i18next } from "@/i18n";
import { retryWhenServerMayAnswer } from "./serverReach";

// A list or a preview the server wasn't there to read says so where it would
// show, never as empty (2026-11-11: offline, the inbox said "새 알림이 없습니다"
// for a server it never asked, and a board card's page opened as nothing), and
// is read again once the server may answer, as a settings section's read is
// (settingsErrors.ts) and a page that couldn't be read says it opens then.

/** What a list says in place of what the server wasn't there to give. */
export function unansweredReadText() {
  return i18next.t("settingsErrors:readUnanswered");
}

/** While `unanswered`, calls read again each time the server may answer (serverReach.ts). */
export function useReadAgainWhenAnswered(unanswered: boolean, read: () => void) {
  const latest = useRef(read);
  latest.current = read;
  useEffect(() => (unanswered ? retryWhenServerMayAnswer(() => latest.current()) : undefined), [unanswered]);
}
