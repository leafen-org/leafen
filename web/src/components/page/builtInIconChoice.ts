import { useState } from "react";
import { personValue, storePersonValue } from "@/lib/browserStorage";
import { NATIVE_ICON_DEFAULT_COLOR, type NativeIconColor } from "@/nativeIconCatalog.generated";
import { recordRecentBuiltInIcon } from "./builtInIconRecent";
import { isNativeIconColor, nativeIconValue } from "./nativeIcons";

// The Icons tab's color, as Notion's: one color every icon picked takes, or,
// with "choose every time", a color chosen for each icon from a row of it in
// every color, while the colors themselves are off; turning it off goes back
// to the default color. Both are the person's, on this browser. A picker
// without colors (`colors` false: a property's, as Notion's — "제거" and
// "랜덤" alone, its icons "… gray", seen 2026-11-17 at 1440 in two tabs)
// draws and picks every icon in the default color, whatever the person chose
// elsewhere.

const COLOR_KEY = "leafen.builtInIconColor";
const EACH_TIME_KEY = "leafen.builtInIconChooseEachTime";

function stored(key: string): string | null {
  return personValue(key, { previous: [key] });
}

function store(key: string, value: string) {
  storePersonValue(key, value);
}

// The tab's random button, where the picker offers a random pick of its own
// (`shuffle`), puts its icon in without it joining "최근 항목", as Notion's
// (seen 2026-11-17 in Chrome at 1440, two presses: the page icon changed and
// the tab's recent ones stayed as they were); elsewhere, and while a color is
// chosen for each icon, it is picked as any.

export function useBuiltInIconChoice(pick: (value: string) => void, colors = true, shuffle?: (value: string) => void) {
  const [chosenColor, setColor] = useState<NativeIconColor>(() => {
    const value = stored(COLOR_KEY);
    return isNativeIconColor(value) ? value : NATIVE_ICON_DEFAULT_COLOR;
  });
  const [chosenEachTime, setEachTime] = useState(() => stored(EACH_TIME_KEY) === "1");
  const color = colors ? chosenColor : NATIVE_ICON_DEFAULT_COLOR;
  const eachTime = colors && chosenEachTime;
  // The icon whose row of colors is open.
  const [choosing, setChoosing] = useState<string | null>(null);

  function changeColor(next: NativeIconColor) {
    setColor(next);
    store(COLOR_KEY, next);
  }

  function pickIcon(name: string) {
    if (eachTime) {
      setChoosing(name);
      return;
    }
    recordRecentBuiltInIcon(name);
    pick(nativeIconValue({ name, color }));
  }

  return {
    /** Whether the picker offers its colors (its ● button), else its icons the default color. */
    colors,
    color,
    eachTime,
    choosing,
    changeColor,
    changeEachTime(next: boolean) {
      setEachTime(next);
      store(EACH_TIME_KEY, next ? "1" : "0");
      setChoosing(null);
      if (!next) changeColor(NATIVE_ICON_DEFAULT_COLOR);
    },
    pickIcon,
    /** The random button's icon. */
    shuffleIcon(name: string) {
      if (shuffle && !eachTime) shuffle(nativeIconValue({ name, color }));
      else pickIcon(name);
    },
    pickColored(name: string, chosen: NativeIconColor) {
      setChoosing(null);
      recordRecentBuiltInIcon(name);
      pick(nativeIconValue({ name, color: chosen }));
    },
    cancel() {
      setChoosing(null);
    },
  };
}
