// Package coedit is a PDF several people are editing at once.
//
// What is shared is not the file. A file is a settled thing — merging two
// people's copies of one means merging two piles of bytes, which cannot be
// done. What is shared is the plan: which pages, from which files, in which
// order, turned and cropped how, with which bookmarks over them. That is a
// list, a tree and a handful of fields, and a list, a tree and a handful of
// fields are things a conflict-free replicated data type merges without asking
// anybody.
//
// So two people reordering the same document keep both reorderings, two people
// rotating different pages keep both rotations, and two people rotating the
// same page settle on one of the two — which is what rotating a page twice
// means. Nobody waits for a lock and nobody loses work, however many of them
// there are.
//
// A plan becomes a file through [Plan.Build], which hands back an
// [ops.Doc] — the same document the command-line tool and the browser build,
// so what comes out of a shared edit is what comes out of any other.
package coedit

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/go-crdt/crdt"
	"github.com/go-crdt/crdt/structured"
	"github.com/go-deltasync/chunk"
)

// The parts a plan is made of. The names are the document's layout, the same
// on every replica, so a plan written by one is read by all.
const (
	pagesPart     = "pdf:pages"
	outlinePart   = "pdf:outline"
	metadataPart  = "pdf:meta"
	sourcesPart   = "pdf:sources"
	chunksPartOld = "chunks"
)

// The fields a page item carries. They are short because every one of them is
// a map key sent over the wire.
const (
	fieldSource = "s" // which file the page comes from
	fieldNumber = "n" // its number in that file, counting from one
	fieldRotate = "r" // a quarter turn at a time, clockwise
	fieldCrop   = "c" // the visible area, as four numbers
)

// The fields a bookmark carries.
const (
	fieldTitle = "t" // what the bookmark says
	fieldPage  = "p" // the page it points at, by identity
)

// A PageID names one page of the plan. It survives being moved, so two people
// moving the same page mean the same page.
type PageID = structured.ItemID

// AtStart is where a page goes when it goes first.
var AtStart = structured.SeqStart

// A BookmarkID names one bookmark.
type BookmarkID = structured.TreeID

// AtRoot is the top of the bookmark tree.
var AtRoot = structured.TreeRoot

// ErrNoSuchPage reports an operation naming a page this replica does not hold —
// one never added, or one somebody else removed.
var ErrNoSuchPage = errors.New("coedit: no such page")

// ErrNoSuchFile reports a page naming a file the plan does not carry.
var ErrNoSuchFile = errors.New("coedit: no such file")

// A Store is where a plan keeps its parts: a composite held on its own, or one
// shared with everybody else over a session.
//
// The two methods are the whole of what a plan needs, and they are the shape a
// session already has: read the map, or change it and say what changed.
type Store interface {
	// Read runs fn against the named map part.
	Read(part string, fn func(*crdt.Map)) error
	// Edit runs fn against the named map part and publishes what it produced.
	Edit(part string, fn func(*crdt.Map) ([]crdt.MapOp, error)) error
}

// A Plan is a PDF being edited: the pages in their order, the bookmarks over
// them, and the files they come from.
type Plan struct {
	store Store
	// chunker cuts a file into the pieces it is stored and sent as. Cutting on
	// the content rather than every so many bytes is what makes replacing one
	// page of a file send one page rather than the file.
	chunker structured.Chunker
}

// Open reads a store as a plan.
func Open(store Store) *Plan {
	return &Plan{store: store, chunker: chunk.Cut(chunk.Config{})}
}

// pages runs fn against the page sequence.
func (p *Plan) pages(fn func(*structured.Sequence)) error {
	return p.store.Read(pagesPart, func(m *crdt.Map) { fn(structured.SequenceOf(m)) })
}

// editPages runs fn against the page sequence and publishes what it produced.
func (p *Plan) editPages(fn func(*structured.Sequence) ([]crdt.MapOp, error)) error {
	return p.store.Edit(pagesPart, func(m *crdt.Map) ([]crdt.MapOp, error) {
		return fn(structured.SequenceOf(m))
	})
}

// outline runs fn against the bookmark tree.
func (p *Plan) outline(fn func(*structured.Tree)) error {
	return p.store.Read(outlinePart, func(m *crdt.Map) { fn(structured.TreeOf(m)) })
}

// editOutline runs fn against the bookmark tree and publishes what it produced.
func (p *Plan) editOutline(fn func(*structured.Tree) ([]crdt.MapOp, error)) error {
	return p.store.Edit(outlinePart, func(m *crdt.Map) ([]crdt.MapOp, error) {
		return fn(structured.TreeOf(m))
	})
}

