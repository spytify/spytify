package openfiles

import (
	"fmt"
	"strings"
)

// Explain adds to a failed rename the programs that hold its files, which
// is why Windows refuses it when they are virus scanners, cloud drives or
// indexers looking at a file just written.
func Explain(err error, paths ...string) error {
	if by := HeldBy(paths...); len(by) > 0 {
		return fmt.Errorf("%w (held by %s)", err, strings.Join(by, ", "))
	}
	return err
}
