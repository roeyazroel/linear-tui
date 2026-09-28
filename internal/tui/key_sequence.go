package tui

import (
	"fmt"
	"strings"
	"time"
)

// KeySequenceBinding describes a multi-key command chord. Sequence is a
// space-separated list of single-rune keys, for example "g a".
type KeySequenceBinding struct {
	Sequence  string
	CommandID string
	Context   string
	Hint      string
}

// KeySequenceState is an alias for string so callers can compare State with
// either the exported constants or string literals.
type KeySequenceState = string

const (
	KeySequenceStateIdle      = "idle"
	KeySequenceStatePending   = "pending"
	KeySequenceStateComplete  = "complete"
	KeySequenceStateInvalid   = "invalid"
	KeySequenceStateTimedOut  = "timed_out"
	KeySequenceIdle           = KeySequenceStateIdle
	KeySequencePending        = KeySequenceStatePending
	KeySequenceComplete       = KeySequenceStateComplete
	KeySequenceInvalid        = KeySequenceStateInvalid
	KeySequenceTimedOut       = KeySequenceStateTimedOut
	defaultKeySequenceTimeout = 750 * time.Millisecond
)

// KeySequenceResult describes the effect of feeding one key to a dispatcher.
type KeySequenceResult struct {
	State     KeySequenceState
	CommandID string
	Prefix    string
	Hint      string
	Reprocess bool
}

type keySequenceEntry struct {
	binding KeySequenceBinding
	keys    []rune
}

// KeySequenceDispatcher is a small, deterministic state machine for keyboard
// chords. It has no UI or timer dependencies; callers provide the current time
// to Feed and decide how to display or reprocess the result.
type KeySequenceDispatcher struct {
	byContext map[string][]keySequenceEntry
	timeout   time.Duration

	pendingKeys    []rune
	pendingContext string
	pendingSince   time.Time
}

// NewKeySequenceDispatcher validates and indexes bindings for a dispatcher.
// A non-positive timeout uses the stable 750ms default.
func NewKeySequenceDispatcher(bindings []KeySequenceBinding, timeout time.Duration) (*KeySequenceDispatcher, error) {
	if timeout <= 0 {
		timeout = defaultKeySequenceTimeout
	}
	dispatcher := &KeySequenceDispatcher{
		byContext: make(map[string][]keySequenceEntry),
		timeout:   timeout,
	}

	for index, raw := range bindings {
		commandID := strings.TrimSpace(raw.CommandID)
		if commandID == "" {
			return nil, fmt.Errorf("binding %d has empty command", index)
		}
		context := strings.TrimSpace(raw.Context)
		if context == "" {
			return nil, fmt.Errorf("binding %d has empty context", index)
		}

		fields := strings.Fields(raw.Sequence)
		if len(fields) < 2 {
			return nil, fmt.Errorf("binding %d sequence must contain at least two keys", index)
		}
		keys := make([]rune, len(fields))
		for keyIndex, field := range fields {
			runes := []rune(field)
			if len(runes) != 1 {
				return nil, fmt.Errorf("binding %d key %q must be single-rune", index, field)
			}
			keys[keyIndex] = runes[0]
		}

		normalized := raw
		normalized.Sequence = strings.Join(fields, " ")
		normalized.CommandID = commandID
		normalized.Context = context
		entry := keySequenceEntry{binding: normalized, keys: keys}

		entries := dispatcher.byContext[context]
		for _, existing := range entries {
			switch {
			case sameKeyRunes(existing.keys, keys):
				return nil, fmt.Errorf("binding %d duplicates sequence %q in context %q", index, normalized.Sequence, context)
			case keyRunesPrefix(existing.keys, keys), keyRunesPrefix(keys, existing.keys):
				return nil, fmt.Errorf("binding %d sequence %q is a complete/prefix conflict in context %q", index, normalized.Sequence, context)
			}
		}
		dispatcher.byContext[context] = append(entries, entry)
	}
	return dispatcher, nil
}

// Feed consumes one key in context at the supplied time. A timed-out or
// invalid prefix is reset; callers can use Reprocess to decide whether the
// current key should be sent through another key handler.
func (dispatcher *KeySequenceDispatcher) Feed(context string, key rune, now time.Time) KeySequenceResult {
	if dispatcher == nil {
		return KeySequenceResult{State: KeySequenceStateIdle}
	}
	context = normalizeKeySequenceContext(context)

	if !dispatcher.Pending() {
		return dispatcher.feedFresh(context, key, now)
	}

	prefix := dispatcher.Prefix()
	hint := dispatcher.PendingHint(context)
	if now.Sub(dispatcher.pendingSince) > dispatcher.timeout {
		dispatcher.Reset()
		return KeySequenceResult{
			State:     KeySequenceStateTimedOut,
			Prefix:    prefix,
			Hint:      hint,
			Reprocess: true,
		}
	}

	// A pending chord is scoped to the context in which it began. Switching
	// panes while a prefix is pending must not complete the old pane's chord.
	if context != dispatcher.pendingContext {
		dispatcher.Reset()
		return dispatcher.feedFresh(context, key, now)
	}

	fullPrefix := append(append([]rune(nil), dispatcher.pendingKeys...), key)
	candidates := dispatcher.matchingCandidates(context, fullPrefix)
	if len(candidates) == 0 {
		dispatcher.Reset()
		return KeySequenceResult{
			State:     KeySequenceStateInvalid,
			Prefix:    prefix,
			Hint:      hint,
			Reprocess: dispatcher.canBegin(context, key),
		}
	}

	for _, candidate := range candidates {
		if sameKeyRunes(candidate.keys, fullPrefix) {
			dispatcher.Reset()
			return KeySequenceResult{
				State:     KeySequenceStateComplete,
				CommandID: candidate.binding.CommandID,
				Prefix:    keyRunesString(fullPrefix),
				Hint:      candidate.binding.Hint,
			}
		}
	}

	dispatcher.pendingKeys = fullPrefix
	return KeySequenceResult{
		State:  KeySequenceStatePending,
		Prefix: keyRunesString(fullPrefix),
		Hint:   dispatcher.hintForCandidates(candidates),
	}
}

