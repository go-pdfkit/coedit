# coedit

[![CI](https://github.com/go-pdfkit/coedit/actions/workflows/ci.yml/badge.svg)](https://github.com/go-pdfkit/coedit/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/go-pdfkit/coedit.svg)](https://pkg.go.dev/github.com/go-pdfkit/coedit)
[![License: BSD-3-Clause](https://img.shields.io/badge/License-BSD--3--Clause-blue.svg)](LICENSE)
[![Coverage](https://img.shields.io/badge/coverage-100%25-brightgreen.svg)](#how-it-is-checked)

A PDF several people are editing at once.

## What is shared is not the file

A file is a settled thing. Merging two people's copies of one means merging
two piles of bytes, which cannot be done.

What is shared is the **plan**: which pages, from which files, in which order,
turned and cropped how, with which bookmarks over them. That is a list, a tree
and a handful of fields — and a list, a tree and a handful of fields are things
a conflict-free replicated data type merges without asking anybody.

So two people reordering the same document keep both reorderings. Two people
rotating different pages keep both rotations. Two people rotating the same page
settle on one of the two, which is what rotating a page twice means. **Nobody
waits for a lock and nobody loses work, however many of them there are.**

```go
plan := coedit.Open(store)
plan.Files().Put("report.pdf", bytes)
first, _ := plan.Append("report.pdf", 3)
plan.Rotate(first, 90)
plan.AddBookmark(coedit.AtRoot, coedit.BookmarkID{}, "Findings", first)
out, _ := plan.Bytes()
```

`Build` hands back an [`ops.Doc`](https://github.com/go-pdfkit/ops) — the same
document the command-line tool and the browser build, so what comes out of a
shared edit is what comes out of any other.

## Where the files live

A file goes in once and is referred to by name from then on, so a plan taking
three pages out of a hundred-page report carries the report once. The bytes are
cut into pieces **on their content** with
[`go-deltasync/chunk`](https://github.com/go-deltasync/chunk), so replacing one
page of a file sends one piece rather than the file, and two files sharing
their bytes share their pieces.

Every piece is stored under the hash of what it holds, and the whole file's
hash travels with the list of them: a peer cannot quietly put something else
there, and a file whose pieces have not all arrived yet is not a file yet —
which is what keeps a half-arrived document from being built.

## Where it runs

A plan needs two things from wherever it is kept: read a map, or change one and
say what changed. That is the [`Store`](https://pkg.go.dev/github.com/go-pdfkit/coedit#Store)
interface, and it is two methods long.

`Local` is a plan held alone — a test, or a participant between one connection
and the next. A session over
[`go-crdt/collab`](https://github.com/go-crdt/collab) is the same two methods,
so the same plan rides a central WebSocket or a peer-to-peer data channel
without knowing which. It builds for `GOOS=js/wasm`, so a browser tab runs it.

## How it is checked

The point of a shared plan is not that two people can edit it. It is that **any
number of them** can, in any order, on any connection, and end up with the same
document. So that is what is tested:

* **25 editors, 40 rounds each**, every one making random appends, inserts,
  moves, removals, rotations and bookmarks, merged in a random order — and then
  asked whether they agree. They do, on all 189 pages, and every one of them
  builds the same file.
* **Two groups of four who cannot reach each other** for twenty rounds, and
  then meet. Nothing either group did is lost.
* **An editor who leaves and comes back** with a snapshot: work done while away
  is not work lost.

That harness found the thing two browser tabs would not have: one person
removing a page while another turns it leaves the item behind, because a field
written after a removal puts the record back. What it does not put back is
which file the page came from — so that is what says whether there is a page
there at all.

100% of statements, including everything a hostile replica can put in a plan:
a rotation of thirty-seven degrees, a crop box of no area, a page numbered with
a word.

## Licence

BSD-3-Clause.
