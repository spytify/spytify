package openfiles

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A held file is tried renameTries times, waiting firstWait and then twice
// as long each time: about 16 s in all.
const (
	renameTries = 7
	firstWait   = 250 * time.Millisecond
)

var (
	rename = os.Rename
	sleep  = time.Sleep
	held   = isHeld
)

// Rename renames from to to. Windows refuses while another program holds
// the file open, as a virus scanner, a cloud drive or an indexer does for a
// moment with a file just written: Rename then tries again, waiting twice
// as long each time, and logs each refusal with the programs holding the
// file. The error it gives up with names them too.
func Rename(from, to string, logf func(format string, args ...any)) error {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	wait := firstWait
	for try := 1; ; try++ {
		err := rename(from, to)
		if err == nil {
			if try > 1 {
				logf("%s: renamed at try %d of %d", filepath.Base(to), try, renameTries)
			}
			return nil
		}
		if !held(err) || try == renameTries {
			return Explain(err, from, to)
		}
		by := "a program Windows does not name"
		if names := HeldBy(from, to); len(names) > 0 {
			by = strings.Join(names, ", ")
		}
		logf("%s: try %d of %d refused, held by %s: trying again in %v", filepath.Base(to), try, renameTries, by, wait)
		sleep(wait)
		wait *= 2
	}
}
