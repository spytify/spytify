package opengl

import "testing"

// The test binary has no opengl\ beside it: nothing loads or starts.
func TestSoftwareNeedsMesaBesideTheExe(t *testing.T) {
	if err := UseSoftware(); err == nil {
		t.Error("UseSoftware loaded Mesa from nowhere")
	}
	if err := StartSoftware(); err == nil {
		t.Error("StartSoftware started a copy without Mesa")
	}
}

func TestSoftwareFollowsTheEnvironment(t *testing.T) {
	t.Setenv(SoftwareEnv, "")
	if Software() {
		t.Error("software without " + SoftwareEnv)
	}
	t.Setenv(SoftwareEnv, "software")
	if !Software() {
		t.Error("not software with " + SoftwareEnv + "=software")
	}
}
