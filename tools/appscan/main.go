// Command appscan refreshes and inspects the installed-application catalog.
//
// Usage:
//
//	go run ./tools/appscan -refresh
//	go run ./tools/appscan -find chrome
//	go run ./tools/appscan -list 40
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"Jarvis/internal/appscan"
)

func main() {
	refresh := flag.Bool("refresh", false, "rescan the machine before printing")
	find := flag.String("find", "", "resolve an application name")
	list := flag.Int("list", 0, "print the first N applications")
	flag.Parse()

	if *refresh {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		catalog, err := appscan.Refresh(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, "appscan:", err)
			os.Exit(1)
		}
		fmt.Printf("scanned %d applications\n", len(catalog.Apps))
	}

	catalog := appscan.Cached()
	if !*refresh {
		fmt.Printf("%d applications, scanned at %s\n", len(catalog.Apps), catalog.ScannedAt.Format(time.RFC3339))
	}

	if *find != "" {
		if app, ok := appscan.Match(catalog.Apps, *find); ok {
			fmt.Printf("match: %s\n  path: %s\n  aumid: %s\n  source: %s\n", app.Name, app.Path, app.AUMID, app.Source)
		} else {
			fmt.Printf("no match for %q\n", *find)
			for _, suggestion := range appscan.Suggestions(catalog.Apps, *find, 5) {
				fmt.Printf("  did you mean: %s\n", suggestion)
			}
		}
	}

	if *list > 0 {
		for i, app := range catalog.Apps {
			if i >= *list {
				break
			}
			fmt.Printf("%3d. %s\n", i+1, app.Name)
		}
	}
}
