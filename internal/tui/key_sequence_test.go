package tui

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestKeySequenceConstructorValidation(t *testing.T) {
	valid := []KeySequenceBinding{{Sequence: "g a", CommandID: "all", Context: "global"}}
	tests := []struct {
		name      string
		bindings  []KeySequenceBinding
		wantError string
	}{
		{name: "empty command", bindings: []KeySequenceBinding{{Sequence: "g a", Context: "global"}}, wantError: "command"},
		{name: "empty context", bindings: []KeySequenceBinding{{Sequence: "g a", CommandID: "all"}}, wantError: "context"},
		{name: "single key", bindings: []KeySequenceBinding{{Sequence: "g", CommandID: "all", Context: "global"}}, wantError: "two"},
		{name: "multi rune key", bindings: []KeySequenceBinding{{Sequence: "gg a", CommandID: "all", Context: "global"}}, wantError: "single-rune"},
		{name: "duplicate in context", bindings: []KeySequenceBinding{{Sequence: "g a", CommandID: "one", Context: "global"}, {Sequence: " g   a ", CommandID: "two", Context: "global"}}, wantError: "duplicate"},
		{name: "complete prefix", bindings: []KeySequenceBinding{{Sequence: "g a", CommandID: "short", Context: "global"}, {Sequence: "g a b", CommandID: "long", Context: "global"}}, wantError: "prefix"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewKeySequenceDispatcher(test.bindings, time.Second)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), test.wantError) {
				t.Fatalf("error = %v, want message containing %q", err, test.wantError)
			}
		})
	}
	if dispatcher, err := NewKeySequenceDispatcher(valid, 0); err != nil || dispatcher == nil {
		t.Fatalf("valid constructor = dispatcher %v, error %v", dispatcher, err)
	}
}

