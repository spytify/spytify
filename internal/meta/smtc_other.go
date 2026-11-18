//go:build !windows

package meta

import "errors"

func newSMTCAPI() (smtcAPI, error) {
	return nil, errors.New("the media session API is Windows only")
}
