package app

import (
	"encoding/json"
	"strings"
)

// The database templates of the screen's new-database panel, said beside
// the route the screen makes them with (POST /pages/{pageId}/database-starter,
// a key's route too), so an agent makes one with the same result
// (2026-11-22, the user: everything a person can do, an agent does to the
// end; before, an agent heard to make a meeting notes database "from the
// meetings template on the screen"). The templates and what each offers to
// choose are read from the catalog the screen's are; those the panel
// doesn't offer (hidden) are left out.

const databaseStarterOperation = "POST /pages/{pageId}/database-starter"

type databaseStarterChoiceNamed struct {
	Key       string `json:"key"`
	DefaultOn bool   `json:"defaultOn"`
}

func init() {
	raw, err := databaseStarterFiles.ReadFile("database_starter_templates.json")
	catalog := struct {
		Templates []struct {
			Key         string                       `json:"key"`
			Name        string                       `json:"name"`
			Hidden      bool                         `json:"hidden"`
			Features    []databaseStarterChoiceNamed `json:"features"`
			ViewChoices []databaseStarterChoiceNamed `json:"viewChoices"`
		} `json:"templates"`
	}{}
	if err != nil || json.Unmarshal(raw, &catalog) != nil {
		return
	}
	named := func(choices []databaseStarterChoiceNamed) string {
		keys := make([]string, 0, len(choices))
		for _, choice := range choices {
			if choice.DefaultOn {
				choice.Key += "*"
			}
			keys = append(keys, choice.Key)
		}
		return strings.Join(keys, ", ")
	}
	templates := []string{}
	for _, template := range catalog.Templates {
		if template.Hidden {
			continue
		}
		line := template.Key + " (" + template.Name + ")"
		if len(template.Features) > 0 {
			line += " — features " + named(template.Features)
		}
		templates = append(templates, line+" — views "+named(template.ViewChoices))
	}
	nativeOperationNotes[nativeOperationShape(databaseStarterOperation)] = "Makes a database from one of the templates the screen's " +
		"new-database panel offers, with the same result: the page — an empty one, such as one notion-create-pages made with no " +
		"content, where the database goes — becomes it, with the template's properties, views, example rows and icon. Body: " +
		"templateId, features and views (its keys below, at least one view; * marks what the " +
		"panel has on), language (the person's, such as ko or en: the template's words), basePageCursor (the page's cursor, " +
		"from GET /pages/{pageId}), baseGraphRevision (0 for a page just made) and mutationId, an ID of yours that makes it once if sent again (left out, the server makes one). Templates: " + strings.Join(templates, "; ") + "."
}
