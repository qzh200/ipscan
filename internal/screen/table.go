package screen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/lobehub/lobe-mygo/pkg/display"
	"github.com/lobehub/lobe-mygo/pkg/layout"
	"github.com/lobehub/lobe-mygo/pkg/lucide"
	"github.com/lobehub/lobe-mygo/pkg/theme"

	"ipscan/internal/app"
	"ipscan/internal/probe"
)

type filterMode int

const (
	allFilter filterMode = iota
	healthyFilter
	needsWorkFilter
	failedFilter
)

func isHealthy(r app.Row) bool {
	return r.Probed && r.Result.Reachable && r.Result.Score >= 70
}

func isFailed(r app.Row) bool {
	return r.Probed && !r.Result.Reachable && !r.Result.UDPOnly
}

func rowMatches(r app.Row, mode filterMode) bool {
	switch mode {
	case healthyFilter:
		return isHealthy(r)
	case needsWorkFilter:
		return r.Probed && !isHealthy(r) && !isFailed(r)
	case failedFilter:
		return isFailed(r)
	}
	return true
}

func countRows(rows []app.Row, mode filterMode) int {
	n := 0
	for _, r := range rows {
		if rowMatches(r, mode) {
			n++
		}
	}
	return n
}

func matchesQuery(r app.Row, query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return true
	}
	hay := strings.ToLower(strings.Join([]string{
		r.Node.Name, r.Node.Server, strconv.Itoa(r.Node.Port), r.Node.Type,
		r.Node.ProtocolLabel(), r.Node.TransportLabel(),
		r.Result.IP, r.Result.GeoLabel(), r.Result.IPTypeLabel(),
	}, " "))
	return strings.Contains(hay, q)
}

type filteredRow struct {
	index int
	row   app.Row
}

const tableRowHeight float32 = 58

// cellGap is the breathing room between two columns; text is clipped this much
// narrower than its cell so an ellipsis never touches the next column.
const cellGap float32 = 10

func cellTextWidth(width float32) float32 {
	if width <= cellGap {
		return width
	}
	return width - cellGap
}

func filterRows(rows []app.Row, mode filterMode, query string) []filteredRow {
	out := make([]filteredRow, 0, len(rows))
	for i, r := range rows {
		if rowMatches(r, mode) && matchesQuery(r, query) {
			out = append(out, filteredRow{index: i, row: r})
		}
	}
	return out
}

// Column widths are fixed so the header and every row line up. The name column
// absorbs the remaining width of the window.
type tableCols struct {
	dot, name, server, ip, geo, transport, delay, jitter, loss, score, arrow float32
}

func newTableCols(c *ui.Context) tableCols {
	w, _ := c.Size()
	fixed := float32(24 + 190 + 120 + 190 + 140 + 66 + 68 + 50 + 88 + 16)
	name := w - 70 - fixed // page padding, card border and inner padding
	if name < 170 {
		name = 170
	}
	return tableCols{
		dot: 24, name: name, server: 190, ip: 120, geo: 190, transport: 140,
		delay: 66, jitter: 68, loss: 50, score: 88, arrow: 16,
	}
}

func table(c *ui.Context, a *app.App, st *State, snap app.Snapshot, tk *theme.Tokens) {
	ui.Column(c).Grow(1).Basis(0).MinHeight(0).Clip().
		Background(tk.BgContainer).Border(1, tk.BorderSecondary).Radius(tk.BorderRadiusLG).
		Children(func() {
			cols := newTableCols(c)
			tableHeader(c, cols, tk)
			layout.Divider(c, layout.DividerProps{})

			rows := filterRows(snap.Rows, filterMode(st.Filter), st.Query)
			if len(rows) == 0 {
				ui.Column(c).Grow(1).Basis(0).Center().Children(func() {
					emptyState(c, snap, tk)
				})
				return
			}
			layout.ScrollArea(c, layout.ScrollAreaProps{Direction: layout.Vertical, Fade: true}, func() {
				for _, item := range rows {
					tableRow(c, a, item, cols, tk, snap.Selected)
				}
				ui.Box(c).Height(6)
			}).Grow(1).Basis(0)
		})
}

func headerCell(c *ui.Context, label string, width float32, tk *theme.Tokens) {
	display.Text(c, label, display.TextProps{Type: display.TextSecondary, FontSize: 11}).
		Width(width).Shrink(0).SingleLine()
}

func tableHeader(c *ui.Context, cols tableCols, tk *theme.Tokens) {
	ui.Row(c).Height(36).PaddingX(14).AlignItems(ui.Center).Children(func() {
		headerCell(c, "", cols.dot, tk)
		headerCell(c, "节点", cols.name, tk)
		headerCell(c, "服务器", cols.server, tk)
		headerCell(c, "IP", cols.ip, tk)
		headerCell(c, "地区 / 运营商", cols.geo, tk)
		headerCell(c, "传输", cols.transport, tk)
		headerCell(c, "延迟", cols.delay, tk)
		headerCell(c, "抖动", cols.jitter, tk)
		headerCell(c, "丢包", cols.loss, tk)
		headerCell(c, "评分", cols.score, tk)
		headerCell(c, "", cols.arrow, tk)
	})
}

