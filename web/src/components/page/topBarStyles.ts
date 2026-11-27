import chromeStyles from "./TopBarChrome.module.css";
import panelStyles from "./TopBarPanel.module.css";
import peopleStyles from "./TopBarPeople.module.css";
import shareStyles from "./TopBarShare.module.css";
import touchStyles from "./TopBarTouch.module.css";

const styleSources = [chromeStyles, shareStyles, peopleStyles, panelStyles, touchStyles];
const keys = new Set(styleSources.flatMap((source) => Object.keys(source)));

export const styles = Object.fromEntries(
  [...keys].map((key) => [
    key,
    styleSources.map((source) => source[key]).filter(Boolean).join(" "),
  ]),
) as Record<string, string>;