func (dispatcher *KeySequenceDispatcher) feedFresh(context string, key rune, now time.Time) KeySequenceResult {
	candidates := dispatcher.matchingCandidates(context, []rune{key})
	if len(candidates) == 0 {
		return KeySequenceResult{State: KeySequenceStateIdle}
	}
	dispatcher.pendingKeys = []rune{key}
	dispatcher.pendingContext = context
	dispatcher.pendingSince = now
	return KeySequenceResult{
		State:  KeySequenceStatePending,
		Prefix: keyRunesString(dispatcher.pendingKeys),
		Hint:   dispatcher.hintForCandidates(candidates),
	}
}

// Reset clears a pending prefix and its start time.
func (dispatcher *KeySequenceDispatcher) Reset() {
	if dispatcher == nil {
		return
	}
	dispatcher.pendingKeys = nil
	dispatcher.pendingContext = ""
	dispatcher.pendingSince = time.Time{}
}

// Pending reports whether a prefix is currently waiting for continuation.
func (dispatcher *KeySequenceDispatcher) Pending() bool {
	return dispatcher != nil && len(dispatcher.pendingKeys) > 0
}

// Prefix returns the current pending prefix in normalized space-separated form.
func (dispatcher *KeySequenceDispatcher) Prefix() string {
	if dispatcher == nil {
		return ""
	}
	return keyRunesString(dispatcher.pendingKeys)
}

// PendingHint returns the hint associated with the pending prefix in context.
func (dispatcher *KeySequenceDispatcher) PendingHint(context string) string {
	if dispatcher == nil || !dispatcher.Pending() {
		return ""
	}
	context = normalizeKeySequenceContext(context)
	if context != dispatcher.pendingContext {
		return ""
	}
	return dispatcher.hintForCandidates(dispatcher.matchingCandidates(context, dispatcher.pendingKeys))
}

func (dispatcher *KeySequenceDispatcher) canBegin(context string, key rune) bool {
	return len(dispatcher.matchingCandidates(context, []rune{key})) > 0
}

// matchingCandidates applies exact-context precedence over global bindings.
// The first group with a matching prefix wins, keeping context-specific
// prefixes from being shadowed by a global chord while still allowing global
// fallback when no exact-context binding applies.
func (dispatcher *KeySequenceDispatcher) matchingCandidates(context string, prefix []rune) []keySequenceEntry {
	if dispatcher == nil {
		return nil
	}
	groups := dispatcher.candidateGroups(context)
	for _, group := range groups {
		matched := make([]keySequenceEntry, 0, len(group))
		for _, candidate := range group {
			if keyRunesStartsWith(candidate.keys, prefix) {
				matched = append(matched, candidate)
			}
		}
		if len(matched) > 0 {
			return matched
		}
	}
	return nil
}

func (dispatcher *KeySequenceDispatcher) candidateGroups(context string) [][]keySequenceEntry {
	global := dispatcher.byContext["global"]
	if context == "global" {
		return [][]keySequenceEntry{global}
	}
	return [][]keySequenceEntry{dispatcher.byContext[context], global}
}

func (dispatcher *KeySequenceDispatcher) hintForCandidates(candidates []keySequenceEntry) string {
	for _, candidate := range candidates {
		if candidate.binding.Hint != "" {
			return candidate.binding.Hint
		}
	}
	return ""
}

func normalizeKeySequenceContext(context string) string {
	context = strings.TrimSpace(context)
	if context == "" {
		return "global"
	}
	return context
}

func keyRunesString(keys []rune) string {
	if len(keys) == 0 {
		return ""
	}
	parts := make([]string, len(keys))
	for index, key := range keys {
		parts[index] = string(key)
	}
	return strings.Join(parts, " ")
}

func sameKeyRunes(left, right []rune) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func keyRunesPrefix(prefix, full []rune) bool {
	if len(prefix) >= len(full) {
		return false
	}
	return keyRunesStartsWith(full, prefix)
}

func keyRunesStartsWith(full, prefix []rune) bool {
	if len(prefix) > len(full) {
		return false
	}
	for index := range prefix {
		if full[index] != prefix[index] {
			return false
		}
	}
	return true
}
