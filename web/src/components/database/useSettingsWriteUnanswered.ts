import { useCallback, useEffect, useRef } from "react";
import { i18next } from "@/i18n";
import { useProductToast } from "@/lib/productToast";
import { isServerUnreachable, retryWhenServerMayAnswer } from "@/lib/serverReach";
import { isUncertainWrite, onUncertainWritesSettled } from "@/lib/uncertainWrites";

/** A request given up on as its screen left, which nothing says. */
export const aborted = (error: unknown) => error instanceof DOMException && error.name === "AbortError";

/**
 * A database's toolbar settings (its sub-items and dependencies, its
 * templates, its sprints) are written straight to the server, not kept in
 * the page's outbox that the connection bar promises to send once connected.
 * A change of them that got no answer is said as one whose outcome isn't
 * known, in the words and the quiet tone of every setting written straight
 * (the settings window, the automations panel: settingsErrors
 * writeUncertain); its control shows what the server last said, and the
 * settings are read again once the server answers, so a change that landed
 * shows then (2026-11-17: offline, a sub-item setting sprang back with
 * nothing said, under the bar's "연결되면 변경을 보냅니다"). Notion's offline
 * settings were not observed (user decision 2026-11-17: fixed by principle,
 * DIV).
 *
 * Gives whether the error was one such, said here.
 */
export function useSettingsWriteUnanswered(readAgain: () => void) {
  const { notify } = useProductToast();
  const latest = useRef(readAgain);
  latest.current = readAgain;
  const waiting = useRef(false);
  useEffect(() => onUncertainWritesSettled(() => {
    if (!waiting.current) return;
    waiting.current = false;
    latest.current();
  }), []);
  return useCallback((error: unknown) => {
    if (!isUncertainWrite(error) || aborted(error)) return false;
    waiting.current = true;
    notify(i18next.t("settingsErrors:writeUncertain"));
    return true;
  }, [notify]);
}

const READ_AGAIN_GAP_MS = 5_000;

/**
 * A read of those settings the server didn't answer (a database opened while
 * it was away, or read again as a change went) says nothing failed, as every
 * read without its server (serverReach.ts): the connection bar says it is
 * away, the settings show what was last read, and they are read again when
 * the server may answer (2026-11-17: a task database opened while the server
 * was down still went unread a minute after it came back — its menu offered
 * "유형 전환", not "작업 데이터베이스 되돌리기", and "추가 설정" had no
 * "스프린트" — until the page was opened again). They go again at most once
 * in READ_AGAIN_GAP_MS: a read turned away (a busy server's 503) while the
 * server answers others would otherwise go again at each of their answers
 * (2026-11-17: some 900 reads in ten seconds).
 *
 * Gives whether the error was one such.
 */
export function useSettingsReadUnanswered(readAgain: () => void) {
  const latest = useRef(readAgain);
  latest.current = readAgain;
  const stop = useRef<(() => void) | null>(null);
  const last = useRef(0);
  const timer = useRef(0);
  useEffect(() => () => { stop.current?.(); stop.current = null; window.clearTimeout(timer.current); }, []);
  return useCallback((error: unknown) => {
    if (!isServerUnreachable(error)) return false;
    stop.current ??= retryWhenServerMayAnswer(() => {
      stop.current?.();
      stop.current = null;
      const read = () => { last.current = Date.now(); latest.current(); };
      window.clearTimeout(timer.current);
      timer.current = window.setTimeout(read, Math.max(0, last.current + READ_AGAIN_GAP_MS - Date.now()));
    });
    return true;
  }, []);
}
