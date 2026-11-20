package screen

import (
	"testing"

	"github.com/oneo-me/onevra/source/components"
)

// A user can drag a column wider than the card; the table must still fit, or the
// last columns get clipped at the right edge (the score badge lost its tail).
func TestFitTableWidthsKeepsTableInsideCard(t *testing.T) {
	const window = 1180
	cols := tableColumnsFor(window)
	content := tableWidth(window)

	state := &components.TableState{}
	state.Columns.Widths = map[string]float32{"name": 700, "geo": 900}

	fitTableWidths(state, cols, content)

	sum := float32(0)
	for _, col := range cols {
		w := state.Columns.Widths[col.ID]
		if w < col.MinWidth {
			t.Fatalf("%s = %.1f, below its minimum %.1f", col.ID, w, col.MinWidth)
		}
		sum += w
	}
	if diff := sum - content; diff > .5 || diff < -.5 {
		t.Fatalf("column widths sum to %.1f, want %.1f", sum, content)
	}
}

// Onevra only remembers the columns it dragged; the rest keep the layout's
// widths, which must stay untouched by the fit.
func TestFitTableWidthsWithoutDragIsNoop(t *testing.T) {
	cols := tableColumnsFor(1180)
	state := &components.TableState{}
	fitTableWidths(state, cols, tableWidth(1180))
	if state.Columns.Widths != nil {
		t.Fatalf("widths = %v, want untouched", state.Columns.Widths)
	}
}
