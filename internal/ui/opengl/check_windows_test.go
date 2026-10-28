package opengl

import (
	"errors"
	"testing"
	"unsafe"
)

func TestPixelFormatDescriptorMatchesWingdi(t *testing.T) {
	if n := unsafe.Sizeof(pixelFormatDescriptor{}); n != 40 {
		t.Fatalf("PIXELFORMATDESCRIPTOR is %d bytes, want 40", n)
	}
}

// The answer depends on the computer (CI's runner has no graphics card);
// either one is fine, anything else is not.
func TestCheckAnswers(t *testing.T) {
	err := Check()
	if err != nil && !errors.Is(err, ErrNoDriver) {
		t.Fatal(err)
	}
	t.Logf("Check: %v", err)
}
