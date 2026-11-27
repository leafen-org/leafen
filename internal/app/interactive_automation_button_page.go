package app

// A page button's formulas and mentions read the page it is on: Notion's
// "이 페이지" — "이 버튼이 포함된 페이지" (seen 2026-10-25) — a page by its
// name, whose own fields follow its "." as a row's do: "Page Name" its
// title, then who made it and when, who edited it last and when
// (databaseFormulaPageFields).

const interactiveAutomationButtonPageName = "Page Name"

// buttonPage is the page a page button's run began on, as a page reference.
func (formulas *interactiveAutomationFormulas) buttonPage() (databaseFormulaReference, bool) {
	page := formulas.authorization.Page
	if formulas.source.Kind != interactiveAutomationSourcePageButton || page == nil {
		return databaseFormulaReference{}, false
	}
	return databaseFormulaReference{Kind: "page", ID: page.Id, Name: databaseFormulaTrim(page.GetString("title"))}, true
}

// buttonPageField is buttonPage().prop(name), as the page is while the run reads it.
func (formulas *interactiveAutomationFormulas) buttonPageField(reference databaseFormulaReference, name string) (any, error) {
	page := formulas.authorization.Page
	if page == nil || reference.ID != page.Id {
		return "", nil
	}
	person := func(field string) any {
		id := page.GetString(field)
		if id == "" {
			return nil
		}
		return newDatabaseFormulaPeople(formulas.app, nil).personReference(id, "")
	}
	switch name {
	case interactiveAutomationButtonPageName:
		return databaseFormulaTrim(page.GetString("title")), nil
	case "Created By":
		return person("created_by"), nil
	case "Last Edited By":
		return person("last_edited_by"), nil
	case "Created Time":
		return databaseFormulaInstant(page.GetDateTime("created").Time().UnixMilli()), nil
	case "Last Edited Time":
		return databaseFormulaInstant(page.GetDateTime("updated").Time().UnixMilli()), nil
	}
	return "", nil
}
