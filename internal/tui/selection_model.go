package tui

// MarkedIssueSelection tracks issue IDs marked for a later bulk action.
//
// The zero value is ready for use. IDs are kept in the order in which they
// were first marked, with range and visible-list operations following the
// order supplied by the caller.
type MarkedIssueSelection struct {
	marked map[string]struct{}
	order  []string
	anchor string
}

// Toggle marks id when it is not marked, and unmarks it otherwise. A
// non-empty id always becomes the anchor for subsequent range extensions,
// including when toggling removes an existing mark.
func (s *MarkedIssueSelection) Toggle(id string) {
	if s == nil || id == "" {
		return
	}

	s.anchor = id
	if _, ok := s.marked[id]; ok {
		delete(s.marked, id)
		s.removeFromOrder(id)
		return
	}

	if s.marked == nil {
		s.marked = make(map[string]struct{})
	}
	s.marked[id] = struct{}{}
	s.order = append(s.order, id)
}

// ExtendRange marks each visible ID between the current anchor and cursorID,
// inclusive. Existing marks are retained. If the anchor is empty or is not
// visible, cursorID becomes the new anchor and only that cursor row is added.
// An empty or non-visible cursor leaves the selection unchanged.
func (s *MarkedIssueSelection) ExtendRange(visibleIDs []string, cursorID string) {
	if s == nil || cursorID == "" {
		return
	}

	visible := uniqueNonEmptyIDs(visibleIDs)
	cursorIndex := indexOfID(visible, cursorID)
	if cursorIndex < 0 {
		return
	}

	anchorIndex := indexOfID(visible, s.anchor)
	if anchorIndex < 0 {
		anchorIndex = cursorIndex
		s.anchor = cursorID
	}

	if anchorIndex <= cursorIndex {
		for _, id := range visible[anchorIndex : cursorIndex+1] {
			s.add(id)
		}
		return
	}
	for i := anchorIndex; i >= cursorIndex; i-- {
		s.add(visible[i])
	}
}

// SelectAllVisible marks all non-empty, unique IDs in visibleIDs, preserving
// their first-seen order. Existing marks and the current anchor are retained.
func (s *MarkedIssueSelection) SelectAllVisible(visibleIDs []string) {
	if s == nil {
		return
	}
	for _, id := range uniqueNonEmptyIDs(visibleIDs) {
		s.add(id)
	}
}

// Clear removes all marks and the range anchor.
func (s *MarkedIssueSelection) Clear() {
	if s == nil {
		return
	}
	s.marked = nil
	s.order = nil
	s.anchor = ""
}

// IsMarked reports whether id is currently marked.
func (s *MarkedIssueSelection) IsMarked(id string) bool {
	if s == nil || id == "" {
		return false
	}
	_, ok := s.marked[id]
	return ok
}

// Count returns the number of marked issue IDs.
func (s *MarkedIssueSelection) Count() int {
	if s == nil {
		return 0
	}
	return len(s.marked)
}

// IDs returns marked issue IDs in deterministic first-marked order.
func (s *MarkedIssueSelection) IDs() []string {
	if s == nil || len(s.order) == 0 {
		return nil
	}

	ids := make([]string, 0, len(s.order))
	for _, id := range s.order {
		if _, ok := s.marked[id]; ok {
			ids = append(ids, id)
		}
	}
	return ids
}

// EffectiveTargets returns marked IDs when any exist. Otherwise it returns
// cursorID when it is non-empty, or an empty slice when no target is present.
func (s *MarkedIssueSelection) EffectiveTargets(cursorID string) []string {
	if s != nil && len(s.marked) > 0 {
		return s.IDs()
	}
	if cursorID == "" {
		return nil
	}
	return []string{cursorID}
}

// Reconcile removes marks and the anchor that are not present in validIDs.
// The remaining marks retain their existing order.
func (s *MarkedIssueSelection) Reconcile(validIDs []string) {
	if s == nil {
		return
	}

	valid := make(map[string]struct{}, len(validIDs))
	for _, id := range validIDs {
		if id != "" {
			valid[id] = struct{}{}
		}
	}

	if len(s.marked) > 0 {
		for id := range s.marked {
			if _, ok := valid[id]; !ok {
				delete(s.marked, id)
			}
		}
	}
	if s.anchor != "" {
		if _, ok := valid[s.anchor]; !ok {
			s.anchor = ""
		}
	}

	if len(s.order) == 0 {
		return
	}
	kept := s.order[:0]
	for _, id := range s.order {
		if _, ok := s.marked[id]; ok {
			kept = append(kept, id)
		}
	}
	if len(kept) == 0 {
		s.order = nil
	} else {
		s.order = kept
	}
}

func (s *MarkedIssueSelection) add(id string) {
	if id == "" {
		return
	}
	if _, ok := s.marked[id]; ok {
		return
	}
	if s.marked == nil {
		s.marked = make(map[string]struct{})
	}
	s.marked[id] = struct{}{}
	s.order = append(s.order, id)
}

func (s *MarkedIssueSelection) removeFromOrder(id string) {
	for i, current := range s.order {
		if current == id {
			copy(s.order[i:], s.order[i+1:])
			s.order = s.order[:len(s.order)-1]
			return
		}
	}
}

func uniqueNonEmptyIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	unique := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return unique
}

func indexOfID(ids []string, id string) int {
	if id == "" {
		return -1
	}
	for i, current := range ids {
		if current == id {
			return i
		}
	}
	return -1
}
