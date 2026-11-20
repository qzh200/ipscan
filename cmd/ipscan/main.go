// Command ipscan opens the node quality inspector.
package main

import (
	_ "embed"
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"ipscan/internal/app"
	"ipscan/internal/probe"
	"ipscan/internal/remote"
	"ipscan/internal/scan"
	"ipscan/internal/screen"
	"ipscan/internal/subscription"
)

// iconPNG is the window and taskbar icon: a white pulse on the app's terracotta.
//
//go:embed icon.png
var iconPNG []byte

func newScanner() *scan.Scanner {
	s := scan.New()
	s.Geo = probe.NewGeoChain()
	s.Workers = 8
	return s
}

func main() {
	controller := app.New(subscription.NewFetcher(), newScanner())
	controller.Remote = remote.NewGlobalping()
	view := screen.NewState()

	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:           "订阅质检 · ipscan",
			Width:           1280,
			Height:          840,
			MinWidth:        1180,
			MinHeight:       640,
			BackgroundColor: "light-dark(#fcfbf9, #211f1c)",
			Content:         ui.View(func(c *ui.Context) { screen.View(c, controller, view) }),
		})
		if err := win.SetIcon(iconPNG); err != nil {
			log.Printf("设置窗口图标失败: %v", err)
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
