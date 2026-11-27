import {
  Smiley as PhosphorSmiley,
  Leaf as PhosphorLeaf,
  Carrot as PhosphorCarrot,
  SoccerBall as PhosphorSoccerBall,
  Airplane as PhosphorAirplane,
  Lightbulb as PhosphorLightbulb,
  CheckCircle as PhosphorCheckCircle,
  PlusCircle as PhosphorPlusCircle,
  Flag as PhosphorFlag,
  Shuffle as PhosphorShuffle,
} from "@phosphor-icons/react";
import { IconSvg, PhosphorIconSvg, type LeafenIconProps } from "./iconPrimitives";

/**
 * The emoji picker's group bar and random button, as Notion's picker draws
 * them (seen 2026-10-07): a face, a leaf, a carrot, a soccer ball, an
 * airplane, a light bulb, a checked circle and a flag.
 */
export const SmileyIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorSmiley} />
);

export const NatureIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorLeaf} />
);

export const FoodIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorCarrot} />
);

export const ActivityIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorSoccerBall} />
);

export const TravelIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorAirplane} />
);

export const LightbulbIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorLightbulb} />
);

export const SymbolsIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorCheckCircle} />
);

export const FlagIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorFlag} />
);

export const ShuffleIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorShuffle} />
);

/** Adding a custom emoji: a filled circle with a plus. */
export const AddCircleFillIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorPlusCircle} weight="fill" />
);

/**
 * A workspace emoji that was deleted, in the text that used it: a dashed
 * square around a question mark, as Notion draws it (seen 2026-11-07).
 */
export const MissingEmojiIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="missing-emoji">
    <rect x="3.4" y="3.4" width="13.2" height="13.2" rx="2.6" strokeDasharray="1.7 2.2" />
    <path d="M8 8.2a2 2 0 1 1 2.9 1.8c-.6.3-.9.7-.9 1.3v.3" />
    <circle cx="10" cy="13.6" r=".5" fill="currentColor" stroke="none" />
  </IconSvg>
);
