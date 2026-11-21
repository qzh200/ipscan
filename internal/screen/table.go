package screen

import (
	"bytes"
	"cmp"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"ipscan/internal/app"
	"ipscan/internal/model"
	"ipscan/internal/probe"
)

// nameCollator orders Chinese node names by pinyin rather than by codepoint.
var nameCollator = collate.New(language.SimplifiedChinese)

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

func filterRows(rows []app.Row, mode filterMode, query string, ipType int) []filteredRow {
	out := make([]filteredRow, 0, len(rows))
	for i, r := range rows {
		if rowMatches(r, mode) && matchesIPType(r, ipType) && matchesQuery(r, query) {
			out = append(out, filteredRow{index: i, row: r})
		}
	}
	return out
}

// matchesIPType splits registry roles: residential/enterprise, datacenter and
// mobile. A node without registry data only appears under "all".
func matchesIPType(r app.Row, kind int) bool {
	if kind == 0 {
		return true
	}
	geo := r.Result.Geo
	if geo == nil {
		return false
	}
	switch kind {
	case 1:
		return !geo.Hosting && !geo.Mobile
	case 2:
		return geo.Hosting
	case 3:
		return geo.Mobile
	}
	return true
}

var ipTypeLabels = []string{"全部 IP", "住宅 / 企业", "机房 IP", "移动网络"}

// baseRows applies the filters that sit outside the status tabs, so the tab
// counts match what the current IP type and search can show.
func baseRows(rows []app.Row, ipType int, query string) []app.Row {
	out := make([]app.Row, 0, len(rows))
	for _, r := range rows {
		if matchesIPType(r, ipType) && matchesQuery(r, query) {
			out = append(out, r)
		}
	}
	return out
}

// tableWidth is the width the table card can show: the window minus the page
// padding, the card border and the gutter the ScrollView's scroll bar paints
// over. Columns are laid out to exactly this, so the table never needs
// horizontal scrolling and the bar never covers the score column.
func tableWidth(windowWidth float32) float32 {
	const chrome = 54 // page padding 22×2 + card border 2, plus rounding
	const scrollbar = 12
	return max(float32(960), windowWidth-chrome-scrollbar)
}

// tableColumnsFor proportions the columns to the window so the table always
// fills the card: no column hogs the extra room on a wide screen, and the
// score column stays visible on a narrow one.
func tableColumnsFor(windowWidth float32) []ui.TableColumn {
	content := tableWidth(windowWidth)
	specs := []struct {
		id, title string
		ratio     float32
		min, max  float32
		align     ui.Align
	}{
		{"name", "节点", .20, 180, 340, ui.Start},
		{"server", "地址", .13, 120, 230, ui.Start},
		{"port", "端口", .045, 52, 80, ui.End},
		{"ip", "IP", .085, 96, 150, ui.Start},
		{"geo", "地区 / 运营商", .185, 150, 380, ui.Start},
		{"network", "传输", .06, 70, 96, ui.Start},
		{"tls", "TLS", .06, 74, 100, ui.Start},
		{"delay", "延迟", .06, 74, 100, ui.End},
		{"jitter", "抖动", .05, 56, 88, ui.End},
		{"loss", "丢包", .04, 48, 72, ui.End},
		{"score", "评分", .075, 82, 120, ui.End},
	}

	widths := make([]float32, len(specs))
	total := float32(0)
	for i, spec := range specs {
		w := max(spec.min, content*spec.ratio)
		if spec.max > 0 {
			w = min(w, spec.max)
		}
		widths[i] = w
		total += w
	}
	// Shrink the two widest flexible columns when minimums overflow a narrow
	// window, then give any surplus to the name and region columns.
	for _, i := range []int{4, 0} {
		if total <= content {
			break
		}
		take := min(total-content, widths[i]-specs[i].min)
		if take > 0 {
			widths[i] -= take
			total -= take
		}
	}
	if total < content {
		extra := content - total
		widths[0] += extra * .45
		widths[4] += extra * .55
	}

	cols := make([]ui.TableColumn, len(specs))
	for i, spec := range specs {
		cols[i] = ui.TableColumn{
			ID: spec.id, Title: spec.title, Width: widths[i],
			MinWidth: spec.min, Align: spec.align, Sortable: true,
		}
	}
	return cols
}

