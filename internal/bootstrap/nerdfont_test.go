package bootstrap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUseNerdFont_OverrideWins(t *testing.T) {
	yes, no := true, false
	if !useNerdFont(&yes) {
		t.Error("nerd_font=true: want true")
	}
	if useNerdFont(&no) {
		t.Error("nerd_font=false: want false")
	}
}

func TestNerdFontInstalled(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		want  bool
	}{
		{name: "v3 name", files: []string{"JetBrainsMono/JetBrainsMonoNerdFont-Regular.ttf"}, want: true},
		{name: "v2 name", files: []string{"Hack Regular Nerd Font Complete.ttf"}, want: true},
		{name: "otf", files: []string{"FiraCodeNerdFont-Bold.otf"}, want: true},
		{name: "plain fonts only", files: []string{"Menlo.ttc", "sub/DejaVuSans.ttf"}, want: false},
		{name: "non-font file", files: []string{"nerdfonts-readme.md", "NerdFontsSymbolsOnly.zip"}, want: false},
		{name: "empty", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			for _, f := range tt.files {
				writeEmptyFile(t, filepath.Join(dir, f))
			}
			missing := filepath.Join(dir, "does-not-exist")
			if got := nerdFontInstalled([]string{missing, dir}); got != tt.want {
				t.Errorf("nerdFontInstalled = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNerdFontInstalled_SymlinkedRoot(t *testing.T) {
	base := t.TempDir()
	realDir := filepath.Join(base, "real")
	writeEmptyFile(t, filepath.Join(realDir, "HackNerdFont-Regular.ttf"))
	link := filepath.Join(base, "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if !nerdFontInstalled([]string{link}) {
		t.Error("Nerd Font under a symlinked font dir was not detected")
	}
}

func writeEmptyFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}
