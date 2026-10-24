package meta

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/spytify/spytify/internal/target"
)

// smtcPrelude loads the Windows media session API (SMTC) in Windows
// PowerShell and finds the target's session by its app id, which holds the
// executable or package name.
const smtcPrelude = `
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
Add-Type -AssemblyName System.Runtime.WindowsRuntime
$asTask = ([System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object {
  $_.Name -eq 'AsTask' -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation` + "`" + `1'
})[0]
function Await($op, [Type]$type) {
  $task = $asTask.MakeGenericMethod($type).Invoke($null, @($op))
  $null = $task.Wait(-1)
  $task.Result
}
$managerType = [Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager, Windows.Media.Control, ContentType = WindowsRuntime]
$propsType = [Windows.Media.Control.GlobalSystemMediaTransportControlsSessionMediaProperties, Windows.Media.Control, ContentType = WindowsRuntime]
$streamType = [Windows.Storage.Streams.IRandomAccessStreamWithContentType, Windows.Storage.Streams, ContentType = WindowsRuntime]
$inputType = [Windows.Storage.Streams.IInputStream, Windows.Storage.Streams, ContentType = WindowsRuntime]
$asStreamForRead = [System.IO.WindowsRuntimeStreamExtensions].GetMethod('AsStreamForRead', [Type[]]@($inputType))
$manager = Await ($managerType::RequestAsync()) $managerType
# Save-Thumbnail writes a thumbnail to path. Windows PowerShell sees the
# stream as a bare COM object, with no methods of its own and no overload
# that takes it; called through reflection, as Await calls AsTask, the
# runtime casts it.
function Save-Thumbnail($ref, [string]$path) {
  $stream = Await ($ref.OpenReadAsync()) $streamType
  $in = $asStreamForRead.Invoke($null, @($stream))
  try {
    $f = [System.IO.File]::Create($path)
    try { $in.CopyTo($f) } finally { $f.Close() }
  } finally { $in.Close() }
}
`

// smtcPoll prints one JSON line per interval, in the shape the macOS poller
// prints, so pollTracker turns both into events. SMTC has no track id and
// reports the position as of its last update; the cover is a thumbnail
// stream, saved to a temporary file once per track.
const smtcPoll = `
$lastKey = ''; $cover = ''; $coverFile = ''; $coverKey = ''
while ($true) {
  $out = [ordered]@{ running = $false }
  try {
    $s = Find-Session
    if ($s) {
      $out.running = $true
      $state = switch ($s.GetPlaybackInfo().PlaybackStatus.ToString()) { 'Playing' { 'playing' } 'Paused' { 'paused' } default { 'stopped' } }
      $out.state = $state
      $tl = $s.GetTimelineProperties()
      $pos = $tl.Position.TotalSeconds
      if ($state -eq 'playing') { $pos += ([DateTimeOffset]::Now - $tl.LastUpdatedTime).TotalSeconds }
      $out.position = [math]::Max(0, $pos)
      $p = Await ($s.TryGetMediaPropertiesAsync()) $propsType
      if ($p -and $p.Title) {
        $key = "$($p.Artist)|$($p.AlbumTitle)|$($p.Title)"
        if ($key -ne $lastKey) { $lastKey = $key; $cover = ''; $coverKey = '' }
        # The thumbnail often comes a moment after the title; a cover that
        # cannot be read is tried once per track and never costs the track.
        if (-not $cover -and $p.Thumbnail -and $coverKey -ne $key) {
          $coverKey = $key
          if ($coverFile) { Remove-Item -LiteralPath $coverFile -ErrorAction SilentlyContinue; $coverFile = '' }
          try {
            $coverFile = Join-Path $env:TEMP ('spytify-cover-' + [guid]::NewGuid().ToString('N'))
            Save-Thumbnail $p.Thumbnail $coverFile
            $cover = ([Uri]$coverFile).AbsoluteUri
          } catch {
            $coverFile = ''
            $out.error = "cover: $($_.Exception.Message)"
          }
        }
        $out.track = [ordered]@{
          id = $null; name = $p.Title; artist = $p.Artist; album = $p.AlbumTitle; albumArtist = $p.AlbumArtist
          duration = [math]::Round(($tl.EndTime - $tl.StartTime).TotalMilliseconds); trackNumber = $p.TrackNumber; artworkUrl = $cover
        }
      }
    }
  } catch { $out.error = $_.Exception.Message }
  [Console]::Out.WriteLine(($out | ConvertTo-Json -Compress -Depth 4))
  [Console]::Out.Flush()
  Start-Sleep -Milliseconds $interval
}
`