// fitTableWidths keeps the table inside its card. Onevra remembers the widths
// the user dragged, and a widened column used to push the last columns past the
// right edge, clipping the score badge. Normalising the remembered widths to
// the card's width keeps a drag's proportions while the table always fits.
func fitTableWidths(state *components.TableState, cols []ui.TableColumn, content float32) {
	if len(state.Columns.Widths) == 0 || content <= 0 {
		return
	}
	widths := make(map[string]float32, len(cols))
	total := float32(0)
	for _, col := range cols {
		w, ok := state.Columns.Widths[col.ID]
		if !ok {
			w = col.Width
		}
		w = max(w, col.MinWidth, 16)
		widths[col.ID] = w
		total += w
	}
	if total <= 0 {
		return
	}
	scale := content / total
	sum := float32(0)
	for _, col := range cols {
		widths[col.ID] = max(widths[col.ID]*scale, col.MinWidth, 16)
		sum += widths[col.ID]
	}
	// The clamp may have left a few pixels over or under the card; spread the
	// difference over the columns that still have room.
	if diff := content - sum; diff != 0 {
		slack := float32(0)
		for _, col := range cols {
			slack += widths[col.ID] - col.MinWidth
		}
		if slack > 0 {
			for _, col := range cols {
				room := widths[col.ID] - col.MinWidth
				widths[col.ID] += diff * room / slack
			}
		}
	}
	state.Columns.Widths = widths
}

func toolbarAndTable(c *ui.Context, a *app.App, st *State, snap app.Snapshot, t *ui.Theme) {
	ui.Column(c).Grow(1).Basis(0).MinHeight(0).Clip().
		Background(t.Surface).Border(1, t.Border).Radius(t.Space(3)).
		Children(func() {
			toolbar(c, a, st, snap, t)
			ui.Box(c).Height(1).Background(t.Border)
			rows := filterRows(snap.Rows, filterMode(st.Filter), st.Query, st.IPType)
			switch {
			case len(snap.Rows) == 0:
				emptyState(c, t, false)
			case len(rows) == 0:
				emptyState(c, t, true)
			default:
				st.Table.Sort = &st.Sort
				st.Table.OnSort = func() {
					// A newly chosen column starts in its natural direction:
					// best score first, everything else ascending.
					if st.Sort.Column != st.sortSeen {
						st.sortSeen = st.Sort.Column
						st.Sort.Descending = defaultDescending(st.Sort.Column)
					}
				}
				sortFiltered(rows, st.Sort)
				windowWidth, _ := c.Size()
				width := tableWidth(windowWidth)
				cols := tableColumnsFor(windowWidth)
				fitTableWidths(&st.Table, cols, width)
				components.ScrollView(c, components.ScrollViewOptions{Axes: components.ScrollVertical}).
					Grow(1).Basis(0).Children(func() {
					ui.Column(c).Width(width).Children(func() {
						components.Table(c, &st.Table, cols, func() {
							for _, item := range rows {
								tableRow(c, a, item, t, snap)
							}
						})
					})
				})
			}
		})
}

func toolbar(c *ui.Context, a *app.App, st *State, snap app.Snapshot, t *ui.Theme) {
	rows := baseRows(snap.Rows, st.IPType, st.Query)
	labels := []string{
		fmt.Sprintf("全部 %d", len(rows)),
		fmt.Sprintf("健康 %d", countRows(rows, healthyFilter)),
		fmt.Sprintf("需改善 %d", countRows(rows, needsWorkFilter)),
		fmt.Sprintf("不可达 %d", countRows(rows, failedFilter)),
	}
	filter := st.Filter
	ui.Row(c).Padding(12, 10).Gap(10).AlignItems(ui.Center).Children(func() {
		components.Tabs(c, &filter, components.TabsHighlight, labels...)
		st.Filter = filter

		ui.Box(c).Width(226).Children(func() {
			components.TextBox(c, &st.Query, components.TextBoxOptions{LeadingIcon: lucide.Search()}).
				Label("搜索节点").Placeholder("搜索节点 / 域名 / IP / 地区")
		})
		ipSelect(c, st)
		ui.Spacer(c)
		if snap.Status == app.StatusProbing {
			components.Button(c, "停止", components.ButtonGhost).OnClick(a.Stop)
		}
		reprobe := components.Button(c, "", components.ButtonGhost)
		reprobe.Label("重新检测")
		reprobe.OnClick(a.Reprobe)
		reprobe.Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				components.Icon(c, lucide.RefreshCw).Size(14, 14)
				components.Text(c, "重新检测")
			})
		})
	})
}

