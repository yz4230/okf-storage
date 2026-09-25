package bundle

import "slices"

// PageRequest selects a page of paths from results sorted in byte order.
// Paging by the last path seen, rather than by offset, keeps pages stable
// while documents are added or removed.
type PageRequest struct {
	// After skips paths up to and including it. Pass the previous Page's
	// Next, or leave empty to start from the beginning.
	After string
	// Limit caps the number of paths returned; zero or less means no cap.
	Limit int
}

type Page struct {
	Paths []string
	// Next is the After for the following page, or empty if no paths follow.
	Next string
}

// paginate returns the page of sorted that req selects.
func paginate(sorted []string, req PageRequest) Page {
	if req.After != "" {
		i, found := slices.BinarySearch(sorted, req.After)
		if found {
			i++
		}
		sorted = sorted[i:]
	}
	if req.Limit > 0 && len(sorted) > req.Limit {
		sorted = sorted[:req.Limit]
		return Page{Paths: sorted, Next: sorted[len(sorted)-1]}
	}
	return Page{Paths: sorted}
}
