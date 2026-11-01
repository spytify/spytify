package telemetry

import (
	"cmp"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
)

// Scrubber replaces what could tell who the user is: the output folder
// becomes <output>, the home folder ~ and the user name <user>. Track
// names are left as they are.
type Scrubber struct {
	home []string
	user *regexp.Regexp

	mu     sync.Mutex
	output []string
}

// SetOutput names the output folder, whose name the user chose and can
// be their own; "" names none. A drive's root is left as it is.
func (s *Scrubber) SetOutput(dir string) {
	if s == nil {
		return
	}
	var output []string
	if dir = filepath.Clean(dir); dir != "." && filepath.Dir(dir) != dir {
		output = []string{dir}
		if slash := filepath.ToSlash(dir); slash != dir {
			output = append(output, slash)
		}
	}
	s.mu.Lock()
	s.output = output
	s.mu.Unlock()
}

// NewScrubber reads the home folder and user name of this computer.
func NewScrubber() *Scrubber {
	s := &Scrubber{}
	if home, err := os.UserHomeDir(); err == nil && len(home) > 1 {
		s.home = []string{home}
		if slash := filepath.ToSlash(home); slash != home {
			s.home = append(s.home, slash)
		}
	}
	if u, err := user.Current(); err == nil {
		name := u.Username
		// DOMAIN\name on Windows.
		if i := strings.LastIndexAny(name, `\/`); i >= 0 {
			name = name[i+1:]
		}
		s.setUser(name)
	}
	return s
}

func (s *Scrubber) setUser(name string) {
	s.user = nil
	if len(name) >= 3 {
		s.user = regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(name) + `\b`)
	}
}

// String scrubs one text.
func (s *Scrubber) String(in string) string {
	if s == nil || in == "" {
		return in
	}
	out := in
	// The output folder first: it is often in the home folder.
	s.mu.Lock()
	output := s.output
	s.mu.Unlock()
	for _, o := range output {
		out = replaceFold(out, o, "<output>")
	}
	for _, h := range s.home {
		out = replaceFold(out, h, "~")
	}
	if s.user != nil {
		out = s.user.ReplaceAllString(out, "<user>")
	}
	return out
}

// Spans are where String scrubs in a text, as byte offsets: in order,
// those that touch made one.
func (s *Scrubber) Spans(in string) [][2]int {
	if s == nil || in == "" {
		return nil
	}
	s.mu.Lock()
	output := s.output
	s.mu.Unlock()
	var spans [][2]int
	for _, old := range slices.Concat(output, s.home) {
		spans = append(spans, indexFold(in, old)...)
	}
	if s.user != nil {
		for _, m := range s.user.FindAllStringIndex(in, -1) {
			spans = append(spans, [2]int{m[0], m[1]})
		}
	}
	slices.SortFunc(spans, func(a, b [2]int) int { return cmp.Compare(a[0], b[0]) })
	var merged [][2]int
	for _, sp := range spans {
		if n := len(merged); n > 0 && sp[0] <= merged[n-1][1] {
			merged[n-1][1] = max(merged[n-1][1], sp[1])
			continue
		}
		merged = append(merged, sp)
	}
	return merged
}

// replaceFold replaces old in s, ignoring case: Windows paths come in any
// case.
func replaceFold(s, old, repl string) string {
	var b strings.Builder
	last := 0
	for _, at := range indexFold(s, old) {
		b.WriteString(s[last:at[0]])
		b.WriteString(repl)
		last = at[1]
	}
	b.WriteString(s[last:])
	return b.String()
}

// indexFold is where old is in s, ignoring case as replaceFold does.
func indexFold(s, old string) [][2]int {
	if old == "" {
		return nil
	}
	lower, lowerOld := strings.ToLower(s), strings.ToLower(old)
	if len(lower) != len(s) || len(lowerOld) != len(old) {
		lower, lowerOld = s, old
	}
	var at [][2]int
	for i := 0; ; {
		j := strings.Index(lower[i:], lowerOld)
		if j < 0 {
			return at
		}
		at = append(at, [2]int{i + j, i + j + len(old)})
		i += j + len(old)
	}
}

// beforeSend is the last look at an event or a timing before it leaves.
func (c *Client) beforeSend(ev *sentry.Event, _ *sentry.EventHint) (out *sentry.Event) {
	// Nothing leaves unscrubbed: an event the scrubbing fails on is dropped.
	defer func() {
		if recover() != nil {
			out = nil
		}
	}()
	s := c.scrub
	ev.ServerName = ""
	ev.User = computerUser()
	ev.Message = s.String(ev.Message)
	ev.Transaction = s.String(ev.Transaction)
	frames := func(st *sentry.Stacktrace) {
		if st != nil {
			for j := range st.Frames {
				f := &st.Frames[j]
				f.AbsPath, f.Filename = s.String(f.AbsPath), s.String(f.Filename)
			}
		}
	}
	for i := range ev.Exception {
		ex := &ev.Exception[i]
		ex.Value = s.String(ex.Value)
		frames(ex.Stacktrace)
	}
	for i := range ev.Threads {
		th := &ev.Threads[i]
		th.Name = s.String(th.Name)
		frames(th.Stacktrace)
	}
	for _, b := range ev.Breadcrumbs {
		c.beforeBreadcrumb(b, nil)
	}
	for k, v := range ev.Tags {
		ev.Tags[k] = s.String(v)
	}
	for _, ctx := range ev.Contexts {
		for k, v := range ctx {
			if str, ok := v.(string); ok {
				ctx[k] = s.String(str)
			}
		}
	}
	for _, sp := range ev.Spans {
		sp.Description = s.String(sp.Description)
	}
	return ev
}

func (c *Client) beforeBreadcrumb(b *sentry.Breadcrumb, _ *sentry.BreadcrumbHint) (out *sentry.Breadcrumb) {
	defer func() {
		if recover() != nil {
			out = nil
		}
	}()
	b.Message = c.scrub.String(b.Message)
	for k, v := range b.Data {
		if str, ok := v.(string); ok {
			b.Data[k] = c.scrub.String(str)
		}
	}
	return b
}

func (c *Client) beforeSendLog(l *sentry.Log) (out *sentry.Log) {
	defer func() {
		if recover() != nil {
			out = nil
		}
	}()
	// The SDK adds the host name when the client has no server name.
	delete(l.Attributes, "sentry.server.address")
	l.Body = c.scrub.String(l.Body)
	for k, v := range l.Attributes {
		if v.Type() == attribute.STRING {
			l.Attributes[k] = attribute.StringValue(c.scrub.String(v.AsString()))
		}
	}
	return l
}

func (c *Client) beforeSendMetric(m *sentry.Metric) (out *sentry.Metric) {
	defer func() {
		if recover() != nil {
			out = nil
		}
	}()
	// The SDK adds the host name when the client has no server name.
	delete(m.Attributes, "sentry.server.address")
	for k, v := range m.Attributes {
		if v.Type() == attribute.STRING {
			m.Attributes[k] = attribute.StringValue(c.scrub.String(v.AsString()))
		}
	}
	return m
}
