// The Hello app's worker: greet answers with a message and counts the
// greetings in the app's temporary store (a new key there needs a lifetime).
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o ../workers/hello.wasm .
package main

import (
	"encoding/json"
	"strings"

	"leafen.org/leafen/sdk/app/go/leafen"
)

//go:wasmexport greet
func greet() int32 {
	input := struct {
		Name string `json:"name"`
	}{}
	if _, err := leafen.Action(&input); err != nil {
		return leafen.Fail("the input isn't JSON")
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "there"
	}
	counted, err := leafen.KV(leafen.KVRequest{Command: "INCR", Key: "greetings", TTL: 86400})
	if err != nil {
		return leafen.Fail(err.Error())
	}
	count := 0
	_ = json.Unmarshal(counted, &count)
	leafen.Log("greeted " + name)
	if err := leafen.OutputJSON(map[string]any{"message": "Hello, " + name + "!", "count": count}); err != nil {
		return leafen.Fail(err.Error())
	}
	return 0
}

func main() {}
