package main

import (
	"encoding/json"

	pdk "github.com/extism/go-pdk"
	"github.com/lingyuins/octopus/plugins/sdk/go/octopus"
)

//export handle
func handle() uint32 {
	var request octopus.Request
	if err := json.Unmarshal(pdk.Input(), &request); err != nil {
		return octopus.Reply(octopus.Failure(err.Error()))
	}
	if request.Event == "relay.request.before_route" {
		return octopus.Reply(octopus.Continue(request.Payload))
	}
	if request.Event == "http.rpc" {
		_, found, err := octopus.KVGet("counter")
		if err != nil {
			return octopus.Reply(octopus.Failure(err.Error()))
		}
		return octopus.Reply(octopus.Continue(map[string]any{"counter_found": found, "plugin": request.PluginID}))
	}
	if request.Event == "relay.completed" {
		if err := octopus.KVSet("last_model", "example"); err != nil {
			return octopus.Reply(octopus.Failure(err.Error()))
		}
	}
	return octopus.Reply(octopus.Observe())
}

func main() {}
