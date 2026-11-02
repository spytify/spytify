//go:build !windows && !darwin && !linux

package core

func volumeOf(string) (drive, fs string) { return "", "" }

func cloudOf(dir string) string {
	if c := homeCloud(dir); c != "" {
		return c
	}
	return "none"
}

func knownFolders() []namedFolder { return nil }

func systemFacts(map[string]string) {}
