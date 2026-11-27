import { useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Download } from "@/icons/leafen";
import { copyImageTold } from "@/lib/copyImage";
import { resolveTheme, useTheme } from "@/lib/theme";
import type { PopoverRect } from "@/lib/popoverPlacement";
import { ChartPopover, ChartRow } from "./ChartPanelMenus";
import { PhoneSettingsListContext } from "./PropertySettingRow";
import { useChartPublication, type ChartPublication } from "./chartViewRegistry";
import { CHART_BACKGROUNDS, chartAloneSvg, chartBackgroundSwatch, chartFileName, chartPicturePng, chartPictureSize, chartPictureSvg,
  saveChartPicture, type ChartBackground } from "./chartExport";
import styles from "./ChartSaveAs.module.css";
import chartStyles from "./chartView.module.css";

/**
 * "다른 이름으로 차트 저장…", as Notion's (seen 2026-10-05): a preview of the
 * picture, its background (the gradient first) and "PNG로 복사", "PNG
 * 다운로드" and "SVG 다운로드". The chart is drawn again, out of sight, at
 * the size its picture holds it for the background chosen (chartExport.ts),
 * and the preview and the files are made from that drawing, the swatches and
 * the picture in the app's theme. The preview stands on the menu itself, so
 * a see-through dark background shows over it as the saved PNG would.
 */
export function ChartSaveAs({ drawPicture }: { drawPicture: ChartPublication["drawPicture"] }) {
  const { t } = useTranslation("chartView");
  const [background, setBackground] = useState<ChartBackground>("gradient");
  const [preview, setPreview] = useState("");
  const [failed, setFailed] = useState(false);
  const stageRef = useRef<HTMLDivElement>(null);
  const theme = resolveTheme(useTheme()[0]);
  const size = chartPictureSize(background);
  const drawn: ReactNode = drawPicture(size);
  const picture = () => stageRef.current?.querySelector<SVGSVGElement>("svg[data-chart-svg]") ?? null;
  useEffect(() => {
    const svg = picture();
    setPreview(svg ? `data:image/svg+xml;charset=utf-8,${encodeURIComponent(chartPictureSvg(svg, background, theme))}` : "");
  }, [background, drawPicture, theme]);

  // Started at the press, as WebKit copies only what a press starts; told as a picture's copy is (lib/copyImage.ts).
  function copyPng() {
    const svg = picture();
    if (!svg) return;
    setFailed(false);
    void copyImageTold(chartPicturePng(svg, background, theme));
  }
  async function download(kind: "png" | "svg") {
    const svg = picture();
    if (!svg) return;
    try {
      const blob = kind === "png" ? await chartPicturePng(svg, background, theme)
        : new Blob([chartAloneSvg(svg)], { type: "image/svg+xml" });
      saveChartPicture(blob, chartFileName(t("pictureFileName"), kind));
      setFailed(false);
    } catch {
      setFailed(true);
    }
  }

  return <div className={styles.saveAs} data-chart-save-as>
    <div className={styles.part}>
      <span className={styles.name}>{t("preview")}</span>
      <div className={styles.preview}>
        {preview && <img src={preview} alt={t("preview")} data-chart-save-preview />}
      </div>
    </div>
    <div className={styles.part}>
      <span className={styles.name}>{t("background")}</span>
      <div className={styles.backgrounds} role="radiogroup" aria-label={t("background")}>
        {CHART_BACKGROUNDS.map((choice) => (
          <button key={choice} type="button" role="radio" aria-checked={background === choice} aria-label={t(`backgrounds.${choice}`)}
            title={t(`backgrounds.${choice}`)} className={styles.background} style={{ background: chartBackgroundSwatch(choice, theme) }}
            data-chart-background={choice} onClick={() => setBackground(choice)} />
        ))}
      </div>
    </div>
    <div className={styles.actions}>
      <button type="button" className={styles.primary} data-chart-save="copy-png" onClick={copyPng}>{t("copyPng")}</button>
      <button type="button" className={styles.button} data-chart-save="png" onClick={() => void download("png")}>{t("downloadPng")}</button>
      <button type="button" className={styles.button} data-chart-save="svg" onClick={() => void download("svg")}>{t("downloadSvg")}</button>
      {failed && <span className={styles.status} role="status" data-chart-save-status="failed">{t("pictureFailed")}</span>}
    </div>
    <div ref={stageRef} className={`${chartStyles.wrap} ${styles.stage}`} style={{ width: size.width }} aria-hidden="true"
      data-chart-save-stage>{drawn}</div>
  </div>;
}

/**
 * The panel's "다른 이름으로 차트 저장…" row, for a chart drawn on the screen;
 * a phone's settings sheet has none, as Notion's (seen 2026-11-18 on a Galaxy
 * A90 in Chrome at 411, in two tabs: its rows end at "차트에 대해 알아보기").
 */
export function ChartSaveAsRow({ viewId }: { viewId: string }) {
  const { t } = useTranslation("chartView");
  const publication = useChartPublication(viewId);
  const [anchor, setAnchor] = useState<PopoverRect | null>(null);
  const phone = useContext(PhoneSettingsListContext);
  if (!publication || phone) return null;
  return <>
    <ChartRow icon={<Download size="regular" />} label={t("saveAs")} marker="saveAs" expanded={Boolean(anchor)} onOpen={setAnchor} />
    {anchor && (
      <ChartPopover anchor={anchor} label={t("saveAs")} width={290} centered tall marker="saveAs" onClose={() => setAnchor(null)}>
        <ChartSaveAs drawPicture={publication.drawPicture} />
      </ChartPopover>
    )}
  </>;
}
