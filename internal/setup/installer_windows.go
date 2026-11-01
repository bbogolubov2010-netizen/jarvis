//go:build windows

package setup

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"Jarvis/internal/netguard"
)

const (
	// OllamaInstallerURL is the official Windows installer. It redirects to
	// the current GitHub release asset and is verified before execution.
	OllamaInstallerURL = "https://ollama.com/download/OllamaSetup.exe"

	// The installer is currently about 1.6 GB; keep a generous hard limit.
	maxInstallerBytes      = int64(3 << 30)
	expectedInstallerBytes = int64(1_700_000_000)

	// Ollama's download endpoint redirects through GitHub. Keep the same
	// limit as netguard and require HTTPS at every hop.
	maxInstallerRedirects = 5
)

// InstallProgress is sent to the UI while Ollama is being installed.
type InstallProgress struct {
	Stage      string `json:"stage"`
	Message    string `json:"message"`
	Percent    int    `json:"percent"`
	Detail     string `json:"detail,omitempty"`
	BytesDone  int64  `json:"bytes_done,omitempty"`
	BytesTotal int64  `json:"bytes_total,omitempty"`
}

// InstallOllama installs Ollama silently. It uses winget first because winget
// verifies the package hash, then falls back to the official signed installer.
func InstallOllama(ctx context.Context, progress func(InstallProgress)) error {
	if OllamaInstalled() {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	emitInstallProgress(progress, InstallProgress{
		Stage:   "install",
		Message: "Installing Ollama...",
		Percent: 2,
		Detail:  "Using Windows Package Manager",
	})

	var wingetErr error
	if winget, err := exec.LookPath("winget"); err == nil {
		args := []string{
			"install",
			"--id", "Ollama.Ollama",
			"--exact",
			"--silent",
			"--accept-package-agreements",
			"--accept-source-agreements",
			"--disable-interactivity",
		}
		cmd := hiddenCommand(ctx, winget, args...)
		stopHeartbeat := make(chan struct{})
		var heartbeat sync.WaitGroup
		heartbeat.Add(1)
		go func() {
			defer heartbeat.Done()
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			percent := 5
			for {
				select {
				case <-stopHeartbeat:
					return
				case <-ctx.Done():
					return
				case <-ticker.C:
					if percent < 85 {
						percent += 2
					}
					emitInstallProgress(progress, InstallProgress{
						Stage:   "install",
						Message: "Installing Ollama...",
						Percent: percent,
						Detail:  "Windows Package Manager",
					})
				}
			}
		}()
		wingetErr = cmd.Run()
		close(stopHeartbeat)
		heartbeat.Wait()
		if wingetErr == nil {
			if waitForInstalled(ctx, 15*time.Second) {
				return nil
			}
			wingetErr = fmt.Errorf("winget finished but Ollama was not found")
		}
	} else {
		wingetErr = fmt.Errorf("winget is not available")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	emitInstallProgress(progress, InstallProgress{
		Stage:   "install",
		Message: "Downloading the official Ollama installer...",
		Percent: 3,
		Detail:  "Windows Package Manager was unavailable or failed",
	})
	if err := downloadAndInstall(ctx, progress); err != nil {
		return fmt.Errorf("automatic installation failed (winget: %v): %w", wingetErr, err)
	}
	return nil
}

func downloadAndInstall(ctx context.Context, progress func(InstallProgress)) error {
	installerPath, err := downloadInstaller(ctx, progress)
	if err != nil {
		return err
	}
	defer os.Remove(installerPath)

	emitInstallProgress(progress, InstallProgress{
		Stage:   "verify",
		Message: "Verifying the installer signature...",
		Percent: 92,
	})
	if err := verifyInstallerSignature(ctx, installerPath); err != nil {
		return err
	}

	emitInstallProgress(progress, InstallProgress{
		Stage:   "install",
		Message: "Installing Ollama...",
		Percent: 94,
		Detail:  "This can take a minute",
	})
	cmd := hiddenCommand(ctx, installerPath,
		"/VERYSILENT",
		"/SUPPRESSMSGBOXES",
		"/NORESTART",
		"/SP-",
	)
	installErr := cmd.Run()
	if installErr != nil {
		if exitError, ok := installErr.(*exec.ExitError); ok {
			switch exitError.ExitCode() {
			case 0, 1641, 3010:
				installErr = nil
			}
		}
	}
	if installErr != nil {
		return fmt.Errorf("installer failed: %w", installErr)
	}
	if !waitForInstalled(ctx, 20*time.Second) {
		return fmt.Errorf("installer finished but ollama.exe was not found")
	}
	return nil
}

// newInstallerClient returns a no-deadline client that still refuses
// non-public destinations. The installer is large and must be allowed to take
// its time, but every hop has to be a public HTTPS address.
func newInstallerClient() *http.Client {
	client := netguard.NewClient(0)
	client.CheckRedirect = installerRedirect
	return client
}

// installerRedirect enforces the installer-specific redirect policy. It is
// stricter than netguard's default because the Ollama download endpoint must
// never downgrade to plain HTTP.
func installerRedirect(req *http.Request, via []*http.Request) error {
	if req.URL.Scheme != "https" {
		return fmt.Errorf("installer redirect is not HTTPS")
	}
	if len(via) >= maxInstallerRedirects {
		return fmt.Errorf("too many installer redirects")
	}
	if !netguard.IsPublicURL(req.Context(), req.URL.String()) {
		return fmt.Errorf("installer redirect is not a public address")
	}
	return nil
}

func downloadInstaller(ctx context.Context, progress func(InstallProgress)) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, OllamaInstallerURL, nil)
	if err != nil {
		return "", fmt.Errorf("build installer request: %w", err)
	}
	request.Header.Set("User-Agent", "Jarvis/1.0 (Windows)")

	client := newInstallerClient()
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("download installer: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download installer: HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maxInstallerBytes {
		return "", fmt.Errorf("installer is unexpectedly large (%d bytes)", response.ContentLength)
	}

	dir := filepath.Join(os.TempDir(), "Jarvis")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create installer directory: %w", err)
	}
	file, err := os.CreateTemp(dir, "OllamaSetup-*.exe")
	if err != nil {
		return "", fmt.Errorf("create installer file: %w", err)
	}
	path := file.Name()
	cleanup := func() {
		_ = file.Close()
		_ = os.Remove(path)
	}

	total := response.ContentLength
	if total <= 0 {
		// Some proxies omit Content-Length; use the known installer size so
		// the progress bar still moves.
		total = expectedInstallerBytes
	}
	var written int64
	buffer := make([]byte, 256*1024)
	lastPercent := -1
	for {
		if err := ctx.Err(); err != nil {
			cleanup()
			return "", err
		}
		read, readErr := response.Body.Read(buffer)
		if read > 0 {
			if written+int64(read) > maxInstallerBytes {
				cleanup()
				return "", fmt.Errorf("installer exceeded the download limit")
			}
			if _, writeErr := file.Write(buffer[:read]); writeErr != nil {
				cleanup()
				return "", fmt.Errorf("write installer: %w", writeErr)
			}
			written += int64(read)
			percent := 5
			if total > 0 {
				percent = 5 + int(written*85/total)
				if percent > 90 {
					percent = 90
				}
			}
			if percent != lastPercent {
				lastPercent = percent
				emitInstallProgress(progress, InstallProgress{
					Stage:      "download",
					Message:    "Downloading Ollama...",
					Percent:    percent,
					BytesDone:  written,
					BytesTotal: total,
					Detail:     formatBytes(written) + " of " + formatBytes(total),
				})
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			cleanup()
			return "", fmt.Errorf("download installer: %w", readErr)
		}
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close installer: %w", err)
	}
	if written == 0 {
		_ = os.Remove(path)
		return "", fmt.Errorf("downloaded installer is empty")
	}
	return path, nil
}

