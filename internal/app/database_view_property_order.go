package app

import (
	"slices"
	"unicode/utf16"
)

// A view keeps no order of its properties until one is set through it, and
// lists its title first and the rest by name meanwhile; once set, the listed
// ones keep their places and those made since follow them by name, as
// Notion's views of every layout do (seen 2026-11-12). The screen's rule is
// web/src/components/database/viewPropertyOrder.ts.

// databaseViewPropertyNamesCompare orders two property names as Notion's
// views do: by their UTF-16 code units, case and accents as they are, so
// "10" < "2a" < "9" < "Banana" < "_x" < "apple" < "Éclair" < "가" < "😀" <
// "Ａ" (seen 2026-11-12).
func databaseViewPropertyNamesCompare(left, right string) int {
	a, b := utf16.Encode([]rune(left)), utf16.Encode([]rune(right))
	return slices.Compare(a, b)
}

// databaseViewDefaultOrder sorts properties as a view without an order of its
// own lists them: the title first, then by name, those of one name as they
// came.
func databaseViewDefaultOrder[Property any](properties []Property, name func(Property) string, title func(Property) bool) []Property {
	sorted := slices.Clone(properties)
	slices.SortStableFunc(sorted, func(left, right Property) int {
		if leftTitle, rightTitle := title(left), title(right); leftTitle != rightTitle {
			if leftTitle {
				return -1
			}
			return 1
		}
		return databaseViewPropertyNamesCompare(name(left), name(right))
	})
	return sorted
}

// viewOrdered is the scope's properties in a view's default order.
func (scope notionViewFilterScope) viewOrdered() []notionViewProperty {
	return databaseViewDefaultOrder(scope.ordered,
		func(property notionViewProperty) string { return property.Name },
		func(property notionViewProperty) bool { return property.OfficialType == notionTitlePropertyID })
}
