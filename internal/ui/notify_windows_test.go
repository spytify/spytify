package ui

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/go-ole/go-ole"
)

// A title or a track name with XML's own characters stays text in the
// toast.
func TestTheToastEscapesItsText(t *testing.T) {
	got := toastXML(`Rock & Roll`, `<b>"Song"</b>`, `C:\Users\listener\icon.png`)
	for _, want := range []string{
		`<text id="1">Rock &amp; Roll</text>`,
		`<text id="2">&lt;b&gt;&#34;Song&#34;&lt;/b&gt;</text>`,
		`src="file:///C:\Users\listener\icon.png"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("%s lacks %s", got, want)
		}
	}
	if got := toastXML("a", "b", ""); !strings.Contains(got, `template="ToastText02"`) || strings.Contains(got, "<image") {
		t.Errorf("without an icon: %s", got)
	}
}

// The Windows Runtime makes the toast in this process: LoadXml reads the
// XML (and refuses a broken one), the factory makes a toast of it, and the
// notifier takes it. Showing it needs a desktop session, which CI may not
// have: its answer is logged, not checked.
func TestAToastIsMadeInProcess(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ole.RoInitialize(1); err != nil {
		var oe *ole.OleError
		if !errors.As(err, &oe) || oe.Code() != 1 {
			t.Fatal(err)
		}
	}
	if _, err := newToast("<toast><visual>"); err == nil || !strings.Contains(err.Error(), "LoadXml") {
		t.Errorf("a broken toast: %v", err)
	}
	toast, err := newToast(toastXML("Track saved", "Song · Artist", ""))
	if err != nil {
		t.Fatal(err)
	}
	defer toast.Release()
	t.Logf("shown: %v", pushToast("org.spytify.ui.test", toast))
}
