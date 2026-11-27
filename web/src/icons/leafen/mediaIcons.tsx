import {
  ArrowCircleDown as PhosphorArrowCircleDown,
  ArrowsCounterClockwise as PhosphorArrowsCounterClockwise,
  HandTap as PhosphorHandTap,
  MagnifyingGlassPlus as PhosphorMagnifyingGlassPlus,
  WarningCircle as PhosphorWarningCircle,
} from "@phosphor-icons/react";
import { IconSvg, PhosphorIconSvg, type LeafenIconProps } from "./iconPrimitives";

// An image block's marks, as Notion's menu and hover tools show them (seen
// 2026-10-07), drawn by Leafen: replace is a loop, download an arrow in a
// circle, caption a picture over two lines, a link a chain with a plus, alt
// text a box with an A, full screen a magnifier with a plus, and alignment a
// picture between two lines, placed left, centered or right.
export const ImageReplaceIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowsCounterClockwise} />
);

/** A circle with an arrow down: Notion's download in its picture view (seen 2026-11-05). */
export const DownloadCircleIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowCircleDown} />
);

export const ZoomInIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorMagnifyingGlassPlus} />
);

/** A finger tapping, as Notion's "상호 작용" over a document's embed (seen 2026-11-12). */
export const HandTapIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorHandTap} />
);

export const CaptionIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="caption">
    <rect x="5" y="3.6" width="10" height="7.2" rx="1.6" />
    <path d="M4 13.6h12" />
    <path d="M4 16.2h8" />
  </IconSvg>
);

// Notion's "코드 복사하기" (seen 2026-11-06): "</>" in a rounded square.
export const CodeCopyIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="code-copy">
    <rect x="3.6" y="3.6" width="12.8" height="12.8" rx="2.8" />
    <path d="M8 8.2l-1.8 1.8 1.8 1.8" />
    <path d="M12 8.2l1.8 1.8-1.8 1.8" />
    <path d="M10.7 7.6l-1.4 4.8" />
  </IconSvg>
);

export const LinkPlusIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="link-plus">
    <path d="M8.4 11.6l3.2-3.2" />
    <path d="M10.6 6.4l1-1a2.5 2.5 0 0 1 3.5 3.5l-1 1" />
    <path d="M9.4 13.6l-1 1a2.5 2.5 0 0 1-3.5-3.5l1-1" />
    <path d="M14.6 12.6v4.4M12.4 14.8h4.4" />
  </IconSvg>
);

export const AltTextIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="alt-text">
    <rect x="3.6" y="5" width="12.8" height="10" rx="1.8" />
    <path d="M6.8 12.6l1.9-5 1.9 5M7.4 11h2.6" />
    <path d="M12.6 7.6v5h1.4" />
  </IconSvg>
);

/** "ALT" in strokes, the mark a picture with alt text shows at its foot (ImageAltBadge). */
export const AltBadgeIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="alt-badge">
    <path d="M2.4 13.4l2.3-6.8 2.3 6.8M3.2 11.2h3" />
    <path d="M9.2 6.6v6.8h3" />
    <path d="M13.4 6.6h4.2M15.5 6.6v6.8" />
  </IconSvg>
);

const ALIGN_X = { left: 4, center: 6.5, right: 9 } as const;

export const ImageAlignIcon = ({ align, ...p }: LeafenIconProps & { align: keyof typeof ALIGN_X }) => (
  <IconSvg {...p} glyph={`image-align-${align}`}>
    <path d="M4 4.4h12" />
    <rect x={ALIGN_X[align]} y="7" width="7" height="6" rx="1.3" />
    <path d="M4 15.6h12" />
  </IconSvg>
);

/** A crop's ratio button: a square over a circle's corner, as Notion's (its squareOnCircle, seen 2026-11-15). */
export const AspectRatioIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="aspect-ratio">
    <rect x="6.75" y="6.75" width="10.5" height="10.5" rx="1.5" />
    <path d="M6.75 13.6A5.75 5.75 0 1 1 13.6 6.75" />
  </IconSvg>
);

/** A filled circle with "!": Notion's mark on an embed it can't preview (exclamationMarkCircleFill, seen 2026-11-15). */
export const ExclamationCircleFillIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg weight="fill" {...p} icon={PhosphorWarningCircle} />
);
