package osopen

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
)

// testScheme is a link scheme the test registers for the current user, so
// the shell hands its links to this test program as it hands an https
// link to the browser.
const testScheme = "spytify-open-test"

// TestMain plays the browser when the shell starts this program with a
// link: it writes the link to the file its first argument names.
func TestMain(m *testing.M) {
	if len(os.Args) == 3 && strings.HasPrefix(os.Args[2], testScheme+":") {
		if os.WriteFile(os.Args[1]+".part", []byte(os.Args[2]), 0o644) != nil || os.Rename(os.Args[1]+".part", os.Args[1]) != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// A link reaches the browser whole: the crash report's link to a new
// GitHub issue runs to 8000 characters and holds &, % and #.
func TestOpenHandsTheBrowserTheWholeLink(t *testing.T) {
	if os.Getenv("CI") == "" && os.Getenv("SPYTIFY_REAL_OPEN") == "" {
		t.Skip("registers a link scheme for the current user: set SPYTIFY_REAL_OPEN=1 to run it")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	opened := filepath.Join(t.TempDir(), "opened")
	register(t, `"`+exe+`" "`+opened+`" "%1"`)
	body := strings.Repeat("panic: a & b = 100% #1 é\n", 180)
	link := testScheme + "://spytify.test/issues/new?" + url.Values{"title": {"Crash: boom"}, "body": {body}}.Encode()
	if len(link) < 7500 || len(link) > 8000 {
		t.Fatalf("the link has %d characters", len(link))
	}

	if err := Open(link); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		got, err := os.ReadFile(opened)
		if err == nil {
			if string(got) != link {
				t.Errorf("the browser got %d characters:\n%.300s\nwant %d:\n%.300s", len(got), got, len(link), link)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the shell started nothing for the link")
		}
	}
}

// register makes command open the links of testScheme, until the test ends.
func register(t *testing.T, command string) {
	t.Helper()
	key := `Software\Classes\` + testScheme
	t.Cleanup(func() {
		for _, sub := range []string{`\shell\open\command`, `\shell\open`, `\shell`, ``} {
			_ = registry.DeleteKey(registry.CURRENT_USER, key+sub)
		}
	})
	for sub, values := range map[string]map[string]string{
		``:                    {"": "URL:Spytify open test", "URL Protocol": ""},
		`\shell\open\command`: {"": command},
	} {
		k, _, err := registry.CreateKey(registry.CURRENT_USER, key+sub, registry.SET_VALUE)
		if err != nil {
			t.Fatal(err)
		}
		for name, value := range values {
			if err := k.SetStringValue(name, value); err != nil {
				k.Close()
				t.Fatal(err)
			}
		}
		k.Close()
	}
}
