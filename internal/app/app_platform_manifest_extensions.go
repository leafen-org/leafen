package app

import (
	"regexp"
	"unicode/utf8"
)

// appPropertyEditorTypes are the property types an app may draw and enter;
// the value keeps its type, so turning the app off shows it as before.
var appPropertyEditorTypes = []string{
	graphValueTypeRichText, graphValueTypeNumber, graphValueTypeSelect, graphValueTypeStatus, graphValueTypeMultiSelect,
	graphValueTypeCheckbox, graphValueTypeURL, graphValueTypeEmail, graphValueTypePhone,
}

// A file opener names files by extension or by media type.
var appFileOpenerTypePattern = regexp.MustCompile(`^(\.[a-z0-9]{1,10}|[a-z]+/[a-z0-9][a-z0-9.+-]{0,80})$`)

// A new file is of one of its opener's extensions.
var appNewFileTypePattern = regexp.MustCompile(`^\.[a-z0-9]{1,10}$`)

const appNewFilesPerOpener = 10

// appFileOpenerProblems checks the files an app opens: each opener has a
// name, 1-20 types and its screen's file, and the blank files it starts from
// are of its extensions, under templates/, for a screen that edits.
func appFileOpenerProblems(manifest appManifest, keys func(string, []string) map[string]bool, add func(string, ...any),
	file func(string, string, string)) {
	openerKeys := []string{}
	for _, opener := range manifest.Surfaces.FileOpeners {
		openerKeys = append(openerKeys, opener.Key)
		appTextProblems(opener.Name, "file opener name", appNameMaxRunes, true, add)
		file("file opener entry", opener.Entry, "ui/")
		if opener.Edit && (manifest.PersonAccess == "" || manifest.PersonAccess == "read") {
			add("file opener %q edits files, which takes personAccess openedFile, content or more", opener.Key)
		}
		if len(opener.Types) == 0 || len(opener.Types) > 20 {
			add("file opener %q names 1-20 types", opener.Key)
		}
		for _, kind := range opener.Types {
			if !appFileOpenerTypePattern.MatchString(kind) {
				add("file opener %q type %q is an extension such as .docx or a type such as application/pdf", opener.Key, kind)
			}
		}
		if len(opener.NewFiles) > 0 && !opener.Edit {
			add("file opener %q starts new files, which takes edit", opener.Key)
		}
		if len(opener.NewFiles) > appNewFilesPerOpener {
			add("file opener %q starts at most %d kinds of new file", opener.Key, appNewFilesPerOpener)
		}
		madeTypes := map[string]bool{}
		for _, made := range opener.NewFiles {
			appTextProblems(made.Name, "new file name", appNameMaxRunes, true, add)
			file("new file template", made.Template, "templates/")
			if !appNewFileTypePattern.MatchString(made.Type) || !appOneOf(made.Type, opener.Types) || madeTypes[made.Type] {
				add("file opener %q new file type %q is one of its extensions such as .docx, once", opener.Key, made.Type)
			}
			madeTypes[made.Type] = true
		}
	}
	keys("file opener", openerKeys)
	if len(manifest.Surfaces.ImportSources) > appImportSourcesPerApp {
		add("an app moves content from at most %d tools", appImportSourcesPerApp)
	}
	sourceKeys := []string{}
	for _, source := range manifest.Surfaces.ImportSources {
		sourceKeys = append(sourceKeys, source.Key)
		appTextProblems(source.Name, "import source name", appNameMaxRunes, true, add)
		appTextProblems(source.Description, "import source description", appImportSourceDescriptionRunes, true, add)
		file("import source entry", source.Entry, "ui/")
	}
	keys("import source", sourceKeys)
}

const (
	appImportSourcesPerApp          = 8
	appImportSourceDescriptionRunes = 80
)

// appExtensionProblems checks the lines an app adds to Leafen's own menus
// (docs/app-platform.md 10-1): each "/" item has a name, up to 8 words to be
// found by, and either one of the app's actions or one of its blocks; each
// menu line has a name, the menus it goes in — a database row's, a page's —
// and one of the app's actions; each property editor has a name, the types
// it takes and its file.
func appExtensionProblems(manifest appManifest, actionSeen, blockSeen map[string]bool,
	keys func(string, []string) map[string]bool, add func(string, ...any), file func(string, string, string)) {
	appFileOpenerProblems(manifest, keys, add, file)
	slashKeys := []string{}
	for _, item := range manifest.SlashItems {
		slashKeys = append(slashKeys, item.Key)
		appTextProblems(item.Name, "slash item name", appNameMaxRunes, true, add)
		if len(item.Keywords) > 8 {
			add("slash item %q has at most 8 keywords", item.Key)
		}
		for _, keyword := range item.Keywords {
			if keyword == "" || utf8.RuneCountInString(keyword) > 24 {
				add("slash item %q keywords are 1-24 characters", item.Key)
			}
		}
		switch {
		case (item.Action == "") == (item.Block == ""):
			add("slash item %q names either an action or a block", item.Key)
		case item.Action != "" && !actionSeen[item.Action]:
			add("slash item %q action %q isn't one of the app's actions", item.Key, item.Action)
		case item.Block != "" && !blockSeen[item.Block]:
			add("slash item %q block %q isn't one of the app's blocks", item.Key, item.Block)
		}
	}
	keys("slash item", slashKeys)
	menuKeys := []string{}
	for _, line := range manifest.MenuActions {
		menuKeys = append(menuKeys, line.Key)
		appTextProblems(line.Name, "menu action name", appNameMaxRunes, true, add)
		seen := map[string]bool{}
		for _, menu := range line.On {
			if !appOneOf(menu, []string{"row", "page"}) || seen[menu] {
				seen["invalid"] = true
			}
			seen[menu] = true
		}
		if len(line.On) == 0 || seen["invalid"] {
			add("menu action %q goes on row, page or both, each once", line.Key)
		}
		if !actionSeen[line.Action] {
			add("menu action %q action %q isn't one of the app's actions", line.Key, line.Action)
		}
	}
	keys("menu action", menuKeys)
	editorKeys := []string{}
	for _, editor := range manifest.PropertyEditors {
		editorKeys = append(editorKeys, editor.Key)
		appTextProblems(editor.Name, "property editor name", appNameMaxRunes, true, add)
		file("property editor entry", editor.Entry, "ui/")
		seen := map[string]bool{}
		for _, kind := range editor.Types {
			if !appOneOf(kind, appPropertyEditorTypes) || seen[kind] {
				seen["invalid"] = true
			}
			seen[kind] = true
		}
		if len(editor.Types) == 0 || seen["invalid"] {
			add("property editor %q types are some of %v, each once", editor.Key, appPropertyEditorTypes)
		}
		if editor.Height < 0 || editor.Height > 480 {
			add("property editor %q height is 0-480", editor.Key)
		}
	}
	keys("property editor", editorKeys)
}
