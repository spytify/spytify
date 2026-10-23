//go:build linux

package platform

import "testing"

func TestAllowedRatesPattern(t *testing.T) {
	out := "Found \"settings\" metadata 31\nupdate: id:0 key:'clock.rate' value:'48000' type:''\nupdate: id:0 key:'clock.allowed-rates' value:'[ 48000 ]' type:''\n"
	m := allowedRates.FindStringSubmatch(out)
	if m == nil || m[1] != " 48000 " {
		t.Fatalf("match = %q", m)
	}
	if m := allowedRates.FindStringSubmatch("update: id:0 key:'clock.allowed-rates' value:'[ 44100, 48000 ]' type:''"); m == nil || m[1] != " 44100, 48000 " {
		t.Fatalf("match = %q", m)
	}
}
