import chromeStyles from "@/components/WorkspaceSettingsChrome.module.css";
import memberStyles from "@/components/WorkspaceSettingsMembers.module.css";
import organizationStyles from "@/components/WorkspaceSettingsOrganization.module.css";
import profileStyles from "@/components/WorkspaceSettingsProfile.module.css";
import responsiveStyles from "@/components/WorkspaceSettingsResponsive.module.css";
import securityStyles from "@/components/WorkspaceSettingsSecurity.module.css";
import storagePolicyStyles from "@/components/WorkspaceSettingsStoragePolicy.module.css";

const sources: Array<Record<string, string>> = [
  chromeStyles,
  memberStyles,
  organizationStyles,
  profileStyles,
  securityStyles,
  storagePolicyStyles,
  responsiveStyles,
];
const keys = new Set(sources.flatMap((source) => Object.keys(source)));

export const styles = Object.fromEntries(
  [...keys].map((key) => [
    key,
    sources.map((source) => source[key]).filter(Boolean).join(" "),
  ]),
) as Record<string, string>;
