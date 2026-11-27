import type { ReactNode } from "react";
import { CheckboxValueIcon, PhoneIcon, TextJustifyLeftSmallIcon, type LeafenIconProps } from "@/icons/leafen";
import {
  ArrowDiagonalUpRightSmallIcon, AtSmallIcon, BulletedListSmallIcon, BurstSmallIcon, LinkSmallIcon, NumberSmallIcon, PeopleFillSmallIcon,
  TextFormatSmallIcon, TriangleDownCircleSmallIcon, ViewCalendarSmallIcon,
} from "@/icons/leafen/automationPropertyGlyphs";
import { PropertyIcon, propertyOwnIcon } from "@/components/database/PropertyIcon";
import type { DbProperty } from "@/lib/types";

// The glyph each kind of property heads an edit-property card with, Notion's
// small ones there (seen 2026-11-22 at 1440 dark, dpr 1): textFormatSmall,
// textJustifyLeftSmall, numberSmall, arrowTriangleDownCircleSmall,
// bulletedList, burstSmall, viewCalendarSmall, peopleFill,
// checkmarkSquareSmall, linkSmall, atSmall, phoneSmall and
// arrowDiagonalUpRightSmall — not the type marks its property list shows.
const CARD_MARKS: Partial<Record<string, (props: LeafenIconProps) => ReactNode>> = {
  title: TextFormatSmallIcon,
  rich_text: TextJustifyLeftSmallIcon,
  number: NumberSmallIcon,
  select: TriangleDownCircleSmallIcon,
  multi_select: BulletedListSmallIcon,
  status: BurstSmallIcon,
  date: ViewCalendarSmallIcon,
  person: PeopleFillSmallIcon,
  checkbox: (props) => <CheckboxValueIcon checked {...props} />,
  url: LinkSmallIcon,
  email: AtSmallIcon,
  phone: PhoneIcon,
  relation: ArrowDiagonalUpRightSmallIcon,
};

/**
 * The mark an edit-property card heads its row with, 16 across: its kind's
 * small glyph (CARD_MARKS); a property given an icon of its own, or of a kind
 * Notion's card was not seen with, its property mark (PropertyIcon).
 */
export function EditPropertyCardMark({ property }: { property: DbProperty }) {
  const Mark = propertyOwnIcon(property) ? undefined : CARD_MARKS[property.type];
  return Mark ? <Mark size="small" aria-hidden="true" /> : <PropertyIcon property={property} size="small" />;
}