// smtcControl sends one transport command; exit 3 means no session, 4 that
// the player refused.
const smtcControl = `
$s = Find-Session
if (-not $s) { exit 3 }
$op = switch ($cmd) {
  'play' { $s.TryPlayAsync() } 'pause' { $s.TryPauseAsync() } 'playpause' { $s.TryTogglePlayPauseAsync() }
  'next' { $s.TrySkipNextAsync() } 'previous' { $s.TrySkipPreviousAsync() } 'restart' { $s.TryChangePlaybackPositionAsync(0) }
}
if (-not (Await $op ([bool]))) { exit 4 }
`

// SMTC reads what plays on Windows through Windows PowerShell, which every
// Windows 10 and 11 install has, and the media session API.
type SMTC struct {
	Interval time.Duration
	Logf     func(format string, args ...any)
	// Mixer is the player's volume in the Windows audio sessions; the
	// media session API has none of its own.
	Mixer interface {
		Volume() (float64, error)
		SetVolume(v float64) error
	}

	mu    sync.Mutex
	names []string
}

func NewSMTC() *SMTC { return &SMTC{} }

// Target sets the player; its process names find its media session.
func (m *SMTC) Target(t target.Target) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.names = nil
	for _, n := range t.ProcessNames {
		if n = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(n)), ".exe"); n != "" {
			m.names = append(m.names, n)
		}
	}
}

// smtcScript assembles a script with its parameters as PowerShell literals.
func smtcScript(names []string, vars map[string]string, body string) string {
	var b strings.Builder
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = psQuote(n)
	}
	fmt.Fprintf(&b, "$names = @(%s)\n", strings.Join(quoted, ", "))
	for k, v := range vars {
		fmt.Fprintf(&b, "$%s = %s\n", k, v)
	}
	b.WriteString(smtcPrelude)
	b.WriteString(body)
	return b.String()
}

// psQuote is a single-quoted PowerShell string: nothing in it is expanded.
func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// encodeCommand is the -EncodedCommand form of a script: base64 of UTF-16LE.
func encodeCommand(script string) string {
	units := utf16.Encode([]rune(script))
	b := make([]byte, 2*len(units))
	for i, u := range units {
		binary.LittleEndian.PutUint16(b[2*i:], u)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func powershell(ctx context.Context, script string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encodeCommand(script))
	hideConsole(cmd)
	return cmd
}

func (m *SMTC) current() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.names...)
}

// Watch starts the poller and reports changes.
func (m *SMTC) Watch(ctx context.Context, t target.Target) (<-chan Event, error) {
	m.Target(t)
	names := m.current()
	if len(names) == 0 {
		return nil, ErrNoPlayer
	}
	interval := m.Interval
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}
	cmd := powershell(ctx, smtcScript(names, map[string]string{"interval": fmt.Sprint(interval.Milliseconds())}, smtcPoll))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("powershell: %w", err)
	}
	out := make(chan Event, 64)
	// SMTC lengths are whole milliseconds.
	tracker := &pollTracker{unit: "ms"}
	go func() {
		defer cmd.Wait()
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1<<16), 1<<20)
		lastErr := ""
		for sc.Scan() {
			var p playerPoll
			if json.Unmarshal(sc.Bytes(), &p) != nil {
				continue
			}
			p.at = time.Now()
			if p.Error != lastErr {
				lastErr = p.Error
				if p.Error != "" && m.Logf != nil {
					m.Logf("now playing: %s", p.Error)
				}
			}
			for _, e := range tracker.next(p) {
				select {
				case out <- e:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

func (m *SMTC) Control(cmd string) error {
	switch cmd {
	case "play", "pause", "playpause", "next", "previous", "restart":
	default:
		return ErrUnsupported
	}
	names := m.current()
	if len(names) == 0 {
		return ErrNoPlayer
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := powershell(ctx, smtcScript(names, map[string]string{"cmd": psQuote(cmd)}, smtcControl)).CombinedOutput()
	var ee *exec.ExitError
	switch {
	case errors.As(err, &ee) && ee.ExitCode() == 3:
		return ErrNoPlayer
	case errors.As(err, &ee) && ee.ExitCode() == 4:
		return fmt.Errorf("the player refused %q", cmd)
	case err != nil:
		return fmt.Errorf("powershell: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (m *SMTC) Volume() (float64, error) {
	if m.Mixer == nil {
		return 0, ErrUnsupported
	}
	return m.Mixer.Volume()
}

func (m *SMTC) SetVolume(v float64) error {
	if m.Mixer == nil {
		return ErrUnsupported
	}
	return m.Mixer.SetVolume(v)
}
