import styles from "./WorkspaceMembershipRequest.module.css";

// What being a member brings, drawn: the workspace's window with its
// sidebar and a page being written (a title, lines, a ticked task), a new
// page card slipping in on the left, a spark at the top right, and its
// people at the foot with a dashed place for the one asking. Leafen's own
// drawing in the place, size and role of Notion's picture over the title
// of its guest window (Notion's 334×205, 24 in, seen 2026-11-12).
// Its lines take the text color and its marks the same fills in both themes.

const BLUE = "rgb(39, 131, 222)";
const YELLOW = "rgb(232, 178, 58)";
const GREEN = "rgb(68, 163, 123)";
const RED = "rgb(226, 85, 72)";

function Line({ x, y, width, strong = false }: { x: number; y: number; width: number; strong?: boolean }) {
  return <rect x={x} y={y} width={width} height={strong ? 8 : 4} rx={strong ? 4 : 2} fill="currentColor" opacity={strong ? 1 : 0.35} />;
}

/** The picture over the title of a guest's window asking to become a member. */
export function MembershipAskPicture() {
  return (
    <svg className={styles.picture} width={334} height={205} viewBox="0 0 334 205" aria-hidden="true" focusable="false"
      data-membership-ask-picture>
      {/* The workspace's window. */}
      <rect x={66} y={18} width={214} height={150} rx={10} className={styles.pictureSurface} stroke="currentColor" strokeWidth={1.5} />
      {[78, 87, 96].map((cx) => <circle key={cx} cx={cx} cy={30} r={2.5} fill="currentColor" opacity={0.35} />)}
      <line x1={114} y1={40} x2={114} y2={168} stroke="currentColor" strokeWidth={1} opacity={0.35} />
      {/* Its sidebar, the open page lit. */}
      <rect x={74} y={48} width={32} height={7} rx={3.5} fill={BLUE} />
      {[62, 74, 86, 98].map((y, row) => <Line key={y} x={76} y={y} width={[26, 20, 24, 16][row] ?? 20} />)}
      {/* The page being written. */}
      <Line x={128} y={50} width={86} strong />
      <Line x={128} y={68} width={136} />
      <Line x={128} y={78} width={118} />
      <Line x={128} y={88} width={128} />
      <rect x={128} y={104} width={11} height={11} rx={3} fill={BLUE} />
      <path d="M130.5 109.6l2.4 2.4 4-4.6" fill="none" stroke="white" strokeWidth={1.6} strokeLinecap="round" strokeLinejoin="round" />
      <Line x={146} y={107.5} width={70} />
      <rect x={128.75} y={121.75} width={9.5} height={9.5} rx={2.5} fill="none" stroke="currentColor" strokeWidth={1.5} opacity={0.6} />
      <Line x={146} y={124.5} width={54} />
      {/* A new page slipping in. */}
      <g transform="rotate(-9 46 112)">
        <rect x={18} y={78} width={56} height={70} rx={7} className={styles.pictureSurface} stroke="currentColor" strokeWidth={1.5} />
        <rect x={27} y={88} width={12} height={12} rx={3} fill={YELLOW} />
        <Line x={27} y={108} width={36} />
        <Line x={27} y={117} width={28} />
        <Line x={27} y={126} width={32} />
      </g>
      {/* A spark for the newest features. */}
      <circle cx={286} cy={26} r={17} fill={RED} />
      <path d="M288.5 15.5l-8.5 12h6.5l-2.5 9.5 8.5-12h-6.5z" fill="white" />
      {/* Its people, and a place for the one asking. */}
      {[[206, BLUE], [228, YELLOW], [250, GREEN]].map(([cx, fill]) => (
        <circle key={cx} cx={cx as number} cy={178} r={15} fill={fill as string} className={styles.pictureRing} strokeWidth={3} />
      ))}
      <circle cx={276} cy={178} r={13.5} className={styles.pictureSurface} stroke="currentColor" strokeWidth={1.5} strokeDasharray="4 3" />
      <path d="M276 172v12M270 178h12" stroke="currentColor" strokeWidth={1.6} strokeLinecap="round" />
    </svg>
  );
}
