// Command ipcsend delivers one authenticated command to a running JARVIS
// instance over the loopback IPC channel. It is a diagnostic helper; the
// endpoint token is protected with DPAPI and readable only by this Windows
// user.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"Jarvis/internal/ipc"
)

func main() {
	command := flag.String("command", "", "command text to deliver")
	configDir := flag.String("config", "", "JARVIS config directory (default %AppData%\\Jarvis)")
	timeout := flag.Duration("timeout", 20*time.Second, "how long to wait for the app endpoint")
	flag.Parse()

	if *command == "" {
		fmt.Fprintln(os.Stderr, "ipcsend: -command is required")
		os.Exit(2)
	}
	dir := *configDir
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "ipcsend:", err)
			os.Exit(1)
		}
		dir = filepath.Join(base, "Jarvis")
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if err := ipc.SendCommand(ctx, dir, *command); err != nil {
		fmt.Fprintln(os.Stderr, "ipcsend:", err)
		os.Exit(1)
	}
	fmt.Println("command delivered")
}