func ipSelect(c *ui.Context, st *State) {
	sel := components.Select(c, &st.IPType)
	sel.Trigger.Label("IP 类型").Width(138)
	sel.Trigger.Children(func() {
		idx := st.IPType
		if idx < 0 || idx >= len(ipTypeLabels) {
			idx = 0
		}
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			components.Icon(c, lucide.ListFilter).Size(13, 13)
			components.Text(c, ipTypeLabels[idx]).SingleLine()
		})
	})
	sel.Popup(func(panel ui.Element) {
		for i, label := range ipTypeLabels {
			item := sel.Item(i)
			item.Children(func() { components.Text(c, label).SingleLine() })
		}
	})
}

func tableRow(c *ui.Context, a *app.App, item filteredRow, t *ui.Theme, snap app.Snapshot) {
	row := item.row
	selected := item.index == snap.Selected
	el := components.TableRow(c, item.index, model.SortName(row.Node.Name), selected, func(col int) {
		cells(c, row, col, t)
	})
	el.MinHeight(44).OnClick(func() { a.Select(item.index) })
	if el.Hovered() && !selected {
		el.Background(t.SurfaceHover)
	}
	busy := row.Probing || snap.Status == app.StatusLoading || snap.Status == app.StatusProbing
	components.ContextMenu(c, el, func(m *components.Menu) {
		if m.Item("查看详情").Chosen() {
			a.Select(item.index)
		}
		m.Separator()
		if m.Item("重新检测此节点").Disabled(busy).Chosen() {
			a.ReprobeNode(item.index)
		}
		if m.Item("全球 Ping（8 个探测点）").Disabled(a.Remote == nil || row.RemoteBusy).Chosen() {
			a.CheckGlobal(item.index)
		}
		m.Separator()
		if m.Item("复制节点名称").Chosen() {
			c.WriteClipboard(row.Node.Name)
		}
		if m.Item("复制服务器地址").Chosen() {
			c.WriteClipboard(row.Node.Address())
		}
		if m.Item("复制解析 IP").Disabled(row.Result.IP == "").Chosen() {
			c.WriteClipboard(row.Result.IP)
		}
		if m.Item("复制节点链接").Disabled(row.Node.Raw == "").Chosen() {
			c.WriteClipboard(row.Node.Raw)
		}
	})
}

