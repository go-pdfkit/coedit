package coedit

import (
	"testing"

	"github.com/go-pdfkit/reader"
)

func TestBookmarksOverAPlan(t *testing.T) {
	p, _ := plan(t, 1, map[string][]byte{"a.pdf": samplePDF(t, 4)})
	var pages []PageID
	for n := 1; n <= 3; n++ {
		id, err := p.Append("a.pdf", n)
		if err != nil {
			t.Fatal(err)
		}
		pages = append(pages, id)
	}
	top, err := p.AddBookmark(AtRoot, BookmarkID{}, "One", pages[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.AddBookmark(top, BookmarkID{}, "Under one", pages[1]); err != nil {
		t.Fatal(err)
	}
	second, err := p.AddBookmark(AtRoot, top, "Two", pages[2])
	if err != nil {
		t.Fatal(err)
	}

	marks := p.Outline()
	if len(marks) != 2 || marks[0].Title != "One" || marks[1].Title != "Two" {
		t.Fatalf("the outline is %+v", marks)
	}
	if len(marks[0].Children) != 1 || marks[0].Children[0].Title != "Under one" {
		t.Errorf("the first bookmark holds %+v", marks[0].Children)
	}
	if !samePage(marks[0].Page, pages[0]) {
		t.Error("the first bookmark points elsewhere")
	}

	// A bookmark points at a page rather than at a number, so moving the page
	// moves the bookmark with it.
	if err := p.Move(pages[0], pages[2]); err != nil {
		t.Fatal(err)
	}
	out, err := p.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	d, err := reader.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	if d.PageCount() != 3 {
		t.Fatalf("%d pages", d.PageCount())
	}

	// Moving and removing entries.
	if err := p.MoveBookmark(second, top, BookmarkID{}); err != nil {
		t.Fatal(err)
	}
	if marks = p.Outline(); len(marks) != 1 || len(marks[0].Children) != 2 {
		t.Errorf("after moving, the outline is %+v", marks)
	}
	if err := p.RemoveBookmark(top); err != nil {
		t.Fatal(err)
	}
	if marks = p.Outline(); len(marks) != 0 {
		t.Errorf("after removing, the outline is %+v", marks)
	}
	// A bookmark under one that is not there cannot be added.
	if _, err := p.AddBookmark(BookmarkID{Site: 9, Seq: 9}, BookmarkID{}, "Nowhere", pages[0]); err == nil {
		t.Error("a bookmark under nothing was added")
	}
}

func TestABookmarkPointingAtAPageThatHasGone(t *testing.T) {
	// A heading whose section has gone is not a heading any more, and neither
	// is anything under it.
	p, _ := plan(t, 1, map[string][]byte{"a.pdf": samplePDF(t, 3)})
	first, err := p.Append("a.pdf", 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.Append("a.pdf", 2)
	if err != nil {
		t.Fatal(err)
	}
	gone, err := p.AddBookmark(AtRoot, BookmarkID{}, "Gone", first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.AddBookmark(gone, BookmarkID{}, "Under the gone one", second); err != nil {
		t.Fatal(err)
	}
	if _, err := p.AddBookmark(AtRoot, gone, "Still here", second); err != nil {
		t.Fatal(err)
	}
	if err := p.Remove(first); err != nil {
		t.Fatal(err)
	}
	out, err := p.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	d, err := reader.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := d.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	root, ok := d.GetDict(cat, "Outlines")
	if !ok {
		t.Fatal("the document carries no outline")
	}
	count, _ := reader.ToInt(mustResolve(d, root.Get("Count")))
	if count != 1 {
		t.Errorf("the outline holds %d entries, want 1", count)
	}
}
