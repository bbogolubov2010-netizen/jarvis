package main

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// TestFrontendDeclaresEveryBoundMethod guards the hand-written TypeScript
// declaration in App.vue. frontend/wailsjs is generated and gitignored, so a
// forgotten signature compiles fine but the UI cannot call the method at
// runtime. This test catches that drift without requiring the Wails CLI.
func TestFrontendDeclaresEveryBoundMethod(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("frontend", "src", "App.vue"))
	if err != nil {
		t.Fatal(err)
	}
	declared := declaredFrontendMethods(string(data))
	if len(declared) == 0 {
		t.Fatal("could not find the Window.go.main.App declaration in App.vue")
	}

	appType := reflect.TypeOf(&App{})
	for i := 0; i < appType.NumMethod(); i++ {
		name := appType.Method(i).Name
		if !declared[name] {
			t.Errorf("App.%s is exported but missing from Window.go.main.App in App.vue", name)
		}
	}
}

func declaredFrontendMethods(source string) map[string]bool {
	windowIndex := strings.Index(source, "interface Window")
	if windowIndex < 0 {
		return nil
	}
	appIndex := strings.Index(source[windowIndex:], "App: {")
	if appIndex < 0 {
		return nil
	}
	appIndex += windowIndex

	openIndex := appIndex + strings.Index(source[appIndex:], "{")
	if openIndex < appIndex {
		return nil
	}
	depth := 0
	endIndex := -1
	for i := openIndex; i < len(source); i++ {
		switch source[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				endIndex = i
			}
		}
		if endIndex >= 0 {
			break
		}
	}
	if endIndex < 0 {
		return nil
	}

	methodPattern := regexp.MustCompile(`(?m)^\s+([A-Z]\w*)\s*\(`)
	methods := make(map[string]bool)
	for _, match := range methodPattern.FindAllStringSubmatch(source[openIndex:endIndex], -1) {
		methods[match[1]] = true
	}
	return methods
}
