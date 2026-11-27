package app

import (
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

const errorCodePocketBaseSurfaceHidden = "LEAFEN_POCKETBASE_SURFACE_HIDDEN"
const errorCodeSourceMapHidden = "LEAFEN_SOURCE_MAP_HIDDEN"

func hidePocketBaseProductSurfaces(event *core.RequestEvent) error {
	path := event.Request.URL.Path
	isDashboard := path == "/_" || strings.HasPrefix(path, "/_/")
	isLeafenAPI := path == apiRoot || strings.HasPrefix(path, apiRoot+"/") || path == nativeMCPPath
	isRawAPI := strings.HasPrefix(path, "/api/") && !isLeafenAPI
	isSourceMap := strings.HasSuffix(strings.ToLower(path), ".map")
	if isDashboard || isRawAPI || isSourceMap {
		if isSourceMap {
			return apiError(event, http.StatusNotFound, errorCodeSourceMapHidden, "Unsupported route.")
		}
		return apiError(event, http.StatusNotFound, errorCodePocketBaseSurfaceHidden, "Unsupported route.")
	}
	return event.Next()
}
