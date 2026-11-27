import { PageIconGlyph, PeopleDatabaseIcon } from "@/components/page/PageIcon";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { directoryPeople, LEAFEN_SYSTEM_USER_ID, personName } from "@/lib/peopleDirectory";
import type { Page } from "@/types";
import { peopleDatabaseHit } from "./searchPeopleHits";

/**
 * A search result's icon, as Notion's search draws People's (seen 2026-11-16
 * at 1440 in a free workspace, twice): People's database its own icon,
 * Notion's person in a ring in gray, 19.2px in the 20px slot
 * (PeopleDatabaseIcon; seen again 2026-11-19), a person of People their
 * picture or, without one, their letter on a 20px circle (PersonAvatar,
 * Notion's 11px letter); any other page its own icon.
 */
export function SearchHitIcon({ page, row }: { page: Page; row?: unknown }) {
  if (peopleDatabaseHit(page)) return <PeopleDatabaseIcon pixels={19.2} />;
  if (row && page.createdBy === LEAFEN_SYSTEM_USER_ID) {
    // A person's row is named for them; the one person of that name gives their picture.
    const title = page.title.trim();
    const named = directoryPeople().filter((person) => personName(person.userId).trim() === title);
    return <PersonAvatar userId={named.length === 1 ? named[0]!.userId : undefined} name={title} size={20} />;
  }
  return <PageIconGlyph page={page} size={18} />;
}
