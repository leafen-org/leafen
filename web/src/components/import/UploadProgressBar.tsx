import styles from "./UploadProgressBar.module.css";

export type UploadProgress = {
  phase: "preparing" | "uploading";
  percent: number;
};

function boundedProgressPercent(progress: UploadProgress) {
  return Math.max(0, Math.min(100, Math.round(progress.percent)));
}

export function UploadProgressValue({ progress }: { progress: UploadProgress }) {
  if (progress.phase === "preparing") return null;
  return <span>{boundedProgressPercent(progress)}%</span>;
}

export function UploadProgressBar({
  className,
  label,
  progress,
}: {
  className?: string;
  label: string;
  progress: UploadProgress;
}) {
  const preparing = progress.phase === "preparing";
  const percent = boundedProgressPercent(progress);
  const state = preparing ? "indeterminate" : "determinate";

  return (
    <div
      className={[styles.track, className].filter(Boolean).join(" ")}
      role="progressbar"
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={preparing ? undefined : percent}
      aria-valuetext={preparing ? label : undefined}
      data-upload-progress-state={state}
    >
      <span style={{ width: preparing ? "32%" : `${percent}%` }} />
    </div>
  );
}
