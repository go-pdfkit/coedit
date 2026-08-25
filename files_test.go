package coedit

import (
	"bytes"
	"testing"

	"github.com/go-crdt/crdt"
)

func TestTheFilesAPlanCarries(t *testing.T) {
	local := NewLocal(1)
	p := Open(local)
	files := p.Files()

	// A file large enough to be cut into several pieces, so that putting it
	// back together is actually tested.
	big := bytes.Repeat([]byte("the quick brown fox jumps over the lazy dog. "), 5000)
	if err := files.Put("big.bin", big); err != nil {
		t.Fatal(err)
	}
	if err := files.Put("small.bin", []byte("short")); err != nil {
		t.Fatal(err)
	}
	got, ok := files.Get("big.bin")
	if !ok || !bytes.Equal(got, big) {
		t.Errorf("the big file came back as %d bytes (%v)", len(got), ok)
	}
	if got, ok := files.Get("small.bin"); !ok || string(got) != "short" {
		t.Errorf("the small file came back as %q (%v)", got, ok)
	}
	if _, ok := files.Get("nowhere.bin"); ok {
		t.Error("a file that was never put came back")
	}
	names := files.Names()
	if len(names) != 2 {
		t.Errorf("the plan carries %v", names)
	}
	// A file put twice is replaced.
	if err := files.Put("small.bin", []byte("longer than before")); err != nil {
		t.Fatal(err)
	}
	if got, _ := files.Get("small.bin"); string(got) != "longer than before" {
		t.Errorf("replacing gave %q", got)
	}
	if err := files.Remove("small.bin"); err != nil {
		t.Fatal(err)
	}
	if _, ok := files.Get("small.bin"); ok {
		t.Error("a removed file came back")
	}
	if err := files.Put("", nil); err == nil {
		t.Error("a file with no name was accepted")
	}
}

func TestTwoFilesSharingTheirPieces(t *testing.T) {
	// The same bytes stored twice are stored once: a piece lives under the
	// hash of what it holds.
	local := NewLocal(1)
	files := Open(local).Files()
	data := bytes.Repeat([]byte("shared"), 40000)
	if err := files.Put("one.bin", data); err != nil {
		t.Fatal(err)
	}
	before := len(local.Snapshot())
	if err := files.Put("two.bin", data); err != nil {
		t.Fatal(err)
	}
	after := len(local.Snapshot())
	// The second copy costs an entry, not a file.
	if after-before > len(data)/4 {
		t.Errorf("storing the same bytes twice grew the plan by %d of %d bytes",
			after-before, len(data))
	}
	if got, ok := files.Get("two.bin"); !ok || !bytes.Equal(got, data) {
		t.Error("the second file did not come back")
	}
}

func TestAFileWhosePiecesHaveNotArrived(t *testing.T) {
	// Between somebody adding a file and its pieces reaching everyone, a
	// replica holds an entry naming pieces it has not got. That is not a
	// file yet, and saying so is what keeps a half-arrived file from being
	// built into a document.
	local := NewLocal(1)
	p := Open(local)
	if err := p.Files().Put("a.bin", bytes.Repeat([]byte("x"), 200000)); err != nil {
		t.Fatal(err)
	}
	// Take a piece away, the way a message that has not arrived leaves one.
	_ = local.Edit(sourcesPart, func(m *crdt.Map) ([]crdt.MapOp, error) {
		for _, key := range m.Keys() {
			if len(key) > 2 && key[:2] == chunkPrefix {
				op, err := m.Delete(key)
				if err != nil {
					return nil, err
				}
				return []crdt.MapOp{op}, nil
			}
		}
		return nil, nil
	})
	if _, ok := p.Files().Get("a.bin"); ok {
		t.Error("a file with a piece missing came back anyway")
	}
}

func TestAFileWhosePiecesAreNotWhatTheySay(t *testing.T) {
	// A whole-file hash travels with the pieces, so a manifest naming the
	// wrong ones is caught rather than handed over as the file.
	local := NewLocal(1)
	p := Open(local)
	if err := p.Files().Put("a.bin", []byte("the real thing")); err != nil {
		t.Fatal(err)
	}
	_ = local.Edit(sourcesPart, func(m *crdt.Map) ([]crdt.MapOp, error) {
		op, err := m.Set(fileKey("a.bin"), []byte("0000 c:nothing"))
		if err != nil {
			return nil, err
		}
		return []crdt.MapOp{op}, nil
	})
	if _, ok := p.Files().Get("a.bin"); ok {
		t.Error("a manifest naming nothing came back as a file")
	}
	// And an entry that is not a manifest at all.
	_ = local.Edit(sourcesPart, func(m *crdt.Map) ([]crdt.MapOp, error) {
		op, err := m.Set(fileKey("a.bin"), nil)
		if err != nil {
			return nil, err
		}
		return []crdt.MapOp{op}, nil
	})
	if _, ok := p.Files().Get("a.bin"); ok {
		t.Error("an empty entry came back as a file")
	}
}

func TestReadingABoxBack(t *testing.T) {
	for _, c := range []struct {
		in   string
		want []float64
	}{
		{"", nil},
		{"1,2,3,4", []float64{1, 2, 3, 4}},
		{"1.5,-2,3e2,4", []float64{1.5, -2, 300, 4}},
		{"1,2,3", nil},
		{"1,2,3,4,5", nil},
		{"1,2,3,x", nil},
	} {
		got := decodeBox(c.in)
		if len(got) != len(c.want) {
			t.Errorf("%q gave %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%q gave %v, want %v", c.in, got, c.want)
				break
			}
		}
	}
}

func TestReadingAPageIdentityBack(t *testing.T) {
	// A bookmark points at a page by identity, which travels as the notation
	// the replicated types print.
	local := NewLocal(7)
	p := Open(local)
	if err := p.Files().Put("a.pdf", []byte("x")); err != nil {
		t.Fatal(err)
	}
	id, err := p.Append("a.pdf", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := pageIDOf(id.String()); !samePage(got, id) {
		t.Errorf("%q came back as %q", id, got)
	}
	for _, s := range []string{"", "root", "nonsense", "x@1", "1@x"} {
		if got := pageIDOf(s); !samePage(got, noPage) {
			t.Errorf("%q came back as a page: %q", s, got)
		}
	}
}
