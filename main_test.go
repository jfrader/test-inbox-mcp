package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRunHandshake(t *testing.T) {
	domain = "smoke.invalid"
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"new_address","arguments":{"local_part":"offline-smoke"}}}`,
	}, "\n")
	var output bytes.Buffer
	var errors bytes.Buffer

	if err := run(strings.NewReader(input), &output, &errors); err != nil {
		t.Fatal(err)
	}
	if errors.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", errors.String())
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 responses, got %d", len(lines))
	}
	for _, line := range lines {
		var response rpcResponse
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatalf("invalid response: %v", err)
		}
		if response.Error != nil {
			t.Fatalf("unexpected RPC error: %s", response.Error.Message)
		}
	}
	if !strings.Contains(lines[1], `"name":"new_address"`) {
		t.Fatal("tools/list did not include new_address")
	}
	if !strings.Contains(lines[2], "offline-smoke@smoke.invalid") {
		t.Fatal("new_address did not use configured domain")
	}
}

func TestExtractMessageDetails(t *testing.T) {
	links := extractLinks(`<a href="https://example.com/confirm">Confirm</a>`, "Code 123456")
	if len(links) != 1 || links[0] != "https://example.com/confirm" {
		t.Fatalf("unexpected links: %#v", links)
	}
	codes := extractCodes("Code 123456, repeated 123456")
	if len(codes) != 1 || codes[0] != "123456" {
		t.Fatalf("unexpected codes: %#v", codes)
	}
}

func TestRejectsUnsafeMailbox(t *testing.T) {
	if err := validateMailbox("../other"); err == nil {
		t.Fatal("expected unsafe mailbox to be rejected")
	}
}
