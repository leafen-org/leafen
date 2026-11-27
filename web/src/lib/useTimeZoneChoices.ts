import { useEffect, useMemo, useState } from "react";
import { i18next } from "@/i18n";
import { afterNextPaint, inSlices } from "@/lib/afterPress";
import { timeZoneChoice, timeZoneChoices, type TimeZoneChoice } from "@/lib/timeZones";
import { loadZoneCities } from "@/components/database/dateTimeZones";

/**
 * The list "시간대" shows for `current`: that zone alone as the section
 * draws, then, while the list can open (`open`), every zone (timeZones.ts
 * timeZoneChoices), its language's places loaded, read once the
 * frame after has painted, in slices a press goes ahead of (afterPress.ts).
 * Reading them all as the section drew held the "기본 설정" press some
 * 90 ms before its paint (2026-11-14, CPU ×2): each zone's offset needs a
 * formatter of its own. A list the automatic switch keeps shut reads none;
 * the offsets are kept (timeZoneOffset), so a list drawn again soon fills
 * at once.
 */
export function useTimeZoneChoices(current: string, open: boolean): TimeZoneChoice[] {
  const language = i18next.language;
  const [filled, setFilled] = useState<{ current: string; language: string; list: TimeZoneChoice[] } | null>(null);
  useEffect(() => {
    if (!open) return;
    const controller = new AbortController();
    const list: TimeZoneChoice[] = [];
    let cancel = () => {};
    // The places a language has no table of come from its CLDR one, as the
    // date window's (timeZones.ts timeZonePlace).
    void loadZoneCities(language).then((cities) => {
      if (controller.signal.aborted) return;
      cancel = afterNextPaint(() => {
        void inSlices(timeZoneChoices(current, cities), (zone) => { list.push(timeZoneChoice(zone, cities)); }, { signal: controller.signal })
          .then((done) => { if (done) setFilled({ current, language, list }); });
      }, controller.signal);
    });
    return () => {
      controller.abort();
      cancel();
    };
  }, [current, language, open]);
  return useMemo(() => filled && filled.current === current && filled.language === language
    ? filled.list : [timeZoneChoice(current)], [filled, current, language]);
}
