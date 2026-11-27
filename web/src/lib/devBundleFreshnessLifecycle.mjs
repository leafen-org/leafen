import { runBundleCheckLifecycle } from "./devBundleFreshnessRetryLifecycle.mjs";
export { runBundleCheckLifecycle };

const MAX_ACTIVE_BUNDLE_CHECK_WORKERS = 1;

const workerSource = `
self.onmessage = async (event) => {
  try {
    const response = await fetch(event.data.url, {
      cache: "no-store",
      headers: { Accept: "text/html" },
    });
    const text = await response.text();
    self.postMessage({ kind: "response", ok: response.ok, status: response.status, text });
  } catch (error) {
    self.postMessage({
      kind: "error",
      message: error instanceof Error ? error.message : String(error),
    });
  }
};
`;

export function createBundleCheckWorkerClient() {
  const sourceUrl = globalThis.URL.createObjectURL(
    new globalThis.Blob([workerSource], { type: 'text/javascript' }),
  );
  const activeWorkers = new Set();
  let disposed = false;

  const request = (url) => {
    if (disposed) return Promise.reject(new Error('bundle-check-worker-disposed'));
    if (activeWorkers.size >= MAX_ACTIVE_BUNDLE_CHECK_WORKERS) {
      return Promise.reject(new Error('bundle-check-worker-capacity-exceeded'));
    }
    return new Promise((resolve, reject) => {
      const worker = new globalThis.Worker(sourceUrl);
      activeWorkers.add(worker);
      const finish = () => {
        activeWorkers.delete(worker);
        worker.terminate();
      };
      worker.onmessage = (event) => {
        const result = event.data;
        finish();
        if (result?.kind !== 'response') {
          reject(new Error(result?.message || 'bundle-check-worker-failed'));
          return;
        }
        resolve({
          ok: result.ok === true,
          status: result.status,
          text: async () => result.text,
        });
      };
      worker.onerror = (event) => {
        event.preventDefault();
        finish();
        reject(new Error(event.message || 'bundle-check-worker-failed'));
      };
      worker.postMessage({ url });
    });
  };

  const dispose = () => {
    if (disposed) return;
    disposed = true;
    for (const worker of activeWorkers) worker.terminate();
    activeWorkers.clear();
    globalThis.URL.revokeObjectURL(sourceUrl);
  };

  return { dispose, request };
}
