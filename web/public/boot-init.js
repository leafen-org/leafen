/* global document, localStorage */

(() => {
  let warm = false;
  try {
    warm = Boolean(localStorage.getItem("leafen:v2:session"));
  } catch {
    warm = false;
  }
  document.documentElement.dataset.leafenV2Warm = warm ? "true" : "false";
})();
