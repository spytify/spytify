package openfiles

import (
	"os"
	"path/filepath"
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
	heldBy = HeldBy
)

// Rename renames from to to. Windows refuses while another program holds
// the file open, as a virus scanner, a cloud drive or an indexer does for a
// moment with a file just written: Rename then tries again, waiting twice
// as long each time, and logs each refusal. It asks Windows which programs
// hold the file only once, when it gives up, and names them in its error:
// a Restart Manager session on every try is what ransomware does to unlock
// files, to a behaviour engine.
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
		logf("%s: try %d of %d refused, held by another program: trying again in %v", filepath.Base(to), try, renameTries, wait)
		sleep(wait)
		wait *= 2
	}
}
