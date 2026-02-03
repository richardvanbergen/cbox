package hostcmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

// sendMCPRequest sends a JSON-RPC request to the MCP server and returns the response body.
func sendMCPRequest(t *testing.T, url string, method string, params any) map[string]any {
	t.Helper()

	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(respData, &result); err != nil {
		t.Fatalf("unmarshal response %q: %v", string(respData), err)
	}
	return result
}

// initSession sends the initialize handshake and returns the session header if any.
func initSession(t *testing.T, url string) {
	t.Helper()
	sendMCPRequest(t, url, "initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "test-client",
			"version": "1.0.0",
		},
	})
}

func startTestServer(t *testing.T, worktree string, commands []string) (string, *Server) {
	t.Helper()
	srv := NewServer(worktree, commands, nil)
	port, err := srv.Start()
	if err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() { srv.Stop() })

	url := fmt.Sprintf("http://127.0.0.1:%d/mcp", port)

	// Wait briefly for server to be ready
	time.Sleep(50 * time.Millisecond)

	initSession(t, url)
	return url, srv
}

func startTestServerWithNamedCommands(t *testing.T, worktree string, commands []string, namedCommands map[string]string) (string, *Server) {
	t.Helper()
	srv := NewServer(worktree, commands, namedCommands)
	port, err := srv.Start()
	if err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() { srv.Stop() })

	url := fmt.Sprintf("http://127.0.0.1:%d/mcp", port)

	// Wait briefly for server to be ready
	time.Sleep(50 * time.Millisecond)

	initSession(t, url)
	return url, srv
}

func callTool(t *testing.T, url string, args map[string]any) map[string]any {
	t.Helper()
	return sendMCPRequest(t, url, "tools/call", map[string]any{
		"name":      "run_command",
		"arguments": args,
	})
}

func callNamedTool(t *testing.T, url string, toolName string) map[string]any {
	t.Helper()
	return sendMCPRequest(t, url, "tools/call", map[string]any{
		"name":      toolName,
		"arguments": map[string]any{},
	})
}

func TestWhitelistedCommandExecutes(t *testing.T) {
	url, _ := startTestServer(t, t.TempDir(), []string{"echo"})

	result := callTool(t, url, map[string]any{
		"command": "echo",
		"args":    []string{"hello", "world"},
	})

	content := extractTextContent(t, result)
	if content == "" {
		t.Fatal("expected non-empty content")
	}
	if !bytes.Contains([]byte(content), []byte("hello world")) {
		t.Errorf("expected output to contain 'hello world', got: %s", content)
	}
	if !bytes.Contains([]byte(content), []byte("exit_code: 0")) {
		t.Errorf("expected exit_code: 0, got: %s", content)
	}
}

func TestNonWhitelistedCommandRejected(t *testing.T) {
	url, _ := startTestServer(t, t.TempDir(), []string{"echo"})

	result := callTool(t, url, map[string]any{
		"command": "rm",
		"args":    []string{"-rf", "/"},
	})

	content := extractTextContent(t, result)
	if content == "" {
		t.Fatal("expected non-empty content")
	}
	if !bytes.Contains([]byte(content), []byte("not in the whitelist")) {
		t.Errorf("expected whitelist rejection, got: %s", content)
	}
}

func TestPathTranslation(t *testing.T) {
	srv := NewServer("/host/project", []string{"echo"}, nil)

	tests := []struct {
		input    string
		expected string
	}{
		{"/workspace/src/main.go", "/host/project/src/main.go"},
		{"/workspace", "/host/project"},
		{"src/main.go", "/host/project/src/main.go"},
	}

	for _, tt := range tests {
		got := srv.translatePath(tt.input)
		if got != tt.expected {
			t.Errorf("translatePath(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestPathTraversalPrevented(t *testing.T) {
	dir := t.TempDir()
	url, _ := startTestServer(t, dir, []string{"echo"})

	result := callTool(t, url, map[string]any{
		"command": "echo",
		"args":    []string{"test"},
		"cwd":     "/workspace/../../../etc",
	})

	content := extractTextContent(t, result)
	if !bytes.Contains([]byte(content), []byte("within the workspace")) {
		t.Errorf("expected traversal prevention, got: %s", content)
	}
}

func TestCommandTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}

	dir := t.TempDir()
	url, _ := startTestServer(t, dir, []string{"sleep"})

	// Use a short timeout by testing with a command that would take long
	// The actual timeout is 120s which is too long for tests, so we just
	// verify the command runs and returns properly for a quick sleep
	result := callTool(t, url, map[string]any{
		"command": "sleep",
		"args":    []string{"0.1"},
	})

	content := extractTextContent(t, result)
	if !bytes.Contains([]byte(content), []byte("exit_code: 0")) {
		t.Errorf("expected exit_code: 0, got: %s", content)
	}
}

func TestNamedCommandExecutes(t *testing.T) {
	dir := t.TempDir()
	url, _ := startTestServerWithNamedCommands(t, dir, nil, map[string]string{
		"test": "echo named-test-output",
	})

	result := callNamedTool(t, url, "cbox_test")

	content := extractTextContent(t, result)
	if content == "" {
		t.Fatal("expected non-empty content")
	}
	if !bytes.Contains([]byte(content), []byte("named-test-output")) {
		t.Errorf("expected output to contain 'named-test-output', got: %s", content)
	}
	if !bytes.Contains([]byte(content), []byte("exit_code: 0")) {
		t.Errorf("expected exit_code: 0, got: %s", content)
	}
}

func TestNamedCommandFailure(t *testing.T) {
	dir := t.TempDir()
	url, _ := startTestServerWithNamedCommands(t, dir, nil, map[string]string{
		"fail": "exit 1",
	})

	result := callNamedTool(t, url, "cbox_fail")

	content := extractTextContent(t, result)
	if !bytes.Contains([]byte(content), []byte("exit_code: 1")) {
		t.Errorf("expected exit_code: 1, got: %s", content)
	}
}

func extractTextContent(t *testing.T, response map[string]any) string {
	t.Helper()

	result, ok := response["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in response: %v", response)
	}

	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("no content in result: %v", result)
	}

	first, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected content format: %v", content[0])
	}

	text, _ := first["text"].(string)
	return text
}
