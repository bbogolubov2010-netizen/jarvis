package voiceengine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const checksumManifestName = "checksums.json"

type checksumManifest struct {
	Algorithm string            `json:"algorithm"`
	Files     map[string]string `json:"files"`
}

// Verify checks the optional checksums.json manifest in the engine root. A
// missing manifest is accepted for backward compatibility, but an invalid or
// tampered manifest is a hard error. Set JARVIS_VOICE_REQUIRE_CHECKSUMS=1 to
// make the manifest mandatory in production.
func (p Paths) Verify() error {
	manifestPath := filepath.Join(p.Root, checksumManifestName)
	data, err := os.ReadFile(manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		if os.Getenv("JARVIS_VOICE_REQUIRE_CHECKSUMS") == "1" {
			return fmt.Errorf("voice engine checksum manifest is missing: %s", manifestPath)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("read voice engine checksums: %w", err)
	}

	var manifest checksumManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("parse voice engine checksums: %w", err)
	}
	if !strings.EqualFold(manifest.Algorithm, "sha256") || len(manifest.Files) == 0 {
		return errors.New("voice engine checksum manifest is empty or uses an unsupported algorithm")
	}

	root := filepath.Clean(p.Root)
	for relative, expected := range manifest.Files {
		if relative == "" || filepath.IsAbs(relative) || strings.Contains(relative, "..") {
			return fmt.Errorf("unsafe checksum path %q", relative)
		}
		path := filepath.Join(root, relative)
		if !strings.HasPrefix(filepath.Clean(path), root+string(os.PathSeparator)) {
			return fmt.Errorf("checksum path escapes engine root: %q", relative)
		}
		actual, err := fileSHA256(path)
		if err != nil {
			return fmt.Errorf("checksum %q: %w", relative, err)
		}
		if !strings.EqualFold(actual, expected) {
			return fmt.Errorf("checksum mismatch for %q", relative)
		}
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
