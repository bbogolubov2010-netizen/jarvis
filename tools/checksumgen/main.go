// Command checksumgen writes the optional voice-engine checksum manifest.
//
// Usage:
//
//	go run ./tools/checksumgen -root build\bin\voice-engine
//	go run ./tools/checksumgen -root build\bin\voice-engine -all
//	go run ./tools/checksumgen -root build\bin\voice-engine -verify
//	go run ./tools/checksumgen -root build\bin\voice-engine -verify-on-demand
//
// By default the manifest splits files into two sections. "files" contains the
// binaries that run on every launch and is verified at startup. "on_demand"
// contains large Whisper models, CUDA runtimes, and Vosk model data; those are
// far too big to hash on every start and are checked by an explicit command or
// after a download. Use -all to hash every file into the startup section.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"Jarvis/internal/voiceengine"
)

type manifest struct {
	Algorithm string            `json:"algorithm"`
	Files     map[string]string `json:"files"`
	OnDemand  map[string]digest `json:"on_demand,omitempty"`
}

type digest struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size,omitempty"`
}

// coreFiles are loaded by the app every time voice input starts. They are
// small enough to verify synchronously; keep this list in sync with the engine
// layout in internal/voiceengine.
var coreFiles = []string{
	"libvosk.dll",
	"libgcc_s_seh-1.dll",
	"libstdc++-6.dll",
	"libwinpthread-1.dll",
	"whisper/whisper-cli.exe",
	"whisper/whisper.dll",
	"whisper/ggml.dll",
	"whisper/ggml-base.dll",
	"whisper/ggml-cpu-x64.dll",
	"whisper/ggml-cpu-sse42.dll",
	"whisper/ggml-cpu-haswell.dll",
	"whisper/ggml-cpu-sandybridge.dll",
	"whisper/ggml-cpu-skylakex.dll",
	"whisper/ggml-cpu-icelake.dll",
	"whisper/ggml-cpu-cannonlake.dll",
	"whisper/ggml-cpu-cascadelake.dll",
	"whisper/ggml-cpu-alderlake.dll",
}

// onDemandMinBytes catches optional runtimes (CUDA) that are not named in
// coreFiles but are too large to hash during startup.
const onDemandMinBytes = int64(64 << 20)

func main() {
	root := flag.String("root", filepath.Join("build", "bin", "voice-engine"), "voice engine directory")
	all := flag.Bool("all", false, "hash every file under the engine root into the startup section")
	output := flag.String("out", "", "manifest path (default <root>/checksums.json)")
	verify := flag.Bool("verify", false, "verify the startup file section of the existing manifest")
	verifyOnDemand := flag.Bool("verify-on-demand", false, "verify large models and runtimes in the existing manifest")
	flag.Parse()

	if *verify || *verifyOnDemand {
		paths := voiceengine.Locate(*root)
		var err error
		if *verify {
			err = paths.Verify()
		}
		if err == nil && *verifyOnDemand {
			err = paths.VerifyOnDemand()
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "checksumgen:", err)
			os.Exit(1)
		}
		fmt.Println("checksums verified")
		return
	}

	if err := run(*root, *all, *output); err != nil {
		fmt.Fprintln(os.Stderr, "checksumgen:", err)
		os.Exit(1)
	}
}

func run(root string, all bool, output string) error {
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("engine root: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("engine root is not a directory: %s", root)
	}
	if output == "" {
		output = filepath.Join(root, "checksums.json")
	}

	result := manifest{
		Algorithm: "sha256",
		Files:     make(map[string]string),
		OnDemand:  make(map[string]digest),
	}

	if all {
		relativeFiles, err := walkFiles(root)
		if err != nil {
			return err
		}
		for _, relative := range relativeFiles {
			value, err := hashFile(filepath.Join(root, filepath.FromSlash(relative)))
			if err != nil {
				return fmt.Errorf("hash %s: %w", relative, err)
			}
			result.Files[relative] = value
		}
	} else {
		for _, relative := range coreFiles {
			path := filepath.Join(root, filepath.FromSlash(relative))
			value, err := hashFile(path)
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Fprintf(os.Stderr, "checksumgen: skipping missing optional file %s\n", relative)
					continue
				}
				return fmt.Errorf("hash %s: %w", relative, err)
			}
			result.Files[relative] = value
		}

		large, err := walkOnDemandFiles(root)
		if err != nil {
			return err
		}
		for _, relative := range large {
			path := filepath.Join(root, filepath.FromSlash(relative))
			value, err := hashFile(path)
			if err != nil {
				return fmt.Errorf("hash %s: %w", relative, err)
			}
			info, err := os.Stat(path)
			if err != nil {
				return fmt.Errorf("stat %s: %w", relative, err)
			}
			result.OnDemand[relative] = digest{SHA256: value, Size: info.Size()}
		}
	}

	if len(result.Files)+len(result.OnDemand) == 0 {
		return fmt.Errorf("no files found under %s", root)
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(output, data, 0o600); err != nil {
		return err
	}
	fmt.Printf("wrote %s (%d startup files, %d on-demand files)\n", output, len(result.Files), len(result.OnDemand))
	return nil
}

func walkFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if skipFile(entry.Name()) {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// walkOnDemandFiles returns large files that must not be hashed at startup.
// Piper is skipped entirely: the app has no text-to-speech integration and
// those files never ship with the product.
func walkOnDemandFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "piper" {
				return filepath.SkipDir
			}
			return nil
		}
		if skipFile(entry.Name()) {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if isOnDemandFile(relative, info.Size()) {
			files = append(files, relative)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func isOnDemandFile(relative string, size int64) bool {
	lower := strings.ToLower(relative)
	name := strings.ToLower(filepath.Base(relative))
	switch filepath.Ext(name) {
	case ".bin", ".onnx":
		return true
	}
	if strings.HasPrefix(lower, "vosk-model-") || strings.Contains(lower, "/vosk-model-") {
		return true
	}
	for _, prefix := range []string{"cublas", "cudart", "nvrtc", "nvblas", "ggml-cuda"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return size >= onDemandMinBytes
}

func skipFile(name string) bool {
	return name == "checksums.json" || strings.HasSuffix(name, ".tmp")
}

func hashFile(path string) (string, error) {
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