func tableRow(c *ui.Context, a *app.App, item filteredRow, cols tableCols, tk *theme.Tokens, selected int) {
	row := item.row
	rowEl := ui.Row(c).Height(tableRowHeight).PaddingX(14).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(tk.FillQuaternary).Cursor(ui.CursorPointer)
	switch {
	case item.index == selected:
		rowEl.Background(tk.FillSecondary)
	case rowEl.Hovered():
		rowEl.Background(tk.FillTertiary)
	}
	rowEl.OnClick(func() { a.Select(item.index) })

	rowEl.Children(func() {
		statusCell(c, row, cols.dot, tk)
		nameCell(c, row, cols.name, tk)
		serverCell(c, row, cols.server, tk)
		ipCell(c, row, cols.ip, tk)
		geoCell(c, row, cols.geo, tk)
		transportCell(c, row, cols.transport, tk)
		textCell(c, delayText(row), cols.delay, toneOf(row, tk), tk)
		textCell(c, jitterText(row), cols.jitter, tk.TextSecondary, tk)
		textCell(c, lossText(row), cols.loss, lossTone(row, tk), tk)
		scoreCell(c, row, cols.score, tk)
		ui.Row(c).Width(cols.arrow).Shrink(0).AlignItems(ui.Center).Children(func() {
			display.Icon(c, lucide.ChevronRight(), display.IconProps{Size: display.IconSmall}).TextColor(tk.TextQuaternary)
		})
	})
}

func statusCell(c *ui.Context, row app.Row, width float32, tk *theme.Tokens) {
	ui.Row(c).Width(width).Shrink(0).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(8, 8).Radius(4).Background(toneOf(row, tk))
	})
}

func nameCell(c *ui.Context, row app.Row, width float32, tk *theme.Tokens) {
	ui.Row(c).Width(width).Shrink(0).Clip().PaddingEnd(cellGap).Gap(6).AlignItems(ui.Center).Children(func() {
		display.Tag(c, row.Node.ProtocolLabel(), display.TagProps{
			Variant: display.TagOutlined, Size: theme.Small, Color: display.TagGray,
		}).Shrink(0)
		display.Text(c, row.Node.Name, display.TextProps{FontSize: 13}).Grow(1).MinWidth(0).MaxWidth(cellTextWidth(width)).SingleLine()
	})
}

func serverCell(c *ui.Context, row app.Row, width float32, tk *theme.Tokens) {
	ui.Column(c).Width(width).Shrink(0).Clip().PaddingEnd(cellGap).Gap(1).Children(func() {
		display.Text(c, row.Node.Address(), display.TextProps{Code: true, FontSize: 12}).Width(cellTextWidth(width)).SingleLine()
		detail := firstNonEmpty(row.Node.ServerName, row.Node.Host, row.Node.Cipher)
		if detail != "" {
			display.Text(c, detail, display.TextProps{Type: display.TextSecondary, FontSize: 11}).Width(cellTextWidth(width)).SingleLine()
		}
	})
}

func ipCell(c *ui.Context, row app.Row, width float32, tk *theme.Tokens) {
	ip := row.Result.IP
	family := row.Result.Family
	if ip == "" {
		ip = "—"
		family = ""
	}
	ui.Column(c).Width(width).Shrink(0).Clip().PaddingEnd(cellGap).Gap(1).Children(func() {
		display.Text(c, ip, display.TextProps{Code: true, FontSize: 12}).Width(cellTextWidth(width)).SingleLine()
		hint := family
		if row.Result.DNS > 0 {
			hint += fmt.Sprintf(" · DNS %dms", row.Result.DNS.Milliseconds())
		}
		if hint == "" {
			hint = "待解析"
		}
		display.Text(c, hint, display.TextProps{Type: display.TextSecondary, FontSize: 11}).Width(cellTextWidth(width)).SingleLine()
	})
}

func geoCell(c *ui.Context, row app.Row, width float32, tk *theme.Tokens) {
	ui.Column(c).Width(width).Shrink(0).Clip().PaddingEnd(cellGap).Gap(1).Children(func() {
		display.Text(c, row.Result.GeoLabel(), display.TextProps{FontSize: 12}).Width(cellTextWidth(width)).SingleLine()
		kind := row.Result.IPTypeLabel()
		if kind == "" {
			kind = "—"
		}
		display.Text(c, kind, display.TextProps{Type: display.TextSecondary, FontSize: 11}).Width(cellTextWidth(width)).SingleLine()
	})
}

