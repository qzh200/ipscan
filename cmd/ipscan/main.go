// Command ipscan opens the node quality inspector.
package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"ipscan/internal/app"
	"ipscan/internal/probe"
	"ipscan/internal/scan"
	"ipscan/internal/screen"
	"ipscan/internal/subscription"
)

func newScanner() *scan.Scanner {
	s := scan.New()
	s.Geo = probe.NewGeoChain()
	s.Workers = 8
	return s
}

func main() {
	controller := app.New(subscription.NewFetcher(), newScanner())
	view := &screen.State{}

	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:           "节点体检",
			Width:           1280,
			Height:          840,
			MinWidth:        1180,
			MinHeight:       640,
			BackgroundColor: "light-dark(#f2f3f5, #17181c)",
			Content:         ui.View(func(c *ui.Context) { screen.View(c, controller, view) }),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
