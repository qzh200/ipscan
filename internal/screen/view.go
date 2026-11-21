// Package screen draws the app with Onevra's warm native design system: a
// branded top bar, a subscription card, summary cards, a data table and a
// full-page node detail. It only reads app.Snapshot and calls app methods.
package screen

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"github.com/oneo-me/onevra/source/theme"

	"ipscan/internal/app"
)

// State holds the view-only controls; the data lives in app.App.
type State struct {
	Filter int
	Query  string
	// IPType filters by registry role: 0 all, 1 residential/enterprise,
	// 2 datacenter, 3 mobile.
	IPType int
	// Sort is the table's current order; Table keeps its column layout and
	// reports header clicks through OnSort.
	Sort     ui.SortOrder
	Table    components.TableState
	sortSeen string
}

// NewState starts with nodes ordered by name, the list's natural reading order.
func NewState() *State {
	return &State{
		Sort:     ui.SortOrder{Column: "name"},
		sortSeen: "name",
	}
}

// View is the root of the window.
func View(c *ui.Context, a *app.App, st *State) {
	theme.Apply(c)
	t := c.Theme()
	a.SetNotifier(c.Invalidate)
	snap := a.Snapshot()

	ui.Column(c).Fill().Background(t.Background).Children(func() {
		ui.Column(c).Grow(1).Basis(0).Padding(26, 22).Gap(16).Children(func() {
			if snap.Selected >= 0 && snap.Selected < len(snap.Rows) {
				detailPage(c, a, snap, t)
				return
			}
			hero(c, snap, t)
			subscriptionCard(c, a, t)
			statusLine(c, a, snap, t)
			if len(snap.Rows) > 0 {
				statsRow(c, st, snap, t)
			}
			toolbarAndTable(c, a, st, snap, t)
			footer(c, snap, st, t)
		})
	})
}

func statusBadge(snap app.Snapshot) (string, components.BadgeTone) {
	switch snap.Status {
	case app.StatusLoading, app.StatusProbing:
		return "检测中", components.BadgeAccent
	case app.StatusError:
		return "出错", components.BadgeDanger
	case app.StatusDone:
		if len(snap.Rows) > 0 {
			return "已完成", components.BadgeSuccess
		}
	}
	return "待检测", components.BadgeNeutral
}

func hero(c *ui.Context, snap app.Snapshot, t *ui.Theme) {
	ui.Row(c).AlignItems(ui.End).Gap(16).Children(func() {
		ui.Column(c).Grow(1).Gap(4).Children(func() {
			components.Text(c, "订阅质检").FontSize(27).Bold()
			components.Text(c, "订阅 → 节点 → 延迟 / 抖动 / TLS / IP 归属 / 全球探测").
				FontSize(12.5).TextColor(t.TextMuted)
		})
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			label, tone := statusBadge(snap)
			components.Badge(c, label, components.BadgeOptions{Tone: tone})
			if snap.Format != "" {
				components.Badge(c, snap.Format, components.BadgeOptions{Tone: components.BadgeNeutral})
			}
			if snap.Status == app.StatusDone && len(snap.Rows) > 0 {
				components.Badge(c, fmt.Sprintf("%d 个节点", len(snap.Rows)), components.BadgeOptions{Tone: components.BadgeAccent})
			}
			components.Text(c, "本地探测 + Globalping").FontSize(11).TextColor(t.TextMuted)
		})
	})
}

func subscriptionCard(c *ui.Context, a *app.App, t *ui.Theme) {
	ui.Row(c).Padding(12).Gap(10).AlignItems(ui.Center).
		Background(t.Surface).Border(1, t.Border).Radius(t.Space(3.5)).
		Shadow(0, t.Space(.5), t.Space(4), 0, ui.RGBA(0, 0, 0, .05)).
		Children(func() {
			components.TextBox(c, a.Input, components.TextBoxOptions{
				LeadingIcon: lucide.Link(),
			}).Label("订阅链接").
				Placeholder("粘贴 Clash / Xray / sing-box 订阅链接或本地文件路径").
				Grow(1).OnSubmit(a.Load)

			components.Button(c, "粘贴", components.ButtonGhost).
				OnClick(func() { *a.Input = strings.TrimSpace(c.ReadClipboard()) })

			start := components.Button(c, "", components.ButtonPrimary)
			start.Label("开始质检")
			start.OnClick(a.Load)
			start.Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					components.Icon(c, lucide.Zap).Size(15, 15)
					components.Text(c, "开始质检")
				})
			})
		})
}

