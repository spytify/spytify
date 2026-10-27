package openfiles

import (
	"bufio"
	"bytes"
	"strconv"
	"strings"

	"github.com/spytify/spytify/internal/proc"
)

// Held lists what process pid holds open under dir, as lsof sees it: open
// files, folders watched with kqueue, and the working folder.
func Held(pid int, dir string) ([]string, error) {
	out, err := proc.Command("lsof", "-n", "-P", "-p", strconv.Itoa(pid), "-F", "n").Output()
	if err != nil && len(out) == 0 {
		return nil, err
	}
	var paths []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		if line := sc.Text(); strings.HasPrefix(line, "n/") {
			paths = append(paths, line[1:])
		}
	}
	return under(paths, dir), nil
}
