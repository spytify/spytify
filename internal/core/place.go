package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// outputFacts say where the output folder is, never its path, so the bug
// tracker can tell an unplugged drive from a cloud folder or one Windows
// protects. err is what outputWritable returned.
func outputFacts(dir string, err error, isDefault bool) map[string]string {
	facts := map[string]string{"output.default": yesNo(isDefault)}
	fi, statErr := os.Stat(dir)
	facts["output.exists"] = yesNo(statErr == nil && fi.IsDir())
	var perr *os.PathError
	if errors.As(err, &perr) {
		facts["output.op"] = perr.Op
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		facts["output.errno"] = strconv.FormatUint(uint64(errno), 10)
	}
	placeFacts(dir, facts)
	return facts
}

// formatFacts writes facts as the log does: key=value, sorted.
func formatFacts(facts map[string]string) string {
	keys := make([]string, 0, len(facts))
	for k := range facts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(' ')
		}
		v := facts[k]
		if v == "" || strings.ContainsAny(v, " \"=") {
			v = strconv.Quote(v)
		}
		fmt.Fprintf(&b, "%s=%s", k, v)
	}
	return b.String()
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// namedFolder is a folder the system names, such as Music.
type namedFolder struct{ name, path string }

// knownFolder names the folder dir is in, the deepest of folders, or
// "elsewhere".
func knownFolder(dir string, folders []namedFolder) string {
	name, longest := "elsewhere", -1
	for _, f := range folders {
		if f.path != "" && inFolder(dir, f.path) && len(f.path) > longest {
			name, longest = f.name, len(f.path)
		}
	}
	return name
}

// inFolder says whether path is folder or inside it, in any case: Windows
// paths come in any.
func inFolder(path, folder string) bool {
	path, folder = filepath.Clean(path), filepath.Clean(folder)
	if strings.EqualFold(path, folder) {
		return true
	}
	if !strings.HasSuffix(folder, string(filepath.Separator)) {
		folder += string(filepath.Separator)
	}
	return len(path) > len(folder) && strings.EqualFold(path[:len(folder)], folder)
}

// cloudName is a cloud provider's name as the bug tracker gets it: a
// business's OneDrive can name the business.
func cloudName(provider string) string {
	p := strings.ToLower(provider)
	for _, c := range []struct{ key, name string }{
		{"onedrive", "OneDrive"}, {"dropbox", "Dropbox"}, {"google", "Google Drive"}, {"icloud", "iCloud"}, {"box", "Box"},
	} {
		if strings.Contains(p, c.key) {
			return c.name
		}
	}
	return "other"
}

// cfaMode reads Windows' EnableControlledFolderAccess value.
func cfaMode(v uint64) string {
	switch v {
	case 0:
		return "off"
	case 1:
		return "on"
	case 2:
		return "audit"
	case 3:
		return "disks"
	case 4:
		return "disks audit"
	}
	return strconv.FormatUint(v, 10)
}
