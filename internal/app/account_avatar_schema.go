package app

// avatarIconPlaces says, for giving out icons, each person's first
// workspace, the people of each workspace and the icon each person shows.
type avatarIconPlaces struct {
	first  map[string]string
	people map[string][]string
	shown  map[string]string
}

// counts counts the icons the others of an account's first workspace show,
// or of the whole server for someone in none.
func (places avatarIconPlaces) counts(accountID string) map[string]int {
	counts := map[string]int{}
	if workspaceID, ok := places.first[accountID]; ok {
		for _, person := range places.people[workspaceID] {
			if person != accountID && places.shown[person] != "" {
				counts[places.shown[person]]++
			}
		}
		return counts
	}
	for person, icon := range places.shown {
		if person != accountID {
			counts[icon]++
		}
	}
	return counts
}
