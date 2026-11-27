import { type RefObject, useLayoutEffect, useSyncExternalStore } from "react";
import { useTranslation } from "react-i18next";
import { settingsWindowStore, trashPanelStore } from "@/lib/productWindows";

function subscribeWindows(listener: () => void) {
  const leaveSettings = settingsWindowStore.subscribe(listener);
  const leaveTrash = trashPanelStore.subscribe(listener);
  return () => { leaveSettings(); leaveTrash(); };
}

function windowShown(): "settings" | "trash" | null {
  return settingsWindowStore.get() ? "settings" : trashPanelStore.get() ? "trash" : null;
}

/**
 * What the screen is, named on the page's landmark and said as it is moved
 * to: the settings or the trash window while one is over it, else the
 * screen's own name. Those windows open and close in lib/productWindows.ts,
 * which this alone follows, so their press does not draw the shell again.
 */
export function RouteLabel({ label, main, className }: { label: string; main: RefObject<HTMLElement | null>; className: string }) {
  const { t } = useTranslation("appShell");
  const shown = useSyncExternalStore(subscribeWindows, windowShown, () => null);
  const named = shown === "settings" ? t("route.settings") : shown === "trash" ? t("route.trash") : label;
  useLayoutEffect(() => {
    if (named) main.current?.setAttribute("aria-label", named);
    else main.current?.removeAttribute("aria-label");
  }, [main, named]);
  return (
    <span className={className} aria-live="polite" aria-atomic="true">
      {named ? t("navigationComplete", { destination: named }) : ""}
    </span>
  );
}
