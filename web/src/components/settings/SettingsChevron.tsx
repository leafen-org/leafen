import { useSidebarLayout } from "@/components/shell/AppShell";
import { CountChevronDown, SidebarChevronDown } from "@/icons/leafen";

/**
 * A settings picker's ⌄, as Notion draws it at each place (seen 2026-11-16
 * in Chrome 1440 and the Galaxy A90's Chrome at 411): its own chevron in a
 * box as wide as the glyph, 14px tall on a computer and 16px on a phone. A
 * filter, a sort, a row's choice, a landing page's and a guest's pages, and a
 * member's teamspaces take the thin one (arrowChevronSingleDownSmall, 8.64
 * by 14 or 9.88 by 16); a member's role and a teamspace's access level the
 * filled one (arrowChevronSingleDownFillSmall, 8.88 by 14 or 10.15 by 16).
 */
export function SettingsChevron({ filled = false, phone, className }: {
  filled?: boolean;
  /** Drawn at a phone's size; the layout's own when not given. */
  phone?: boolean;
  className?: string;
}) {
  const layoutPhone = useSidebarLayout(true).phone;
  const size = (phone ?? layoutPhone) ? "small" : "tiny";
  return filled
    ? <SidebarChevronDown className={className} size={size} trim aria-hidden="true" />
    : <CountChevronDown className={className} size={size} trim aria-hidden="true" />;
}
