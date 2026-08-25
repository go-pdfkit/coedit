package coedit

import (
	"github.com/go-crdt/crdt"
	"github.com/go-crdt/crdt/structured"
)

// A Bookmark is one entry of the plan's outline.
type Bookmark struct {
	ID    BookmarkID
	Title string
	// Page is the page it points at, by identity rather than by number, so
	// that a bookmark still points at its page after somebody else has moved
	// it. It is empty for a bookmark that points at nothing.
	Page     PageID
	Children []Bookmark
}

// AddBookmark puts an entry under a parent, after a sibling — [AtRoot] for the
// top and the zero identity for first.
func (p *Plan) AddBookmark(parent, after BookmarkID, title string, page PageID) (BookmarkID, error) {
	var id BookmarkID
	err := p.editOutline(func(t *structured.Tree) ([]crdt.MapOp, error) {
		node, ops, err := t.Insert(parent, after)
		if err != nil {
			return nil, err
		}
		id = node
		// The node was just created and the field names are this package's
		// own, so neither of these can be refused.
		titleOp, _ := t.Records().SetField(node.String(), fieldTitle, []byte(title))
		pageOp, _ := t.Records().SetField(node.String(), fieldPage, []byte(page.String()))
		return append(ops, titleOp, pageOp), nil
	})
	return id, err
}

// MoveBookmark puts an entry under another parent, after another sibling.
func (p *Plan) MoveBookmark(node, parent, after BookmarkID) error {
	return p.editOutline(func(t *structured.Tree) ([]crdt.MapOp, error) {
		return t.Move(node, parent, after)
	})
}

// RemoveBookmark drops an entry and everything under it.
func (p *Plan) RemoveBookmark(node BookmarkID) error {
	return p.editOutline(func(t *structured.Tree) ([]crdt.MapOp, error) {
		return t.RemoveSubtree(node)
	})
}

// Outline is the bookmark tree as it now stands.
func (p *Plan) Outline() []Bookmark {
	var out []Bookmark
	_ = p.outline(func(t *structured.Tree) {
		out = readChildren(t, AtRoot)
	})
	return out
}

// readChildren gathers one level of the tree and everything under it.
func readChildren(t *structured.Tree, parent BookmarkID) []Bookmark {
	var out []Bookmark
	for _, node := range t.Children(parent) {
		b := Bookmark{ID: node}
		if v, ok := t.Records().GetField(node.String(), fieldTitle); ok {
			b.Title = string(v)
		}
		if v, ok := t.Records().GetField(node.String(), fieldPage); ok {
			b.Page = pageIDOf(string(v))
		}
		b.Children = readChildren(t, node)
		out = append(out, b)
	}
	return out
}
