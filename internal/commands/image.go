package commands

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	imageRequestTimeout = 90 * time.Second
	maxImageBytes       = 25 << 20
	defaultImageSize    = "1024x1024"
)

var allowedImageSizes = map[string]struct{}{
	"512x512":   {},
	"1024x1024": {},
	"1024x768":  {},
	"768x1024":  {},
	"1536x1024": {},
}

// RegisterImageCommands registers image generation.
func RegisterImageCommands(m *Manager) {
	m.Register(Command{
		Name:        "imagine",
		Aliases:     []string{"image", "draw", "generate"},
		Description: "generates an image online: /imagine <description> [--size WxH]",
		Handler:     imageHandler,
	})
}

func imageHandler(input string) string {
	prompt := strings.TrimSpace(input)
	size := defaultImageSize
	if index := strings.LastIndex(prompt, "--size "); index >= 0 {
		candidate := strings.TrimSpace(prompt[index+len("--size "):])
		if _, ok := allowedImageSizes[candidate]; ok {
			size = candidate
			prompt = strings.TrimSpace(prompt[:index])
		}
	}
	return GenerateImage(prompt, size)
}

// GenerateImage downloads an image from Pollinations and stores it locally.
// The prompt leaves the machine: this is disclosed in the UI and README.
func GenerateImage(prompt, size string) string {
	return GenerateImageContext(context.Background(), prompt, size)
}

// GenerateImageContext is the cancellable form used by the app. The caller is
// responsible for the opt-in network policy check.
func GenerateImageContext(ctx context.Context, prompt, size string) string {
	if ctx == nil {
		ctx = context.Background()
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "Usage: /imagine <description> [--size WxH]\nSizes: 512x512, 1024x1024, 1024x768, 768x1024, 1536x1024"
	}
	if err := ctx.Err(); err != nil {
		return "Image generation cancelled, Sir."
	}
	if _, ok := allowedImageSizes[size]; !ok {
		size = defaultImageSize
	}
	width, height, ok := parseSize(size)
	if !ok {
		return "Invalid image size."
	}

	requestURL := fmt.Sprintf(
		"https://image.pollinations.ai/prompt/%s?width=%d&height=%d&nologo=true",
		urlEncode(prompt), width, height,
	)
	client := &http.Client{Timeout: imageRequestTimeout}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return fmt.Sprintf("Image generation failed: %s", err)
	}
	request.Header.Set("User-Agent", "JARVIS/1.0 (Windows desktop assistant)")

	response, err := client.Do(request)
	if err != nil {
		return fmt.Sprintf("Image generation failed: %s", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Sprintf("Image generation failed: service returned %d", response.StatusCode)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "" && !strings.HasPrefix(contentType, "image/") {
		return fmt.Sprintf("Image generation failed: unexpected content type %q", contentType)
	}

	dir := filepath.Join(os.TempDir(), "jarvis_images")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Sprintf("Failed to prepare image directory: %s", err)
	}

	path := filepath.Join(dir, randomID()+".jpg")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Sprintf("Failed to save image: %s", err)
	}

	written, copyErr := copyImageBody(file, response.Body, maxImageBytes)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || written == 0 {
		_ = os.Remove(path)
		if copyErr != nil {
			return fmt.Sprintf("Failed to save image: %s", copyErr)
		}
		if closeErr != nil {
			return fmt.Sprintf("Failed to save image: %s", closeErr)
		}
		return "Image generation failed: empty response."
	}

	return ResultImageSavedPrefix + path + ":" + size
}

// copyImageBody copies at most limit bytes and reports oversized responses
// instead of silently saving a truncated image.
func copyImageBody(dst io.Writer, src io.Reader, limit int64) (int64, error) {
	written, err := io.Copy(dst, io.LimitReader(src, limit+1))
	if err != nil {
		return written, err
	}
	if written > limit {
		return written, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return written, nil
}

func parseSize(size string) (int, int, bool) {
	parts := strings.Split(size, "x")
	if len(parts) != 2 {
		return 0, 0, false
	}
	width, err := strconv.Atoi(parts[0])
	if err != nil || width <= 0 {
		return 0, 0, false
	}
	height, err := strconv.Atoi(parts[1])
	if err != nil || height <= 0 {
		return 0, 0, false
	}
	return width, height, true
}

func urlEncode(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

func randomID() string {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buffer)
}
