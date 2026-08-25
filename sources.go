package coedit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/go-crdt/crdt"
)

// A Files is where the plan keeps the documents its pages come from.
//
// A file is put in once and referred to by name from then on, so a plan that
// takes three pages out of a hundred-page report carries the report once. The
// bytes are cut into pieces on their content rather than every so many bytes,
// which is what makes replacing one page of a file send one piece rather than
// the file.
type Files interface {
	// Put stores a file under a name, replacing whatever was there.
	Put(name string, data []byte) error
	// Get reads one back.
	Get(name string) ([]byte, bool)
	// Names is every file the plan carries.
	Names() []string
	// Remove drops one. The pages that came from it are left as they are and
	// will not build until it is put back, which is what removing a file a
	// document is made of means.
	Remove(name string) error
}

// Files is where the documents this plan's pages come from are kept.
func (p *Plan) Files() Files { return &planFiles{p: p} }

type planFiles struct{ p *Plan }

// Put stores a file under a name.
func (f *planFiles) Put(name string, data []byte) error {
	if name == "" {
		return fmt.Errorf("coedit: a file needs a name")
	}
	sum := sha256.Sum256(data)
	pieces := f.p.chunker(data)
	// The pieces go in first, then the entry naming them, so that no replica
	// ever sees a file whose pieces have not arrived.
	return f.p.store.Edit(sourcesPart, func(m *crdt.Map) ([]crdt.MapOp, error) {
		var ops []crdt.MapOp
		var names []string
		for _, piece := range pieces {
			key := chunkKey(piece)
			names = append(names, key)
			if _, held := m.Get(key); held {
				continue
			}
			// Both keys are this package's own — a prefix and a hash — so
			// neither can be refused.
			op, _ := m.Set(key, piece)
			ops = append(ops, op)
		}
		op, _ := m.Set(fileKey(name), []byte(encodeManifest(hex.EncodeToString(sum[:]), names)))
		return append(ops, op), nil
	})
}

// Get reads a file back, and reports whether every piece of it is here.
func (f *planFiles) Get(name string) ([]byte, bool) {
	var out []byte
	whole := false
	_ = f.p.store.Read(sourcesPart, func(m *crdt.Map) {
		entry, ok := m.Get(fileKey(name))
		if !ok {
			return
		}
		sum, pieces, ok := decodeManifest(string(entry))
		if !ok {
			return
		}
		for _, key := range pieces {
			piece, ok := m.Get(key)
			if !ok {
				return // a piece has not arrived yet
			}
			out = append(out, piece...)
		}
		// What comes out is what went in, or it is not the file: a piece
		// stored under the hash of its contents cannot be quietly swapped,
		// and the whole is checked as well so that a manifest naming the
		// wrong pieces is caught too.
		got := sha256.Sum256(out)
		whole = hex.EncodeToString(got[:]) == sum
	})
	if !whole {
		return nil, false
	}
	return out, true
}

// Names is every file the plan carries, in the order a map hands them over.
func (f *planFiles) Names() []string {
	var out []string
	_ = f.p.store.Read(sourcesPart, func(m *crdt.Map) {
		for _, key := range m.Keys() {
			if name, ok := fileName(key); ok {
				out = append(out, name)
			}
		}
	})
	return out
}

// Remove drops a file. Its pieces are left where they are, since another file
// may be made of some of the same ones.
func (f *planFiles) Remove(name string) error {
	return f.p.store.Edit(sourcesPart, func(m *crdt.Map) ([]crdt.MapOp, error) {
		op, _ := m.Delete(fileKey(name))
		return []crdt.MapOp{op}, nil
	})
}

// The two kinds of key the sources part holds, told apart by their first byte
// so that no file can be named the same as a piece.
const (
	filePrefix  = "f:"
	chunkPrefix = "c:"
)

// fileKey is where a file's entry lives.
func fileKey(name string) string { return filePrefix + name }

// fileName reads a file's name back out of its key.
func fileName(key string) (string, bool) {
	if len(key) < len(filePrefix) || key[:len(filePrefix)] != filePrefix {
		return "", false
	}
	return key[len(filePrefix):], true
}

// chunkKey is where a piece lives: under the hash of what it holds, so two
// files sharing a piece share it rather than carrying it twice.
func chunkKey(piece []byte) string {
	sum := sha256.Sum256(piece)
	return chunkPrefix + hex.EncodeToString(sum[:])
}

// A manifest is a file's whole-file hash and the pieces it is made of, in
// order, with a space between.
func encodeManifest(sum string, pieces []string) string {
	out := sum
	for _, p := range pieces {
		out += " " + p
	}
	return out
}

// decodeManifest reads one back.
func decodeManifest(s string) (sum string, pieces []string, ok bool) {
	fields := splitFields(s)
	if len(fields) < 1 {
		return "", nil, false
	}
	return fields[0], fields[1:], true
}

// splitFields cuts on spaces, ignoring runs of them.
func splitFields(s string) []string {
	var out []string
	start := -1
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ' ' {
			if start >= 0 {
				out = append(out, s[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	return out
}
