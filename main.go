package main

import (
	"embed"
	"io/fs"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend
var assets embed.FS

var version = "dev"

func main() {
	sub, _ := fs.Sub(assets, "frontend")
	app := NewApp()
	cfg := app.c.Cfg.UI
	err := wails.Run(&options.App{
		Title:            "MiniMonitor",
		Width:            cfg.WinW,
		Height:           cfg.WinH,
		MinWidth:         120,
		MinHeight:        40,
		Frameless:        true,
		AlwaysOnTop:      cfg.OnTop,
		DisableResize:    false,
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 0},
		AssetServer:      &assetserver.Options{Assets: sub},
		OnStartup:        app.startup,
		OnDomReady:       app.domReady,
		OnBeforeClose:    app.beforeClose,
		OnShutdown:       app.shutdown,
		Bind:             []interface{}{app},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "minimon-6e1f3a52-singleton",
			OnSecondInstanceLaunch: func(_ options.SecondInstanceData) {
				if app.ctx != nil {
					runtime.WindowUnminimise(app.ctx)
					runtime.WindowShow(app.ctx)
				}
			},
		},
		Windows: &windows.Options{
			WebviewIsTransparent: true,
			WindowIsTranslucent:  false,
			DisableWindowIcon:    false,
			Theme:                windows.Dark,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
