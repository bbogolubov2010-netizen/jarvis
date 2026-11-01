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
	Algorithm string                    `json:"algorithm"`
	Files     map[string]string         `json:"files"`
	OnDemand  map[string]checksumDigest `json:"on_demand,omitempty"`
}

// checksumDigest describes a large file that is verified on demand instead of
// during startup. Hashing multi-gigabyte models on every launch would make the
// app feel broken, so the fast manifest only covers the runtime binaries.
type checksumDigest struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size,omitempty"`
}

// Verify checks the optional checksums.json manifest in the engine root. A
// missing manifest is accepted for backward compatibility, but an invalid or
// tampered manifest is a hard error. Set JARVIS_VOICE_REQUIRE_CHECKSUMS=1 to
// make the manifest mandatory in production.
//
// Only the "files" section is verified. Large models and optional runtimes live
// in "on_demand" and are checked by VerifyOnDemand.
func (p Paths) Verify() error {
	manifest, err := p.loadManifest()
	if err != nil || manifest == nil {
		return err
	}
	if len(manifest.Files) == 0 {
		return errors.New("voice engine checksum manifest has no startup files")
	}
	for relative, expected := range manifest.Files {
		if err := p.verifyFile(relative, expected, 0); err != nil {
			return err
		}
	}
	return nil
}

// VerifyOnDemand checks the large binaries and models listed in the manifest.
// It is deliberately not part of startup verification: the model set can be
// several gigabytes. Call it after a download or from an explicit check.
func (p Paths) VerifyOnDemand() error {
	manifest, err := p.loadManifest()
	if err != nil || manifest == nil {
		return err
	}
	for relative, expected := range manifest.OnDemand {
		if expected.SHA256 == "" {
			return fmt.Errorf("checksum %q has no digest", relative)
		}
		if err := p.verifyFile(relative, expected.SHA256, expected.Size); err != nil {
			return err
		}
	}
	return nil
}

func (p Paths) verifyFile(relative, expected string, size int64) error {
	path, err := p.resolveManifestPath(relative)
	if err != nil {
		return err
	}
	if size > 0 {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("checksum %q: %w", relative, err)
		}
		if info.Size() != size {
			return fmt.Errorf("size mismatch for %q", relative)
		}
	}
	actual, err := fileSHA256(path)
	if err != nil {
		return fmt.Errorf("checksum %q: %w", relative, err)
	}
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("checksum mismatch for %q", relative)
	}
	return nil
}

func (p Paths) loadManifest() (*checksumManifest, error) {
	manifestPath := filepath.Join(p.Root, checksumManifestName)
	data, err := os.ReadFile(manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		if os.Getenv("JARVIS_VOICE_REQUIRE_CHECKSUMS") == "1" {
			return nil, fmt.Errorf("voice engine checksum manifest is missing: %s", manifestPath)
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read voice engine checksums: %w", err)
	}

	var manifest checksumManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse voice engine checksums: %w", err)
	}
	if !strings.EqualFold(manifest.Algorithm, "sha256") {
		return nil, errors.New("voice engine checksum manifest uses an unsupported algorithm")
	}
	if len(manifest.Files)+len(manifest.OnDemand) == 0 {
		return nil, errors.New("voice engine checksum manifest is empty")
	}
	return &manifest, nil
}

func (p Paths) resolveManifestPath(relative string) (string, error) {
	if relative == "" || !filepath.IsLocal(relative) {
		return "", fmt.Errorf("unsafe checksum path %q", relative)
	}
	root := filepath.Clean(p.Root)
	path := filepath.Clean(filepath.Join(root, filepath.FromSlash(relative)))
	if !strings.HasPrefix(path, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("checksum path escapes engine root: %q", relative)
	}
	return path, nil
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
