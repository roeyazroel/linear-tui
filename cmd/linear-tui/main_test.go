package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestRunAuthHelp(t *testing.T) {
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	code := run([]string{"auth", "help"})

	_ = w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(out, "auth login") || !strings.Contains(out, "auth logout") {
		t.Fatalf("usage output = %q", out)
	}
}

func TestRunAuthUnknown(t *testing.T) {
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w

	code := run([]string{"auth", "nope"})

	_ = w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()
	if code != 1 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(out, "Unknown auth command") {
		t.Fatalf("stderr = %q", out)
	}
}

func TestRunProjectRequiresTeam(t *testing.T) {
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w

	code := run([]string{"--project", "Website"})

	_ = w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()
	if code != 2 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(out, "--project requires --team") {
		t.Fatalf("stderr = %q", out)
	}
}

func TestRunVersion(t *testing.T) {
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	code := run([]string{"--version"})

	_ = w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if strings.TrimSpace(buf.String()) == "" {
		t.Fatal("expected version output")
	}
}
