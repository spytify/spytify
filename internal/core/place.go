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
// tracker can tell an unplugged drive from a cloud folder or one the
// system protects. err is what outputWritable returned.
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

// placeFacts adds where dir is: the kind of drive and its file system,
// the cloud that syncs it, the user's folder it is in, and where the
// Music folder is, which the former default output folder was in. Each
// system answers in its own file.
func placeFacts(dir string, facts map[string]string) {
	drive, fs := volumeOf(dir)
	if drive != "" {
		facts["output.drive"] = drive
	}
	if fs != "" {
		facts["output.fs"] = fs
	}
	facts["output.cloud"] = cloudOf(dir)
	folders := knownFolders()
	facts["output.known"] = knownFolder(dir, folders)
	if where, cloud := musicFolder(folders); where != "" {
		facts["output.music"], facts["output.music_cloud"] = where, cloud
	}
	systemFacts(facts)
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

// musicFolder says whether the Music folder is ~/Music, where the former
// default output folder went, or elsewhere (moved, or named in the
// system's language), and which cloud syncs it; nothing when the system
// does not say.
func musicFolder(folders []namedFolder) (where, cloud string) {
	var music, home string
	for _, f := range folders {
		switch f.name {
		case "Music":
			music = f.path
		case "home":
			home = f.path
		}
	}
	if music == "" || home == "" {
		return "", ""
	}
	where = "elsewhere"
	if samePath(music, filepath.Join(home, "Music")) {
		where = "home"
	}
	return where, cloudOf(music)
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

func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// firstUnder is the name of the folder of root that path is in, or "".
func firstUnder(path, root string) string {
	if !inFolder(path, root) || samePath(path, root) {
		return ""
	}
	rest := strings.TrimLeft(filepath.Clean(path)[len(filepath.Clean(root)):], string(filepath.Separator))
	name, _, _ := strings.Cut(rest, string(filepath.Separator))
	return name
}

// clouds are the cloud services told apart, as their names and folders
// start; any other is "other".
var clouds = []struct{ key, name string }{
	{"onedrive", "OneDrive"}, {"dropbox", "Dropbox"}, {"google", "Google Drive"}, {"icloud", "iCloud"},
	{"nextcloud", "Nextcloud"}, {"pcloud", "pCloud"}, {"mega", "MEGA"}, {"box", "Box"},
}

// cloudName is a cloud provider's name as the bug tracker gets it: a
// business's OneDrive, or a Google account, can name someone.
func cloudName(provider string) string {
	p := strings.ToLower(provider)
	for _, c := range clouds {
		if strings.Contains(p, c.key) {
			return c.name
		}
	}
	return "other"
}

// homeCloud names the cloud whose folder in the home folder holds dir,
// such as ~/Dropbox, directly or through a link, or says "".
func homeCloud(dir string) string {
	for _, home := range homes() {
		for _, p := range []string{dir, resolved(dir)} {
			name := strings.ToLower(firstUnder(p, home))
			for _, c := range clouds {
				if strings.HasPrefix(name, c.key) {
					return c.name
				}
			}
		}
	}
	return ""
}

// homes is the home folder, and where its links lead when elsewhere.
func homes() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	out := []string{home}
	if real, err := filepath.EvalSymlinks(home); err == nil && !samePath(real, home) {
		out = append(out, real)
	}
	return out
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

// existingFolder is dir, or its closest parent that exists.
func existingFolder(dir string) string {
	for {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return dir
		}
		dir = parent
	}
}

// resolved is the closest folder of dir that exists, its links followed:
// where the files really go.
func resolved(dir string) string {
	at := existingFolder(dir)
	if real, err := filepath.EvalSymlinks(at); err == nil {
		return real
	}
	return at
}
