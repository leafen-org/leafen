package app

import (
	"cmp"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// A list takes how many it answers as limit (or page_size, pageSize, the
// names an agent also gives it), and one that answers a fixed window says
// it didn't read it rather than ignoring it (2026-11-16, an agent calling
// every operation: a scan's items with limit=3 answered 37, as if it had
// been read). Every answer of an operation whose reading of the query is
// known (scripts/native-operation-inputs) names the query names it didn't
// read in Leafen-Unread-Query, and leafen_call gives them as unread_query.

const apiUnreadQueryHeader = "Leafen-Unread-Query"

// apiQueryLimit is the limit a list's query asks for, 0 for none or one
// that isn't a whole number above 0.
func apiQueryLimit(query url.Values) int {
	// Each name read by itself, so the operations' description finds them.
	limit, err := strconv.Atoi(strings.TrimSpace(cmp.Or(query.Get("limit"), query.Get("page_size"), query.Get("pageSize"))))
	if err != nil || limit < 1 {
		return 0
	}
	return limit
}

// noteUnreadQuery names, on the answer, the query names the request's
// operation doesn't read.
func noteUnreadQuery(request *http.Request, header http.Header) {
	if request.URL.RawQuery == "" {
		return
	}
	input, known := apiOperationInputOf(request)
	if !known {
		return
	}
	unread := []string{}
	for name := range request.URL.Query() {
		if !apiErrorQueryKept(name) && !slices.ContainsFunc(input.Query, func(field nativeOperationInputField) bool { return field.Name == name }) {
			unread = append(unread, name)
		}
	}
	if len(unread) > 0 {
		slices.Sort(unread)
		header.Set(apiUnreadQueryHeader, strings.Join(unread, ", "))
	}
}
