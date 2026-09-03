package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const nerdFontURL = "https://github.com/ryanoasis/nerd-fonts/releases/latest/download/NerdFontsSymbolsOnly.zip"

// FontLocations is where one platform keeps fonts. UserDir is where --setup
// installs, Dirs is every directory scanned for an existing Nerd Font with the
// user directory first, and CanInstall is false where copying a file is not
// enough to register a font.
type FontLocations struct {
	UserDir    string
	Dirs       []string
	CanInstall bool
}

// fontLocations returns the table row for goos. It takes goos, home and
// getenv as parameters so tests can ask for any platform on any host.
func fontLocations(goos, home string, getenv func(string) string) FontLocations {
	switch goos {
	case "darwin":
		userDir := path.Join(home, "Library", "Fonts")
		return FontLocations{
			UserDir:    userDir,
			Dirs:       []string{userDir, "/Library/Fonts"},
			CanInstall: true,
		}
	case "windows":
		localAppData := strings.TrimSpace(getenv("LOCALAPPDATA"))
		if localAppData == "" {
			localAppData = windowsJoin(home, "AppData", "Local")
		}
		windowsDir := strings.TrimSpace(getenv("WINDIR"))
		if windowsDir == "" {
			windowsDir = `C:\Windows`
		}
		userDir := windowsJoin(localAppData, "Microsoft", "Windows", "Fonts")
		return FontLocations{
			UserDir:    userDir,
			Dirs:       []string{userDir, windowsJoin(windowsDir, "Fonts")},
			CanInstall: false,
		}
	default:
		dataHome := strings.TrimSpace(getenv("XDG_DATA_HOME"))
		if dataHome == "" {
			dataHome = path.Join(home, ".local", "share")
		}
		userDir := path.Join(dataHome, "fonts")
		return FontLocations{
			UserDir: userDir,
			Dirs: []string{
				userDir,
				path.Join(home, ".fonts"),
				"/usr/share/fonts",
				"/usr/local/share/fonts",
			},
			CanInstall: true,
		}
	}
}

func windowsJoin(base string, elements ...string) string {
	joined := strings.TrimRight(strings.ReplaceAll(base, "/", `\`), `\`)
	for _, element := range elements {
		joined += `\` + strings.Trim(element, `/\`)
	}
	return joined
}

// findNerdFont walks dirs and returns the first font file whose normalized
// name contains "nerdfont" and ends in .ttf or .otf. Missing or unreadable
// directories are skipped.
func findNerdFont(dirs []string) (path string, ok bool) {
	normalize := strings.NewReplacer(" ", "", "-", "", "_", "")
	for _, dir := range dirs {
		var found string
		_ = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				if entry != nil && entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			extension := strings.ToLower(filepath.Ext(entry.Name()))
			if extension != ".ttf" && extension != ".otf" {
				return nil
			}
			name := normalize.Replace(strings.ToLower(entry.Name()))
			if strings.Contains(name, "nerdfont") {
				found = path
				return filepath.SkipAll
			}
			return nil
		})
		if found != "" {
			return found, true
		}
	}
	return "", false
}

// installNerdFont downloads the Symbols Nerd Font archive through fetch,
// unpacks every .ttf into dir, and returns the installed paths. A Linux font
// cache refresh is best-effort because the copied font is already usable.
func installNerdFont(dir string, fetch func(url string) (io.ReadCloser, error)) ([]string, error) {
	body, err := fetch(nerdFontURL)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	archive, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	installed := make([]string, 0)
	for _, entry := range zr.File {
		if !strings.EqualFold(filepath.Ext(entry.Name), ".ttf") {
			continue
		}
		destination := filepath.Join(dir, filepath.Base(entry.Name))
		if err := installFontFile(entry, destination); err != nil {
			return installed, err
		}
		installed = append(installed, destination)
	}
	if len(installed) == 0 {
		return nil, errors.New("font archive contains no .ttf files")
	}

	if runtime.GOOS == "linux" {
		if cache, err := exec.LookPath("fc-cache"); err == nil {
			_ = exec.Command(cache, "-f").Run()
		}
	}
	return installed, nil
}

func installFontFile(entry *zip.File, destination string) error {
	source, err := entry.Open()
	if err != nil {
		return err
	}
	defer source.Close()

	tmp, err := os.CreateTemp(filepath.Dir(destination), ".claude-readout-font-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if _, err := io.Copy(tmp, source); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, destination)
}

func fetchNerdFont(url string) (io.ReadCloser, error) {
	client := http.Client{Timeout: 60 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("download returned %s", response.Status)
	}
	return response.Body, nil
}
