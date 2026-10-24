package telemetry

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/spytify/spytify/pkg/proto"
)

// Checks is what the diagnostics' Send to the developers button sends:
// the checks that fail and the end of spytifyd's log.
type Checks struct {
	Failing []proto.Check
	Log     []string
}

// failingNames are the names of the checks that fail, sorted, so the same
// failures group into one Sentry issue.
func (r Checks) failingNames() []string {
	var names []string
	for _, c := range r.Failing {
		names = append(names, c.Name)
	}
	sort.Strings(names)
	return names
}

// ChecksPreview is the report as it will leave.
func (c *Client) ChecksPreview(r Checks) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Version: %s\nSystem: %s/%s\nCountry: from the connection, or %s\n\nChecks that fail:\n",
		proto.Version, runtime.GOOS, runtime.GOARCH, orUnknown(Region()))
	for _, ch := range r.Failing {
		fmt.Fprintf(&b, "  %s: %s\n", ch.Name, c.scrub.String(ch.Detail))
	}
	if len(r.Log) > 0 {
		b.WriteString("\nEnd of the recorder's log:\n")
		b.WriteString(c.scrub.String(strings.Join(r.Log, "\n")))
	}
	return b.String()
}

// SendChecks sends the checks that fail and the user's message, if any.
func (c *Client) SendChecks(r Checks, message string) error {
	if !c.Available() {
		return errors.New("this build cannot send reports")
	}
	hub, done, err := c.oneShot()
	if err != nil {
		return err
	}
	names := r.failingNames()
	ev := sentry.NewEvent()
	ev.Level = sentry.LevelWarning
	ev.Message = "stuck: " + strings.Join(names, ", ")
	ev.Fingerprint = append([]string{"stuck"}, names...)
	ev.Tags["checks"] = strings.Join(names, ", ")
	details := sentry.Context{}
	for _, ch := range r.Failing {
		details[ch.Name] = ch.Detail
	}
	ev.Contexts["checks"] = details
	if len(r.Log) > 0 {
		ev.Attachments = []*sentry.Attachment{{Filename: "spytifyd.log", ContentType: "text/plain", Payload: []byte(c.scrub.String(strings.Join(r.Log, "\n")))}}
	}
	id := hub.CaptureEvent(ev)
	if !done() || id == nil {
		return errors.New("the report did not reach the developers; try again later")
	}
	if strings.TrimSpace(message) != "" {
		return c.sendFeedback(*id, message)
	}
	return nil
}

// maxFeedback is the longest message Sentry takes, in characters: it
// refuses a longer one rather than cutting it.
const maxFeedback = 4096

// sendFeedback sends the user's message as Sentry user feedback tied to
// an event. The Go SDK has no call for it, so this builds the envelope.
func (c *Client) sendFeedback(event sentry.EventID, message string) error {
	dsn, err := sentry.NewDsn(c.dsn)
	if err != nil {
		return err
	}
	message = c.scrub.String(strings.TrimSpace(message))
	if r := []rune(message); len(r) > maxFeedback {
		message = string(r[:maxFeedback])
	}
	id := uuid4()
	now := c.opts.Now()
	header, _ := json.Marshal(map[string]any{"event_id": id, "sent_at": now.UTC().Format(time.RFC3339)})
	item, _ := json.Marshal(map[string]any{"type": "feedback"})
	payload, err := json.Marshal(map[string]any{
		"event_id":    id,
		"timestamp":   float64(now.UnixNano()) / 1e9,
		"platform":    "go",
		"release":     "spytify@" + strings.TrimPrefix(proto.Version, "v"),
		"environment": environment(proto.Version),
		"contexts":    map[string]any{"feedback": map[string]any{"message": message, "associated_event_id": string(event)}},
	})
	if err != nil {
		return err
	}
	body := bytes.Join([][]byte{header, item, payload}, []byte("\n"))
	req, err := http.NewRequest(http.MethodPost, dsn.GetAPIURL().String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-sentry-envelope")
	req.Header.Set("X-Sentry-Auth", fmt.Sprintf("Sentry sentry_version=7, sentry_client=spytify/%s, sentry_key=%s", proto.Version, dsn.GetPublicKey()))
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("your message did not reach the developers: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("your message did not reach the developers: %s", resp.Status)
	}
	return nil
}

// uuid4 is a new event ID as Sentry wants it: a random, version 4 UUID in
// hex.
func uuid4() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return hex.EncodeToString(b[:])
}
