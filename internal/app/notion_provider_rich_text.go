package app

type notionProviderRichText struct {
	Type      string                `json:"type"`
	PlainText string                `json:"plain_text"`
	Href      string                `json:"href"`
	Mention   notionProviderMention `json:"mention"`
	Text      struct {
		Content string `json:"content"`
		Link    *struct {
			URL string `json:"url"`
		} `json:"link"`
	} `json:"text"`
	Equation struct {
		Expression string `json:"expression"`
	} `json:"equation"`
	Annotations struct {
		Bold          bool   `json:"bold"`
		Italic        bool   `json:"italic"`
		Strikethrough bool   `json:"strikethrough"`
		Underline     bool   `json:"underline"`
		Code          bool   `json:"code"`
		Color         string `json:"color"`
	} `json:"annotations"`
}

type notionProviderUser struct {
	Object    string  `json:"object"`
	ID        string  `json:"id"`
	Name      *string `json:"name"`
	AvatarURL *string `json:"avatar_url"`
	Type      string  `json:"type"`
	Person    struct {
		Email *string `json:"email"`
	} `json:"person"`
	Bot struct {
		WorkspaceName *string `json:"workspace_name"`
	} `json:"bot"`
}

type notionProviderMention struct {
	Type string             `json:"type"`
	User notionProviderUser `json:"user"`
	Page struct {
		ID string `json:"id"`
	} `json:"page"`
	Database struct {
		ID string `json:"id"`
	} `json:"database"`
	Date struct {
		Start    string  `json:"start"`
		End      *string `json:"end"`
		TimeZone *string `json:"time_zone"`
	} `json:"date"`
	// TemplateMention is a template's "when duplicated" mention: a date
	// ("today", "now") or a person ("me") taking its value when it is used.
	TemplateMention struct {
		Type string `json:"type"`
		Date string `json:"template_mention_date"`
		User string `json:"template_mention_user"`
	} `json:"template_mention"`
	LinkMention struct {
		Href         string `json:"href"`
		Title        string `json:"title"`
		IconURL      string `json:"icon_url"`
		LinkProvider string `json:"link_provider"`
		Description  string `json:"description"`
		ThumbnailURL string `json:"thumbnail_url"`
	} `json:"link_mention"`
}
