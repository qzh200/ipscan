package screen

import (
	"math"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"

	"ipscan/internal/flags"
	"ipscan/internal/model"
)

// nodeName draws the flag of the country a subscription name carries, then the
// name itself. Names without a country prefix, or codes without a picture, stay
// plain text.
func nodeName(c *ui.Context, name string, size float32, wrap bool) ui.Element {
	code, rest, ok := model.CountryPrefix(name)
	if !ok {
		rest = strings.TrimSpace(name)
	}
	if src := flags.Image(code); ok && src != nil {
		height := float32(math.Round(float64(size) * .8))
		row := ui.Row(c).Gap(6).AlignItems(ui.Center).MinWidth(0).Grow(1)
		row.Children(func() {
			ui.Image(c, src).Height(height).Shrink(0)
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