func cells(c *ui.Context, row app.Row, col int, t *ui.Theme) {
	switch col {
	case 0:
		ui.Row(c).Gap(6).AlignItems(ui.Center).MinWidth(0).Children(func() {
			components.Badge(c, row.Node.ProtocolLabel(), components.BadgeOptions{Tone: components.BadgeNeutral})
			nodeName(c, row.Node.Name, 12.5, false)
		})
	case 1:
		stack(c, func() {
			components.Text(c, row.Node.Server).FontSize(12).SingleLine()
			if detail := firstNonEmpty(row.Node.Host, row.Node.Cipher); detail != "" {
				muted(c, t, detail, 11)
			}
		})
	case 2:
		stack(c, func() {
			components.Text(c, strconv.Itoa(row.Node.Port)).FontSize(12).SingleLine()
			muted(c, t, carrierOf(row.Node), 11)
		})
	case 3:
		ip := row.Result.IP
		if ip == "" {
			ip = "—"
		}
		hint := row.Result.Family
		if row.Result.DNS > 0 {
			hint += fmt.Sprintf(" · DNS %dms", row.Result.DNS.Milliseconds())
		}
		stack(c, func() {
			components.Text(c, ip).FontSize(12).SingleLine()
			muted(c, t, hint, 11)
		})
	case 4:
		stack(c, func() {
			components.Text(c, row.Result.GeoLabel()).FontSize(12).SingleLine()
			muted(c, t, row.Result.IPTypeLabel(), 11)
		})
	case 5:
		network := strings.ToUpper(row.Node.Network)
		if network == "" {
			network = "TCP"
		}
		stack(c, func() {
			components.Text(c, network).FontSize(12).SingleLine()
			if detail := transportDetail(row.Node); detail != "" {
				muted(c, t, detail, 11)
			}
		})
	case 6:
		label := securityLabel(row.Node.Security)
		color := t.Text
		switch label {
		case "REALITY":
			color = t.Accent
		case "无":
			color = t.TextMuted
		}
		stack(c, func() {
			components.Text(c, label).FontSize(12).TextColor(color).SingleLine()
			if sni := firstNonEmpty(row.Node.ServerName, row.Node.Host); sni != "" {
				muted(c, t, sni, 11)
			}
		})
	case 7:
		ui.Column(c).Gap(1).AlignItems(ui.End).Children(func() {
			components.Text(c, delayText(row)).FontSize(12).TextColor(toneOf(row, t)).SingleLine()
			if row.RemoteBusy {
				muted(c, t, "全球…", 10)
			} else if row.RemoteErr != "" {
				components.Text(c, "全球失败").FontSize(10).TextColor(t.Danger).SingleLine()
			} else if row.Remote != nil {
				label := "全球 —"
				switch {
				case row.Remote.Reachable > 0 && row.Remote.Median > 0:
					label = "全球 " + compactDuration(row.Remote.Median)
				case row.Remote.Shared > 0:
					label = "全球 边缘"
				}
				components.Text(c, label).FontSize(10).TextColor(t.Accent).SingleLine()
			}
		})
	case 8:
		components.Text(c, jitterText(row)).FontSize(12).TextColor(t.TextMuted).SingleLine()
	case 9:
		components.Text(c, lossText(row)).FontSize(12).TextColor(lossTone(row, t)).SingleLine()
	case 10:
		scoreCell(c, row, t)
	}
}

func stack(c *ui.Context, build func()) {
	ui.Column(c).Gap(1).MinWidth(0).Children(build)
}

func muted(c *ui.Context, t *ui.Theme, value string, size float32) ui.Element {
	if value == "" {
		value = "—"
	}
	return components.Text(c, value).FontSize(size).TextColor(t.TextMuted).SingleLine()
}

func scoreCell(c *ui.Context, row app.Row, t *ui.Theme) {
	switch {
	case row.Probing:
		components.Text(c, "检测中…").FontSize(11.5).TextColor(t.TextMuted).SingleLine()
	case !row.Probed:
		components.Text(c, "…").FontSize(12).TextColor(t.TextMuted)
	case row.Result.UDPOnly:
		components.Badge(c, "未探测", components.BadgeOptions{Tone: components.BadgeWarning})
	case row.Result.Score == 0:
		components.Text(c, "—").FontSize(12).TextColor(t.TextMuted)
	default:
		tone := components.BadgeSuccess
		switch {
		case row.Result.Score < 50:
			tone = components.BadgeDanger
		case row.Result.Score < 70:
			tone = components.BadgeWarning
		}
		components.Badge(c, fmt.Sprintf("%d %s", row.Result.Score, row.Result.Grade),
			components.BadgeOptions{Tone: tone})
	}
}

func emptyState(c *ui.Context, t *ui.Theme, filtered bool) {
	title, desc := "粘贴订阅链接，开始质检", "解析 Clash / Xray / sing-box 订阅，逐节点测量 TCP 握手、抖动、丢包与 TLS 证书，并查询 IP 归属。"
	if filtered {
		title, desc = "没有匹配的节点", "换一个筛选条件，或清空搜索框。"
	}
	ui.Column(c).Grow(1).Basis(0).Center().Padding(48).Gap(10).Children(func() {
		ui.Row(c).Size(46, 46).Radius(14).Background(t.Accent.Alpha(.1)).Center().Children(func() {
			components.Icon(c, lucide.Globe).Size(24, 24).TextColor(t.Accent)
		})
		components.Text(c, title).FontSize(16).Bold()
		components.Text(c, desc).FontSize(12).TextColor(t.TextMuted).Wrap().MaxWidth(520)
	})
}

