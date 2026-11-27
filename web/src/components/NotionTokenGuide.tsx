import { useTranslation } from "react-i18next";
import { GuidePlayer } from "./guide/GuidePlayer";
import type { LeafenImportWords } from "./notionTokenGuideLeafen";
import { notionTokenGuideScreens, type NotionTokenGuideWords } from "./notionTokenGuideScreens";

// Animated walkthrough of the Notion token flow, step by step as the import
// screen words it (importDialog:notionSteps), styled to read like a screen
// recording of Notion's developer tools: create a connection, give it the
// pages to move, copy its API token, then put the token into Leafen and pick
// what to move. Scenes are faithful CSS replicas (no bundled screenshots) so
// text stays localized and crisp; the shared guide player plays them.
export default function NotionTokenGuide() {
  const { t } = useTranslation(["notionTokenGuide", "importDialog"]);
  const words = t("notionTokenGuide:steps", { returnObjects: true }) as { title: string; caption: string }[];
  const leafen: LeafenImportWords = {
    windowTitle: t("importDialog:window.title"),
    windowLead: t("importDialog:window.lead"),
    // The window's three steps by hand (NotionImportPanel's), the last named for its press.
    steps: [t("importDialog:wizard.stepLabels.0"), t("importDialog:wizard.stepLabels.1"), t("importDialog:migration.approve")],
    notionStepsTitle: t("importDialog:notionSteps.title"),
    openTokenPage: t("importDialog:openTokenPage"),
    tokenLabel: t("importDialog:tokenLabel"),
    tokenPlaceholder: t("importDialog:tokenPlaceholder"),
    saveToken: t("importDialog:handoff.saveToken"),
    rootPickerTitle: t("importDialog:rootPickerTitle"),
    rootSearch: t("importDialog:rootSearch"),
    rootKindPage: t("importDialog:rootKindPage"),
    back: t("importDialog:wizard.back"),
    next: t("importDialog:wizard.next"),
  };
  const screens = notionTokenGuideScreens(
    t("notionTokenGuide:screen", { returnObjects: true }) as NotionTokenGuideWords,
    leafen,
  );
  return (
    <GuidePlayer
      label={t("notionTokenGuide:region")}
      shows={t("notionTokenGuide:shows")}
      scenes={words.map((scene, index) => ({ ...scene, ...screens[index] }))}
    />
  );
}
