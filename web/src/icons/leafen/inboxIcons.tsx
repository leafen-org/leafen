import {
  ArrowUUpLeft as PhosphorArrowUUpLeft,
  ChatText as PhosphorChatText,
  Envelope as PhosphorEnvelope,
  LockOpen as PhosphorLockOpen,
  Notification as PhosphorNotification,
  Spinner as PhosphorSpinner,
} from "@phosphor-icons/react";
import { IconSvg, PhosphorIconSvg, type LeafenIconProps } from "./iconPrimitives";

// The inbox's marks, as Notion's (seen 2026-10-07): unread is a square with a
// dot at its corner, read the same square with a check there, taking a
// notification out of the archive a U-turn arrow, updates a burst, and
// unpinning a crossed-out pin.
export const MarkUnreadIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorNotification} />
);

export const MarkReadIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="mark-read">
    <path d="M10.6 4.6H5.4a.8.8 0 0 0-.8.8v9.2a.8.8 0 0 0 .8.8h9.2a.8.8 0 0 0 .8-.8V9.4" />
    <path d="M12.4 5.4l1.4 1.4 2.6-3" />
  </IconSvg>
);

export const UnarchiveIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowUUpLeft} />
);

export const SparkleBurstIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorSpinner} />
);

// The inbox filter's "사용 권한" and "토론", as Notion's (seen 2026-11-13): an
// open padlock with its keyhole, and a square speech bubble with two lines.
export const InboxPermissionsIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorLockOpen} />
);

export const InboxDiscussionsIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorChatText} />
);

// A phone's inbox head, as Notion's (seen 2026-11-12 at 402pt, iPhone
// Safari, dark): its notification settings an eight-toothed gear with a small
// hole, 17.3pt across in its 22px box (teeth 8pt out, the gaps 6.2, the hole
// 2.2, to the lines' middles), and its filter three shortening lines (10, 8
// and 5pt, 2.7pt apart, the middle one 0.5pt under the centre) in a 16.6pt
// ring; lines 1.3pt. The desktop inbox's filter is the lines alone (FilterIcon).
export const InboxSettingsIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="inbox-settings">
    <path d="M8.77 5.70L8.79 4.31A5.82 5.82 0 0 1 11.21 4.31L11.23 5.70A4.47 4.47 0 0 1 12.17 6.09L13.17 5.12A5.82 5.82 0 0 1 14.88 6.83L13.91 7.83A4.47 4.47 0 0 1 14.30 8.77L15.69 8.79A5.82 5.82 0 0 1 15.69 11.21L14.30 11.23A4.47 4.47 0 0 1 13.91 12.17L14.88 13.17A5.82 5.82 0 0 1 13.17 14.88L12.17 13.91A4.47 4.47 0 0 1 11.23 14.30L11.21 15.69A5.82 5.82 0 0 1 8.79 15.69L8.77 14.30A4.47 4.47 0 0 1 7.83 13.91L6.83 14.88A5.82 5.82 0 0 1 5.12 13.17L6.09 12.17A4.47 4.47 0 0 1 5.70 11.23L4.31 11.21A5.82 5.82 0 0 1 4.31 8.79L5.70 8.77A4.47 4.47 0 0 1 6.09 7.83L5.12 6.83A5.82 5.82 0 0 1 6.83 5.12L7.83 6.09A4.47 4.47 0 0 1 8.77 5.70Z" />
    <circle cx="10" cy="10" r="1.58" />
  </IconSvg>
);

export const InboxFilterCircleIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="inbox-filter-circle">
    <circle cx="10" cy="10" r="5.56" />
    <path d="M6.84 8.42h6.32" />
    <path d="M7.57 10.36h4.86" />
    <path d="M8.66 12.3h2.68" />
  </IconSvg>
);

/** A closed letter, its flap and the two lines from its lower corners, as Notion's phone invite row for a typed address (seen 2026-11-16 in iPhone Safari at 402). */
export const EnvelopeLetterIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorEnvelope} />
);
