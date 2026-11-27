// The type options of i18next 27 for Leafen's words, as its runtime options
// in index.ts are. The catalogs load at run time and are not typed, so a
// t() call's options are what the call passes: it may leave them out or pass
// none (undefined), as before 27 made the interpolation check strict; and an
// empty line is a translation (returnEmptyString in index.ts).
import "i18next";

declare module "i18next" {
  interface CustomTypeOptions {
    strictInterpolationChecks: false;
    returnEmptyString: true;
  }
}
