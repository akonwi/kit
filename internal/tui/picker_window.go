package tui

// listWindow is the visible range of a picker list: items [Start, End) plus
// whether overflow rows are shown above and below them.
type listWindow struct {
	Start, End   int
	Above, Below bool
}

// Rows is the number of rows the window occupies, including overflow rows.
func (w listWindow) Rows() int {
	rows := w.End - w.Start
	if w.Above {
		rows++
	}
	if w.Below {
		rows++
	}
	return rows
}

// shapeListWindow fits items starting at start into rows, spending one row on
// each overflow indicator only when items are hidden on that side.
func shapeListWindow(count, rows, start int) listWindow {
	if count <= rows {
		return listWindow{End: count}
	}
	start = max(0, min(start, count-1))
	window := listWindow{Start: start, Above: start > 0}
	capacity := rows
	if window.Above {
		capacity--
	}
	window.Below = start+capacity < count
	if window.Below {
		capacity--
	}
	window.End = min(count, start+max(0, capacity))
	return window
}

// resolveListWindow keeps the window anchored at start where possible, never
// leaves blank rows below while items are hidden above, and when reveal is a
// valid index moves just far enough to show it.
func resolveListWindow(count, rows, start, reveal int) listWindow {
	if rows <= 0 {
		return listWindow{}
	}
	window := shapeListWindow(count, rows, start)
	if reveal >= 0 && reveal < count {
		for range count {
			if reveal < window.Start {
				window = shapeListWindow(count, rows, reveal)
			} else if reveal >= window.End && window.End > window.Start {
				window = shapeListWindow(count, rows, window.Start+reveal-window.End+1)
			} else {
				break
			}
		}
	}
	// Dropping the lower indicator can leave blank rows; pull hidden items
	// back in from above until the list is full again.
	for window.Start > 0 && !window.Below {
		previous := shapeListWindow(count, rows, window.Start-1)
		if previous.Below {
			break
		}
		window = previous
	}
	return window
}
