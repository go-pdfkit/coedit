package coedit

import (
	"strconv"
	"strings"

	"github.com/go-crdt/crdt"
	"github.com/go-crdt/crdt/structured"
)

// A bookmark points at a page by identity rather than by number, so that it
// still points at its page after somebody else has moved one in front of it.
// The identity travels as the notation the replicated types print — "seq@site"
// — because that is what both ends already agree on.
func pageIDOf(s string) PageID {
	if s == "" || s == "root" {
		return PageID{}
	}
	at := strings.IndexByte(s, '@')
	if at < 0 {
		return PageID{}
	}
	seq, err := strconv.ParseUint(s[:at], 10, 64)
	if err != nil {
		return PageID{}
	}
	site, err := strconv.ParseUint(s[at+1:], 10, 64)
	if err != nil {
		return PageID{}
	}
	return PageID(crdt.ID{Site: crdt.SiteID(site), Seq: seq})
}

// samePage reports whether two identities name the same page.
func samePage(a, b PageID) bool { return crdt.ID(a) == crdt.ID(b) }

// noPage is the identity that names no page at all.
var noPage = structured.ItemID{}
