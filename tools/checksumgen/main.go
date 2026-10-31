// Command checksumgen writes the optional voice-engine checksum manifest.
//
// Usage:
//
//	go run ./tools/checksumgen -root build\bin\voice-engine
//	go run ./tools/checksumgen -root build\bin\voice-engine -all
//
// The default manifest covers the executable and DLL files that actually run
// on this machine. Use -all to hash every file under the engine root as well,
// including Whisper models; verification then takes noticeably longer at
// startup.
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
}

var coreFiles = []string{
	"libvosk.dll",
	"whisper/whisper-cli.exe",
	"whisper/whisper.dll",
	"whisper/ggml.dll",
}

func main() {
	root := flag.String("root", filepath.Join("build", "bin", "voice-engine"), "voice engine directory")
	all := flag.Bool("all", false, "hash every file under the engine root")
	output := flag.String("out", "", "manifest path (default <root>/checksums.json)")
	verify := flag.Bool("verify", false, "verify the existing manifest instead of writing it")
	flag.Parse()

	if *verify {
		if err := voiceengine.Locate(*root).Verify(); err != nil {
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

	relativeFiles := coreFiles
	if all {
		relativeFiles, err = walkFiles(root)
		if err != nil {
			return err
		}
	}

	files := make(map[string]string, len(relativeFiles))
	for _, relative := range relativeFiles {
		path := filepath.Join(root, filepath.FromSlash(relative))
		digest, err := hashFile(path)
		if err != nil {
			if !all && os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "checksumgen: skipping missing optional file %s\n", relative)
				continue
			}
			return fmt.Errorf("hash %s: %w", relative, err)
		}
		files[relative] = digest
	}
	if len(files) == 0 {
		return fmt.Errorf("no files found under %s", root)
	}

	data, err := json.MarshalIndent(manifest{Algorithm: "sha256", Files: files}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(output, data, 0o600); err != nil {
		return err
	}
	fmt.Printf("wrote %s (%d files)\n", output, len(files))
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
		name := entry.Name()
		if name == "checksums.json" || strings.HasSuffix(name, ".tmp") {
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
