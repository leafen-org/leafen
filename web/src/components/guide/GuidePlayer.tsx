import { useEffect, useMemo, useState, type CSSProperties, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { ChevronLeft, ChevronRight, PauseIcon, PlayIcon } from "../icons";
import styles from "./GuidePlayer.module.css";

/** One scene of a moving screen guide. */
export type GuideScene = {
  /** Its heading under the screen; the player puts the scene's number before it. */
  title: string;
  /** What the person does in it, under the heading. */
  caption: string;
  /**
   * The drawn screen, laid inside the player's fixed frame (304px tall, 276px
   * on a phone; its width is the guide's). Scenes draw another app's screen
   * with CSS, may lay out by the frame's width with @container, and use the
   * player's parts from ./GuideParts for the window bar, the cursor and its
   * clicks, and the typing caret.
   */
  picture: ReactNode;
  /** How long the scene plays before the next one; 3 seconds when left out. */
  durationMs?: number;
};

const SCENE_MS = 3000;

function prefersReducedMotion(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-reduced-motion: reduce)").matches
  );
}

/**
 * Plays a moving screen guide: a guide passes its scenes, the name of the
 * region and one line saying what it shows, and the player shows one scene at
 * a time in a fixed-size frame with the step dots above, and the scene's
 * heading, "Step m of n" and previous / play-pause / next below. It reads as
 * an example, not as the screen itself (the one-thing-per-screen decision of
 * 2026-11-09): an "Example" mark and the line head a sheet of its own look,
 * and nothing drawn in the frame can be pointed at, focused or pressed — the
 * button to press stands outside it. It plays on its own; people who ask for
 * reduced motion get it paused and without the cursor, and scenes show what
 * their animations end on (a scene's own CSS does that for its parts).
 */
export function GuidePlayer({ label, shows, scenes }: {
  label: string;
  /** What the example shows, said beside its "Example" mark. */
  shows: string;
  scenes: readonly GuideScene[];
}) {
  const { t } = useTranslation("guidePlayer");
  const reducedMotion = useMemo(prefersReducedMotion, []);
  const count = scenes.length;
  const [scene, setScene] = useState(0);
  const [playing, setPlaying] = useState(!reducedMotion);
  // Remount the active scene each time it (re)activates so its animations
  // restart from the beginning.
  const [sceneRun, setSceneRun] = useState(0);
  const shown = Math.min(scene, Math.max(0, count - 1));
  const active = scenes[shown];
  const duration = active?.durationMs ?? SCENE_MS;

  useEffect(() => {
    if (!playing || count === 0) return;
    const timer = window.setTimeout(() => {
      setScene((current) => (current + 1) % count);
      setSceneRun((run) => run + 1);
    }, duration);
    return () => window.clearTimeout(timer);
  }, [playing, scene, sceneRun, count, duration]);

  const goTo = (next: number) => {
    setScene(((next % count) + count) % count);
    setSceneRun((run) => run + 1);
  };

  if (!active) return null;
  return (
    <div className={styles.guide}>
      <section
        className={styles.player}
        aria-label={label}
        data-walkthrough=""
        data-guide-example=""
        data-playing={playing ? "true" : undefined}
      >
        <p className={styles.exampleHead}>
          <span className={styles.exampleMark}>{t("guidePlayer:example")}</span>
          <span className={styles.exampleShows}>{shows}</span>
        </p>
        <div className={styles.progressRow} role="navigation" aria-label={label}>
          {scenes.map((entry, index) => (
            <button
              key={index}
              type="button"
              className={styles.progressSegment}
              aria-label={`${index + 1}. ${entry.title}`}
              aria-current={index === shown ? "step" : undefined}
              data-state={index < shown ? "done" : index === shown ? "active" : undefined}
              style={
                index === shown
                  ? ({ "--guide-scene-ms": `${entry.durationMs ?? SCENE_MS}ms` } as CSSProperties)
                  : undefined
              }
              onClick={() => goTo(index)}
            >
              <span />
            </button>
          ))}
        </div>
        <div className={styles.stage} key={`${shown}-${sceneRun}`}>
          {/* A picture of a screen: nothing in it takes the pointer or the focus. */}
          <div className={styles.recording} data-guide-screen="" inert>
            {active.picture}
          </div>
        </div>
        <div className={styles.captionRow}>
          <p className={styles.caption}>
            <strong>
              {shown + 1}. {active.title}
            </strong>
            <span>{active.caption}</span>
          </p>
          <div className={styles.controls}>
            <span className={styles.stepCounter}>
              {t("guidePlayer:stepLabel", { n: shown + 1, total: count })}
            </span>
            <button type="button" aria-label={t("guidePlayer:prev")} onClick={() => goTo(shown - 1)}>
              <ChevronLeft size="small" aria-hidden="true" />
            </button>
            <button
              type="button"
              aria-label={playing ? t("guidePlayer:pause") : t("guidePlayer:play")}
              onClick={() => setPlaying((current) => !current)}
            >
              {playing ? <PauseIcon size="tiny" aria-hidden="true" /> : <PlayIcon size="tiny" aria-hidden="true" />}
            </button>
            <button type="button" aria-label={t("guidePlayer:next")} onClick={() => goTo(shown + 1)}>
              <ChevronRight size="small" aria-hidden="true" />
            </button>
          </div>
        </div>
      </section>
    </div>
  );
}
