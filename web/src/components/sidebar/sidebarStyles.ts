import chromeStyles from "./SidebarChrome.module.css";
import layoutStyles from "./SidebarLayout.module.css";
import treeStyles from "./SidebarTree.module.css";
import viewStyles from "./SidebarViews.module.css";
import workspaceMenuStyles from "./SidebarWorkspaceMenu.module.css";
import phoneStyles from "./SidebarPhone.module.css";
import wideTouchStyles from "./SidebarWideTouch.module.css";
import listEndStyles from "./SidebarListEnd.module.css";
import foldedSectionStyles from "./SidebarFoldedSections.module.css";
import { keysFocusHolder } from "@/lib/lastPress";

// A row or a section's head the keys' focus is in shows its tools (data-keys-within).
for (const holder of [chromeStyles.sectionLabel, viewStyles.shortcutItem, viewStyles.viewRowItem, treeStyles.treeRow]) keysFocusHolder(holder);

const modules = [chromeStyles, treeStyles, layoutStyles, viewStyles, workspaceMenuStyles, phoneStyles, wideTouchStyles, listEndStyles, foldedSectionStyles];

const keys = new Set(modules.flatMap((module) => Object.keys(module)));

export const styles = Object.fromEntries(
  [...keys].map((key) => [
    key,
    modules.map((module) => module[key]).filter(Boolean).join(" "),
  ]),
) as Record<string, string>;
