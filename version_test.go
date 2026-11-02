package main

import (
	"encoding/json"
	"os"
	"testing"
)

// TestVersionMatchesWailsConfig keeps the build-time default in version.go and
// the packaged metadata in wails.json from drifting apart.
func TestVersionMatchesWailsConfig(t *testing.T) {
	data, err := os.ReadFile("wails.json")
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Info struct {
			ProductVersion string `json:"productVersion"`
		} `json:"info"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if config.Info.ProductVersion != version {
		t.Fatalf("wails.json productVersion = %q, version.go = %q", config.Info.ProductVersion, version)
	}
}
