// Package footer lays out the right side of the terminal client's footer: the
// working directory with its Git status and pull request, and plugin footer
// items, fitting as much of that content as the width allows.
package footer

import (
	"fmt"
	"strconv"
	"strings"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/hyperlink"
	"github.com/rockorager/go-uucode"
)

const (
	middleDot  = "·"
	truncation = "…"
	// itemGap is the width of the " · " between footer parts.
	itemGap = 3
)

// Location is the footer's default content: a working directory followed by
// its Git status, such as `~/kit (main* · PR #64)`.
type Location struct {
	// Text is the directory with its Git suffix.
	Text string
	// Base is the directory alone.
	Base string
	// LinkText is the pull-request label within Text, such as `PR #64`, or
	// empty when there is no pull request to open.
	LinkText string
	// URL is the validated pull-request address.
	URL string
}

// VCSLocation describes directory with status, which is nil outside a Git
// worktree. A pull request is shown only on a branch, with a positive number
// and a credential-free http(s) address.
func VCSLocation(directory string, status *protocol.VCSStatus) Location {
	location := Location{Text: directory, Base: directory}
	if status == nil {
		return location
	}
	var head string
	switch status.Head.Kind {
	case protocol.VCSHeadBranch, protocol.VCSHeadUnborn:
		head = status.Head.Name
	case protocol.VCSHeadDetached:
		head = "detached@" + status.Head.OID[:min(7, len(status.Head.OID))]
	}
	if head == "" {
		return location
	}
	if status.Dirty {
		head += "*"
	}
	if number, target, ok := pullRequest(status); ok {
		location.LinkText = "PR #" + strconv.Itoa(number)
		location.URL = target
		head += " " + middleDot + " " + location.LinkText
	}
	location.Text = directory + " (" + head + ")"
	return location
}

func pullRequest(status *protocol.VCSStatus) (int, string, bool) {
	if status.PullRequest == nil || status.Head.Kind != protocol.VCSHeadBranch {
		return 0, "", false
	}
	pr := status.PullRequest
	if pr.Number <= 0 {
		return 0, "", false
	}
	safe := hyperlink.SafeExternal(pr.URL)
	if safe == "" || (!strings.HasPrefix(safe, "https://") && !strings.HasPrefix(safe, "http://")) {
		return 0, "", false
	}
	return pr.Number, safe, true
}

// Span is one run of footer text.
type Span struct {
	Text string
	// URL is set on the pull-request label, which opens it.
	URL string
	// Item marks plugin content, drawn with Style. Other text is muted.
	Item  bool
	Style protocol.PluginFooterStyle
}

// Spans fits the location and the plugin's items into width cells, right
// aligned by the caller. The directory is shortened from its start before the
// Git status; at very narrow widths a complete pull-request label outranks
// the branch and directory. With items, the location gets about half the
// width, and items that do not fit become `… N more`. A plugin may hide the
// location. A width of zero or less is treated as unbounded.
func Spans(footer protocol.PluginFooter, location Location, width int) []Span {
	if width <= 0 {
		width = 1 << 20
	}
	if len(footer.Items) == 0 {
		if footer.LocationHidden {
			return nil
		}
		return locationSpans(fitLocation(location, width), location)
	}
	var spans []Span
	used := 0
	if !footer.LocationHidden && location.Text != "" {
		// Keep the customary half-width cap for directory-only locations. A
		// Git suffix can borrow space from items, but keep their labeled
		// overflow whenever the viewport can fit both.
		overflowWidth := uucode.StringWidth(fmt.Sprintf("%s %d more", truncation, len(footer.Items)))
		withOverflow := max(0, width-overflowWidth-itemGap)
		budget := min(width/2, withOverflow)
		if suffix, ok := gitSuffix(location); ok {
			minimum := uucode.StringWidth(strings.TrimPrefix(suffix, " "))
			if location.LinkText != "" && strings.HasSuffix(suffix, " "+middleDot+" "+location.LinkText+")") && minimum > width {
				minimum = uucode.StringWidth("(" + location.LinkText + ")")
			}
			firstItemWidth := 0
			for _, segment := range footer.Items[0].Content {
				firstItemWidth += uucode.StringWidth(segment.Text)
			}
			budget = max(budget, min(withOverflow, max(minimum, width-firstItemWidth-itemGap)))
			if len(footer.Items) == 1 && minimum+firstItemWidth+itemGap <= width {
				budget = max(budget, width-firstItemWidth-itemGap)
			}
			if minimum > withOverflow {
				budget = min(minimum, width)
			}
		}
		if fitted := fitLocation(location, budget); fitted != "" {
			spans = append(spans, locationSpans(fitted, location)...)
			used = uucode.StringWidth(fitted)
		}
	}
	for index, item := range footer.Items {
		text := ""
		for _, segment := range item.Content {
			text += segment.Text
		}
		gap := 0
		if used > 0 {
			gap = itemGap
		}
		reserve := 0
		if index+1 < len(footer.Items) {
			reserve = itemGap + uucode.StringWidth(fmt.Sprintf("%s %d more", truncation, len(footer.Items)-index-1))
		}
		if used+gap+uucode.StringWidth(text)+reserve > width {
			overflow := fmt.Sprintf("%s %d more", truncation, len(footer.Items)-index)
			if used+gap+uucode.StringWidth(overflow) <= width {
				if used > 0 {
					spans = append(spans, Span{Text: " " + middleDot + " "})
				}
				spans = append(spans, Span{Text: overflow})
			}
			break
		}
		if used > 0 {
			spans = append(spans, Span{Text: " " + middleDot + " "})
		}
		for _, segment := range item.Content {
			spans = append(spans, Span{Text: segment.Text, Item: true, Style: segment.Style})
		}
		used += gap + uucode.StringWidth(text)
	}
	return spans
}

