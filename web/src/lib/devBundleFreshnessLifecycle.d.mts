export interface BundleCheckResponse {
  ok: boolean;
  status: number;
  text(): Promise<string>;
}

export interface BundleCheckLifecycle {
  attempts: number;
  response: BundleCheckResponse;
  statuses: number[];
}

export interface BundleCheckWorkerClient {
  dispose(): void;
  request(url: string): Promise<BundleCheckResponse>;
}

export function runBundleCheckLifecycle(input: {
  request(url: string): Promise<BundleCheckResponse>;
  url: string;
}): Promise<BundleCheckLifecycle>;

export function createBundleCheckWorkerClient(): BundleCheckWorkerClient;
