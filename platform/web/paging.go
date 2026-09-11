package web

import (
	"net/http"
	"strconv"
)

// A long list is shown a page at a time. The store caps every list it
// returns, so these only decide how much of a capped list one page shows.
const (
	cardsPerPage = 24
	rowsPerPage  = 50
)

// listPage says where the reader is in a long list and how to reach the
// pages either side. An empty Prev or Next means there is no such page.
type listPage struct {
	Page  int
	Pages int
	Total int
	Prev  string
	Next  string
}

// pageOf cuts one page out of a list and works out the links either side.
// A page number outside the list settles on the nearest real page, so a
// stale link still lands somewhere sensible.
func pageOf[Item any](items []Item, r *http.Request, perPage int) ([]Item, listPage) {
	if perPage < 1 {
		perPage = 1
	}
	view := listPage{Page: 1, Pages: 1, Total: len(items)}
	if len(items) > 0 {
		view.Pages = (len(items) + perPage - 1) / perPage
	}
	view.Page = wantedPage(r, view.Pages)
	start := (view.Page - 1) * perPage
	end := start + perPage
	if end > len(items) {
		end = len(items)
	}
	if view.Page > 1 {
		view.Prev = pageLink(r, view.Page-1)
	}
	if view.Page < view.Pages {
		view.Next = pageLink(r, view.Page+1)
	}
	return items[start:end], view
}

// wantedPage reads the page asked for, held inside the list.
func wantedPage(r *http.Request, pages int) int {
	wanted, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || wanted < 1 {
		return 1
	}
	if wanted > pages {
		return pages
	}
	return wanted
}

// pageLink keeps the rest of the query, such as a search or a filter, so
// paging through a filtered list stays filtered.
func pageLink(r *http.Request, page int) string {
	query := r.URL.Query()
	if page <= 1 {
		query.Del("page")
	} else {
		query.Set("page", strconv.Itoa(page))
	}
	if len(query) == 0 {
		return r.URL.Path
	}
	return r.URL.Path + "?" + query.Encode()
}
