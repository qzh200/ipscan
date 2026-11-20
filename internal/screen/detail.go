package screen

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"

	"ipscan/internal/app"
)

func detailPage(c *ui.Context, a *app.App, snap app.Snapshot, t *ui.Theme) {
	index := snap.Selected
	if index < 0 || index >= len(snap.Rows) {
		return
	}
	row := snap.Rows[index]
	windowWidth, _ := c.Size()

	components.ScrollView(c, components.ScrollViewOptions{Axes: components.ScrollVertical}).
		Grow(1).Basis(0).Children(func() {
		ui.Column(c).Width(max(640, windowWidth-58)).Gap(14).Children(func() {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				back := components.Button(c, "", components.ButtonGhost)
				back.Label("返回列表")
				back.OnClick(func() { a.Select(-1) })
				back.Children(func() {
					ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
						components.Icon(c, lucide.ArrowLeft).Size(14, 14)
						components.Text(c, "返回列表")
					})
				})
				ui.Spacer(c)
				if row.Node.Raw != "" {
					components.Button(c, "复制节点链接", components.ButtonGhost).
						OnClick(func() { c.WriteClipboard(row.Node.Raw) })
				}
			})

			ui.Column(c).FillWidth().Gap(7).Children(func() {
				components.Text(c, row.Node.Name).FontSize(23).Bold().Wrap()
				ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
					components.Badge(c, row.Node.ProtocolLabel(), components.BadgeOptions{Tone: components.BadgeAccent})
					components.Badge(c, row.Node.TransportLabel(), components.BadgeOptions{Tone: components.BadgeNeutral})
					if row.Probed && row.Result.Score > 0 {
						tone := components.BadgeSuccess
						switch {
						case row.Result.Score < 50:
							tone = components.BadgeDanger
						case row.Result.Score < 70:
							tone = components.BadgeWarning
						}
						components.Badge(c, fmt.Sprintf("%s · %d 分", row.Result.Grade, row.Result.Score),
							components.BadgeOptions{Tone: tone})
					}
					ui.Spacer(c)
					components.Text(c, row.Node.Address()).FontSize(12).TextColor(t.TextMuted).Shrink(1).MinWidth(0).SingleLine()
				})
			})

			globalCard(c, a, row, index, t)
			if row.Result.Err != "" {
				banner(c, t, row.Result.Err)
			}

			ui.Row(c).FillWidth().Gap(14).AlignItems(ui.Start).Children(func() {
				qualityCard(c, row, t)
				geoCard(c, row, t)
			})
			if row.Result.TLS != nil {
				tlsCard(c, row, t)
			}
			paramsCard(c, row, t)
		})
	})
}

// cardBox builds a surface card. flex cards share a row's width; fixed cards
// keep a width; the rest fill the column they sit in.
func cardBox(c *ui.Context, t *ui.Theme, title string, width float32, flex bool, build func()) {
	el := ui.Column(c).Padding(16).Gap(9).
		Background(t.Surface).Border(1, t.Border).Radius(t.Space(3))
	switch {
	case flex:
		el.Grow(1).Basis(0)
	case width > 0:
		el.Width(width)
	default:
		el.FillWidth()
	}
	el.Children(func() {
		components.Text(c, title).FontSize(13).Bold()
		ui.Box(c).Height(1).Background(t.Border.Alpha(.7))
		build()
	})
}

func kv(c *ui.Context, t *ui.Theme, key, value string) {
	if value == "" {
		value = "—"
	}
	ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
		components.Text(c, key).FontSize(11.5).TextColor(t.TextMuted).Width(98).Shrink(0)
		components.Text(c, value).FontSize(12.5).Grow(1).MinWidth(0).Wrap()
	})
}

func banner(c *ui.Context, t *ui.Theme, message string) {
	ui.Row(c).FillWidth().Padding(10, 14).Gap(8).AlignItems(ui.Center).
		Background(t.Warning.Alpha(.09)).Border(1, t.Warning.Alpha(.25)).Radius(t.Radius).
		Children(func() {
			components.Icon(c, lucide.TriangleAlert).Size(15, 15).TextColor(t.Warning)
			components.Text(c, message).FontSize(12).TextColor(t.Warning).Wrap().Grow(1)
		})
}

func qualityCard(c *ui.Context, row app.Row, t *ui.Theme) {
	cardBox(c, t, "连接质量", 0, true, func() {
		kv(c, t, "可达性", reachLabel(row))
		kv(c, t, "解析 IP", row.Result.TargetLabel())
		kv(c, t, "DNS 耗时", formatDelay(row.Result.DNS))
		kv(c, t, "TCP 延迟", fmt.Sprintf("min %s · avg %s · max %s",
			formatDelay(row.Result.Min), formatDelay(row.Result.Avg), formatDelay(row.Result.Max)))
		kv(c, t, "抖动", formatDelay(row.Result.Jitter))
		kv(c, t, "丢包", lossText(row))
		kv(c, t, "采样", fmt.Sprintf("%d/%d 次握手成功", row.Result.Succeeded, row.Result.Samples))
	})
}

func geoCard(c *ui.Context, row app.Row, t *ui.Theme) {
	cardBox(c, t, "IP 归属", 340, false, func() {
		geo := row.Result.Geo
		if geo == nil {
			kv(c, t, "归属", "未查询到（离线或查询失败）")
			return
		}
		place := geo.Country
		if geo.Region != "" && geo.Region != geo.City {
			place = joinSpace(place, geo.Region)
		}
		kv(c, t, "位置", joinSpace(place, geo.City))
		kv(c, t, "运营商", geo.ISP)
		kv(c, t, "ASN", geo.ASN)
		kv(c, t, "IP 类型", row.Result.IPTypeLabel())
	})
}

