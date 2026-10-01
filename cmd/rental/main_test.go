package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLILineProtocolAndReopen(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping exec-based integration test in short mode")
	}
	exe := buildBinary(t)

	stateDir := filepath.Join(t.TempDir(), "state")
	lines := []string{
		`{"op":"create","order_id":"O1","rent_time":0,"devices":[{"id":"A","hourly_rate":100,"daily_cap":800,"deposit":500},{"id":"B","hourly_rate":100,"daily_cap":800,"deposit":200}]}`,
		`{"op":"return","order_id":"O1","returns":[{"device_id":"A","time":1500}]}`,
		`{"op":"return","order_id":"O1","returns":[{"device_id":"BOGUS","time":1500},{"device_id":"B","time":1500}]}`,
		`not json`,
	}
	out := runCLI(t, exe, stateDir, lines)
	if len(out) != 4 {
		t.Fatalf("got %d responses: %v", len(out), out)
	}
	if !out[0]["ok"].(bool) {
		t.Fatalf("create failed: %v", out[0])
	}
	if !out[1]["ok"].(bool) {
		t.Fatalf("return A failed: %v", out[1])
	}
	if codeOf(out[2]) != "unknown_device" {
		t.Fatalf("batch should reject atomically: %v", out[2])
	}
	if codeOf(out[3]) != "invalid_json" {
		t.Fatalf("bad line should be distinguishable error: %v", out[3])
	}

	// Restart against the same state dir and finish the order.
	out = runCLI(t, exe, stateDir, []string{
		`{"op":"return","order_id":"O1","returns":[{"device_id":"B","time":61,"damage":300}]}`,
		`{"op":"query","order_id":"O1"}`,
	})
	if !out[0]["ok"].(bool) {
		t.Fatalf("return B after reopen failed: %v", out[0])
	}
	data := out[1]["data"].(map[string]any)
	orders := data["orders"].([]any)
	if orders[0].(map[string]any)["status"] != "settled" {
		t.Fatalf("order not settled after reopen: %v", orders[0])
	}
}

func buildBinary(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "rental.exe")
	cmd := exec.Command("go", "build", "-o", exe, ".")
	if err := cmd.Run(); err != nil {
		t.Fatalf("build: %v", err)
	}
	return exe
}

func runCLI(t *testing.T, exe, stateDir string, lines []string) []map[string]any {
	t.Helper()
	cmd := exec.Command(exe, "--state", stateDir)
	cmd.Stdin = strings.NewReader(strings.Join(lines, "\n") + "\n")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("run: %v stderr=%s", err, stderr.String())
	}
	var out []map[string]any
	sc := bufio.NewScanner(&stdout)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("response not json: %q", sc.Text())
		}
		out = append(out, m)
	}
	return out
}

func codeOf(m map[string]any) string {
	e, ok := m["error"].(map[string]any)
	if !ok {
		return ""
	}
	c, _ := e["code"].(string)
	return c
}
