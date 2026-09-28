package tui

import (
	"reflect"
	"testing"
)

func TestMarkedIssueSelection_ZeroValueAndToggle(t *testing.T) {
	var selection MarkedIssueSelection

	if selection.Count() != 0 {
		t.Fatalf("zero-value Count() = %d, want 0", selection.Count())
	}
	if selection.IsMarked("issue-1") {
		t.Fatal("zero-value selection marks issue-1")
	}
	if got := selection.IDs(); len(got) != 0 {
		t.Fatalf("zero-value IDs() = %#v, want empty", got)
	}

	selection.Toggle("")
	if selection.Count() != 0 {
		t.Fatalf("Toggle(\"\") changed count to %d", selection.Count())
	}

	selection.Toggle("issue-2")
	selection.Toggle("issue-1")
	if got, want := selection.IDs(), []string{"issue-2", "issue-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("IDs() = %#v, want %#v", got, want)
	}
	if !selection.IsMarked("issue-2") || !selection.IsMarked("issue-1") {
		t.Fatal("toggled issues are not marked")
	}

	selection.Toggle("issue-2")
	if got, want := selection.IDs(), []string{"issue-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("IDs() after unmark = %#v, want %#v", got, want)
	}
	if selection.IsMarked("issue-2") {
		t.Fatal("toggled-off issue-2 remains marked")
	}
}

func TestMarkedIssueSelection_ExtendRange(t *testing.T) {
	var selection MarkedIssueSelection
	visible := []string{"issue-1", "", "issue-2", "issue-2", "issue-3", "issue-4"}

	selection.Toggle("issue-2")
	selection.ExtendRange(visible, "issue-4")
	if got, want := selection.IDs(), []string{"issue-2", "issue-3", "issue-4"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("forward range IDs() = %#v, want %#v", got, want)
	}

	selection.Clear()
	selection.Toggle("issue-4")
	selection.ExtendRange(visible, "issue-2")
	if got, want := selection.IDs(), []string{"issue-4", "issue-3", "issue-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("reverse range IDs() = %#v, want %#v", got, want)
	}

	selection.Clear()
	selection.Toggle("outside-visible")
	selection.ExtendRange(visible, "issue-3")
	if got, want := selection.IDs(), []string{"outside-visible", "issue-3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("invalid-anchor range IDs() = %#v, want %#v", got, want)
	}

	selection.Clear()
	selection.Toggle("issue-1")
	selection.ExtendRange([]string{"issue-1", "issue-2"}, "missing")
	if got, want := selection.IDs(), []string{"issue-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("invalid-cursor range IDs() = %#v, want %#v", got, want)
	}
}

func TestMarkedIssueSelection_SelectAllVisibleAndClear(t *testing.T) {
	var selection MarkedIssueSelection
	selection.Toggle("preexisting")
	selection.SelectAllVisible([]string{"issue-3", "", "issue-1", "issue-3", "issue-2"})

	if got, want := selection.IDs(), []string{"preexisting", "issue-3", "issue-1", "issue-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SelectAllVisible IDs() = %#v, want %#v", got, want)
	}
	if got, want := selection.Count(), 4; got != want {
		t.Fatalf("Count() = %d, want %d", got, want)
	}

	selection.Clear()
	if selection.Count() != 0 || len(selection.IDs()) != 0 {
		t.Fatalf("Clear() left selection: count=%d IDs=%#v", selection.Count(), selection.IDs())
	}
}

func TestMarkedIssueSelection_EffectiveTargets(t *testing.T) {
	var selection MarkedIssueSelection

	if got := selection.EffectiveTargets(""); len(got) != 0 {
		t.Fatalf("empty EffectiveTargets() = %#v, want empty", got)
	}
	if got, want := selection.EffectiveTargets("cursor"), []string{"cursor"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cursor EffectiveTargets() = %#v, want %#v", got, want)
	}

	selection.Toggle("marked")
	if got, want := selection.EffectiveTargets("cursor"), []string{"marked"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("marked EffectiveTargets() = %#v, want %#v", got, want)
	}
	if got, want := selection.EffectiveTargets(""), []string{"marked"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("marked/no-cursor EffectiveTargets() = %#v, want %#v", got, want)
	}
}

func TestMarkedIssueSelection_Reconcile(t *testing.T) {
	var selection MarkedIssueSelection
	selection.SelectAllVisible([]string{"issue-3", "issue-1", "issue-2"})
	selection.Toggle("anchor")
	selection.Toggle("anchor") // Keep the anchor while removing its mark.
	selection.ExtendRange([]string{"issue-1", "issue-2", "issue-3"}, "issue-2")

	selection.Reconcile([]string{"issue-1", "issue-3"})
	if got, want := selection.IDs(), []string{"issue-3", "issue-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Reconcile IDs() = %#v, want %#v", got, want)
	}

	selection.Toggle("issue-3")
	selection.Reconcile([]string{"issue-1"})
	if selection.IsMarked("issue-3") {
		t.Fatal("Reconcile left invalid issue-3 marked")
	}
	if got := selection.IDs(); !reflect.DeepEqual(got, []string{"issue-1"}) {
		t.Fatalf("Reconcile after anchor removal IDs() = %#v, want [issue-1]", got)
	}

	selection.Clear()
	selection.Toggle("stale")
	selection.Reconcile(nil)
	if selection.Count() != 0 || len(selection.IDs()) != 0 {
		t.Fatalf("Reconcile(nil) left selection: count=%d IDs=%#v", selection.Count(), selection.IDs())
	}
}