func tlsCard(c *ui.Context, row app.Row, t *ui.Theme) {
	info := row.Result.TLS
	cardBox(c, t, "TLS 证书", 0, false, func() {
		kv(c, t, "握手耗时", formatDelay(row.Result.TLSMs))
		kv(c, t, "TLS 版本", joinDot(info.Version, info.Cipher))
		kv(c, t, "证书 CN", info.CommonName)
		kv(c, t, "签发者", info.Issuer)
		if !info.NotAfter.IsZero() {
			kv(c, t, "证书到期", fmt.Sprintf("%s（剩 %d 天）", info.NotAfter.Format("2006-01-02"), daysLeft(info.NotAfter)))
		}
		if info.Error != "" {
			kv(c, t, "握手错误", info.Error)
		} else {
			verified := "未校验"
			if info.Verified {
				verified = "通过"
			}
			kv(c, t, "主机名校验", verified)
		}
	})
}

func paramsCard(c *ui.Context, row app.Row, t *ui.Theme) {
	cardBox(c, t, "节点参数", 0, false, func() {
		ui.Column(c).Gap(6).Children(func() {
			for _, p := range row.Node.Params() {
				kv(c, t, p.Key, p.Value)
			}
		})
	})
}

func globalCard(c *ui.Context, a *app.App, row app.Row, index int, t *ui.Theme) {
	idle := a.Remote != nil && !row.RemoteBusy && row.RemoteErr == "" && row.Remote == nil
	if idle {
		ui.Row(c).FillWidth().Padding(14, 16).Gap(10).AlignItems(ui.Center).
			Background(t.Surface).Border(1, t.Border).Radius(t.Space(3)).Children(func() {
			components.Icon(c, lucide.Globe).Size(16, 16).TextColor(t.Accent)
			components.Text(c, "全球网络测试").FontSize(13).Bold()
			components.Text(c, "从全球 8 个探测点 ping 该地址，约 5–15 秒。").
				FontSize(11.5).TextColor(t.TextMuted).Grow(1).SingleLine()
			components.Button(c, "全球 Ping（8 个探测点）", components.ButtonPrimary).
				OnClick(func() { a.CheckGlobal(index) })
		})
		return
	}

	ui.Column(c).FillWidth().Padding(16).Gap(10).
		Background(t.Surface).Border(1, t.Border).Radius(t.Space(3)).Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			components.Icon(c, lucide.Globe).Size(16, 16).TextColor(t.Accent)
			components.Text(c, "全球网络测试").FontSize(13).Bold()
			ui.Spacer(c)
			components.Text(c, "Globalping · 8 个探测点").FontSize(11).TextColor(t.TextMuted)
		})
		switch {
		case a.Remote == nil:
			components.Text(c, "未配置远程探测服务").FontSize(12).TextColor(t.TextMuted)
		case row.RemoteBusy:
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				components.Spinner(c, components.SpinnerRing).Size(16, 16)
				components.Text(c, "正在向全球探测点发起 ping…").FontSize(12).TextColor(t.TextMuted)
			})
		case row.RemoteErr != "":
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				components.Icon(c, lucide.TriangleAlert).Size(15, 15).TextColor(t.Warning)
				components.Text(c, row.RemoteErr).FontSize(12).TextColor(t.Warning).Wrap().Grow(1)
				components.Button(c, "重试", components.ButtonGhost).OnClick(func() { a.CheckGlobal(index) })
			})
		case row.Remote != nil:
			rep := row.Remote
			components.Text(c, rep.Summary()).FontSize(13).Bold()
			components.Text(c, fmt.Sprintf("via %s · %s", rep.Provider, rep.At.Format("15:04:05"))).
				FontSize(11).TextColor(t.TextMuted)
			limit := len(rep.Probes)
			if limit > 12 {
				limit = 12
			}
			ui.Column(c).Gap(0).Children(func() {
				for _, p := range rep.Probes[:limit] {
					probeRow(c, t, p.LocationLabel(), p.LatencyLabel(), p.LossLabel(), p.Loss > 0)
				}
			})
			if len(rep.Probes) > limit {
				components.Text(c, fmt.Sprintf("另有 %d 个探测点…", len(rep.Probes)-limit)).
					FontSize(11).TextColor(t.TextMuted)
			}
			components.Button(c, "重新测试", components.ButtonGhost).AlignSelf(ui.Start).OnClick(func() { a.CheckGlobal(index) })
		}
	})
}

func probeRow(c *ui.Context, t *ui.Theme, location, latency, loss string, warn bool) {
	ui.Row(c).Height(24).Gap(10).AlignItems(ui.Center).Children(func() {
		components.Text(c, location).FontSize(11.5).TextColor(t.TextMuted).Grow(1).MinWidth(0).SingleLine()
		components.Text(c, latency).FontSize(11.5).Width(72).Shrink(0)
		tone := components.BadgeNeutral
		if warn {
			tone = components.BadgeWarning
		}
		components.Badge(c, loss, components.BadgeOptions{Tone: tone})
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

func daysLeft(at time.Time) int {
	d := time.Until(at)
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
