package main

import (
	"embed"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	winapi "golang.org/x/sys/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	startHidden := false
	voiceAgent := false
	voiceSupervisor := false
	voiceWake := false
	args := os.Args[1:]
	for _, arg := range args {
		switch arg {
		case "--hidden":
			startHidden = true
		case "--voice-agent":
			voiceAgent = true
		case "--voice-agent-supervisor":
			voiceSupervisor = true
		case "--voice-wake":
			voiceWake = true
		case "--voice-command-b64":
			// Removed: commands are delivered over authenticated IPC so they
			// never appear in a process command line. Ignore the legacy flag.
		}
	}

	if voiceSupervisor {
		runVoiceAgentSupervisor()
		return
	}
	if voiceAgent {
		runVoiceAgent()
		return
	}

	app := NewApp(startHidden)
	app.pendingVoiceWake = voiceWake

	err := wails.Run(&options.App{
		Title:             "JARVIS - AI Assistant",
		Width:             1280,
		Height:            800,
		MinWidth:          900,
		MinHeight:         600,
		MaxWidth:          1920,
		MaxHeight:         1200,
		DisableResize:     false,
		Frameless:         false,
		StartHidden:       startHidden,
		HideWindowOnClose: false,
		BackgroundColour:  &options.RGBA{R: 10, G: 14, B: 26, A: 255},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			BackdropType:         windows.Mica,
			DisableWindowIcon:    false,
		},
		Linux:      &linux.Options{},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "jarvis-desktop-assistant-v1",
			OnSecondInstanceLaunch: func(data options.SecondInstanceData) {
				app.handleSecondInstance(data.Args)
			},
		},
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		message := "JARVIS could not start:\n\n" + err.Error()
		fmt.Fprintln(os.Stderr, message)
		_, _ = winapi.MessageBox(
			0,
			winapi.StringToUTF16Ptr(message),
			winapi.StringToUTF16Ptr("JARVIS startup error"),
			0x00000010, // MB_ICONERROR
		)
		os.Exit(1)
	}
}
