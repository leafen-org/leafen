import type { ReactNode } from "react";
import type { ViewLinkScreen } from "@/lib/viewLinkScreen";
import styles from "./ViewLinkView.module.css";

/**
 * The window around a view link's screen in a wider browser, the frame of
 * that screen's size in the middle of it: drawn alike while the link is read
 * (SharedPageLoading) and once it is (ViewLinkView), so the page doesn't move
 * from the whole window into the frame.
 */
export function ViewLinkFrameHost({ screen, children }: { screen: ViewLinkScreen; children: ReactNode }) {
  return <div className={styles.host} data-view-link={screen.viewport}>{children}</div>;
}
