// Package screen draws the app: a subscription bar, a summary strip, a table
// of nodes and a detail drawer. It only reads app.Snapshot and calls app
// methods, so the UI can be driven headlessly in tests.
package screen

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/lobehub/lobe-mygo/pkg/action"
	"github.com/lobehub/lobe-mygo/pkg/display"
	"github.com/lobehub/lobe-mygo/pkg/feedback"
	"github.com/lobehub/lobe-mygo/pkg/field"
	"github.com/lobehub/lobe-mygo/pkg/lucide"
	"github.com/lobehub/lobe-mygo/pkg/overlay"
	"github.com/lobehub/lobe-mygo/pkg/theme"

	"ipscan/internal/app"
)

// State holds the view-only controls; the data lives in app.App.
type State struct {
	Filter int
	Query  string
}

// View is the root of the window.
func View(c *ui.Context, a *app.App, st *State) {
	theme.Apply(c)
	tk := theme.TokensOf(c)
	a.SetNotifier(c.Invalidate)
	snap := a.Snapshot()

	ui.Column(c).Fill().Background(tk.BgLayout).Children(func() {
		ui.Column(c).Grow(1).Basis(0).Padding(20, 16).Gap(12).Children(func() {
			subscriptionBar(c, a, tk)
			statusStrip(c, a, snap, tk)
			if len(snap.Rows) > 0 {
				statsRow(c, snap, tk)
			}
			toolbar(c, a, st, snap, tk)
			table(c, a, st, snap, tk)
			footer(c, snap, tk)
		})
		detailDrawer(c, a, snap, tk)
	})
}

func subscriptionBar(c *ui.Context, a *app.App, tk *theme.Tokens) {
	ui.Row(c).Padding(12).Gap(10).AlignItems(ui.Center).
		Background(tk.BgContainer).Border(1, tk.BorderSecondary).Radius(tk.BorderRadiusLG).
		Children(func() {
			display.Icon(c, lucide.Link(), display.IconProps{Size: display.IconMiddle}).TextColor(tk.TextTertiary)
			ui.Column(c).Grow(1).Basis(0).AlignItems(ui.Stretch).Children(func() {
				input := field.Input(c, a.Input, field.InputProps{
					Variant:     field.InputOutlined,
					Size:        theme.Large,
					Placeholder: "粘贴 Clash / Xray / sing-box 订阅链接，或本地文件路径",
				})
				input.OnSubmit(a.Load)
			})
			action.Button(c, "粘贴", action.ButtonProps{Size: theme.Large}).
				OnClick(func() { *a.Input = strings.TrimSpace(c.ReadClipboard()) })
			button := action.StyleButton(c, ui.ButtonBase(c), "", action.ButtonProps{Variant: action.Primary, Size: theme.Large})
			button.OnClick(a.Load)
			button.Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					display.Icon(c, lucide.Zap(), display.IconProps{Size: display.IconSmall})
					ui.Text(c, "开始体检")
				})
			})
		})
}

func statusStrip(c *ui.Context, a *app.App, snap app.Snapshot, tk *theme.Tokens) {
	switch snap.Status {
	case app.StatusError:
		feedback.Alert(c, snap.Message, feedback.AlertProps{
			Type: feedback.Error, Variant: feedback.Soft,
			Description: "链接需可公开访问；本地文件可用完整路径。",
		})
	case app.StatusLoading, app.StatusProbing:
		percent := float32(0)
		if snap.Total > 0 {
			percent = float32(snap.Done) / float32(snap.Total) * 100
		}
		ui.Row(c).Gap(12).AlignItems(ui.Center).Padding(2, 2).Children(func() {
			feedback.Spin(c, feedback.SpinProps{Size: theme.Small}, nil)
			display.Text(c, snap.Message, display.TextProps{Type: display.TextSecondary, FontSize: 13}).Grow(1).SingleLine()
			ui.Column(c).Width(220).Children(func() {
				feedback.Progress(c, percent, feedback.ProgressProps{Size: theme.Small, Color: tk.Primary})
			})
			display.Text(c, fmt.Sprintf("%d/%d", snap.Done, snap.Total), display.TextProps{Type: display.TextSecondary, FontSize: 12, Code: true})
			action.Button(c, "停止", action.ButtonProps{Variant: action.Text, Size: theme.Small}).OnClick(a.Stop)
		})
	default:
		if len(snap.Rows) == 0 {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				display.Text(c, "支持 Clash YAML、Xray / sing-box JSON、vmess / vless / trojan / ss / hysteria2 链接及 base64 订阅",
					display.TextProps{Type: display.TextSecondary, FontSize: 12, Rows: 1})
				ui.Spacer(c)
				if snap.Format != "" {
					display.Tag(c, snap.Format, display.TagProps{Variant: display.TagOutlined, Size: theme.Small})
				}
			})
			return
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			display.Icon(c, lucide.CircleCheck(), display.IconProps{Size: display.IconSmall}).TextColor(tk.Success)
			display.Text(c, snap.Message, display.TextProps{Type: display.TextSecondary, FontSize: 13}).SingleLine()
			if snap.Format != "" {
				display.Tag(c, snap.Format, display.TagProps{Variant: display.TagOutlined, Size: theme.Small})
			}
			warningsTag(c, snap, tk)
			geoTag(c, snap)
			ui.Spacer(c)
			display.Text(c, "耗时 "+formatElapsed(snap.Elapsed), display.TextProps{Type: display.TextSecondary, FontSize: 12, Code: true})
		})
	}
}

