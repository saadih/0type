// Command 0type (GUI) is the windowed build: a Wails settings window backed by
// the same dictation engine as the console cmd/0type. It runs in the system
// tray, so closing the window hides it instead of quitting.
package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()
	err := wails.Run(&options.App{
		Title:            "0type",
		Width:            480,
		Height:           720,
		MinWidth:         420,
		MinHeight:        560,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 18, G: 18, B: 22, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		OnBeforeClose:    app.beforeClose,
		Bind:             []interface{}{app},
		// One running copy only. Two instances would each install the global
		// hook and both paste the same dictation, so a launch while one is
		// already in the tray just surfaces the existing window.
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               "0type.saadih.github.io",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) { app.trayOpen() },
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
