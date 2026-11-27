/**
 * A file let go where nothing takes it stays out of the browser, which would
 * otherwise leave the app to open it (seen 2026-11-05). Places that take files
 * have already handled the event by the time it reaches the window.
 */
export function keepDroppedFilesInApp() {
  const carriesFiles = (event: DragEvent) => Array.from(event.dataTransfer?.types ?? []).includes("Files");
  const fileField = (target: EventTarget | null) => target instanceof HTMLInputElement && target.type === "file";
  window.addEventListener("dragover", (event) => {
    if (!carriesFiles(event) || event.defaultPrevented || fileField(event.target)) return;
    event.preventDefault();
    if (event.dataTransfer) event.dataTransfer.dropEffect = "none";
  });
  window.addEventListener("drop", (event) => {
    if (carriesFiles(event) && !fileField(event.target)) event.preventDefault();
  });
}
