package main

import "strings"

// version is the application version. Release builds override it with
//
//	wails build -ldflags "-linkmode=internal -X main.version=1.2.3"
//
// Keep the default in sync with wails.json so development builds and packaged
// executables report the same thing.
var version = "0.1.0"

func appVersion() string {
	if value := strings.TrimSpace(version); value != "" {
		return value
	}
	return "dev"
}

// GetVersion returns the running build version for the UI and future updater.
func (a *App) GetVersion() string {
	return appVersion()
}