func verifyInstallerSignature(ctx context.Context, path string) error {
	powershell := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	if _, err := os.Stat(powershell); err != nil {
		powershell = "powershell.exe"
	}
	escaped := strings.ReplaceAll(path, "'", "''")
	script := "$s = Get-AuthenticodeSignature -LiteralPath '" + escaped + "'; " +
		"if ($null -eq $s -or $s.Status -ne 'Valid') { Write-Output ('INVALID:' + $s.Status); exit 1 }; " +
		"$subject = [string]$s.SignerCertificate.Subject; " +
		"if ($subject -notmatch 'Ollama') { Write-Output ('UNEXPECTED_SIGNER:' + $subject); exit 2 }; " +
		"Write-Output 'VALID'"
	cmd := hiddenCommand(ctx, powershell, "-NoProfile", "-NonInteractive", "-Command", script)
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		return fmt.Errorf("installer signature check failed: %s", text)
	}
	if text != "VALID" {
		return fmt.Errorf("installer signature check failed: %s", text)
	}
	return nil
}

func waitForInstalled(ctx context.Context, timeout time.Duration) bool {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if OllamaInstalled() {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return OllamaInstalled()
		case <-ticker.C:
		}
	}
}

// StartOllama launches the installed Ollama background app and waits for its
// API to answer. It is safe to call when Ollama is already running.
func StartOllama(ctx context.Context) error {
	if ollamaRunning() {
		return nil
	}
	path := OllamaPath()
	if path == "" {
		return fmt.Errorf("Ollama is not installed")
	}

	appPath := filepath.Join(filepath.Dir(path), "ollama app.exe")
	if info, err := os.Stat(appPath); err == nil && !info.IsDir() {
		if cmd := detachedHiddenCommand(appPath); cmd.Start() == nil {
			_ = cmd.Process.Release()
			if waitForOllama(ctx, 45*time.Second) == nil {
				return nil
			}
		}
	}

	cmd := detachedHiddenCommand(path, "serve")
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start Ollama: %w", err)
	}
	_ = cmd.Process.Release()
	if err := waitForOllama(ctx, 60*time.Second); err != nil {
		return fmt.Errorf("Ollama did not start: %w", err)
	}
	return nil
}

func waitForOllama(ctx context.Context, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ollamaRunning() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("timed out waiting for Ollama")
		case <-ticker.C:
		}
	}
}

func hiddenCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}

// detachedHiddenCommand starts a long-lived process that must outlive the
// setup request. It deliberately does not use CommandContext.
func detachedHiddenCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}

func emitInstallProgress(progress func(InstallProgress), event InstallProgress) {
	if progress != nil {
		progress(event)
	}
}

func formatBytes(bytes int64) string {
	if bytes <= 0 {
		return "unknown size"
	}
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.2f GB", float64(bytes)/gb)
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/mb)
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/kb)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
