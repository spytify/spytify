package ui

import (
	"fmt"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/spytify/spytify/internal/ui/opengl"
)

type notified struct {
	fyne.App
	sent []*fyne.Notification
}

func (n *notified) SendNotification(x *fyne.Notification) { n.sent = append(n.sent, x) }

// Started from its menu entry, the app has no terminal: the desktop's
// notification says what to install.
func TestNoWindowNotifiesWhatToInstall(t *testing.T) {
	fa := &notified{App: test.NewTempApp(t)}
	tel := untracked(t, fa.Preferences())
	t.Cleanup(tel.Close)
	noGL := fmt.Errorf("%w: VersionUnavailable: GLX: Failed to create context: GLXBadFBConfig", opengl.ErrNoDriver)
	NoWindow(fa, tel, nil, noGL)
	if len(fa.sent) != 1 {
		t.Fatalf("%d notifications, want 1", len(fa.sent))
	}
	for _, want := range []string{"can't open its window", "apt install libgl1-mesa-dri", "dnf install mesa-dri-drivers", "GLXBadFBConfig"} {
		if !strings.Contains(fa.sent[0].Content, want) {
			t.Errorf("notification %q lacks %q", fa.sent[0].Content, want)
		}
	}
}

// With no display, there is nothing to install: the app says where to
// open it from.
func TestNoWindowSaysWhereToOpenItWithNoDisplay(t *testing.T) {
	fa := &notified{App: test.NewTempApp(t)}
	tel := untracked(t, fa.Preferences())
	t.Cleanup(tel.Close)
	noDisplay := fmt.Errorf("%w: PlatformUnavailable: Failed to detect any supported platform", opengl.ErrNoDisplay)
	NoWindow(fa, tel, nil, noDisplay)
	if len(fa.sent) != 1 {
		t.Fatalf("%d notifications, want 1", len(fa.sent))
	}
	got := fa.sent[0].Content
	for _, want := range []string{"no display answers", "desktop session", "PlatformUnavailable"} {
		if !strings.Contains(got, want) {
			t.Errorf("notification %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "apt install") {
		t.Errorf("notification %q says to install OpenGL drivers", got)
	}
}
