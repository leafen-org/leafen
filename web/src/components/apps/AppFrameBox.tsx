import { useState, type CSSProperties, type ReactNode } from "react";
import styles from "./AppFrameBox.module.css";

// What a person sees while an app opens: nothing for the first second (most
// open sooner), then one line that stays until the app shows, as Leafen's own
// loading does. The frame itself
// stays unseen until it has Leafen's theme (useAppFrameReveal), and before this
// a busy machine left the area blank for ten seconds after "앱을 여는 중…" went
// (drive, vault and web clip screens, 2026-11-06).
const QUIET_MS = 1000;
const FADE_MS = 140;

/** The waiting line, timed from when the load began, so the line the frame's
 * box draws after the loading state's line carries on instead of starting over. */
export function AppWaitingLine({ since, label, className, over }: {
  since: number;
  label: string;
  className?: string;
  over?: boolean;
}) {
  const [left] = useState(() => Math.round(since + QUIET_MS - performance.now()));
  const shown = left + FADE_MS <= 0;
  return (
    <div role="status" className={[className, over ? styles.over : "", styles.reveal].filter(Boolean).join(" ")}
      style={shown ? undefined : { "--app-wait-delay": `${Math.max(0, left)}ms` } as CSSProperties}
      data-shown={shown ? "" : undefined} data-app-frame-waiting>
      {label}
    </div>
  );
}

/** An app's frame in a box that says the app is opening until the frame shows. */
export function AppFrameBox({ waiting, since, label, lineClassName, className, style, children }: {
  waiting: boolean;
  since: number;
  label: string;
  lineClassName?: string;
  className?: string;
  style?: CSSProperties;
  children: ReactNode;
}) {
  return (
    <div className={[styles.box, className].filter(Boolean).join(" ")} style={style}>
      {children}
      {waiting ? <AppWaitingLine since={since} label={label} className={lineClassName} over /> : null}
    </div>
  );
}
