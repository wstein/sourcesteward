// SPDX-License-Identifier: EUPL-1.2
package hosting

// LinkNextPage reads the `Link` header of a list page and returns the number of the page that
// follows it. It is only the signal that another page exists: the URL in the header is never
// requested, the caller asks for the page by number through the typed reader. A header that
// announces rel="next" without a usable page number is malformed, not the last page.
func LinkNextPage(link string) (next int, more bool, err error) {
	next, more = nextPageFromLink(link)
	if !more && hasNextRel(link) {
		return 0, false, malformed("unusable Link header")
	}
	return next, more, nil
}
