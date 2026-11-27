package app

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

func registerPageStyleMetadataHooks(app core.App) {
	app.OnRecordCreateExecute(pagesCollection).Bind(&hook.Handler[*core.RecordEvent]{
		Id:       "leafen-page-style-default",
		Priority: -100,
		Func: func(event *core.RecordEvent) error {
			if event.Record.GetString("font") == "" {
				event.Record.Set("font", pageFontDefault)
			}
			return event.Next()
		},
	})
}
