package telemetry

import (
	"bytes"
	"image"
	"image/png"

	"github.com/getsentry/sentry-go"
)

// SetScreenshot sets what takes a picture of the window for a problem's
// report. It runs on the goroutine that reports the problem; no picture,
// or one that fails, leaves the report without one.
func (c *Client) SetScreenshot(take func() image.Image) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.screenshot = take
	c.mu.Unlock()
}

// Private says a text names the user, their home folder or the output
// folder, which a report leaves out.
func (c *Client) Private(s string) bool {
	return c != nil && c.scrub.String(s) != s
}

// takeScreenshot is the window's picture, or nil.
func (c *Client) takeScreenshot() (img image.Image) {
	defer func() {
		if recover() != nil {
			img = nil
		}
	}()
	c.mu.Lock()
	take := c.screenshot
	c.mu.Unlock()
	if take == nil {
		return nil
	}
	return take()
}

// screenshot is a picture of the window as a report's attachment, which
// Sentry shows as the issue's screenshot by its name. A picture that
// cannot be encoded is left out: the report goes without it.
func screenshot(img image.Image) (att *sentry.Attachment) {
	defer func() {
		if recover() != nil {
			att = nil
		}
	}()
	if img == nil {
		return nil
	}
	var b bytes.Buffer
	if png.Encode(&b, img) != nil {
		return nil
	}
	return &sentry.Attachment{Filename: "screenshot.png", ContentType: "image/png", Payload: b.Bytes()}
}
