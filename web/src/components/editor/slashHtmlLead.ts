import { useProductPagesSelect } from "@/components/product/productPagesStore";

/**
 * Whether "/" and "+" lead "추천" with the HTML embed (slashSuggestions): in a
 * build with the app platform, for a member of the page's workspace, not a
 * guest. Notion leads with HTML only where the workspace's AI is on for the
 * person, which a guest never has (read 2026-11-18 from its menu code:
 * isAiEnabledOnSpace; seen in Chrome at 1440 in two tabs: a guest's "추천"
 * had no HTML place with comment access while suggesting and with edit
 * access, its "/" menu offering HTML all the same, where the owner's had).
 */
export function useSlashHtmlLead(workspaceId: string | undefined, appPlatform: boolean) {
  const guest = useProductPagesSelect((pages) => workspaceId !== undefined
    && pages?.workspaces?.find(({ id }) => id === workspaceId)?.guest === true);
  return appPlatform && !guest;
}
