//go:build darwin

package main

import (
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:                    "DevBox",
		Width:                    1100,
		Height:                   760,
		MinWidth:                 1050,
		MinHeight:                700,
		EnableDefaultContextMenu: false,
		StartHidden:              startHidden(),
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 17, G: 24, B: 39, A: 1},
		OnStartup:        app.startup,
		OnBeforeClose:    app.beforeClose,
		OnShutdown:       app.shutdown,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               "dev.harungecit.devbox",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) { app.showWindow() },
		},
		Bind: []interface{}{
			app,
		},
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
			About: &mac.AboutInfo{
				Title:   "DevBox",
				Message: "Development Environment Manager",
			},
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
