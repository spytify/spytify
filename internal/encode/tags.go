package encode

import (
	"context"
	"strings"

	"github.com/spytify/spytify/internal/proc"
)

// Tags returns an audio file's own tags by lower-case name, "title" and
// "artist" among them: ID3 frames, FLAC comments and MP4 atoms alike. Only
// the file's head is read.
func (f *FFmpeg) Tags(ctx context.Context, path string) (map[string]string, error) {
	out, err := proc.CommandContext(ctx, f.Path, "-v", "error", "-nostdin", "-i", path, "-f", "ffmetadata", "-").Output()
	if err != nil {
		return nil, err
	}
	return parseMetadata(string(out)), nil
}

// parseMetadata reads ffmpeg's metadata format: a name=value line per tag,
// with =, ;, #, \ and new lines escaped by a backslash, and comment lines
// starting with ; or #. The file's own tags come before the first [STREAM]
// or [CHAPTER] section.
func parseMetadata(s string) map[string]string {
	tags := map[string]string{}
	var name, value strings.Builder
	inValue, escaped, comment, lineStart := false, false, false, true
	end := func() {
		if inValue && !comment {
			tags[strings.ToLower(name.String())] = value.String()
		}
		name.Reset()
		value.Reset()
		inValue, comment, lineStart = false, false, true
	}
	for _, r := range s {
		if lineStart {
			if r == '[' {
				break
			}
			comment, lineStart = r == ';' || r == '#', false
		}
		switch {
		case escaped:
			escaped = false
		case r == '\\':
			escaped = true
			continue
		case r == '\n':
			end()
			continue
		case r == '=' && !inValue:
			inValue = true
			continue
		}
		if inValue {
			value.WriteRune(r)
		} else {
			name.WriteRune(r)
		}
	}
	end()
	return tags
}
