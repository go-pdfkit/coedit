package coedit

import (
	"fmt"
	"testing"

	"github.com/go-crdt/crdt"
	"github.com/go-crdt/crdt/structured"
	"github.com/go-pdfkit/reader"
)

// structuredSequence is the sequence a page part holds, for a test that pokes
// at a field directly the way another replica would.
func structuredSequence(m *crdt.Map) *structured.Sequence { return structured.SequenceOf(m) }

// samplePDF writes a document of n pages, each carrying its own number, so
// that a page can be told from another after it has been moved.
func samplePDF(t *testing.T, n int) []byte {
	t.Helper()
	w := reader.NewWriter("1.7")
	pagesRef := w.Reserve()
	kids := make(reader.Array, 0, n)
	for i := 1; i <= n; i++ {
		content := w.Add(&reader.Stream{Dict: reader.Dict{},
			Raw: []byte(fmt.Sprintf("page %d", i))})
		kids = append(kids, w.Add(reader.Dict{
			"Type": reader.Name("Page"), "Parent": pagesRef, "Contents": content,
		}))
	}
	w.Put(pagesRef, reader.Dict{"Type": reader.Name("Pages"), "Kids": kids,
		"Count":    reader.Integer(n),
		"MediaBox": reader.Array{reader.Integer(0), reader.Integer(0), reader.Integer(300), reader.Integer(400)}})
	root := w.Add(reader.Dict{"Type": reader.Name("Catalog"), "Pages": pagesRef})
	out, err := w.Finish(reader.Dict{"Root": root})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// plan is a plan held alone, with the given files already in it.
func plan(t *testing.T, site crdt.SiteID, files map[string][]byte) (*Plan, *Local) {
	t.Helper()
	local := NewLocal(site)
	p := Open(local)
	for name, data := range files {
		if err := p.Files().Put(name, data); err != nil {
			t.Fatal(err)
		}
	}
	return p, local
}

// contents reads back what every page of a built document says.
func contents(t *testing.T, out []byte) []string {
	t.Helper()
	d, err := reader.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, d.PageCount())
	for i := 1; i <= d.PageCount(); i++ {
		data, err := d.PageContent(i)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, string(data))
	}
	return got
}

func TestAPlanBecomesADocument(t *testing.T) {
	p, _ := plan(t, 1, map[string][]byte{"a.pdf": samplePDF(t, 3)})
	for _, n := range []int{3, 1, 2} {
		if _, err := p.Append("a.pdf", n); err != nil {
			t.Fatal(err)
		}
	}
	out, err := p.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	got := contents(t, out)
	want := []string{"page 3", "page 1", "page 2"}
	if !sameStrings(got, want) {
		t.Errorf("the document says %v, want %v", got, want)
	}
}

