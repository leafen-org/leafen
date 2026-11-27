//go:build !leafen_noapps

package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	appsdk "leafen.org/leafen/sdk/app"
)

// `leafen app init` writes a new app's folder: its manifest, a screen on the
// app SDK and, when asked, a Go worker with a copy of the SDK's helper, so
// it builds without fetching anything (on a closed network too).

var appInitSlugCleaner = regexp.MustCompile(`[^a-z0-9-]+`)

func appInitCommand() *cobra.Command {
	id, slug, name, developer, worker := "", "", "", "", false
	command := &cobra.Command{
		Use: "init <folder>", Short: "Write a new app's folder", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			folder := args[0]
			if entries, err := os.ReadDir(folder); err == nil && len(entries) > 0 {
				return fmt.Errorf("%s isn't empty", folder)
			}
			if slug == "" {
				slug = strings.Trim(appInitSlugCleaner.ReplaceAllString(strings.ToLower(filepath.Base(folder)), "-"), "-")
			}
			if !appSlugPattern.MatchString(slug) {
				return fmt.Errorf("the slug %q isn't 2-40 lowercase letters, digits or dashes: pass --slug", slug)
			}
			if id == "" {
				id = "com.example." + slug
			}
			if !appIDPattern.MatchString(id) {
				return fmt.Errorf("the id %q isn't a reverse domain name such as com.example.approvals: pass --id", id)
			}
			if name == "" {
				name = strings.ToUpper(slug[:1]) + strings.ReplaceAll(slug[1:], "-", " ")
			}
			files, err := appInitFiles(id, slug, name, developer, worker)
			if err != nil {
				return err
			}
			for path, data := range files {
				target := filepath.Join(folder, filepath.FromSlash(path))
				if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(target, data, 0o644); err != nil {
					return err
				}
			}
			out := command.OutOrStdout()
			fmt.Fprintf(out, "wrote %s (%s, /apps/%s)\n\n", folder, id, slug)
			fmt.Fprintf(out, "  leafen app dev %s        try it on a throwaway local Leafen\n", folder)
			fmt.Fprintf(out, "  leafen app validate %s   check it as Leafen does\n", folder)
			fmt.Fprintf(out, "  leafen app build %s      pack it into a .leafenapp file\n", folder)
			if worker {
				fmt.Fprintln(out, "\nThe Go worker in worker/ builds to workers/main.wasm on each of these (Go 1.24 or later).")
			}
			return nil
		},
	}
	command.Flags().StringVar(&id, "id", "", "the app's id, a reverse domain name (default com.example.<slug>)")
	command.Flags().StringVar(&slug, "slug", "", "the app's address under /apps/ (default: the folder's name)")
	command.Flags().StringVar(&name, "name", "", "the app's name")
	command.Flags().StringVar(&developer, "developer", "Your name", "who makes the app")
	command.Flags().BoolVar(&worker, "worker", false, "add a Go worker with an action")
	return command
}

