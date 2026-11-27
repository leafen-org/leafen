import { useEffect, useState, type ReactNode } from "react";
import { useDatabaseAutomationCount } from "./databaseAutomationCount";

type FirstAutomationExits = { onBack(): void; onClose(): void };

/**
 * The view settings' "자동화" on a database with no automation yet, as
 * Notion's (seen 2026-11-15 in two tabs, 1440 dark): the new one's window
 * opens over the settings, which stay behind it under its shade and are
 * there again as it closes; the one made shows the automations' list in
 * their place, its "←" back to the settings. With some already, or for a
 * reader, "자동화" opens the list as before; a phone's and a wide touch
 * screen's settings are sheets, whose "자동화" opens the automations' own.
 */
export function useSettingsFirstAutomation(settingsOpen: boolean, automation: {
  databaseId: string;
  automationCount?: number;
  readOnly?: boolean;
} | null | undefined) {
  const [open, setOpen] = useState(false);
  // What its list last held in this window, ahead of the page's next read.
  const count = useDatabaseAutomationCount(automation?.databaseId ?? "", automation?.automationCount);
  useEffect(() => { if (!settingsOpen) setOpen(false); }, [settingsOpen]);
  const first = settingsOpen && Boolean(automation) && !automation?.readOnly && count === 0;
  return {
    /** The settings' "자동화": the first one's window where there is none, else the list (`toList`). */
    onAutomations: (toList: (() => void) | undefined) => toList && (first ? () => setOpen(true) : toList),
    /** The first one's window, drawn over the settings while it is open: cancelled, the settings; made, the list. */
    window: (render: (exits: FirstAutomationExits) => ReactNode, toList: (() => void) | undefined) => open ? render({
      onBack: () => setOpen(false),
      onClose: () => { setOpen(false); toList?.(); },
    }) : null,
  };
}
