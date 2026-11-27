import { useEffect, useState } from "react";
import { deviceToReturnTo } from "@/lib/desktopFirstRun";
import { returnToLocalProfile } from "@/lib/profileSwitch";

/**
 * The way back from another server's sign-in in the app's own screen: to
 * this device, or on a phone, which runs no server of its own, to its start
 * (FirstInstall). It is offered once the app says there is somewhere to go
 * back to (deviceToReturnTo) — none on a computer whose organization named
 * the server and that serves nothing itself, where the way back led to the
 * same sign-in (2026-11-14).
 */
export function useWayBackToDevice(offered: boolean): { leave?: () => void; toStart: boolean } {
  const [there, setThere] = useState(false);
  useEffect(() => {
    if (!offered) return undefined;
    let current = true;
    void deviceToReturnTo().then((answer) => { if (current) setThere(answer); });
    return () => { current = false; };
  }, [offered]);
  return {
    leave: offered && there ? returnToLocalProfile : undefined,
    toStart: offered && /Android|iPhone|iPad/u.test(navigator.userAgent),
  };
}
