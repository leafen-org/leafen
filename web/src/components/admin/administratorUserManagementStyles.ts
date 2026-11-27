import chromeStyles from "../WorkspaceSettingsChrome.module.css";
import memberStyles from "../WorkspaceSettingsMembers.module.css";
import responsiveStyles from "../WorkspaceSettingsResponsive.module.css";
import storageStyles from "../WorkspaceSettingsStoragePolicy.module.css";
import cleanupStyles from "./AdministratorUserManagement.module.css";

const sources: Array<Record<string, string>> = [
  chromeStyles,
  memberStyles,
  responsiveStyles,
  storageStyles,
  cleanupStyles,
];
const keys = new Set(sources.flatMap((source) => Object.keys(source)));

export const administratorUserStyles = Object.fromEntries(
  [...keys].map((key) => [
    key,
    sources.map((source) => source[key]).filter(Boolean).join(" "),
  ]),
) as Record<string, string>;
