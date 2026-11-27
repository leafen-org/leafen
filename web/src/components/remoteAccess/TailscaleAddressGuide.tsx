import { useTranslation } from "react-i18next";
import { isPackagedClient } from "@/lib/packagedClient";
import { GuidePlayer } from "../guide/GuidePlayer";
import { tailscaleAddressGuideScreens, type TailscaleGuideLook } from "./tailscaleAddressGuideScreens";

/**
 * The moving screen guide under the Tailscale step of choosing the outside
 * address (RemoteAddressChoice): Leafen opens Tailscale's sign-in and shows
 * its QR code, the phone's camera reads it, Tailscale's sign-in, Leafen's step
 * to allow the address with a new QR code, Tailscale's page that turns on
 * HTTPS and Funnel, and the address in Leafen. Leafen's screens are drawn with
 * their own words and in the look they have there — the first run's
 * full-window step or Settings' dialog; the shared player plays them.
 */
export function TailscaleAddressGuide({ look }: { look: TailscaleGuideLook }) {
  const { t } = useTranslation(["tailscaleAddressGuide", "serverConsole", "common"]);
  const scenes = t("tailscaleAddressGuide:scenes", { returnObjects: true }) as { title: string; caption: string }[];
  const app = isPackagedClient();
  const screens = tailscaleAddressGuideScreens(look, {
    title: t("serverConsole:remoteChoice.title"),
    loginOpened: t("serverConsole:tunnel.tailscaleLoginOpened"),
    // The app opens the allow page itself; a browser leaves it to the button (outsideOpener.ts).
    allow: t(app ? "serverConsole:tunnel.tailscaleAllowOpened" : "serverConsole:tunnel.tailscaleAllow"),
    allowButton: t(app ? "serverConsole:tunnel.tailscaleOpenAgain" : "serverConsole:tunnel.tailscaleOpen"),
    openAgain: t("serverConsole:tunnel.tailscaleOpenAgain"),
    signInQR: t("serverConsole:remoteChoice.signInQR"),
    allowQR: t("serverConsole:remoteChoice.allowQR"),
    fact: t("serverConsole:remoteChoice.tailscaleFact"),
    back: t("common:actions.back"),
    later: t("serverConsole:remoteChoice.later"),
    doneTitle: t("serverConsole:remoteChoice.doneTitle"),
    addressQR: t("serverConsole:remoteChoice.addressQR"),
    tailnetName: t("serverConsole:tunnel.tailscaleTailnetName"),
    finish: t("serverConsole:remoteChoice.finish"),
  });
  return (
    <GuidePlayer
      label={t("tailscaleAddressGuide:region")}
      shows={t("tailscaleAddressGuide:shows")}
      scenes={scenes.map((scene, index) => ({ ...scene, picture: screens[index] }))}
    />
  );
}
