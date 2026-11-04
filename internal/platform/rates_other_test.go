//go:build !linux

package platform

import "github.com/spytify/spytify/pkg/proto"

// ratesChecks are the sample rates checks, which only Linux makes.
func ratesChecks() []proto.Check { return nil }