func TestAPlanDrawingOnSeveralFiles(t *testing.T) {
	p, _ := plan(t, 1, map[string][]byte{
		"a.pdf": samplePDF(t, 2),
		"b.pdf": samplePDF(t, 2),
	})
	if _, err := p.Append("b.pdf", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Append("a.pdf", 1); err != nil {
		t.Fatal(err)
	}
	out, err := p.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if got := contents(t, out); len(got) != 2 {
		t.Errorf("the document has %d pages: %v", len(got), got)
	}
}

func TestAPlanWaitingForAFile(t *testing.T) {
	// A plan naming a file this replica has not received yet cannot be built,
	// and says which one it is waiting for.
	p, _ := plan(t, 1, nil)
	if _, err := p.Append("missing.pdf", 1); err != nil {
		t.Fatal(err)
	}
	_, err := p.Bytes()
	if err == nil {
		t.Fatal("a plan with no file built anyway")
	}
	if !contains(err.Error(), "missing.pdf") {
		t.Errorf("it does not say which file: %v", err)
	}
}

func TestAPlanWithNothingInIt(t *testing.T) {
	p, _ := plan(t, 1, nil)
	if _, err := p.Bytes(); err == nil {
		t.Error("an empty plan built a document")
	}
}

func TestAPageThatIsNotInItsFile(t *testing.T) {
	p, _ := plan(t, 1, map[string][]byte{"a.pdf": samplePDF(t, 2)})
	if _, err := p.Append("a.pdf", 9); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Bytes(); err == nil {
		t.Error("a page past the end of its file built anyway")
	}
	if _, err := p.Append("a.pdf", 0); err == nil {
		t.Error("page nought was accepted")
	}
}

func TestAFileThatWillNotOpen(t *testing.T) {
	p, _ := plan(t, 1, map[string][]byte{"a.pdf": []byte("not a pdf")})
	if _, err := p.Append("a.pdf", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Bytes(); err == nil {
		t.Error("a file that is not one built anyway")
	}
}

func TestTurningAndCroppingAPage(t *testing.T) {
	p, _ := plan(t, 1, map[string][]byte{"a.pdf": samplePDF(t, 2)})
	first, err := p.Append("a.pdf", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Rotate(first, 90); err != nil {
		t.Fatal(err)
	}
	if err := p.Crop(first, []float64{10, 10, 200, 300}); err != nil {
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
	page, err := d.Page(1)
	if err != nil {
		t.Fatal(err)
	}
	rot, _ := reader.ToInt(mustResolve(d, page.Get("Rotate")))
	if rot != 90 {
		t.Errorf("the page is turned %d", rot)
	}
	box, ok := reader.ToArray(mustResolve(d, page.Get("CropBox")))
	if !ok || len(box) != 4 {
		t.Fatalf("the crop box is %v", box)
	}
	// A rotation is what the page will have, not one added to what it has.
	if err := p.Rotate(first, 450); err != nil {
		t.Fatal(err)
	}
	if got := p.Pages()[0].Rotate; got != 90 {
		t.Errorf("four hundred and fifty degrees came out as %d", got)
	}
	// And a box of nothing puts the source page's own back.
	if err := p.Crop(first, nil); err != nil {
		t.Fatal(err)
	}
	if got := p.Pages()[0].Crop; got != nil {
		t.Errorf("the crop box is still %v", got)
	}
	if err := p.Crop(first, []float64{1, 2}); err == nil {
		t.Error("a box of two numbers was accepted")
	}
	if err := p.Rotate(PageID{}, 90); err == nil {
		t.Error("a page that is not there was turned")
	}
}

func mustResolve(d *reader.Document, o reader.Object) reader.Object {
	out, _ := d.Resolve(o)
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestWhatAHostileReplicaCanPutInAPlan(t *testing.T) {
	// Every field of a page arrives from somebody else, and somebody else may
	// be running anything at all. A plan carrying nonsense says so rather than
	// building a document out of it.
	p, local := plan(t, 1, map[string][]byte{"a.pdf": samplePDF(t, 3)})
	id, err := p.Append("a.pdf", 1)
	if err != nil {
		t.Fatal(err)
	}
	poke := func(field, value string) {
		t.Helper()
		if err := local.Edit(pagesPart, func(m *crdt.Map) ([]crdt.MapOp, error) {
			op, err := structuredSequence(m).SetField(id, field, []byte(value))
			if err != nil {
				return nil, err
			}
			return []crdt.MapOp{op}, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	poke(fieldRotate, "37")
	if _, err := p.Bytes(); err == nil {
		t.Error("a rotation of thirty-seven degrees built a document")
	}
	poke(fieldRotate, "0")
	poke(fieldCrop, "1,1,1,1")
	if _, err := p.Bytes(); err == nil {
		t.Error("a box of no area built a document")
	}
	poke(fieldCrop, "")
	if _, err := p.Bytes(); err != nil {
		t.Fatalf("putting it back left it broken: %v", err)
	}
	// A number that is not one leaves an item that is not a page.
	poke(fieldNumber, "not a number")
	if n := p.Len(); n != 0 {
		t.Errorf("a page numbered with a word is still a page: %d", n)
	}
}

func TestMovingAndInsertingWhereThereIsNothing(t *testing.T) {
	p, _ := plan(t, 1, map[string][]byte{"a.pdf": samplePDF(t, 2)})
	nowhere := PageID{Site: 9, Seq: 99}
	if _, err := p.InsertAfter(nowhere, "a.pdf", 1); err == nil {
		t.Error("a page was inserted after nothing")
	}
	real, err := p.Append("a.pdf", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Move(real, nowhere); err == nil {
		t.Error("a page was moved after nothing")
	}
	if err := p.Move(nowhere, real); err == nil {
		t.Error("a page that is not there was moved")
	}
	if err := p.Remove(nowhere); err == nil {
		t.Error("a page that is not there was removed")
	}
	if p.Len() != 1 {
		t.Errorf("the plan holds %d pages", p.Len())
	}
}

func TestATurnGivenBackwards(t *testing.T) {
	p, _ := plan(t, 1, map[string][]byte{"a.pdf": samplePDF(t, 2)})
	id, err := p.Append("a.pdf", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Rotate(id, -90); err != nil {
		t.Fatal(err)
	}
	if got := p.Pages()[0].Rotate; got != 270 {
		t.Errorf("a quarter turn the other way came out as %d", got)
	}
}

func TestAPlanReloadedFromWhatItWas(t *testing.T) {
	p, local := plan(t, 1, map[string][]byte{"a.pdf": samplePDF(t, 3)})
	if _, err := p.Append("a.pdf", 2); err != nil {
		t.Fatal(err)
	}
	back, err := LoadLocal(1, local.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if got := Open(back).Len(); got != 1 {
		t.Errorf("the reloaded plan holds %d pages", got)
	}
	if back.Composite() == nil {
		t.Error("the replica underneath is not there")
	}
	if _, err := LoadLocal(1, []byte("not a snapshot")); err == nil {
		t.Error("nonsense was loaded as a plan")
	}
}

func TestAPartAPlanCannotHave(t *testing.T) {
	// A part name that is not one is refused rather than quietly creating a
	// part no other replica would bind.
	local := NewLocal(1)
	if err := local.Read("", func(*crdt.Map) {}); err == nil {
		t.Error("a part with no name was read")
	}
	if err := local.Edit("", func(*crdt.Map) ([]crdt.MapOp, error) { return nil, nil }); err == nil {
		t.Error("a part with no name was edited")
	}
}
