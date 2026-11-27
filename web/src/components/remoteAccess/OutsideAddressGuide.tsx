import { useTranslation } from "react-i18next";
import { serverOrigin } from "@/lib/serverOrigin";
import { GuidePlayer } from "../guide/GuidePlayer";
import { outsideAddressGuideScreens, type OutsideGuideWords } from "./outsideAddressGuideScreens";
import type { TailscaleGuideLook } from "./tailscaleAddressGuideScreens";

// The second scene shows the most to read.
const SCENE_MS = [3400, 3600, 3000, 3800];

/**
 * The example on the outside address's choice, before anything is chosen
 * (the one-thing-per-screen decision of 2026-11-09, 2): what an address of
 * its own does — today only this computer opens Leafen, with Tailscale a free
 * account signs in, Leafen gets an address, and a phone and a chat app on the
 * internet open it there. Leafen's screen is drawn in the look it has where
 * the guide stands (the first run's full-window step or Settings' dialog).
 */
export function OutsideAddressGuide({ look }: { look: TailscaleGuideLook }) {
  const { t } = useTranslation(["outsideAddressGuide", "serverConsole"]);
  const scenes = t("outsideAddressGuide:scenes", { returnObjects: true }) as { title: string; caption: string }[];
  const words: OutsideGuideWords = {
    here: new URL(serverOrigin()).host,
    pageTitle: t("outsideAddressGuide:mock.pageTitle"),
    unreachable: t("outsideAddressGuide:mock.unreachable"),
    doneTitle: t("serverConsole:remoteChoice.doneTitle"),
    addressQR: t("serverConsole:remoteChoice.addressQR"),
    finish: t("serverConsole:remoteChoice.finish"),
    chatApp: t("outsideAddressGuide:mock.chatApp"),
    chatAsk: t("outsideAddressGuide:mock.chatAsk"),
    chatAnswer: t("outsideAddressGuide:mock.chatAnswer"),
  };
  const screens = outsideAddressGuideScreens(look, words);
  return (
    <GuidePlayer
      label={t("outsideAddressGuide:region")}
      shows={t("outsideAddressGuide:shows")}
      scenes={scenes.map((scene, index) => ({ ...scene, picture: screens[index], durationMs: SCENE_MS[index] }))}
    />
  );
}