func appInitFiles(id, slug, name, developer string, worker bool) (map[string][]byte, error) {
	manifest := appManifest{Schema: 1, ID: id, Slug: slug, Version: "0.1.0", Name: map[string]string{"en": name},
		Description: map[string]string{"en": "What " + name + " does."}, Developer: appManifestDeveloper{Name: developer}, Category: "work",
		Surfaces: appManifestSurfaces{FullPage: &appManifestEntry{Entry: "ui/index.html"}}, PersonAccess: "read"}
	if worker {
		manifest.Workers = []appManifestWorker{{Name: "main", Module: "workers/main.wasm", Functions: []string{"hello"}}}
		manifest.Actions = []appManifestAction{{Key: "hello", Name: map[string]string{"en": "Say hello"},
			Input: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","maxLength":60}}}`),
			RunAs: "app", Worker: "main", Function: "hello", Risk: "low"}}
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	module, err := fs.ReadFile(appsdk.Files, "dist/index.js")
	if err != nil {
		return nil, err
	}
	agents, err := fs.ReadFile(appsdk.Files, "AGENTS.md")
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{
		appManifestFile:        append(encoded, '\n'),
		"ui/index.html":        []byte(strings.ReplaceAll(appInitPage, "{{name}}", name)),
		"ui/app.js":            []byte(appInitScript(worker)),
		"ui/leafen-app-sdk.js": module,
		"README.md":            []byte(strings.NewReplacer("{{name}}", name, "{{slug}}", slug).Replace(appInitReadme)),
		"AGENTS.md":            agents,
		".gitignore":           []byte("*.leafenapp\n"),
	}
	if !worker {
		return files, nil
	}
	files[".gitignore"] = []byte("*.leafenapp\n/workers/main.wasm\n")
	files["worker/go.mod"] = []byte("module " + slug + "/worker\n\ngo 1.24\n")
	files["worker/main.go"] = []byte(strings.ReplaceAll(appInitWorker, "{{module}}", slug+"/worker"))
	helper := map[string]string{}
	raw, err := fs.ReadFile(appsdk.Files, "dist/go-helper.json")
	if err != nil || json.Unmarshal(raw, &helper) != nil || len(helper) == 0 {
		return nil, errors.New("this build lacks the SDK's Go helper")
	}
	for path, text := range helper {
		files["worker/"+path] = []byte(text)
	}
	return files, nil
}

const appInitPage = `<!doctype html>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{name}}</title>
<style>
  :root { color-scheme: light dark; font: 15px/1.5 system-ui, sans-serif; }
  body { margin: 24px; }
  [data-theme="dark"] body { background: #191919; color: #e9e9e7; }
  button, input { font: inherit; }
</style>
<h1>{{name}}</h1>
<p id="hello">Connecting to Leafen…</p>
<script type="module" src="./app.js"></script>
`

func appInitScript(worker bool) string {
	script := `// The app's screen: it reaches Leafen only through the SDK's channel.
import { connect } from "./leafen-app-sdk.js";

const leafen = await connect();
document.documentElement.dataset.theme = leafen.context.theme;
leafen.onTheme((theme) => { document.documentElement.dataset.theme = theme; });

const me = await leafen.api("GET", "/account/profile");
const name = me.status === 200 ? me.body.displayName : "there";
document.getElementById("hello").textContent = ` + "`Hello, ${name}.`" + `;
`
	if worker {
		script += `
// The worker's action, run on the server.
const button = document.createElement("button");
button.textContent = "Ask the worker";
button.addEventListener("click", async () => {
  const { output } = await leafen.action("hello", { name });
  await leafen.notice(output.message);
});
document.body.append(button);
`
	}
	return script
}

const appInitWorker = `// The app's worker. Each function the manifest's worker lists is exported
// here; Leafen builds this folder to workers/main.wasm:
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o ../workers/main.wasm .
package main

import "{{module}}/leafen"

//go:wasmexport hello
func hello() int32 {
	input := struct {
		Name string ` + "`json:\"name\"`" + `
	}{}
	if _, err := leafen.Action(&input); err != nil {
		return leafen.Fail("the input isn't JSON")
	}
	leafen.Log("said hello to " + input.Name)
	if err := leafen.OutputJSON(map[string]string{"message": "Hello from the worker, " + input.Name + "!"}); err != nil {
		return leafen.Fail(err.Error())
	}
	return 0
}

func main() {}
`

const appInitReadme = "# {{name}}\n\nA Leafen app. `leafen-app.json` says what it is and what it may reach; `ui/` is its\n" +
	"screen, which reaches Leafen through `ui/leafen-app-sdk.js` (the app SDK, Apache-2.0).\n\n" +
	"- `leafen app dev .` — try it on a throwaway local Leafen; it reinstalls when files change.\n" +
	"- `leafen app validate .` — check it as Leafen does.\n" +
	"- `leafen app build .` — pack it into `{{slug}}-<version>.leafenapp` for an administrator to install.\n" +
	"- `leafen app key key.json`, then `leafen app sign <file> --key key.json` — sign it (keep the key secret).\n"
