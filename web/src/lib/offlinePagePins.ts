import { localFirstProductCache } from "./localFirstProductCache";
import { createOfflinePagePinService } from "./offlinePagePinService";

export const offlinePagePinService = createOfflinePagePinService(
  localFirstProductCache.pins,
);
