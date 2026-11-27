import { useEffect, useState } from "react";
import { api } from "@/api";
import type { Session } from "@/types";

// Whether the signed-in account administers this server, read once per
// sign-in and shared: only an administrator can turn on what other notes
// point at (an outside address, the server's move), so whoever else reads
// them hears what is so, not what to do about it.
const administratorOf = new Map<string, Promise<boolean>>();

/** Whether the signed-in account administers this server, read once per sign-in. */
export function readServerAdministrator(session: Session): Promise<boolean> {
  let reading = administratorOf.get(session.token);
  if (!reading) {
    reading = api.readAccountState(session).then((state) => state.administrator === true, () => false);
    administratorOf.set(session.token, reading);
  }
  return reading;
}

export function useServerAdministrator(session: Session | null | undefined, ask = true) {
  const [administrator, setAdministrator] = useState(false);
  useEffect(() => {
    if (!session || !ask) return undefined;
    let live = true;
    void readServerAdministrator(session).then((value) => { if (live) setAdministrator(value); });
    return () => { live = false; };
  }, [ask, session]);
  return administrator;
}
