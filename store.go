package coedit

import (
	"github.com/go-crdt/crdt"
)

// Local is a plan held on its own, with nobody else editing it: the shape a
// test takes, and what a participant edits between one connection and the
// next.
//
// Its snapshot is what a session is joined with, so work done alone is not
// work lost.
type Local struct {
	doc *crdt.Composite
}

// NewLocal returns an empty plan this site edits alone.
func NewLocal(site crdt.SiteID) *Local { return &Local{doc: crdt.NewComposite(site)} }

// LoadLocal rebuilds a plan from a snapshot, to be edited as site.
func LoadLocal(site crdt.SiteID, snapshot []byte) (*Local, error) {
	doc, err := crdt.LoadComposite(site, snapshot)
	if err != nil {
		return nil, err
	}
	return &Local{doc: doc}, nil
}

// Read runs fn against the named map part.
func (l *Local) Read(part string, fn func(*crdt.Map)) error {
	m, err := l.doc.Map(part)
	if err != nil {
		return err
	}
	fn(m)
	return nil
}

// Edit runs fn against the named map part. There is nobody to publish to, so
// the operations it produced are already everywhere they are going.
func (l *Local) Edit(part string, fn func(*crdt.Map) ([]crdt.MapOp, error)) error {
	m, err := l.doc.Map(part)
	if err != nil {
		return err
	}
	_, err = fn(m)
	return err
}

// Snapshot is the whole plan, to be reloaded or to join a session with.
func (l *Local) Snapshot() []byte { return l.doc.Snapshot() }

// Composite is the replica underneath, for a caller merging two plans by hand.
func (l *Local) Composite() *crdt.Composite { return l.doc }

// Merge takes everything another replica has that this one lacks. It is what
// two people who have been editing apart do when they meet, and it is the same
// merge a session does — there is only one.
func (l *Local) Merge(other *Local) error {
	// The other replica refuses if it has collected below where this one
	// stands: what it gave back is not in its differences any more, so it
	// cannot say what this one is missing. Such a pair is merged from the
	// other side, or through their snapshots; it is not something this can
	// paper over.
	ops, err := other.doc.OpsSince(l.doc.Version())
	if err != nil {
		return err
	}
	return l.doc.Apply(ops...)
}
