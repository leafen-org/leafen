import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { GraphRow } from "@/contentGraph";
import type { DatabaseMyTask } from "@/databaseToolbarConfigWire.generated";
import { useProductToast } from "@/lib/productToast";
import { isServerUnreachable, retryWhenServerMayAnswer } from "@/lib/serverReach";

type ShownEdit = { values: GraphRow["values"]; generation: number; sent: boolean };

/**
 * Sends a task's steps, each kept in the outbox as it is given — behind the
 * task's making and the steps given before it — and sent there in turn
 * (rowValuesKept.ts): given one after another's answer, a step still waiting
 * for the first's went with the tab at a reload (2026-11-25: a title typed on
 * a card made in a column, its status set first, was lost while the card's
 * making waited for the server). One the server can't be reached for that
 * no outbox keeps is sent again, the same step with its same id, once it may
 * answer (serverReach.ts); one it refuses ends the send.
 */
function sendUntilTaken(steps: ReadonlyArray<() => Promise<unknown>>): Promise<void> {
  return Promise.all(steps.map((step) => new Promise<void>((resolve, reject) => {
    let stop: (() => void) | undefined;
    const attempt = () => {
      stop?.();
      stop = undefined;
      step().then(() => resolve(), (error: unknown) => {
        if (!isServerUnreachable(error)) {
          reject(error);
          return;
        }
        stop = retryWhenServerMayAnswer(attempt);
      });
    };
    attempt();
  }))).then(() => undefined);
}

/**
 * What a task's row changes in "내 작업" — its name, status and date, typed
 * in its cell, ticked or moved to another group — shows at once, as a
 * database view's cell edit does (user decision 2026-11-12: a press answers
 * on the next frame; 2026-11-12: on a stalled network a name typed showed as
 * "새 작업" until the network came back). The change goes to the task's own
 * database behind, kept in the outbox after the row's last (sendUntilTaken);
 * the task list read after it takes over. One the server refuses goes back,
 * and says so.
 */
export function useTaskEditsShownAtOnce(tasks: readonly DatabaseMyTask[], onChanged: () => void) {
  const { t } = useTranslation("databaseView");
  const { notify } = useProductToast();
  const [shown, setShown] = useState<ReadonlyMap<string, ShownEdit>>(() => new Map());
  const generation = useRef(0);
  // The task list read after a change was sent says what the server has.
  useEffect(() => setShown((current) => {
    if (![...current.values()].some(({ sent }) => sent)) return current;
    return new Map([...current].filter(([, edit]) => !edit.sent));
  }), [tasks]);
  const latest = useRef({ notify, onChanged, t });
  latest.current = { notify, onChanged, t };

  const write = useCallback((rowId: string, values: GraphRow["values"], steps: ReadonlyArray<() => Promise<unknown>>) => {
    if (!steps.length) return;
    const made = ++generation.current;
    setShown((current) => new Map(current).set(rowId, { values: { ...current.get(rowId)?.values, ...values }, generation: made, sent: false }));
    const settle = (keep: boolean) => setShown((current) => {
      const edit = current.get(rowId);
      if (!edit || edit.generation !== made) return current;
      const next = new Map(current);
      if (keep) next.set(rowId, { ...edit, sent: true });
      else next.delete(rowId);
      return next;
    });
    void sendUntilTaken(steps).then(() => {
      settle(true);
      latest.current.onChanged();
    }, () => {
      settle(false);
      latest.current.notify(latest.current.t("databaseView:toast.couldntSaveChanges"), "error");
      latest.current.onChanged();
    });
  }, []);

  return { shown, write };
}
