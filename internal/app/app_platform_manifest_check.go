package app

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// The checks an app's manifest passes before it is kept: names and keys in
// their forms, every file it names present in its package, every trigger
// running a function a worker declares, addresses of the allowed kinds and
// texts within their lengths.

var (
	appIDPattern      = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*){2,}$`)
	appSlugPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,39}$`)
	appVersionPattern = regexp.MustCompile(`^(0|[1-9]\d{0,5})\.(0|[1-9]\d{0,5})\.(0|[1-9]\d{0,5})(-[0-9A-Za-z.-]{1,32})?$`)
	appKeyPattern     = regexp.MustCompile(`^[a-z][a-zA-Z0-9_-]{0,47}$`)
	appLocalePattern  = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8}){0,2}$`)
	appColorPattern   = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	appSecretPattern  = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	appCategories     = []string{"work", "connectors", "mail", "data", "agents", "public", "games", "personal", "developer"}
	appDeviceKinds    = []string{"camera", "clipboard-write", "fullscreen", "geolocation", "microphone"}
	appDirectKinds    = []string{"connect", "font", "frame", "img", "media"}
	appPersonAccess   = []string{"", "openedFile", "read", "content", "structure", "admin"}
)

// appManifestProblems lists what is wrong with a manifest against the files
// of its package; none means it may be installed.
func appManifestProblems(manifest appManifest, files map[string]bool) []string {
	problems := []string{}
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	file := func(label, path, prefix string) {
		if !strings.HasPrefix(path, prefix) || !files[path] {
			add("%s %q isn't a file under %s in the package", label, path, prefix)
		}
	}
	keys := func(label string, values []string) map[string]bool {
		seen := map[string]bool{}
		for _, value := range values {
			if !appKeyPattern.MatchString(value) {
				add("%s %q isn't a valid key", label, value)
			} else if seen[value] {
				add("%s %q appears twice", label, value)
			}
			seen[value] = true
		}
		return seen
	}

	if manifest.Schema != 1 {
		add("schema must be 1")
	}
	if len(manifest.ID) > 100 || !appIDPattern.MatchString(manifest.ID) {
		add("id must be a reverse domain name such as com.example.approvals")
	}
	if !appSlugPattern.MatchString(manifest.Slug) {
		add("slug must be 2-40 lowercase letters, digits or dashes")
	}
	if !appVersionPattern.MatchString(manifest.Version) {
		add("version must be semantic, such as 1.2.0")
	}
	if manifest.MinLeafen != "" && !appVersionPattern.MatchString(manifest.MinLeafen) {
		add("minLeafen must be a semantic version")
	}
	appTextProblems(manifest.Name, "name", appNameMaxRunes, true, add)
	appTextProblems(manifest.Description, "description", appDescriptionMaxRunes, false, add)
	if name := strings.TrimSpace(manifest.Developer.Name); name == "" || utf8.RuneCountInString(name) > appNameMaxRunes {
		add("developer.name is required and at most %d characters", appNameMaxRunes)
	}
	if manifest.Developer.URL != "" && !appHTTPSURL(manifest.Developer.URL) {
		add("developer.url must be an https address")
	}
	if fingerprint := manifest.Developer.KeyFingerprint; fingerprint != "" && !appHexPattern(fingerprint, 64) {
		add("developer.keyFingerprint must be 64 hexadecimal characters")
	}
	if manifest.Icon != "" {
		file("icon", manifest.Icon, "ui/")
	}
	if manifest.Category != "" && !appOneOf(manifest.Category, appCategories) {
		add("category %q isn't one of %s", manifest.Category, strings.Join(appCategories, ", "))
	}
	if len(manifest.Tags) > 8 {
		add("at most 8 tags")
	}
	for _, tag := range manifest.Tags {
		if tag == "" || utf8.RuneCountInString(tag) > 24 {
			add("tags are 1-24 characters")
		}
	}

	if manifest.Surfaces.FullPage != nil {
		file("surfaces.fullPage.entry", manifest.Surfaces.FullPage.Entry, "ui/")
	}
	if manifest.Surfaces.SidePanel != nil {
		file("surfaces.sidePanel.entry", manifest.Surfaces.SidePanel.Entry, "ui/")
	}
	blockKeys := []string{}
	for _, block := range manifest.Surfaces.Blocks {
		blockKeys = append(blockKeys, block.Key)
		appTextProblems(block.Name, "block name", appNameMaxRunes, true, add)
		file("block entry", block.Entry, "ui/")
		if block.DefaultHeight < 0 || block.DefaultHeight > 2000 {
			add("block %q defaultHeight is 0-2000", block.Key)
		}
	}
	blockSeen := keys("block", blockKeys)

	dataKeys := []string{}
	for _, data := range manifest.Data {
		dataKeys = append(dataKeys, data.Key)
		file("data file", data.File, "data/")
	}
	dataSeen := keys("data", dataKeys)
	if !appOneOf(manifest.PersonAccess, appPersonAccess) {
		add("personAccess is one of openedFile, read, content, structure or admin")
	}
	if manifest.PersonAccess == appOpenedFileAccess && len(manifest.Surfaces.FileOpeners) == 0 {
		add("personAccess openedFile reaches only a file a file opener opened; declare surfaces.fileOpeners")
	}

	appNetworkProblems(manifest.Network, add)
	appRelayOAuthProblems(manifest.Network, add)
	appSettingProblems(manifest, add)
	appCredentialProblems(manifest, add)
	for _, device := range manifest.Device {
		if !appOneOf(device, appDeviceKinds) {
			add("device %q isn't one of %s", device, strings.Join(appDeviceKinds, ", "))
		}
	}
	if realtime := manifest.Realtime; realtime != nil && (realtime.MaxRooms < 0 || realtime.MaxRooms > appRoomsPerAppMax) {
		add("realtime.maxRooms is 1-%d", appRoomsPerAppMax)
	}
	if realtime := manifest.Realtime; realtime != nil && (realtime.MaxParticipants < 1 || realtime.MaxParticipants > 200) {
		add("realtime.maxParticipants is 1-200")
	}
	if kv := manifest.KV; kv != nil && (kv.MaxBytes < 1 || kv.MaxBytes > 64<<20 || kv.MaxTTLSeconds < 1 || kv.MaxTTLSeconds > 7*24*3600) {
		add("kv.maxBytes is up to 64 MB and kv.maxTtlSeconds up to 7 days")
	}

	functions := map[string]bool{}
	workerNames := []string{}
	for _, worker := range manifest.Workers {
		workerNames = append(workerNames, worker.Name)
		file("worker module", worker.Module, "workers/")
		if !strings.HasSuffix(worker.Module, ".wasm") {
			add("worker %q module must be a .wasm file", worker.Name)
		}
		if len(worker.Functions) == 0 {
			add("worker %q declares no functions", worker.Name)
		}
		for name := range keys("worker "+worker.Name+" function", worker.Functions) {
			functions[worker.Name+"/"+name] = true
		}
		if worker.MemoryMB < 0 || worker.MemoryMB > 256 || worker.TimeoutSeconds < 0 || worker.TimeoutSeconds > 60 {
			add("worker %q memoryMb is up to 256 and timeoutSeconds up to 60", worker.Name)
		}
		if worker.LongRunning != (worker.Run != "") {
			add("worker %q is longRunning exactly when it names the function to keep running in run", worker.Name)
		} else if worker.Run != "" && !slices.Contains(worker.Functions, worker.Run) {
			add("worker %q runs %s, which it doesn't list among its functions", worker.Name, worker.Run)
		}
	}
	keys("worker", workerNames)
	runs := func(label, worker, function string) {
		if !functions[worker+"/"+function] {
			add("%s runs %s/%s, which no worker declares", label, worker, function)
		}
	}
	scheduleKeys := []string{}
	for _, schedule := range manifest.Schedules {
		scheduleKeys = append(scheduleKeys, schedule.Key)
		runs("schedule "+schedule.Key, schedule.Worker, schedule.Function)
		if problem := appCronProblem(schedule.Cron); problem != "" {
			add("schedule %q: %s", schedule.Key, problem)
		}
		appTextProblems(schedule.Name, fmt.Sprintf("schedule %q name", schedule.Key), appNameMaxRunes, false, add)
		if schedule.TimeZone != "" && !appTimeZoneValid(schedule.TimeZone) {
			add("schedule %q timeZone is an IANA time zone such as Asia/Seoul", schedule.Key)
		}
	}
	keys("schedule", scheduleKeys)
	webhookKeys := []string{}
	for _, webhook := range manifest.Webhooks {
		webhookKeys = append(webhookKeys, webhook.Key)
		runs("webhook "+webhook.Key, webhook.Worker, webhook.Function)
		appWebhookProblems(webhook, add)
	}
	keys("webhook", webhookKeys)
	endpointKeys := []string{}
	for _, endpoint := range manifest.Endpoints {
		endpointKeys = append(endpointKeys, endpoint.Key)
		runs("endpoint "+endpoint.Key, endpoint.Worker, endpoint.Function)
		appTextProblems(endpoint.Name, fmt.Sprintf("endpoint %q name", endpoint.Key), appNameMaxRunes, true, add)
		seen := map[string]bool{}
		for _, method := range endpoint.Methods {
			if !appOneOf(method, appEndpointMethods) {
				add("endpoint %q method %q isn't one of %s", endpoint.Key, method, strings.Join(appEndpointMethods, ", "))
			} else if seen[method] {
				add("endpoint %q names method %s twice", endpoint.Key, method)
			}
			seen[method] = true
		}
		for _, method := range endpoint.FileBodies {
			if !appOneOf(method, []string{"PUT", "POST", "PATCH"}) || (len(endpoint.Methods) > 0 && !seen[method]) {
				add("endpoint %q fileBodies names %q, which isn't one of its PUT, POST or PATCH", endpoint.Key, method)
			}
		}
		if endpoint.Pages && !endpoint.Public {
			add("endpoint %q serves pages only when public: a page's requests can't carry an app password", endpoint.Key)
		}
		if endpoint.Pages && !slices.ContainsFunc(slices.Collect(maps.Keys(files)), func(name string) bool {
			return strings.HasPrefix(name, appEndpointPagesFolder)
		}) {
			add("endpoint %q serves pages, but the package has no files under %s", endpoint.Key, appEndpointPagesFolder)
		}
	}
	keys("endpoint", endpointKeys)
	eventKeys := []string{}
	for _, event := range manifest.Events {
		eventKeys = append(eventKeys, event.Key)
		runs("event "+event.Key, event.Worker, event.Function)
		if len(event.Types) == 0 {
			add("event %q names the changes it runs on", event.Key)
		}
		for _, kind := range event.Types {
			if !appOneOf(kind, eventWebhookTypes) {
				add("event %q type %q isn't one of %s", event.Key, kind, strings.Join(eventWebhookTypes, ", "))
			}
			if event.DataSource != "" && !strings.HasPrefix(kind, "page.") {
				add("event %q with a dataSource runs on page changes only, not %q", event.Key, kind)
			}
		}
		if (len(event.Properties) > 0 || len(event.Where) > 0) && event.DataSource == "" {
			add("event %q properties and where need its dataSource", event.Key)
		}
		if len(event.Properties) > 20 || len(event.Where) > 10 {
			add("event %q names at most 20 properties and 10 conditions", event.Key)
		}
		for _, condition := range event.Where {
			set := 0
			for _, given := range []bool{condition.Equals != nil, condition.NotEquals != nil, condition.Empty != nil} {
				if given {
					set++
				}
			}
			if strings.TrimSpace(condition.Property) == "" || set != 1 {
				add("event %q where: each condition names a property and one of equals, notEquals or empty", event.Key)
			}
		}
	}
	keys("event", eventKeys)
	if realtime := manifest.Realtime; realtime != nil && (realtime.Worker != "" || realtime.Function != "") {
		runs("realtime", realtime.Worker, realtime.Function)
	}
	if realtime := manifest.Realtime; realtime != nil && realtime.Loop && realtime.Function == "" {
		add("realtime.loop needs the worker function that runs in each room")
	}
	actionKeys := []string{}
	for _, action := range manifest.Actions {
		actionKeys = append(actionKeys, action.Key)
	}
	actionSeen := keys("action", actionKeys)
	for _, action := range manifest.Actions {
		appTextProblems(action.Name, "action name", appNameMaxRunes, true, add)
		appTextProblems(action.Description, "action description", appDescriptionMaxRunes, false, add)
		runs("action "+action.Key, action.Worker, action.Function)
		if action.RunAs != "person" && action.RunAs != "app" {
			add("action %q runAs is person or app", action.Key)
		}
		if !appOneOf(action.Risk, []string{"low", "medium", "high"}) {
			add("action %q risk is low, medium or high", action.Key)
		}
		if action.Undo != "" && (!actionSeen[action.Undo] || action.Undo == action.Key) {
			add("action %q undo names another action", action.Key)
		}
		if len(action.Input) > 16<<10 || len(action.Input) > 0 && !appJSONObject(action.Input) {
			add("action %q input is a JSON Schema object up to 16 KB", action.Key)
		}
		properties := appSchemaPropertyOrder(action.Input)
		for field, label := range action.InputLabels {
			if !appOneOf(field, properties) {
				add("action %q inputLabels names %q, which isn't in its input", action.Key, field)
			}
			appTextProblems(label, fmt.Sprintf("action %q input label %q", action.Key, field), appNameMaxRunes, true, add)
		}
	}
	appExtensionProblems(manifest, actionSeen, blockSeen, keys, add, file)
	if agent := manifest.Agent; agent != nil {
		appTextProblems(agent.Summary, "agent summary", 120, false, add)
		appTextProblems(agent.Instructions, "agent instructions", 2000, false, add)
		resourceKeys := []string{}
		for _, resource := range agent.Resources {
			resourceKeys = append(resourceKeys, resource.Key)
			if !actionSeen[resource.Action] || resource.Name == "" || utf8.RuneCountInString(string(resource.Name)) > appNameMaxRunes {
				add("agent resource %q needs a name and one of the app's actions", resource.Key)
			}
		}
		keys("agent resource", resourceKeys)
		promptKeys := []string{}
		for _, prompt := range agent.Prompts {
			promptKeys = append(promptKeys, prompt.Key)
			if prompt.Name == "" || utf8.RuneCountInString(string(prompt.Name)) > appNameMaxRunes || utf8.RuneCountInString(prompt.Template) > 4000 {
				add("agent prompt %q needs a name and a template up to 4000 characters", prompt.Key)
			}
		}
		keys("agent prompt", promptKeys)
	}
	if guests := manifest.Guests; guests != nil {
		appGuestRoomNameProblems(guests, keys("guest room", guests.Rooms), add)
		if len(guests.Rooms) == 0 || guests.MaxPerRoom < 1 || guests.MaxPerRoom > 100 {
			add("guests need at least one room and maxPerRoom 1-100")
		}
		if manifest.Realtime == nil {
			add("guests join realtime rooms: declare realtime")
		}
		if guests.ResultDatabase != "" && !dataSeen[guests.ResultDatabase] {
			add("guests.resultDatabase names one of the app's data")
		}
	}
	if pwa := manifest.PWA; pwa != nil {
		if utf8.RuneCountInString(pwa.ShortName) > 12 || pwa.ThemeColor != "" && !appColorPattern.MatchString(pwa.ThemeColor) {
			add("pwa.shortName is up to 12 characters and pwa.themeColor a #rrggbb color")
		}
		for _, path := range pwa.Offline {
			file("pwa offline file", path, "ui/")
		}
	}
	if manifest.Surfaces.FullPage == nil && manifest.Surfaces.SidePanel == nil && len(manifest.Surfaces.Blocks) == 0 &&
		len(manifest.Surfaces.FileOpeners) == 0 && len(manifest.Surfaces.ImportSources) == 0 &&
		len(manifest.Actions) == 0 && len(manifest.Schedules) == 0 && len(manifest.Webhooks) == 0 && len(manifest.Events) == 0 &&
		len(manifest.Endpoints) == 0 {
		add("the app shows nowhere and does nothing: declare a surface, an action, a schedule, a webhook or an endpoint")
	}
	return problems
}

func appNetworkProblems(network appManifestNetwork, add func(string, ...any)) {
	for _, direct := range network.Direct {
		if !appHTTPSURL(strings.Replace(direct.URL, "://*.", "://", 1)) {
			add("network.direct %q must be an https address", direct.URL)
		}
		if len(direct.Kinds) == 0 {
			add("network.direct %q names what it is for", direct.URL)
		}
		for _, kind := range direct.Kinds {
			if !appOneOf(kind, appDirectKinds) {
				add("network.direct %q kind %q isn't one of %s", direct.URL, kind, strings.Join(appDirectKinds, ", "))
			}
		}
	}
	relays := []string{}
	for _, relay := range network.Relay {
		relays = append(relays, relay.URL)
		parsed, err := url.Parse(relay.URL)
		if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
			parsed.Scheme != "https" && !(parsed.Scheme == "http" && relay.PrivateNetwork) {
			add("network.relay %q must be an https address (http only on the private network)", relay.URL)
		} else if reason := appUnreachableHost(parsed.Hostname()); reason != "" {
			add("network.relay %q reaches %s, which apps may never reach, even on the private network", relay.URL, reason)
		} else if host := parsed.Hostname(); strings.Contains(host, "*") {
			// Only a whole first name, over a domain of two names or more: "*.example.com".
			domain, wildcard := strings.CutPrefix(host, "*.")
			if !wildcard || strings.Contains(domain, "*") || strings.Count(domain, ".") < 1 || relay.PrivateNetwork {
				add("network.relay %q may start its host with \"*.\" only, over a public domain of two names or more", relay.URL)
			}
		}
		for _, secret := range relay.Secrets {
			if !appSecretPattern.MatchString(secret) {
				add("network.relay %q secret %q must be UPPER_CASE", relay.URL, secret)
			}
		}
	}
	for _, socket := range network.Sockets {
		if _, setting := appSettingName(socket.Host); setting {
			// An address the administrator sets: appSettingProblems checks it.
			continue
		}
		if socket.Host == "" || strings.ContainsAny(socket.Host, "/:@ ") || socket.Port < 1 || socket.Port > 65535 {
			add("network.sockets needs a host name and a port 1-65535")
		} else if reason := appUnreachableHost(socket.Host); reason != "" {
			add("network.sockets %q reaches %s, which apps may never reach, even on the private network", socket.Host, reason)
		}
	}
	oauthKeys := map[string]bool{}
	for _, provider := range network.OAuth {
		if !appKeyPattern.MatchString(provider.Key) || oauthKeys[provider.Key] {
			add("network.oauth key %q is invalid or repeated", provider.Key)
		}
		oauthKeys[provider.Key] = true
		if !appHTTPSURL(provider.AuthorizeURL) {
			add("network.oauth %q authorizeUrl must be an https address", provider.Key)
		}
		if provider.Owner != "" && provider.Owner != "person" && provider.Owner != "app" {
			add("network.oauth %q owner is person or app", provider.Key)
		}
		if len(provider.AuthorizeParams) > 8 {
			add("network.oauth %q has at most 8 authorizeParams", provider.Key)
		}
		for name, value := range provider.AuthorizeParams {
			if !appOAuthParamPattern.MatchString(name) || appOneOf(name, appOAuthOwnParams) || len(value) > 200 || strings.ContainsAny(value, "\r\n") {
				add("network.oauth %q authorizeParams %q is not the app's to set", provider.Key, name)
			}
		}
		covered := false
		for _, relay := range relays {
			if strings.HasPrefix(provider.TokenURL, relay) {
				covered = true
			}
		}
		if !appHTTPSURL(provider.TokenURL) || !covered {
			add("network.oauth %q tokenUrl must be an https address under one of network.relay", provider.Key)
		}
	}
}

// appRelayOAuthProblems checks that each relay entry names only sign-ins the
// app declares.
func appRelayOAuthProblems(network appManifestNetwork, add func(string, ...any)) {
	declared := map[string]bool{}
	for _, provider := range network.OAuth {
		declared[provider.Key] = true
	}
	for _, relay := range network.Relay {
		for _, key := range relay.OAuth {
			if !declared[key] {
				add("network.relay %q oauth %q names no network.oauth entry", relay.URL, key)
			}
		}
	}
}

func appTextProblems(text map[string]string, label string, maxRunes int, required bool, add func(string, ...any)) {
	if required && len(text) == 0 {
		add("%s needs at least one language", label)
	}
	for locale, value := range text {
		if !appLocalePattern.MatchString(locale) {
			add("%s language %q isn't a language tag such as ko or en", label, locale)
		}
		if strings.TrimSpace(value) == "" || utf8.RuneCountInString(value) > maxRunes {
			add("%s (%s) is 1-%d characters", label, locale, maxRunes)
		}
	}
}

// appCronProblem accepts five fields whose minute field runs at least five
// minutes apart: a number, a list of numbers or */n with n of 5 or more.
func appCronProblem(cron string) string {
	fields := strings.Fields(cron)
	if len(fields) != 5 {
		return "cron has five fields"
	}
	minute := fields[0]
	if strings.HasPrefix(minute, "*/") {
		step, err := strconv.Atoi(minute[2:])
		if err != nil || step < 5 || step > 59 {
			return "a */n minute field runs every 5 minutes or less often"
		}
		return ""
	}
	values := []int{}
	for _, part := range strings.Split(minute, ",") {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 || value > 59 {
			return "the minute field is a number, a list of numbers or */n"
		}
		values = append(values, value)
	}
	sort.Ints(values)
	for index := range values {
		gap := 60 - values[len(values)-1] + values[0]
		if index > 0 {
			gap = values[index] - values[index-1]
		}
		if len(values) > 1 && gap < 5 {
			return "runs are at least five minutes apart"
		}
	}
	return ""
}

func appOneOf(value string, allowed []string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

var (
	appOAuthParamPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	// What Leafen sets itself on a sign-in.
	appOAuthOwnParams = []string{"response_type", "client_id", "redirect_uri", "state", "code_challenge", "code_challenge_method", "scope"}
)

func appHTTPSURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Hostname() != "" && parsed.User == nil &&
		parsed.RawQuery == "" && parsed.Fragment == "" && utf8.RuneCountInString(raw) <= 2048
}

func appHexPattern(value string, length int) bool {
	if len(value) != length {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func appJSONObject(raw json.RawMessage) bool {
	value := map[string]any{}
	return json.Unmarshal(raw, &value) == nil
}

// appSchemaPropertyOrder is the order an input schema lists its properties in.
func appSchemaPropertyOrder(raw json.RawMessage) []string {
	order := []string{}
	top := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &top) != nil || len(top["properties"]) == 0 {
		return order
	}
	decoder := json.NewDecoder(bytes.NewReader(top["properties"]))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return order
	}
	for decoder.More() {
		token, err := decoder.Token()
		key, isKey := token.(string)
		if err != nil || !isKey {
			return order
		}
		var skip json.RawMessage
		if decoder.Decode(&skip) != nil {
			return order
		}
		order = append(order, key)
	}
	return order
}

// appTimeZoneValid takes an IANA time zone name, such as Asia/Seoul.
func appTimeZoneValid(zone string) bool {
	if zone == "" || len(zone) > 64 || zone == "Local" {
		return false
	}
	_, err := time.LoadLocation(zone)
	return err == nil
}
