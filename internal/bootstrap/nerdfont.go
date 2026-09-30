package bootstrap

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// useNerdFont resolves the nerd_font config setting: an explicit value
// wins, otherwise the installed fonts are scanned. A terminal can't report
// which font it renders with, so detection only proves a Nerd Font is
// installed; users whose terminal uses a different one set nerd_font to
// false.
func useNerdFont(override *bool) bool {
	if override != nil {
		return *override
	}
	return nerdFontInstalled(fontDirs())
}

// nerdFontInstalled reports whether any font file under dirs is a patched
// Nerd Font. Patched fonts carry "NerdFont" (v3) or "Nerd Font" (v2) in
// their file name. Each dir is resolved first, because WalkDir does not
// follow a symlinked root (dotfile managers often link the font dir).
// Missing or unreadable directories are skipped.
func nerdFontInstalled(dirs []string) bool {
	found := false
	for _, dir := range dirs {
		root, err := filepath.EvalSymlinks(dir)
		if err != nil {
			continue
		}
		_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if isNerdFontFile(d.Name()) {
				found = true
				return fs.SkipAll
			}
			return nil
		})
		if found {
			return true
		}
	}
	return false
}

// fontDirs lists the font directories for this OS, per-user first since
// that is where Nerd Fonts are usually installed and the scan stops at the
// first match.
func fontDirs() []string {
	// Without a home dir only the system-wide directories are scanned.
	home, _ := os.UserHomeDir()
	var dirs []string
	switch runtime.GOOS {
	case "darwin":
		if home != "" {
			dirs = append(dirs, filepath.Join(home, "Library", "Fonts"))
		}
		// /System/Library/Fonts is SIP-protected and never holds one.
		dirs = append(dirs, "/Library/Fonts")
	case "windows":
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			dirs = append(dirs, filepath.Join(local, "Microsoft", "Windows", "Fonts"))
		}
		if win := os.Getenv("WINDIR"); win != "" {
			dirs = append(dirs, filepath.Join(win, "Fonts"))
		}
	default:
		dataHome := os.Getenv("XDG_DATA_HOME")
		if dataHome == "" && home != "" {
			dataHome = filepath.Join(home, ".local", "share")
		}
		if dataHome != "" {
			dirs = append(dirs, filepath.Join(dataHome, "fonts"))
		}
		if home != "" {
			dirs = append(dirs, filepath.Join(home, ".fonts"))
		}
		dirs = append(dirs, "/usr/local/share/fonts", "/usr/share/fonts")
	}
	return dirs
}

// isNerdFontFile reports whether name is a font file (.ttf, .otf, .ttc)
// named like a patched Nerd Font.
func isNerdFontFile(name string) bool {
	lower := strings.ToLower(name)
	switch filepath.Ext(lower) {
	case ".ttf", ".otf", ".ttc":
	default:
		return false
	}
	return strings.Contains(strings.ReplaceAll(lower, " ", ""), "nerdfont")
}