func warningsTag(c *ui.Context, snap app.Snapshot, tk *theme.Tokens) {
	if len(snap.Warnings) == 0 {
		return
	}
	shown := snap.Warnings
	if len(shown) > 6 {
		shown = shown[:6]
	}
	tag := display.Tag(c, fmt.Sprintf("跳过 %d 条", len(snap.Warnings)), display.TagProps{
		Variant: display.TagFilled, Color: display.TagWarning, Size: theme.Small,
	})
	overlay.Tooltip(c, tag, strings.Join(shown, "\n"), overlay.TooltipProps{Placement: overlay.Bottom})
}

// geoTag explains a missing IP registry lookup instead of leaving every
// region cell at "—" without a reason.
func geoTag(c *ui.Context, snap app.Snapshot) {
	if snap.GeoNote == "" || len(snap.Rows) == 0 {
		return
	}
	tag := display.Tag(c, "归属查询失败", display.TagProps{
		Variant: display.TagOutlined, Color: display.TagWarning, Size: theme.Small,
	})
	overlay.Tooltip(c, tag, snap.GeoNote, overlay.TooltipProps{Placement: overlay.Bottom})
}

func statsRow(c *ui.Context, snap app.Snapshot, tk *theme.Tokens) {
	health := float32(0)
	if snap.Stats.Total > 0 {
		health = float32(snap.Stats.Reachable) / float32(snap.Stats.Total) * 100
	}
	ui.Row(c).Gap(12).AlignItems(ui.Stretch).Children(func() {
		statCard(c, tk, "节点总数", fmt.Sprintf("%d", snap.Stats.Total), fmt.Sprintf("%d 条已检测", snap.Stats.Probed), tk.Text)
		statCard(c, tk, "可达", fmt.Sprintf("%d", snap.Stats.Reachable), fmt.Sprintf("覆盖 %.0f%%", health), tk.Success)
		statCard(c, tk, "平均延迟", formatDelay(snap.Stats.AvgDelay), "TCP 握手均值", tk.Primary)
		statCard(c, tk, "平均评分", fmt.Sprintf("%d", snap.Stats.AvgScore), fmt.Sprintf("%d 个健康节点", snap.Stats.Healthy), tk.Warning)
	})
}

func statCard(c *ui.Context, tk *theme.Tokens, label, value, hint string, tone ui.Color) {
	ui.Column(c).Grow(1).Basis(0).Padding(14).Gap(3).
		Background(tk.FillTertiary).Radius(tk.BorderRadius).Children(func() {
		display.Text(c, label, display.TextProps{Type: display.TextSecondary, FontSize: 12})
		ui.Text(c, value).FontSize(22).FontWeight(700).TextColor(tone)
		display.Text(c, hint, display.TextProps{Type: display.TextSecondary, FontSize: 11})
	})
}

func toolbar(c *ui.Context, a *app.App, st *State, snap app.Snapshot, tk *theme.Tokens) {
	rows := snap.Rows
	healthy := countRows(rows, healthyFilter)
	needsWork := countRows(rows, needsWorkFilter)
	failed := countRows(rows, failedFilter)
	labels := []string{
		fmt.Sprintf("全部 %d", len(rows)),
		fmt.Sprintf("健康 %d", healthy),
		fmt.Sprintf("需改善 %d", needsWork),
		fmt.Sprintf("不可达 %d", failed),
	}
	filter := st.Filter
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		action.Segmented(c, &filter, labels, action.SegmentedProps{Size: theme.Small, Variant: action.SegmentedFilled})
		st.Filter = filter

		ui.Column(c).Width(250).AlignItems(ui.Stretch).Children(func() {
			field.SearchBar(c, &st.Query, field.SearchBarProps{
				Variant: field.InputOutlined, Size: theme.Small,
				Placeholder: "搜索节点 / 域名 / IP / 地区",
			})
		})
		ui.Spacer(c)
		if snap.Status == app.StatusProbing {
			action.Button(c, "停止", action.ButtonProps{Variant: action.Default, Size: theme.Small}).OnClick(a.Stop)
		}
		reprobe := action.Button(c, "重新检测", action.ButtonProps{Variant: action.Default, Size: theme.Small})
		reprobe.OnClick(a.Reprobe)
		overlay.Tooltip(c, reprobe, "沿用当前订阅，重新探测所有节点", overlay.TooltipProps{Placement: overlay.Top})
	})
}

func footer(c *ui.Context, snap app.Snapshot, tk *theme.Tokens) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		display.Text(c, "IP 归属与风险标记来自 ip-api.com；其余参数取自订阅原文", display.TextProps{Type: display.TextSecondary, FontSize: 11})
		ui.Spacer(c)
		if len(snap.Rows) > 0 {
			display.Text(c, fmt.Sprintf("%d 个节点 · 点击行查看全部参数", len(snap.Rows)), display.TextProps{Type: display.TextSecondary, FontSize: 11})
		}
	})
}

func formatDelay(d time.Duration) string {
	if d <= 0 {
		return "—"
	}
	return fmt.Sprintf("%d ms", d.Milliseconds())
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "—"
	}
	if d < time.Second {
		return fmt.Sprintf("%d ms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1f s", d.Seconds())
}

// formatElapsed never hides a zero wall time: an instant fake scan still took
// less than a millisecond, not "unknown".
func formatElapsed(d time.Duration) string {
	if d < time.Millisecond {
		return "<1 ms"
	}
	return formatDuration(d)
}
