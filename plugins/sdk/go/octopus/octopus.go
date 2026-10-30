// Package octopus provides the TinyGo PDK for Octopus WebAssembly plugins.
package octopus

import (
	"encoding/json"
	"errors"

	pdk "github.com/extism/go-pdk"
)

const APIVersion = 1

type Request struct {
	APIVersion int             `json:"api_version"`
	RequestID  string          `json:"request_id"`
	PluginID   string          `json:"plugin_id"`
	Event      string          `json:"event"`
	AttemptID  string          `json:"attempt_id,omitempty"`
	Payload    json.RawMessage `json:"payload"`
	Config     map[string]any  `json:"config,omitempty"`
}

type Response struct {
	APIVersion int    `json:"api_version"`
	Decision   string `json:"decision"`
	Payload    any    `json:"payload,omitempty"`
	Tag        string `json:"tag,omitempty"`
	Error      string `json:"error,omitempty"`
}

type HostRequest struct {
	APIVersion int    `json:"api_version"`
	Capability string `json:"capability"`
	Payload    any    `json:"payload"`
}

type HostResponse struct {
	OK      bool            `json:"ok"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   string          `json:"error,omitempty"`
}

//go:wasmimport extism:host/user host_call
func hostCall(offset uint64) uint64

func Host(capability string, payload any, result any) error {
	input, err := json.Marshal(HostRequest{APIVersion: APIVersion, Capability: capability, Payload: payload})
	if err != nil {
		return err
	}
	offset := pdk.AllocateBytes(input).Offset()
	output, err := pdk.Memory(hostCall(offset)).ReadBytes()
	if err != nil {
		return err
	}
	var response HostResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return err
	}
	if !response.OK {
		return errors.New(response.Error)
	}
	if result != nil {
		return json.Unmarshal(response.Payload, result)
	}
	return nil
}

func Continue(payload any) Response {
	return Response{APIVersion: APIVersion, Decision: "continue", Payload: payload}
}
func Observe() Response { return Response{APIVersion: APIVersion, Decision: "observe"} }
func Failure(message string) Response {
	return Response{APIVersion: APIVersion, Decision: "continue", Error: message}
}

func Reply(response Response) uint32 {
	data, err := json.Marshal(response)
	if err != nil {
		pdk.SetError(err)
		return 1
	}
	pdk.Output(data)
	return 0
}

func KVGet(key string) (string, bool, error) {
	var result struct {
		Value string `json:"value"`
		Found bool   `json:"found"`
	}
	err := Host("kv.get", map[string]string{"key": key}, &result)
	return result.Value, result.Found, err
}

func KVSet(key, value string) error {
	return Host("kv.set", map[string]string{"key": key, "value": value}, nil)
}
func SecretSet(key, value string) error {
	return Host("secret.set", map[string]string{"key": key, "value": value}, nil)
}