func statusLine(c *ui.Context, a *app.App, snap app.Snapshot, t *ui.Theme) {
	switch snap.Status {
	case app.StatusError:
		ui.Row(c).Padding(10, 14).Gap(10).AlignItems(ui.Center).
			Background(t.Danger.Alpha(.08)).Border(1, t.Danger.Alpha(.25)).Radius(t.Radius).
			Children(func() {
				components.Icon(c, lucide.CircleX).Size(16, 16).TextColor(t.Danger)
				components.Text(c, snap.Message).FontSize(12.5).TextColor(t.Danger).Wrap().Grow(1)
				components.Text(c, "链接需可公开访问；本地文件可用完整路径。").FontSize(11.5).TextColor(t.TextMuted)
			})
	case app.StatusLoading, app.StatusProbing:
		percent := 0.0
		if snap.Total > 0 {
			percent = float64(snap.Done) / float64(snap.Total)
		}
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			components.Spinner(c, components.SpinnerRing).Size(16, 16)
			components.Text(c, snap.Message).FontSize(12.5).TextColor(t.TextMuted).Grow(1).SingleLine()
			ui.Box(c).Width(200).Children(func() {
				components.Progress(c, percent)
			})
			components.Text(c, fmt.Sprintf("%d/%d", snap.Done, snap.Total)).FontSize(11.5).TextColor(t.TextMuted)
			components.Button(c, "停止", components.ButtonGhost).OnClick(a.Stop)
		})
	default:
		if len(snap.Rows) == 0 {
			return
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			components.Icon(c, lucide.CircleCheck).Size(15, 15).TextColor(t.Success)
			components.Text(c, snap.Message).FontSize(12.5).TextColor(t.TextMuted).SingleLine()
			if len(snap.Warnings) > 0 {
				components.Badge(c, fmt.Sprintf("跳过 %d 条", len(snap.Warnings)), components.BadgeOptions{Tone: components.BadgeWarning})
			}
			if snap.GeoNote != "" {
				components.Badge(c, "归属查询失败", components.BadgeOptions{Tone: components.BadgeWarning})
			}
			ui.Spacer(c)
			components.Text(c, "耗时 "+formatElapsed(snap.Elapsed)).FontSize(11.5).TextColor(t.TextMuted)
		})
	}
}

func statsRow(c *ui.Context, st *State, snap app.Snapshot, t *ui.Theme) {
	stats := app.Summarize(baseRows(snap.Rows, st.IPType, st.Query))
	reachPct := 0.0
	if stats.Total > 0 {
		reachPct = float64(stats.Reachable) / float64(stats.Total) * 100
	}
	ui.Row(c).Gap(12).AlignItems(ui.Stretch).Children(func() {
		statCard(c, t, lucide.Server, "节点总数", fmt.Sprintf("%d", stats.Total),
			fmt.Sprintf("%d 条已检测", stats.Probed), t.Text)
		statCard(c, t, lucide.Wifi, "TCP 可达", fmt.Sprintf("%d", stats.Reachable),
			fmt.Sprintf("覆盖 %.0f%%", reachPct), t.Success)
		statCard(c, t, lucide.Timer, "平均延迟", formatDelay(stats.AvgDelay),
			"TCP 握手均值", t.Accent)
		statCard(c, t, lucide.Gauge, "平均评分", fmt.Sprintf("%d", stats.AvgScore),
			fmt.Sprintf("%d 个健康节点", stats.Healthy), t.Warning)
	})
}

func statCard(c *ui.Context, t *ui.Theme, icon func() *ui.SVG, label, value, hint string, tone ui.Color) {
	ui.Column(c).Grow(1).Basis(0).Padding(14).Gap(3).
		Background(t.Surface).Border(1, t.Border).Radius(t.Space(3)).
		Children(func() {
			ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
				components.Icon(c, icon).Size(14, 14).TextColor(t.TextMuted)
				components.Text(c, label).FontSize(12).TextColor(t.TextMuted)
			})
			components.Text(c, value).FontSize(24).Bold().TextColor(tone)
			components.Text(c, hint).FontSize(11).TextColor(t.TextMuted).SingleLine()
		})
}

func footer(c *ui.Context, snap app.Snapshot, st *State, t *ui.Theme) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		components.Text(c, "IP 归属 ip-api.com → ipwho.is · 全球探测 Globalping").FontSize(11).TextColor(t.TextMuted)
		ui.Spacer(c)
		if len(snap.Rows) > 0 {
			visible := len(filterRows(snap.Rows, filterMode(st.Filter), st.Query, st.IPType))
			label := fmt.Sprintf("%d 个节点", len(snap.Rows))
			if visible != len(snap.Rows) {
				label = fmt.Sprintf("显示 %d / %d 个节点", visible, len(snap.Rows))
			}
			components.Text(c, label+" · 点击行查看全部参数 · 右键更多操作").
				FontSize(11).TextColor(t.TextMuted)
		}
	})
}

func formatDelay(d time.Duration) string {
	if d <= 0 {
		return "—"
	}
	return fmt.Sprintf("%d ms", d.Milliseconds())
}

func formatElapsed(d time.Duration) string {
	if d < time.Millisecond {
		return "<1 ms"
	}
	if d < time.Second {
		return fmt.Sprintf("%d ms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1f s", d.Seconds())
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
