package app

// A public page request refused says which field and why, as the rest of the
// API does (request_refusal.go): none of it is a secret — a token that reads
// is still answered "not found" when no link has it.

// publicContentSnapshotRefusal is why a snapshot request isn't one, or nil.
func publicContentSnapshotRefusal(input publicContentSnapshotRequest) error {
	switch {
	case input.Action != publicContentActionSnapshot:
		return refuse(errPublicContentInvalid, "action is %s, not %q", publicContentActionSnapshot, input.Action)
	case len(input.PageID) > maxGraphIdentifierLength:
		return refuse(errPublicContentInvalid, "pageId is at most %d characters", maxGraphIdentifierLength)
	case len(input.PagesCursor) > maxPublicContentCursorBytes:
		return refuse(errPublicContentInvalid, "pagesCursor is at most %d characters", maxPublicContentCursorBytes)
	case len(input.GraphCursor) > maxPublicContentCursorBytes:
		return refuse(errPublicContentInvalid, "graphCursor is at most %d characters", maxPublicContentCursorBytes)
	case input.SourceRevision != nil && *input.SourceRevision < 0:
		return refuse(errPublicContentInvalid, "sourceRevision is 0 or more, not %d", *input.SourceRevision)
	}
	switch input.SourceKind {
	case publicContentSourceShare, publicContentSourceView:
		switch {
		case input.Token == "":
			return refuse(errPublicContentInvalid, "token is empty: a %s link's page is read by its token", input.SourceKind)
		case len(input.Token) > maxPublicContentTokenCharacters:
			return refuse(errPublicContentInvalid, "token is at most %d characters", maxPublicContentTokenCharacters)
		case input.SiteSlug != "":
			return refuse(errPublicContentInvalid, "siteSlug goes only with sourceKind %s", publicContentSourceSite)
		}
		return nil
	case publicContentSourceSite:
		if input.Token != "" {
			return refuse(errPublicContentInvalid, "token goes only with sourceKind %s or %s", publicContentSourceShare, publicContentSourceView)
		}
		return publicSiteRouteRefusal(input.SiteSlug)
	}
	return refuse(errPublicContentInvalid, "sourceKind is %s, %s or %s, not %q",
		publicContentSourceShare, publicContentSourceView, publicContentSourceSite, input.SourceKind)
}

// publicContentSearchRefusal is why a site search request isn't one, or nil.
func publicContentSearchRefusal(input publicContentSearchRequest) error {
	switch {
	case input.Action != publicContentActionSearchSite:
		return refuse(errPublicContentInvalid, "action is %s, not %q", publicContentActionSearchSite, input.Action)
	case input.SourceRevision < 0:
		return refuse(errPublicContentInvalid, "sourceRevision is 0 or more, not %d", input.SourceRevision)
	case input.Query == "":
		return refuse(errPublicContentInvalid, "query is empty")
	case len([]rune(input.Query)) > maxPublicContentQueryCharacters:
		return refuse(errPublicContentInvalid, "query is at most %d characters", maxPublicContentQueryCharacters)
	case len(input.Cursor) > maxPublicContentCursorBytes:
		return refuse(errPublicContentInvalid, "cursor is at most %d characters", maxPublicContentCursorBytes)
	}
	return publicSiteRouteRefusal(input.SiteSlug)
}

// publicSiteRouteRefusal is why a site's address name isn't one, or nil.
func publicSiteRouteRefusal(siteSlug string) error {
	switch {
	case siteSlug == "":
		return refuse(errPublicContentInvalid, "siteSlug is empty")
	case len(siteSlug) > maxPublicContentSiteSlugCharacters:
		return refuse(errPublicContentInvalid, "siteSlug is at most %d characters", maxPublicContentSiteSlugCharacters)
	case !publicSiteSlugPattern.MatchString(siteSlug):
		return refuse(errPublicContentInvalid, "siteSlug is lowercase letters and digits joined by single dashes, not %q", siteSlug)
	}
	return nil
}
