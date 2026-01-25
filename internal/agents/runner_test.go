package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestRunner_RunStreamsOutput verifies streaming stdout and stderr.
func TestRunner_RunStreamsOutput(t *testing.T) {
	runner := NewRunner()
	runner.ExecCmd = helperExecCmd("success")

	provider := testProvider{binary: "helper"}
	var lines []string
	var linesMu sync.Mutex

	err := runner.Run(context.Background(), provider, "prompt", "context", AgentRunOptions{}, func(AgentEvent) {}, func(line string) {
		linesMu.Lock()
		lines = append(lines, line)
		linesMu.Unlock()
	}, func(error) {})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	linesMu.Lock()
	linesCopy := append([]string(nil), lines...)
	linesMu.Unlock()

	if !containsLine(linesCopy, "hello") {
		t.Fatalf("expected stdout line, got: %#v", linesCopy)
	}
	if !containsLine(linesCopy, "stderr: warn") {
		t.Fatalf("expected stderr line, got: %#v", linesCopy)
	}
}

// TestRunner_RunCancel verifies cancellation stops the process.
func TestRunner_RunCancel(t *testing.T) {
	runner := NewRunner()
	runner.ExecCmd = helperExecCmd("sleep")

	provider := testProvider{binary: "helper"}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := runner.Run(ctx, provider, "prompt", "context", AgentRunOptions{}, func(AgentEvent) {}, func(string) {}, func(error) {})
	if err == nil {
		t.Fatalf("expected cancellation error")
	}
}

// TestRunner_RunNonZero verifies non-zero exit propagates error.
func TestRunner_RunNonZero(t *testing.T) {
	runner := NewRunner()
	runner.ExecCmd = helperExecCmd("fail")

	provider := testProvider{binary: "helper"}
	err := runner.Run(context.Background(), provider, "prompt", "context", AgentRunOptions{}, func(AgentEvent) {}, func(string) {}, func(error) {})
	if err == nil {
		t.Fatalf("expected non-zero exit error")
	}
}

// TestRunnerHelperProcess is a helper process for runner tests.
func TestRunnerHelperProcess(t *testing.T) {
	if os.Getenv("AGENT_TEST_HELPER") != "1" {
		return
	}

	mode := os.Getenv("AGENT_TEST_MODE")
	switch mode {
	case "success":
		_, _ = fmt.Fprintln(os.Stdout, `{"text":"hello"}`)
		_, _ = fmt.Fprintln(os.Stderr, "warn")
		os.Exit(0)
	case "fail":
		_, _ = fmt.Fprintln(os.Stderr, "boom")
		os.Exit(2)
	case "sleep":
		time.Sleep(5 * time.Second)
		os.Exit(0)
	default:
		os.Exit(0)
	}
}

// helperExecCmd returns an ExecCmd replacement for helper process testing.
func helperExecCmd(mode string) func(ctx context.Context, name string, args ...string) *exec.Cmd {
	return func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestRunnerHelperProcess")
		cmd.Env = append(os.Environ(),
			"AGENT_TEST_HELPER=1",
			fmt.Sprintf("AGENT_TEST_MODE=%s", mode),
		)
		return cmd
	}
}

// containsLine checks if any line contains a substring.
func containsLine(lines []string, needle string) bool {
	for _, line := range lines {
		if strings.Contains(line, needle) {
			return true
		}
	}
	return false
}

// testProvider is a minimal provider for runner tests.
type testProvider struct {
	binary string
}

// Name returns the provider name.
func (p testProvider) Name() string {
	return "test"
}

// ResolveBinary returns a fixed binary for testing.
func (p testProvider) ResolveBinary() (string, bool) {
	return p.binary, true
}

// BuildArgs returns no args for testing.
func (p testProvider) BuildArgs(string, string, AgentRunOptions) []string {
	return nil
}

// ParseStreamLine extracts text from a simple JSON payload.
func (p testProvider) ParseStreamLine(line []byte) (string, bool) {
	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(line, &payload); err != nil {
		return "", false
	}
	if payload.Text == "" {
		return "", false
	}
	return payload.Text, true
}
