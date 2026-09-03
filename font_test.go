package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestFontLocations(t *testing.T) {
	tests := []struct {
		name string
		goos string
		home string
		env  map[string]string
		want FontLocations
	}{
		{
			name: "darwin",
			goos: "darwin",
			home: "/Users/alice",
			want: FontLocations{
				UserDir:    "/Users/alice/Library/Fonts",
				Dirs:       []string{"/Users/alice/Library/Fonts", "/Library/Fonts"},
				CanInstall: true,
			},
		},
		{
			name: "linux with XDG data home",
			goos: "linux",
			home: "/home/alice",
			env:  map[string]string{"XDG_DATA_HOME": "/data/alice"},
			want: FontLocations{
				UserDir:    "/data/alice/fonts",
				Dirs:       []string{"/data/alice/fonts", "/home/alice/.fonts", "/usr/share/fonts", "/usr/local/share/fonts"},
				CanInstall: true,
			},
		},
		{
			name: "linux without XDG data home",
			goos: "linux",
			home: "/home/alice",
			want: FontLocations{
				UserDir:    "/home/alice/.local/share/fonts",
				Dirs:       []string{"/home/alice/.local/share/fonts", "/home/alice/.fonts", "/usr/share/fonts", "/usr/local/share/fonts"},
				CanInstall: true,
			},
		},
		{
			name: "windows with environment directories",
			goos: "windows",
			home: `C:\Users\alice`,
			env: map[string]string{
				"LOCALAPPDATA": `D:\Profiles\alice\Local`,
				"WINDIR":       `D:\Windows`,
			},
			want: FontLocations{
				UserDir:    `D:\Profiles\alice\Local\Microsoft\Windows\Fonts`,
				Dirs:       []string{`D:\Profiles\alice\Local\Microsoft\Windows\Fonts`, `D:\Windows\Fonts`},
				CanInstall: false,
			},
		},
		{
			name: "windows without environment directories",
			goos: "windows",
			home: `C:\Users\alice`,
			want: FontLocations{
				UserDir:    `C:\Users\alice\AppData\Local\Microsoft\Windows\Fonts`,
				Dirs:       []string{`C:\Users\alice\AppData\Local\Microsoft\Windows\Fonts`, `C:\Windows\Fonts`},
				CanInstall: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(key string) string { return tt.env[key] }
			if got := fontLocations(tt.goos, tt.home, getenv); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("fontLocations(%q, %q) = %#v, want %#v", tt.goos, tt.home, got, tt.want)
			}
		})
	}
}

func TestFindNerdFont(t *testing.T) {
	t.Run("finds v3 font in nested directory after a missing directory", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "nested", "JetBrainsMonoNerdFont-Regular.ttf")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("font"), 0o644); err != nil {
			t.Fatal(err)
		}

		got, ok := findNerdFont([]string{filepath.Join(root, "missing"), root})
		if !ok || got != path {
			t.Fatalf("findNerdFont() = %q, %v, want %q, true", got, ok, path)
		}
	})

	t.Run("finds v2 font with spaces", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "JetBrains Mono Nerd Font Complete.otf")
		if err := os.WriteFile(path, []byte("font"), 0o644); err != nil {
			t.Fatal(err)
		}

		got, ok := findNerdFont([]string{root})
		if !ok || got != path {
			t.Fatalf("findNerdFont() = %q, %v, want %q, true", got, ok, path)
		}
	})

	t.Run("rejects a non-font file", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "nerdfont.txt"), []byte("not a font"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got, ok := findNerdFont([]string{root}); ok {
			t.Fatalf("findNerdFont() = %q, true, want no match", got)
		}
	})
}

func TestInstallNerdFont(t *testing.T) {
	t.Setenv("PATH", "")
	archive := zipFixture(t, []zipEntry{
		{name: "LICENSE", contents: "license"},
		{name: "README.md", contents: "readme"},
		{name: "SymbolsNerdFontMono-Regular.ttf", contents: "mono font"},
		{name: "../evil/SymbolsNerdFont-Regular.ttf", contents: "symbols font"},
	})
	dir := filepath.Join(t.TempDir(), "fonts")
	fetch := func(url string) (io.ReadCloser, error) {
		if url != nerdFontURL {
			t.Fatalf("fetch URL = %q, want %q", url, nerdFontURL)
		}
		return io.NopCloser(bytes.NewReader(archive)), nil
	}

	got, err := installNerdFont(dir, fetch)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(dir, "SymbolsNerdFontMono-Regular.ttf"),
		filepath.Join(dir, "SymbolsNerdFont-Regular.ttf"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("installed paths = %q, want %q", got, want)
	}
	for i, path := range want {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read installed font: %v", readErr)
		}
		wantData := []string{"mono font", "symbols font"}[i]
		if string(data) != wantData {
			t.Errorf("%s contains %q, want %q", path, data, wantData)
		}
		if runtime.GOOS != "windows" {
			info, statErr := os.Stat(path)
			if statErr != nil {
				t.Fatal(statErr)
			}
			if info.Mode().Perm() != 0o644 {
				t.Errorf("%s mode = %o, want 644", path, info.Mode().Perm())
			}
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "evil")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("path traversal created a directory outside the destination: %v", err)
	}
	for _, name := range []string{"LICENSE", "README.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("non-font archive entry %s was installed: %v", name, err)
		}
	}
}

func TestInstallNerdFontReturnsFetchError(t *testing.T) {
	want := errors.New("download failed")
	_, got := installNerdFont(t.TempDir(), func(string) (io.ReadCloser, error) {
		return nil, want
	})
	if !errors.Is(got, want) {
		t.Fatalf("installNerdFont() error = %v, want %v", got, want)
	}
}

func TestInstallNerdFontRejectsArchiveWithoutFonts(t *testing.T) {
	t.Setenv("PATH", "")
	archive := zipFixture(t, []zipEntry{{name: "README.md", contents: "no fonts here"}})
	_, err := installNerdFont(t.TempDir(), func(string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(archive)), nil
	})
	if err == nil {
		t.Fatal("installNerdFont() succeeded without installing a font")
	}
}

type zipEntry struct {
	name     string
	contents string
}

func zipFixture(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, fixture := range entries {
		entry, err := zw.Create(fixture.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(entry, fixture.contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