func TestKeySequenceContextAndGlobalPrecedence(t *testing.T) {
	dispatcher, err := NewKeySequenceDispatcher([]KeySequenceBinding{
		{Sequence: "g a", CommandID: "global-all", Context: "global", Hint: "Global all"},
		{Sequence: "g i", CommandID: "issue", Context: "issues", Hint: "Issue"},
	}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(10, 0)
	if result := dispatcher.Feed("issues", 'g', now); result.State != KeySequenceStatePending || result.Prefix != "g" {
		t.Fatalf("issue first key = %#v, want pending g", result)
	}
	if hint := dispatcher.PendingHint("issues"); hint != "Issue" {
		t.Fatalf("issue pending hint = %q, want Issue", hint)
	}
	if result := dispatcher.Feed("issues", 'i', now.Add(10*time.Millisecond)); result.State != KeySequenceStateComplete || result.CommandID != "issue" {
		t.Fatalf("issue complete = %#v", result)
	}
	if result := dispatcher.Feed("", 'g', now.Add(20*time.Millisecond)); result.State != KeySequenceStatePending {
		t.Fatalf("empty context global first key = %#v", result)
	}
	if result := dispatcher.Feed("", 'a', now.Add(30*time.Millisecond)); result.State != KeySequenceStateComplete || result.CommandID != "global-all" {
		t.Fatalf("global complete = %#v", result)
	}
	if result := dispatcher.Feed("other", 'g', now.Add(40*time.Millisecond)); result.State != KeySequenceStatePending {
		t.Fatalf("fallback global first key = %#v", result)
	}
	if result := dispatcher.Feed("other", 'a', now.Add(50*time.Millisecond)); result.CommandID != "global-all" {
		t.Fatalf("fallback global complete = %#v", result)
	}
}

func TestKeySequenceCaseAndWhitespaceNormalization(t *testing.T) {
	dispatcher, err := NewKeySequenceDispatcher([]KeySequenceBinding{{Sequence: " G   A ", CommandID: "upper", Context: " global ", Hint: "Upper"}}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(20, 0)
	if result := dispatcher.Feed("global", 'g', now); result.State != KeySequenceStateIdle {
		t.Fatalf("lowercase key = %#v, want idle because case is preserved", result)
	}
	if result := dispatcher.Feed("global", 'G', now); result.State != KeySequenceStatePending || result.Prefix != "G" {
		t.Fatalf("uppercase first key = %#v", result)
	}
	if result := dispatcher.Feed("global", 'A', now.Add(time.Millisecond)); result.State != KeySequenceStateComplete || result.CommandID != "upper" {
		t.Fatalf("uppercase complete = %#v", result)
	}
}

func TestKeySequencePendingCompleteAndHint(t *testing.T) {
	dispatcher, err := NewKeySequenceDispatcher([]KeySequenceBinding{
		{Sequence: "g a", CommandID: "all", Context: "global", Hint: "Go all"},
	}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(30, 0)
	if dispatcher.Pending() || dispatcher.Prefix() != "" {
		t.Fatal("new dispatcher has pending input")
	}
	result := dispatcher.Feed("global", 'g', now)
	if want := (KeySequenceResult{State: KeySequenceStatePending, Prefix: "g", Hint: "Go all"}); !reflect.DeepEqual(result, want) {
		t.Fatalf("pending result = %#v, want %#v", result, want)
	}
	if !dispatcher.Pending() || dispatcher.Prefix() != "g" {
		t.Fatalf("pending accessors = pending=%v prefix=%q", dispatcher.Pending(), dispatcher.Prefix())
	}
	result = dispatcher.Feed("global", 'a', now.Add(100*time.Millisecond))
	if want := (KeySequenceResult{State: KeySequenceStateComplete, CommandID: "all", Prefix: "g a", Hint: "Go all"}); !reflect.DeepEqual(result, want) {
		t.Fatalf("complete result = %#v, want %#v", result, want)
	}
	if dispatcher.Pending() || dispatcher.Prefix() != "" {
		t.Fatal("complete did not reset pending input")
	}
}

func TestKeySequenceInvalidAndReprocess(t *testing.T) {
	dispatcher, err := NewKeySequenceDispatcher([]KeySequenceBinding{
		{Sequence: "g a", CommandID: "all", Context: "global"},
		{Sequence: "x y", CommandID: "other", Context: "global"},
	}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(40, 0)
	dispatcher.Feed("global", 'g', now)
	result := dispatcher.Feed("global", 'x', now.Add(time.Millisecond))
	if result.State != KeySequenceStateInvalid || !result.Reprocess || result.Prefix != "g" {
		t.Fatalf("invalid reprocessable result = %#v", result)
	}
	dispatcher.Feed("global", 'g', now.Add(2*time.Millisecond))
	result = dispatcher.Feed("global", 'z', now.Add(3*time.Millisecond))
	if result.State != KeySequenceStateInvalid || result.Reprocess {
		t.Fatalf("invalid non-reprocessable result = %#v", result)
	}
	if dispatcher.Pending() {
		t.Fatal("invalid input left pending state")
	}
	if result := dispatcher.Feed("global", 'q', now.Add(4*time.Millisecond)); result.State != KeySequenceStateIdle || result.Reprocess {
		t.Fatalf("unbound idle result = %#v", result)
	}
}

func TestKeySequenceTimeoutAndResetAccessors(t *testing.T) {
	dispatcher, err := NewKeySequenceDispatcher([]KeySequenceBinding{{Sequence: "g a", CommandID: "all", Context: "global"}}, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(50, 0)
	dispatcher.Feed("global", 'g', now)
	result := dispatcher.Feed("global", 'a', now.Add(101*time.Millisecond))
	if result.State != KeySequenceStateTimedOut || !result.Reprocess || result.Prefix != "g" {
		t.Fatalf("timeout result = %#v", result)
	}
	if dispatcher.Pending() || dispatcher.Prefix() != "" {
		t.Fatal("timeout did not reset state")
	}
	dispatcher.Feed("global", 'g', now.Add(200*time.Millisecond))
	dispatcher.Reset()
	if dispatcher.Pending() || dispatcher.Prefix() != "" || dispatcher.PendingHint("global") != "" {
		t.Fatal("Reset did not clear pending accessors")
	}
}

func TestKeySequenceContextIsolation(t *testing.T) {
	dispatcher, err := NewKeySequenceDispatcher([]KeySequenceBinding{
		{Sequence: "g a", CommandID: "issues", Context: "issues"},
		{Sequence: "g m", CommandID: "menu", Context: "menu"},
	}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(60, 0)
	if result := dispatcher.Feed("issues", 'g', now); result.State != KeySequenceStatePending {
		t.Fatalf("issues prefix = %#v", result)
	}
	// A pending prefix belongs to the context that started it; a context switch
	// must not let the old sequence complete in the new context.
	if result := dispatcher.Feed("menu", 'a', now.Add(time.Millisecond)); result.State != KeySequenceStateIdle {
		t.Fatalf("context switch continuation = %#v, want idle", result)
	}
	if result := dispatcher.Feed("menu", 'g', now.Add(2*time.Millisecond)); result.State != KeySequenceStatePending {
		t.Fatalf("menu prefix = %#v", result)
	}
	if result := dispatcher.Feed("menu", 'm', now.Add(3*time.Millisecond)); result.CommandID != "menu" {
		t.Fatalf("menu complete = %#v", result)
	}
}
