import { useEffect, useState } from "react";
import { api } from "@/api";
import { SecondDeviceOffer } from "@/components/auth/SecondDeviceOffer";
import { outsideAddress } from "@/lib/addressReach";
import { phoneOfferHeld, settlePhoneOffer } from "@/lib/phoneOffer";
import { fetchRuntimeConfig, subscribeRuntimeConfig } from "@/lib/runtimeConfigService";
import { useAfterTyping } from "@/lib/typingHold";
import type { Session } from "@/types";
import { BackupSignInScreen } from "./BackupSignInScreen";
import { FirstRunLayer } from "./FirstRunLayer";

/**
 * The phone a first setup on an address only this computer opens couldn't
 * offer (the first-setup decision of 2026-10-26, 3): once the server has an
 * outside address and the account still has one device, "이제 휴대폰도
 * 등록해 두세요" with its QR and code, once. The address choice's own last
 * screen, which shows a phone's QR, settles it as well.
 */
export function PhoneOfferLater({ session, blocked, onSession }: { session: Session; blocked: boolean; onSession(session: Session): void }) {
  const [offering, setOffering] = useState(false);
  const [backup, setBackup] = useState(false);

  useEffect(() => {
    if (!phoneOfferHeld(session.user.id)) return undefined;
    let current = true;
    const consider = (address: string) => {
      if (!outsideAddress(address) || !phoneOfferHeld(session.user.id)) return;
      void api.listAuthSessions(session).then((sessions) => {
        if (!current) return;
        if (sessions.filter((item) => item.registered).length <= 1) setOffering(true);
        else settlePhoneOffer(session.user.id);
      }, () => undefined);
    };
    void fetchRuntimeConfig().then(({ config }) => consider(config.publicAddress ?? ""), () => undefined);
    const stop = subscribeRuntimeConfig((config) => consider(config.publicAddress ?? ""));
    return () => {
      current = false;
      stop();
    };
  }, [session]);

  const clear = useAfterTyping(offering && !blocked);
  if (!clear) return null;
  // The phone is still to register: a backup sign-in comes back to the offer.
  if (backup) return <BackupSignInScreen session={session} onSession={onSession} onDone={() => setBackup(false)} />;
  return (
    <FirstRunLayer view="phone" data={{ "data-phone-offer-later": "" }}>
      <SecondDeviceOffer session={session} onDone={() => {
        settlePhoneOffer(session.user.id);
        setOffering(false);
      }} onBackup={() => setBackup(true)} />
    </FirstRunLayer>
  );
}
