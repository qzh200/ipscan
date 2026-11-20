package screen

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/lobehub/lobe-mygo/pkg/action"
	"github.com/lobehub/lobe-mygo/pkg/display"
	"github.com/lobehub/lobe-mygo/pkg/feedback"
	"github.com/lobehub/lobe-mygo/pkg/theme"

	"ipscan/internal/app"
)

func detailDrawer(c *ui.Context, a *app.App, snap app.Snapshot, tk *theme.Tokens) {
	row, ok := a.SelectedRow()
	if !ok {
		return
	}
	open := true
	feedback.Drawer(c, &open, feedback.DrawerProps{
		Placement: feedback.Right,
		Title:     row.Node.Name,
		Size:      480,
	}, func() {
		detailBody(c, a, row, snap.Selected, tk)
	})
	if !open {
		a.Select(-1)
	}
}

func detailBody(c *ui.Context, a *app.App, row app.Row, index int, tk *theme.Tokens) {
	ui.Column(c).Gap(16).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			display.Tag(c, row.Node.ProtocolLabel(), display.TagProps{Variant: display.TagFilled, Color: display.TagBlue})
			display.Tag(c, row.Node.TransportLabel(), display.TagProps{Variant: display.TagOutlined, Color: display.TagGray})
			if row.Probed && row.Result.Score > 0 {
				display.Tag(c, fmt.Sprintf("%s · %d 分", row.Result.Grade, row.Result.Score), display.TagProps{
					Variant: display.TagFilled, Color: scoreTagColor(row.Result),
				})
			}
			ui.Spacer(c)
			display.Text(c, row.Node.Address(), display.TextProps{Code: true, FontSize: 12, Type: display.TextSecondary})
		})

		if row.Result.Err != "" {
			feedback.Alert(c, "探测结果", feedback.AlertProps{
				Type: feedback.Warning, Variant: feedback.Soft, Description: row.Result.Err,
			})
		}

		remoteSection(c, a, row, index, tk)

		sectionTitle(c, "连接质量", tk)
		kv(c, tk, "可达性", reachLabel(row))
		kv(c, tk, "解析 IP", row.Result.TargetLabel())
		kv(c, tk, "DNS 耗时", formatDelay(row.Result.DNS))
		kv(c, tk, "TCP 延迟", fmt.Sprintf("min %s · avg %s · max %s",
			formatDelay(row.Result.Min), formatDelay(row.Result.Avg), formatDelay(row.Result.Max)))
		kv(c, tk, "抖动", formatDelay(row.Result.Jitter))
		kv(c, tk, "丢包", lossText(row))
		kv(c, tk, "采样", fmt.Sprintf("%d/%d 次握手成功", row.Result.Succeeded, row.Result.Samples))
		if row.Result.TLS != nil {
			tlsRows(c, tk, row)
		}

		sectionTitle(c, "IP 归属", tk)
		geo := row.Result.Geo
		if geo == nil {
			kv(c, tk, "归属", "未查询到（离线或查询失败）")
		} else {
			place := geo.Country
			if geo.Region != "" && geo.Region != geo.City {
				place = joinSpace(place, geo.Region)
			}
			kv(c, tk, "位置", joinSpace(place, geo.City))
			kv(c, tk, "运营商", geo.ISP)
			kv(c, tk, "ASN", geo.ASN)
			kv(c, tk, "IP 类型", row.Result.IPTypeLabel())
		}

		sectionTitle(c, "节点参数", tk)
		for _, p := range row.Node.Params() {
			kv(c, tk, p.Key, p.Value)
		}

		ui.Row(c).Gap(8).PaddingY(4).Children(func() {
			if row.Node.Raw != "" {
				action.Button(c, "复制节点链接", action.ButtonProps{Variant: action.Default}).
					OnClick(func() { c.WriteClipboard(row.Node.Raw) })
			}
			ui.Spacer(c)
			action.Button(c, "关闭", action.ButtonProps{Variant: action.Primary}).
				OnClick(func() { a.Select(-1) })
		})
	})
}

func tlsRows(c *ui.Context, tk *theme.Tokens, row app.Row) {
	t := row.Result.TLS
	kv(c, tk, "TLS 握手", formatDelay(row.Result.TLSMs))
	kv(c, tk, "TLS 版本", joinDot(t.Version, t.Cipher))
	kv(c, tk, "证书 CN", t.CommonName)
	kv(c, tk, "签发者", t.Issuer)
	if !t.NotAfter.IsZero() {
		kv(c, tk, "证书到期", fmt.Sprintf("%s（剩 %d 天）", t.NotAfter.Format("2006-01-02"), daysLeft(t.NotAfter)))
	}
	if t.Error != "" {
		kv(c, tk, "握手错误", t.Error)
	} else {
		verified := "未校验"
		if t.Verified {
			verified = "通过"
		}
		kv(c, tk, "主机名校验", verified)
	}
}

