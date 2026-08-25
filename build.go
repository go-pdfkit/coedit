package coedit

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/go-pdfkit/ops"
	"github.com/go-pdfkit/reader"
)

// Build turns the plan into a document ready to be written.
//
// Every page named by the plan is taken from the file it names, in the order
// the plan puts them, turned and cropped as the plan says. The bookmarks are
// written over the pages they point at; one pointing at a page that is no
// longer there is left out rather than pointed at the wrong one.
//
// A plan naming a file this replica has not received yet cannot be built, and
// says which file it is waiting for. That is not a failure — it is what a
// participant sees for the moment between somebody adding a file and its
// pieces arriving.
func (p *Plan) Build() (*ops.Doc, error) {
	pages := p.Pages()
	if len(pages) == 0 {
		return nil, fmt.Errorf("coedit: the plan has no pages")
	}
	files := p.Files()
	sources := map[string]*reader.Document{}
	for _, page := range pages {
		if _, held := sources[page.Source]; held {
			continue
		}
		data, ok := files.Get(page.Source)
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrNoSuchFile, page.Source)
		}
		src, err := reader.Open(data)
		if err != nil {
			return nil, fmt.Errorf("coedit: %q will not open: %w", page.Source, err)
		}
		sources[page.Source] = src
	}

	// Every page is taken on its own so that the plan's order is the file's,
	// whatever order the pages sit in inside their sources.
	var out *ops.Doc
	// where says which page of the built document each page of the plan
	// became, so that a bookmark can be pointed at it.
	where := map[string]int{}
	for _, page := range pages {
		src := sources[page.Source]
		if page.Number > src.PageCount() {
			return nil, fmt.Errorf("coedit: %q has no page %d", page.Source, page.Number)
		}
		one := ops.FromDocument(src)
		// The number was checked against the file's own count a line ago, so
		// there is a page there to select.
		_ = one.Select(strconv.Itoa(page.Number))
		if page.Rotate != 0 {
			if err := one.SetRotation("1", page.Rotate); err != nil {
				return nil, err
			}
		}
		if len(page.Crop) == 4 {
			if err := one.Crop("1", [4]float64{page.Crop[0], page.Crop[1], page.Crop[2], page.Crop[3]}); err != nil {
				return nil, err
			}
		}
		if out == nil {
			out = one
		} else {
			out.Append(one)
		}
		where[page.ID.String()] = out.PageCount()
	}
	p.writeOutline(out, where)
	return out, nil
}

// Bytes builds the plan and writes it out.
func (p *Plan) Bytes() ([]byte, error) {
	doc, err := p.Build()
	if err != nil {
		return nil, err
	}
	return doc.Bytes()
}

// writeOutline puts the plan's bookmarks over the pages they point at. A
// bookmark pointing at a page that is no longer in the plan is left out, and
// so is everything under it, since a heading whose section has gone is not a
// heading any more.
func (p *Plan) writeOutline(doc *ops.Doc, where map[string]int) {
	marks := p.Outline()
	if len(marks) == 0 {
		return
	}
	doc.SetOutline(convertBookmarks(marks, where))
}

// convertBookmarks turns the plan's tree into the one a document carries.
func convertBookmarks(marks []Bookmark, where map[string]int) []ops.Bookmark {
	var out []ops.Bookmark
	for _, m := range marks {
		page, ok := where[m.Page.String()]
		if !ok {
			continue
		}
		out = append(out, ops.Bookmark{
			Title:    strings.TrimSpace(m.Title),
			Page:     page,
			Children: convertBookmarks(m.Children, where),
		})
	}
	return out
}
