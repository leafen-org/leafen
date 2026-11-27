import { useEffect, useState } from "react";
import { api } from "@/api";
import type { AdministratorSettings } from "@/administratorWire.generated";
import { retryWhenServerMayAnswer } from "@/lib/serverReach";
import { onUncertainWritesSettled } from "@/lib/uncertainWrites";
import type { Session } from "@/types";

/**
 * Whether the signed-in account administers this server, read once per
 * session with the account's state: its administrators find the server's
 * settings in the settings window, which only they read.
 */
export function useAdministratorAccess(session: Session | null) {
  const [settings, setSettings] = useState<AdministratorSettings | null>(null);
  // Whether the server has said if the account administers it: until then,
  // no settings says nothing (BackupFailureNotice keeps its band).
  const [known, setKnown] = useState(false);

  useEffect(() => {
    setKnown(false);
    if (!session) {
      setSettings(null);
      return;
    }
    let cancelled = false;
    let stopRetrying = () => {};
    const read = (keep = false) => {
      api.readAccountState(session).then((state) => state.administrator
        ? api.administratorSettings(session)
        : null).then((loaded) => {
        if (cancelled) return;
        stopRetrying();
        setSettings(loaded);
        setKnown(true);
      }).catch(() => {
        if (!cancelled && !keep) setSettings(null);
      });
    };
    // A change sent without the server's answer may have been taken after
    // all: the settings the window's server sections start from are read
    // again once it answers, keeping what was read if that read fails.
    const stopSettled = onUncertainWritesSettled(() => read(true));
    // Read again until the server answers, or a window opened while it was away
    // goes on without the server's settings (2026-11-04).
    stopRetrying = retryWhenServerMayAnswer(() => read());
    read();
    return () => {
      cancelled = true;
      stopRetrying();
      stopSettled();
    };
  }, [session]);

  return { settings, setSettings, known };
}