func sectionTitle(c *ui.Context, title string, tk *theme.Tokens) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, title).FontSize(13).FontWeight(600).TextColor(tk.Text)
		ui.Box(c).Grow(1).Height(1).Background(tk.FillQuaternary)
	})
}

// remoteSection runs and renders the optional worldwide ping.
func remoteSection(c *ui.Context, a *app.App, row app.Row, index int, tk *theme.Tokens) {
	sectionTitle(c, "全球网络测试", tk)
	if a.Remote == nil {
		kv(c, tk, "状态", "未配置远程探测服务")
		return
	}
	switch {
	case row.RemoteBusy:
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			feedback.Spin(c, feedback.SpinProps{Size: theme.Small}, nil)
			display.Text(c, "正在向全球探测点发起 ping…", display.TextProps{Type: display.TextSecondary, FontSize: 12})
		})
	case row.RemoteErr != "":
		feedback.Alert(c, "Globalping 测量失败", feedback.AlertProps{
			Type: feedback.Warning, Variant: feedback.Soft, Description: row.RemoteErr,
		})
		action.Button(c, "重试", action.ButtonProps{Variant: action.Default, Size: theme.Small}).
			OnClick(func() { a.CheckGlobal(index) })
	case row.Remote != nil:
		rep := row.Remote
		display.Text(c, rep.Summary(), display.TextProps{FontSize: 12, Strong: true})
		display.Text(c, fmt.Sprintf("via %s · %s", rep.Provider, rep.At.Format("15:04:05")),
			display.TextProps{Type: display.TextSecondary, FontSize: 11})
		limit := len(rep.Probes)
		if limit > 12 {
			limit = 12
		}
		for _, p := range rep.Probes[:limit] {
			ui.Row(c).Height(22).Gap(8).AlignItems(ui.Center).Children(func() {
				display.Text(c, p.LocationLabel(), display.TextProps{Type: display.TextSecondary, FontSize: 11}).
					Grow(1).MinWidth(0).SingleLine()
				display.Text(c, p.LatencyLabel(), display.TextProps{FontSize: 11, Code: true}).
					Width(64).Shrink(0).SingleLine()
				display.Text(c, p.LossLabel(), display.TextProps{Type: display.TextSecondary, FontSize: 11}).
					Width(36).Shrink(0)
			})
		}
		if len(rep.Probes) > limit {
			display.Text(c, fmt.Sprintf("另有 %d 个探测点…", len(rep.Probes)-limit),
				display.TextProps{Type: display.TextSecondary, FontSize: 11})
		}
		action.Button(c, "重新测试", action.ButtonProps{Variant: action.Text, Size: theme.Small}).
			OnClick(func() { a.CheckGlobal(index) })
	default:
		action.Button(c, "全球 Ping（8 个探测点）", action.ButtonProps{Variant: action.Primary}).
			OnClick(func() { a.CheckGlobal(index) })
		display.Text(c, "通过 Globalping 从全球 8 个探测点 ping 该地址，约 5–15 秒。目标地址会发送给 api.globalping.io。",
			display.TextProps{Type: display.TextSecondary, FontSize: 11})
	}
}

func kv(c *ui.Context, tk *theme.Tokens, key, value string) {
	if value == "" {
		value = "—"
	}
	ui.Row(c).Gap(12).AlignItems(ui.Start).PaddingY(1).Children(func() {
		display.Text(c, key, display.TextProps{Type: display.TextSecondary, FontSize: 12}).
			Width(104).Shrink(0)
		display.Text(c, value, display.TextProps{FontSize: 12}).Grow(1).MinWidth(0)
	})
}

func reachLabel(row app.Row) string {
	switch {
	case !row.Probed:
		return "待检测"
	case row.Result.UDPOnly:
		return "QUIC/UDP 未做握手"
	case row.Result.Reachable:
		return "TCP 可达"
	default:
		return "不可达"
	}
}

func daysLeft(t time.Time) int {
	d := time.Until(t)
	if d < 0 {
		return 0
	}
	return int(d.Hours() / 24)
}

func joinSpace(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + " " + b
}

func joinDot(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + " · " + b
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