// A Page is one page of the plan, as it now stands.
type Page struct {
	ID     PageID
	Source string
	Number int
	Rotate int
	Crop   []float64
}

// Pages is every page of the plan, in order.
//
// An item that names no file is not a page. That is not a nicety: one person
// removing a page while another turns it leaves the item behind with the turn
// on it and nothing else, because a field written after a removal puts the
// record back. What it does not put back is which file the page came from, so
// that is what says whether there is a page there at all.
func (p *Plan) Pages() []Page {
	var out []Page
	_ = p.pages(func(s *structured.Sequence) {
		for _, id := range s.Items() {
			page := readPage(s, id)
			if page.Source == "" || page.Number < 1 {
				continue
			}
			out = append(out, page)
		}
	})
	return out
}

// Len is how many pages the plan holds.
func (p *Plan) Len() int { return len(p.Pages()) }

// readPage gathers one page's fields.
func readPage(s *structured.Sequence, id PageID) Page {
	page := Page{ID: id}
	if v, ok := s.GetField(id, fieldSource); ok {
		page.Source = string(v)
	}
	if v, ok := s.GetField(id, fieldNumber); ok {
		page.Number, _ = strconv.Atoi(string(v))
	}
	if v, ok := s.GetField(id, fieldRotate); ok {
		page.Rotate, _ = strconv.Atoi(string(v))
	}
	if v, ok := s.GetField(id, fieldCrop); ok {
		page.Crop = decodeBox(string(v))
	}
	return page
}

// Append puts a page of a file at the end of the plan.
func (p *Plan) Append(file string, number int) (PageID, error) {
	last := AtStart
	_ = p.pages(func(s *structured.Sequence) {
		if items := s.Items(); len(items) > 0 {
			last = items[len(items)-1]
		}
	})
	return p.InsertAfter(last, file, number)
}

// InsertAfter puts a page of a file after another page, or first for
// [AtStart].
func (p *Plan) InsertAfter(after PageID, file string, number int) (PageID, error) {
	if number < 1 {
		return PageID{}, fmt.Errorf("coedit: page %d is not a page", number)
	}
	var id PageID
	err := p.editPages(func(s *structured.Sequence) ([]crdt.MapOp, error) {
		new, ops, err := s.Insert(after, nil)
		if err != nil {
			return nil, err
		}
		id = new
		// The item was just created and the field names are this package's
		// own, so neither of these can be refused.
		source, _ := s.SetField(new, fieldSource, []byte(file))
		count, _ := s.SetField(new, fieldNumber, []byte(strconv.Itoa(number)))
		return append(ops, source, count), nil
	})
	return id, err
}

// Move puts a page after another one, or first for [AtStart]. Two people
// moving the same page settle on one of the two moves, and two people moving
// different pages keep both.
func (p *Plan) Move(page, after PageID) error {
	return p.editPages(func(s *structured.Sequence) ([]crdt.MapOp, error) {
		op, err := s.Move(page, after)
		if err != nil {
			return nil, err
		}
		return []crdt.MapOp{op}, nil
	})
}

// Remove drops a page.
func (p *Plan) Remove(page PageID) error {
	return p.editPages(func(s *structured.Sequence) ([]crdt.MapOp, error) {
		return s.Remove(page)
	})
}

// Rotate turns a page, in quarters clockwise. It is the rotation the page will
// have, not one added to what it has: two people asking for the same rotation
// agree, and two asking for different ones settle on one.
func (p *Plan) Rotate(page PageID, degrees int) error {
	return p.setField(page, fieldRotate, strconv.Itoa(normaliseTurn(degrees)))
}

// Crop sets what of a page is visible, as the four numbers a PDF rectangle is.
// A box of nothing puts back whatever the source page said.
func (p *Plan) Crop(page PageID, box []float64) error {
	if len(box) == 0 {
		return p.setField(page, fieldCrop, "")
	}
	if len(box) != 4 {
		return fmt.Errorf("coedit: a box takes four numbers, not %d", len(box))
	}
	return p.setField(page, fieldCrop, encodeBox(box))
}

// setField writes one of a page's fields.
func (p *Plan) setField(page PageID, field, value string) error {
	return p.editPages(func(s *structured.Sequence) ([]crdt.MapOp, error) {
		if s.IndexOf(page) < 0 {
			return nil, ErrNoSuchPage
		}
		// The page is here — the line above says so — and the field name is
		// this package's own.
		op, _ := s.SetField(page, field, []byte(value))
		return []crdt.MapOp{op}, nil
	})
}

// normaliseTurn puts a rotation in the quarter turns a PDF allows.
func normaliseTurn(degrees int) int {
	d := degrees % 360
	if d < 0 {
		d += 360
	}
	return d / 90 * 90
}