func carrierOf(n model.Node) string {
	if probe.UDPTransport(n) {
		return "UDP"
	}
	return "TCP"
}

func transportDetail(n model.Node) string {
	if n.Path != "" {
		return n.Path
	}
	if n.ServiceName != "" {
		return "svc " + n.ServiceName
	}
	if n.Flow != "" {
		return n.Flow
	}
	return ""
}

func securityLabel(security string) string {
	switch strings.ToLower(strings.TrimSpace(security)) {
	case "", "none":
		return "无"
	default:
		return strings.ToUpper(security)
	}
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
	if !row.Probed {
		return "—"
	}
	if !row.Result.Reachable {
		if row.Result.UDPOnly {
			return "—"
		}
		return "100%"
	}
	return fmt.Sprintf("%.0f%%", row.Result.Loss*100)
}

func lossTone(row app.Row, t *ui.Theme) ui.Color {
	if !row.Probed || !row.Result.Reachable {
		return t.TextMuted
	}
	if row.Result.Loss > 0 {
		return t.Warning
	}
	return t.TextMuted
}

func toneOf(row app.Row, t *ui.Theme) ui.Color {
	switch {
	case !row.Probed:
		return t.TextMuted
	case row.Result.UDPOnly:
		return t.Warning
	case !row.Result.Reachable:
		return t.Danger
	case row.Result.Score >= 70:
		return t.Success
	case row.Result.Score >= 50:
		return t.Warning
	default:
		return t.Danger
	}
}

func compactDuration(d time.Duration) string {
	if d >= time.Second {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dms", d.Milliseconds())
}

// defaultDescending gives a newly chosen column its natural first direction.
func defaultDescending(column string) bool { return column == "score" }

// sortFiltered orders rows in place. Missing values (unprobed nodes, QUIC
// nodes without TCP latency) stay at the end whichever direction is chosen.
func sortFiltered(rows []filteredRow, order ui.SortOrder) {
	if order.Column == "" || len(rows) < 2 {
		return
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := &rows[i].row, &rows[j].row
		aOK, bOK := hasSortValue(a, order.Column), hasSortValue(b, order.Column)
		if aOK != bOK {
			return aOK
		}
		c := compareRows(a, b, order.Column)
		if c == 0 {
			return false
		}
		if order.Descending {
			return c > 0
		}
		return c < 0
	})
}

func hasSortValue(r *app.Row, column string) bool {
	switch column {
	case "delay":
		return r.Probed && r.Result.Reachable && r.Result.Avg > 0
	case "jitter", "loss", "score":
		return r.Probed && r.Result.Reachable
	case "ip":
		return r.Result.IP != ""
	}
	return true
}

func compareRows(a, b *app.Row, column string) int {
	switch column {
	case "name":
		return nameCollator.CompareString(model.SortName(a.Node.Name), model.SortName(b.Node.Name))
	case "server":
		return strings.Compare(strings.ToLower(a.Node.Server), strings.ToLower(b.Node.Server))
	case "port":
		return cmp.Compare(a.Node.Port, b.Node.Port)
	case "ip":
		return compareIP(a.Result.IP, b.Result.IP)
	case "geo":
		return strings.Compare(strings.ToLower(a.Result.GeoLabel()), strings.ToLower(b.Result.GeoLabel()))
	case "network":
		return strings.Compare(strings.ToUpper(a.Node.Network), strings.ToUpper(b.Node.Network))
	case "tls":
		return strings.Compare(strings.ToUpper(a.Node.Security), strings.ToUpper(b.Node.Security))
	case "delay":
		return cmp.Compare(a.Result.Avg, b.Result.Avg)
	case "jitter":
		return cmp.Compare(a.Result.Jitter, b.Result.Jitter)
	case "loss":
		return cmp.Compare(a.Result.Loss, b.Result.Loss)
	case "score":
		return cmp.Compare(a.Result.Score, b.Result.Score)
	}
	return 0
}

func compareIP(a, b string) int {
	ia, ib := net.ParseIP(a), net.ParseIP(b)
	if ia != nil && ib != nil {
		return bytes.Compare(ia.To16(), ib.To16())
	}
	return strings.Compare(a, b)
}