// gitSuffix is the ` (branch…)` part of location after its directory.
func gitSuffix(location Location) (string, bool) {
	if location.Base == "" || !strings.HasPrefix(location.Text, location.Base+" (") || !strings.HasSuffix(location.Text, ")") {
		return "", false
	}
	return location.Text[len(location.Base):], true
}

// fitLocation keeps the Git suffix whole when it fits, shortening only the
// directory first. At very narrow widths a complete pull-request label
// outranks branch and directory; an incomplete label is never shown.
func fitLocation(location Location, width int) string {
	if width <= 0 {
		return ""
	}
	suffix, ok := gitSuffix(location)
	if !ok {
		return truncateStart(location.Text, width)
	}
	suffixWidth := uucode.StringWidth(suffix)
	if suffixWidth < width {
		return truncateStart(location.Base, width-suffixWidth) + suffix
	}
	// The separating space belongs to the directory; omit it when no
	// directory fits.
	if bare := strings.TrimPrefix(suffix, " "); uucode.StringWidth(bare) <= width {
		return bare
	}
	if location.LinkText != "" && strings.HasSuffix(suffix, " "+middleDot+" "+location.LinkText+")") {
		pr := "(" + location.LinkText + ")"
		if uucode.StringWidth(pr) <= width {
			return pr
		}
		return truncation
	}
	return truncateStart(location.Base, width)
}

// locationSpans links only the complete pull-request label at the end of the
// Git suffix. Shortening that clips the label leaves plain text rather than a
// misleading link.
func locationSpans(fitted string, location Location) []Span {
	if fitted == "" {
		return nil
	}
	if location.URL == "" || location.LinkText == "" || !strings.HasSuffix(fitted, location.LinkText+")") {
		return []Span{{Text: fitted}}
	}
	start := len(fitted) - len(location.LinkText) - 1
	spans := make([]Span, 0, 3)
	if start > 0 {
		spans = append(spans, Span{Text: fitted[:start]})
	}
	return append(spans, Span{Text: location.LinkText, URL: location.URL}, Span{Text: ")"})
}

// truncateStart keeps the end of value within maximum cells, marking the cut
// with an ellipsis.
func truncateStart(value string, maximum int) string {
	if maximum <= 0 {
		return ""
	}
	if uucode.StringWidth(value) <= maximum {
		return value
	}
	type grapheme struct{ start, width int }
	var graphemes []grapheme
	iterator := uucode.NewGraphemeWidthIterator(value)
	for {
		next, ok := iterator.Next()
		if !ok {
			break
		}
		graphemes = append(graphemes, grapheme{start: next.Start, width: next.Width})
	}
	remaining := maximum - 1
	start := len(graphemes)
	for start > 0 && graphemes[start-1].width <= remaining {
		start--
		remaining -= graphemes[start].width
	}
	if start == len(graphemes) {
		return truncation
	}
	return truncation + value[graphemes[start].start:]
}
