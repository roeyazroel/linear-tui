package tui

import (
	"sort"

	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

// BuildCommentThreads turns Linear's flat comment activity into a stable
// forest. Comments whose parent is missing are kept as roots, while replies
// are nested to arbitrary depth. Invalid duplicate IDs and parent cycles are
// discarded from the relationship (the comment itself is retained once).
func BuildCommentThreads(comments []linearapi.Comment) []CommentThread {
	if len(comments) == 0 {
		return nil
	}

	ordered := append([]linearapi.Comment(nil), comments...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return commentLess(ordered[i], ordered[j])
	})

	nodes := make([]*commentThreadNode, 0, len(ordered))
	byID := make(map[string]*commentThreadNode, len(ordered))
	for _, comment := range ordered {
		if comment.ID == "" {
			continue
		}
		if _, exists := byID[comment.ID]; exists {
			continue
		}
		parentID := comment.ParentID
		if parentID == "" && comment.Parent != nil {
			parentID = comment.Parent.ID
		}
		item := &commentThreadNode{comment: cloneComment(comment), parentID: parentID}
		nodes = append(nodes, item)
		byID[item.comment.ID] = item
	}

	// Start with every resolvable parent edge, then break each cycle by
	// detaching its earliest comment. That keeps chronological roots stable
	// even when a later reply points back to an earlier comment.
	order := make(map[*commentThreadNode]int, len(nodes))
	for index, item := range nodes {
		order[item] = index
		if item.parentID == "" || item.parentID == item.comment.ID {
			continue
		}
		item.parent = byID[item.parentID]
	}
	for _, item := range nodes {
		path := make([]*commentThreadNode, 0)
		pathIndex := make(map[*commentThreadNode]int)
		for current := item; current != nil; current = current.parent {
			if cycleStart, found := pathIndex[current]; found {
				cycle := path[cycleStart:]
				detach := cycle[0]
				for _, candidate := range cycle[1:] {
					if order[candidate] < order[detach] {
						detach = candidate
					}
				}
				detach.parent = nil
				break
			}
			pathIndex[current] = len(path)
			path = append(path, current)
		}
	}
	for _, item := range nodes {
		if item.parent != nil {
			item.parent.children = append(item.parent.children, item)
		}
	}

	threads := make([]CommentThread, 0, len(nodes))
	for _, item := range nodes {
		if item.parent == nil {
			threads = append(threads, commentThreadFromNode(item))
		}
	}
	return threads
}

func commentLess(left, right linearapi.Comment) bool {
	if !left.CreatedAt.Equal(right.CreatedAt) {
		return left.CreatedAt.Before(right.CreatedAt)
	}
	if left.ID != right.ID {
		return left.ID < right.ID
	}
	leftParent, rightParent := left.ParentID, right.ParentID
	if leftParent == "" && left.Parent != nil {
		leftParent = left.Parent.ID
	}
	if rightParent == "" && right.Parent != nil {
		rightParent = right.Parent.ID
	}
	if leftParent != rightParent {
		return leftParent < rightParent
	}
	if left.Body != right.Body {
		return left.Body < right.Body
	}
	return left.Author.ID < right.Author.ID
}

func cloneComment(comment linearapi.Comment) linearapi.Comment {
	clone := comment
	if comment.Parent != nil {
		parent := *comment.Parent
		clone.Parent = &parent
	}
	if len(comment.Reactions) > 0 {
		clone.Reactions = append([]linearapi.Reaction(nil), comment.Reactions...)
	}
	return clone
}

type commentThreadNode struct {
	comment  linearapi.Comment
	parentID string
	parent   *commentThreadNode
	children []*commentThreadNode
}

func commentThreadFromNode(item *commentThreadNode) CommentThread {
	thread := CommentThread{
		Comment:   cloneComment(item.comment),
		ParentID:  item.parentID,
		Reactions: append([]linearapi.Reaction(nil), item.comment.Reactions...),
	}
	if len(item.children) > 0 {
		thread.Replies = make([]CommentThread, 0, len(item.children))
		for _, child := range item.children {
			thread.Replies = append(thread.Replies, commentThreadFromNode(child))
		}
	}
	return thread
}
