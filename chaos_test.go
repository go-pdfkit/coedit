package coedit

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/go-crdt/crdt"
)

// The point of a shared plan is not that two people can edit it. It is that
// any number of them can, in any order, on any connection, and end up with the
// same document — so that is what is tested: many replicas, each making its
// own random edits, merged in a random order, and asked whether they agree.
//
// Two browser tabs would not have found any of the things this does.
func TestManyEditorsEndUpWithTheSamePlan(t *testing.T) {
	const (
		editors = 25
		rounds  = 40
	)
	file := samplePDF(t, 8)

	// Everyone starts from the same plan, which is how a session begins:
	// somebody creates it and the rest join.
	first := NewLocal(1)
	origin := Open(first)
	if err := origin.Files().Put("a.pdf", file); err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= 4; n++ {
		if _, err := origin.Append("a.pdf", n); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := first.Snapshot()

	replicas := make([]*Local, editors)
	plans := make([]*Plan, editors)
	for i := range replicas {
		local, err := LoadLocal(crdt.SiteID(i+1), snapshot)
		if err != nil {
			t.Fatal(err)
		}
		replicas[i] = local
		plans[i] = Open(local)
	}

	rng := rand.New(rand.NewSource(20260825))
	for round := 0; round < rounds; round++ {
		for i, p := range plans {
			edit(t, rng, p, i)
		}
		// A random pair meets and exchanges everything each lacks, which is
		// what a session does when a message arrives and what two people
		// working apart do when they meet.
		for k := 0; k < editors; k++ {
			a, b := rng.Intn(editors), rng.Intn(editors)
			if a == b {
				continue
			}
			if err := replicas[a].Merge(replicas[b]); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Everyone meets everyone, twice round, so that nothing is left in flight.
	for pass := 0; pass < 2; pass++ {
		for i := range replicas {
			for j := range replicas {
				if i == j {
					continue
				}
				if err := replicas[i].Merge(replicas[j]); err != nil {
					t.Fatal(err)
				}
			}
		}
	}

	// The whole point: the same plan, page for page, field for field.
	want := describe(plans[0])
	for i, p := range plans[1:] {
		if got := describe(p); got != want {
			t.Fatalf("replica %d ended up with\n%s\nand replica 0 with\n%s", i+1, got, want)
		}
	}
	// And the same document, since a plan that agrees but builds differently
	// would agree about nothing that matters.
	out, err := plans[0].Bytes()
	if err != nil {
		t.Fatal(err)
	}
	first0 := contents(t, out)
	for i, p := range plans[1:] {
		other, err := p.Bytes()
		if err != nil {
			t.Fatalf("replica %d: %v", i+1, err)
		}
		if got := contents(t, other); !sameStrings(got, first0) {
			t.Errorf("replica %d built %v, replica 0 built %v", i+1, got, first0)
		}
	}
	t.Logf("%d editors, %d rounds each, agreed on %d pages", editors, rounds, len(first0))
}

// edit makes one random change, of every kind a plan has.
func edit(t *testing.T, rng *rand.Rand, p *Plan, who int) {
	t.Helper()
	pages := p.Pages()
	switch rng.Intn(6) {
	case 0:
		if _, err := p.Append("a.pdf", 1+rng.Intn(8)); err != nil {
			t.Fatal(err)
		}
	case 1:
		if len(pages) == 0 {
			return
		}
		at := pages[rng.Intn(len(pages))]
		if _, err := p.InsertAfter(at.ID, "a.pdf", 1+rng.Intn(8)); err != nil {
			t.Fatal(err)
		}
	case 2:
		if len(pages) < 2 {
			return
		}
		from := pages[rng.Intn(len(pages))]
		to := pages[rng.Intn(len(pages))]
		if samePage(from.ID, to.ID) {
			return
		}
		// A move of a page somebody else has just removed is not an error
		// worth failing over: it is what happens.
		_ = p.Move(from.ID, to.ID)
	case 3:
		if len(pages) < 2 {
			return
		}
		_ = p.Remove(pages[rng.Intn(len(pages))].ID)
	case 4:
		if len(pages) == 0 {
			return
		}
		_ = p.Rotate(pages[rng.Intn(len(pages))].ID, 90*rng.Intn(4))
	case 5:
		if len(pages) == 0 {
			return
		}
		at := pages[rng.Intn(len(pages))]
		if _, err := p.AddBookmark(AtRoot, BookmarkID{},
			fmt.Sprintf("from %d", who), at.ID); err != nil {
			t.Fatal(err)
		}
	}
}

// describe writes down everything a plan holds, so that two of them can be
// compared as one string rather than field by field.
func describe(p *Plan) string {
	out := ""
	for _, page := range p.Pages() {
		out += fmt.Sprintf("%s %s#%d r%d c%v\n", page.ID, page.Source, page.Number, page.Rotate, page.Crop)
	}
	out += describeMarks(p.Outline(), 0)
	return out
}

func describeMarks(marks []Bookmark, depth int) string {
	out := ""
	for _, m := range marks {
		for i := 0; i < depth; i++ {
			out += "  "
		}
		out += fmt.Sprintf("%s %q -> %s\n", m.ID, m.Title, m.Page)
		out += describeMarks(m.Children, depth+1)
	}
	return out
}

func TestEditorsWhoCannotReachEachOther(t *testing.T) {
	// Two groups edit for a while with no way to reach each other — a train,
	// a flight, a conference wireless — and then meet. Nothing either group
	// did is lost, and both end up with the same document.
	file := samplePDF(t, 6)
	origin := NewLocal(1)
	start := Open(origin)
	if err := start.Files().Put("a.pdf", file); err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= 3; n++ {
		if _, err := start.Append("a.pdf", n); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := origin.Snapshot()

	const perSide = 4
	var left, right []*Local
	var leftPlans, rightPlans []*Plan
	for i := 0; i < perSide*2; i++ {
		local, err := LoadLocal(crdt.SiteID(i+1), snapshot)
		if err != nil {
			t.Fatal(err)
		}
		if i < perSide {
			left = append(left, local)
			leftPlans = append(leftPlans, Open(local))
		} else {
			right = append(right, local)
			rightPlans = append(rightPlans, Open(local))
		}
	}

	rng := rand.New(rand.NewSource(4242))
	// Each side works away, seeing only itself.
	for round := 0; round < 20; round++ {
		for side, plans := range [][]*Plan{leftPlans, rightPlans} {
			for i, p := range plans {
				edit(t, rng, p, side*perSide+i)
			}
		}
		mergeAll(t, left)
		mergeAll(t, right)
	}
	// One page each side turned, so that both sides have said something about
	// the same document and neither is empty.
	if pages := leftPlans[0].Pages(); len(pages) > 0 {
		if err := leftPlans[0].Rotate(pages[0].ID, 180); err != nil {
			t.Fatal(err)
		}
	}
	mergeAll(t, left)

	// And then the two meet.
	everyone := append(append([]*Local{}, left...), right...)
	mergeAll(t, everyone)
	mergeAll(t, everyone)

	all := append(append([]*Plan{}, leftPlans...), rightPlans...)
	want := describe(all[0])
	for i, p := range all[1:] {
		if got := describe(p); got != want {
			t.Fatalf("after meeting, replica %d differs from replica 0", i+1)
		}
	}
	if len(all[0].Pages()) == 0 {
		t.Error("everything was lost")
	}
	t.Logf("two groups of %d, twenty rounds apart, agreed on %d pages",
		perSide, len(all[0].Pages()))
}

// mergeAll has everyone take everything everyone else has.
func mergeAll(t *testing.T, replicas []*Local) {
	t.Helper()
	for i := range replicas {
		for j := range replicas {
			if i == j {
				continue
			}
			if err := replicas[i].Merge(replicas[j]); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestAnEditorWhoLeavesAndComesBack(t *testing.T) {
	// Work done while away is not work lost: a snapshot is what a session is
	// rejoined with.
	file := samplePDF(t, 4)
	first := NewLocal(1)
	p := Open(first)
	if err := p.Files().Put("a.pdf", file); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Append("a.pdf", 1); err != nil {
		t.Fatal(err)
	}

	away, err := LoadLocal(2, first.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	q := Open(away)
	// Both sides work with no way to reach each other.
	if _, err := p.Append("a.pdf", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Append("a.pdf", 3); err != nil {
		t.Fatal(err)
	}

	// The one who was away comes back as a fresh replica of its own snapshot,
	// which is what rejoining is.
	back, err := LoadLocal(2, away.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if err := back.Merge(first); err != nil {
		t.Fatal(err)
	}
	if err := first.Merge(back); err != nil {
		t.Fatal(err)
	}
	if got, want := len(Open(back).Pages()), 3; got != want {
		t.Errorf("%d pages came back, want %d", got, want)
	}
	if describe(Open(back)) != describe(Open(first)) {
		t.Error("the two do not agree")
	}
}
