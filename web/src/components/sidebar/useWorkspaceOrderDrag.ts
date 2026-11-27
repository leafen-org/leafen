import { useEffect, useMemo, useState } from "react";
import { api } from "@/api";
import { loadSession } from "@/lib/authSessionStore";
import { personValue, storePersonValue } from "@/lib/browserStorage";

const ORDER_KEY = "leafen:workspace-order";
const START_AFTER = 4;

function storedOrder(personId: string): string[] {
  try { return JSON.parse(personValue(ORDER_KEY, { userId: personId }) ?? "[]") as string[]; } catch { return []; }
}

/**
 * The switcher's workspaces in the order this person dragged them to, which
 * follows the account to every device, as Notion's (seen 2026-11-16: dragged
 * in Chrome, the same order in iPhone Safari): kept with the account
 * (account_workspace_order.go) and on this device, which shows it at once
 * while the account's is read. A row pressed and pulled floats under the
 * pointer while the others step aside, as Notion's (seen 2026-11-03);
 * pulled, it doesn't open.
 */
export function useWorkspaceOrderDrag<T extends { id: string }>(personId: string, items: readonly T[]) {
  const [order, setOrder] = useState(() => storedOrder(personId));
  useEffect(() => {
    const session = loadSession();
    if (!session) return undefined;
    const reading = new AbortController();
    void api.readWorkspaceOrder(session, reading.signal).then(({ workspaceIds }) => {
      if (reading.signal.aborted || !workspaceIds.length) return;
      setOrder(workspaceIds);
      storePersonValue(ORDER_KEY, JSON.stringify(workspaceIds), { userId: personId });
    }).catch(() => undefined);
    return () => reading.abort();
  }, [personId]);
  const ordered = useMemo(() => {
    const at = new Map(order.map((id, index) => [id, index]));
    return items.map((item, index) => ({ item, index }))
      .sort((a, b) => (at.get(a.item.id) ?? order.length + a.index) - (at.get(b.item.id) ?? order.length + b.index))
      .map(({ item }) => item);
  }, [items, order]);
  useEffect(() => {
    const down = (start: PointerEvent) => {
      const pressed = start.target instanceof Element ? start.target.closest<HTMLElement>("[data-workspace-order-row]") : null;
      if (!pressed || start.button !== 0 || !start.isPrimary) return;
      const rows = [...(pressed.parentElement?.querySelectorAll<HTMLElement>(":scope > [data-workspace-order-row]") ?? [])];
      const from = rows.indexOf(pressed), step = pressed.offsetHeight;
      let to = from, dragging = false;
      const clear = () => rows.forEach((row) => {
        row.style.transform = ""; row.style.transition = ""; row.style.zIndex = "";
        row.removeAttribute("data-dragging");
      });
      const move = (event: PointerEvent) => {
        const moved = event.clientY - start.clientY;
        if (!dragging) {
          if (Math.abs(moved) < START_AFTER) return;
          // It is no press any more: the row won't open as it is let go.
          pressed.dispatchEvent(new PointerEvent("pointercancel", { bubbles: true, pointerId: start.pointerId }));
          dragging = true;
          pressed.setAttribute("data-dragging", "true");
          pressed.style.zIndex = "2";
        }
        to = Math.max(0, Math.min(rows.length - 1, from + Math.round(moved / step)));
        pressed.style.transform = `translateY(${moved}px)`;
        rows.forEach((row, index) => {
          if (row === pressed) return;
          row.style.transition = "transform 120ms ease";
          row.style.transform = index > from && index <= to ? `translateY(${-step}px)` : index < from && index >= to ? `translateY(${step}px)` : "";
        });
      };
      const up = (event: PointerEvent) => {
        // The cancel sent above to end the press isn't this drag's end.
        if (event.type === "pointercancel" && !dragging && !event.isTrusted) return;
        window.removeEventListener("pointermove", move);
        window.removeEventListener("pointerup", up, true);
        window.removeEventListener("pointercancel", up);
        clear();
        if (!dragging) return;
        event.stopPropagation();
        // The click that ends the pull isn't a choice of the row.
        const swallow = (click: Event) => { click.preventDefault(); click.stopPropagation(); };
        window.addEventListener("click", swallow, { capture: true, once: true });
        window.setTimeout(() => window.removeEventListener("click", swallow, { capture: true }), 200);
        if (to === from) return;
        const ids = rows.map((row) => row.dataset.workspaceOrderRow ?? "");
        ids.splice(to, 0, ...ids.splice(from, 1));
        setOrder(ids);
        storePersonValue(ORDER_KEY, JSON.stringify(ids), { userId: personId });
        const session = loadSession();
        if (session) void api.saveWorkspaceOrder(session, ids).catch(() => undefined);
      };
      window.addEventListener("pointermove", move);
      window.addEventListener("pointerup", up, true);
      window.addEventListener("pointercancel", up);
    };
    document.addEventListener("pointerdown", down, true);
    return () => document.removeEventListener("pointerdown", down, true);
  }, [personId]);
  return ordered;
}
