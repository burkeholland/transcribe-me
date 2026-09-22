package main

import (
	"embed"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	root, data, pathErr := appPaths()
	app := NewApp(root, data)
	app.initErr = pathErr
	if err := wails.Run(&options.App{
		Title: "Transcribe Me", Width: 1366, Height: 824, MinWidth: 900, MinHeight: 620,
		Frameless:        true,
		BackgroundColour: &options.RGBA{R: 251, G: 251, B: 253, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets, Handler: app.mediaHandler()},
		OnStartup:        app.startup, OnShutdown: app.shutdown, OnBeforeClose: app.beforeClose,
		Bind:        []interface{}{app},
		DragAndDrop: &options.DragAndDrop{EnableFileDrop: true, DisableWebViewDrop: true},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			DisableWindowIcon:    false,
			Theme:                windows.SystemDefault,
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "534ac6cc-58b7-4220-bde8-e879fd42f846",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) {
				if app.ctx != nil {
					runtime.WindowUnminimise(app.ctx)
					runtime.WindowShow(app.ctx)
				}
			},
		},
	}); err != nil {
		println("Transcribe Me could not start:", err.Error())
		os.Exit(1)
	}
}