func transportCell(c *ui.Context, row app.Row, width float32, tk *theme.Tokens) {
	sub := row.Node.Path
	if sub == "" {
		sub = row.Node.ServiceName
	}
	if sub == "" {
		sub = row.Node.Flow
	}
	if sub == "" {
		sub = firstNonEmpty(row.Node.ServerName, row.Node.Host, row.Node.Cipher)
	}
	ui.Column(c).Width(width).Shrink(0).Clip().PaddingEnd(cellGap).Gap(1).Children(func() {
		display.Text(c, row.Node.TransportLabel(), display.TextProps{FontSize: 12}).Width(cellTextWidth(width)).SingleLine()
		if sub != "" {
			display.Text(c, sub, display.TextProps{Type: display.TextSecondary, FontSize: 11, Code: true}).Width(cellTextWidth(width)).SingleLine()
		}
	})
}

func textCell(c *ui.Context, value string, width float32, color ui.Color, tk *theme.Tokens) {
	ui.Box(c).Width(width).Shrink(0).Clip().PaddingEnd(cellGap).Children(func() {
		ui.Text(c, value).FontSize(12).TextColor(color).Width(cellTextWidth(width)).SingleLine()
	})
}

func scoreCell(c *ui.Context, row app.Row, width float32, tk *theme.Tokens) {
	ui.Row(c).Width(width).Shrink(0).Clip().PaddingEnd(cellGap).Gap(6).AlignItems(ui.Center).Children(func() {
		switch {
		case !row.Probed:
			display.Text(c, "…", display.TextProps{Type: display.TextSecondary, FontSize: 12})
			return
		case row.Result.UDPOnly:
			display.Tag(c, "未探测", display.TagProps{
				Variant: display.TagFilled, Size: theme.Small, Color: display.TagWarning,
			})
			return
		case row.Result.Score == 0:
			display.Text(c, "—", display.TextProps{Type: display.TextSecondary, FontSize: 12})
			return
		}
		display.Tag(c, strconv.Itoa(row.Result.Score), display.TagProps{
			Variant: display.TagFilled, Size: theme.Small, Color: scoreTagColor(row.Result),
		})
		display.Text(c, row.Result.Grade, display.TextProps{Type: display.TextSecondary, FontSize: 11}).SingleLine()
	})
}

func delayText(row app.Row) string {
	if !row.Probed {
		return "…"
	}
	if !row.Result.Reachable {
		return "—"
	}
	return fmt.Sprintf("%d ms", row.Result.Avg.Milliseconds())
}

func jitterText(row app.Row) string {
	if !row.Probed || !row.Result.Reachable {
		return "—"
	}
	return fmt.Sprintf("±%d ms", row.Result.Jitter.Milliseconds())
}

func lossText(row app.Row) string {
	if !row.Probed || !row.Result.Reachable {
		if row.Result.UDPOnly {
			return "—"
		}
		return "100%"
	}
	return fmt.Sprintf("%.0f%%", row.Result.Loss*100)
}

func lossTone(row app.Row, tk *theme.Tokens) ui.Color {
	if !row.Probed || !row.Result.Reachable {
		return tk.TextSecondary
	}
	if row.Result.Loss > 0 {
		return tk.Warning
	}
	return tk.TextSecondary
}

func toneOf(row app.Row, tk *theme.Tokens) ui.Color {
	switch {
	case !row.Probed:
		return tk.TextQuaternary
	case row.Result.UDPOnly:
		return tk.Warning
	case !row.Result.Reachable:
		return tk.Error
	case row.Result.Score >= 85:
		return tk.Success
	case row.Result.Score >= 70:
		return tk.Success
	case row.Result.Score >= 50:
		return tk.Warning
	default:
		return tk.Error
	}
}

func scoreTagColor(r probe.Result) display.TagColor {
	switch {
	case r.UDPOnly:
		return display.TagWarning
	case r.Score >= 70:
		return display.TagSuccess
	case r.Score >= 50:
		return display.TagWarning
	default:
		return display.TagError
	}
}

func emptyState(c *ui.Context, snap app.Snapshot, tk *theme.Tokens) {
	if len(snap.Rows) == 0 {
		display.Empty(c, display.EmptyProps{
			Icon:        lucide.Globe(),
			Title:       "粘贴订阅链接，开始节点体检",
			Description: "解析 Clash / Xray / sing-box 订阅，逐个节点测量 TCP 握手、抖动、丢包与 TLS 证书，并查询 IP 归属和机房标记。",
		})
		return
	}
	display.Empty(c, display.EmptyProps{
		Icon:        lucide.ListFilter(),
		Title:       "没有匹配的节点",
		Description: "换一个筛选条件，或清空搜索框。",
	})
}
