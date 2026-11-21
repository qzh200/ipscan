package screen

import (
	"math"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"

	"ipscan/internal/flags"
	"ipscan/internal/model"
)

// nodeName draws the flag of the country a subscription name carries, then the
// name itself. Names without a country prefix, or codes without a picture, stay
// plain text.
func nodeName(c *ui.Context, name string, size float32, wrap bool) ui.Element {
	code, rest, _ := model.CountryPrefix(name)
	if src := flags.Image(code); src != nil {
		// Size both axes: a height-only image fills the row's free width and a
		// white field (JP, KR) turns into a stretched blank bar.
		imgW, imgH := ui.ImageSize(src)
		height := float32(math.Round(float64(size) * 1.15))
		width := height * (imgW / imgH)
		row := ui.Row(c).Gap(6).AlignItems(ui.Center).MinWidth(0).Grow(1)
		row.Children(func() {
			// The hairline keeps white-field flags from reading as a stray dot.
			frame := ui.Row(c).Shrink(0).Width(width+2).Height(height+2).
				Border(1, c.Theme().Border).Radius(2).Clip()
			frame.Children(func() {
				ui.Image(c, src).Size(width, height)
			})
			text := components.Text(c, rest).FontSize(size).MinWidth(0).Grow(1)
			if wrap {
				text.Wrap()
			} else {
				text.SingleLine()
			}
		})
		return row
	}
	text := components.Text(c, rest).FontSize(size).MinWidth(0).Grow(1)
	if wrap {
		return text.Wrap()
	}
	return text.SingleLine()
}
