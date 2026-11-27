import { useId } from "react";
import { AVATAR_ICONS } from "@/avatarIcons.generated";
import { safePresentationImage } from "@/components/page/pagePresentation";
import { avatarIconName, avatarKind } from "@/components/people/PersonAvatar";
import { personAvatarValue, personIsBot, personName } from "@/lib/peopleDirectory";
import { serverPath } from "@/lib/serverOrigin";
import { usePeopleDirectoryRevision } from "@/lib/workspacePeopleDirectory";
import chartStyles from "./chartView.module.css";

/** How much more room under the plot a row of faces takes than a row of names (Notion's, seen 2026-11-12: 670 for 675). */
export const CHART_FACES_TALLER = 5;

/**
 * A person's letter on a chart's face: a connection's bot's first character as
 * its name has it, a person's first letter in capitals (Notion's, seen
 * 2026-11-12: "h" for "hermes agent", "테", "C").
 */
function faceLetter(id: string) {
  const name = personName(id).trim();
  const first = Array.from(name)[0] ?? "";
  if (!first) return "?";
  return personIsBot(id) ? first : first.toUpperCase();
}

/**
 * The people a chart's X axis goes by, each as their face under their group in
 * place of their name, as Notion's chart draws them (seen 2026-11-12, 1440
 * dark): a 20 circle on the element background inside a 1 border, its middle
 * under the group's, its top 8 under the plot, holding the picture or the
 * first letter at 10 in the secondary letters' color; the group with no one a
 * "?" on the same circle. Bots are circles there too. The groups' names stay
 * in their tips.
 */
export function ChartPersonFaces({ faces, top }: {
  faces: ReadonlyArray<{ key: string; x: number; empty: boolean }>;
  top: number;
}) {
  usePeopleDirectoryRevision();
  const clip = `chart-face-${useId().replace(/:/g, "")}`;
  return (
    <g>
      <defs>
        <clipPath id={clip} clipPathUnits="objectBoundingBox"><circle cx={0.5} cy={0.5} r={0.5} /></clipPath>
      </defs>
      {faces.map(({ key, x, empty }) => {
        const cx = Math.round(x), cy = Math.round(top) + 10;
        const value = empty ? "" : personAvatarValue(key);
        const kind = avatarKind(value);
        const icon = avatarIconName(value);
        const image = kind === "image" ? safePresentationImage(value) : "";
        let inside;
        if (icon) {
          inside = (
            <svg x={cx - 7.5} y={cy - 7.5} width={15} height={15} viewBox="0 0 256 256" className={chartStyles.faceLetter}>
              {AVATAR_ICONS[icon].regular.map((d) => <path key={d} d={d} />)}
            </svg>
          );
        } else if (image) {
          inside = <image href={image.startsWith("/api/") ? serverPath(image) : image} x={cx - 9} y={cy - 9} width={18} height={18}
            preserveAspectRatio="xMidYMid slice" clipPath={`url(#${clip})`} />;
        } else {
          inside = (
            <text className={chartStyles.faceLetter} x={cx} y={cy} textAnchor="middle" dominantBaseline="central"
              style={kind === "emoji" ? { fontSize: 12 } : undefined}>
              {empty ? "?" : kind === "emoji" ? value : faceLetter(key)}
            </text>
          );
        }
        return (
          <g key={key} data-chart-face={key} role="img" aria-label={empty ? "?" : personName(key)}>
            <circle className={chartStyles.face} cx={cx} cy={cy} r={9.5} />
            {inside}
          </g>
        );
      })}
    </g>
  );
}
