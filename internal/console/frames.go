// Package console streams a bot's stdout/stderr to clients and carries their
// input to the bot process.
package console

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// Stream identifies the origin of a log frame.
type Stream string

const (
	Stdout Stream = "stdout"
	Stderr Stream = "stderr"
)

// maxFrame bounds a single Docker frame we are willing to buffer.
const maxFrame = 1 << 20

// Frame is one decoded log entry.
type Frame struct {
	Stream Stream
	Time   time.Time // zero if the entry carried no timestamp
	Data   []byte
}

// Decoder reads Docker's multiplexed stream format (used when the container
// has no TTY): an 8-byte header {stream, 0, 0, 0, size(BE uint32)} per frame.
// It is correct for arbitrary read boundaries (short reads, one byte at a time).
type Decoder struct {
	r          io.Reader
	timestamps bool
	hdr        [8]byte
}

// NewDecoder wraps r. If timestamps is true each payload is expected to start
// with an RFC 3339 timestamp and a space (Docker "Timestamps: true").
func NewDecoder(r io.Reader, timestamps bool) *Decoder { return &Decoder{r: r, timestamps: timestamps} }

// Next returns the next frame, io.EOF at a clean end of stream, or
// io.ErrUnexpectedEOF if the stream ends mid-frame.
func (d *Decoder) Next() (Frame, error) {
	for {
		if _, err := io.ReadFull(d.r, d.hdr[:]); err != nil {
			if err == io.EOF {
				return Frame{}, io.EOF
			}
			return Frame{}, io.ErrUnexpectedEOF
		}
		var st Stream
		switch d.hdr[0] {
		case 1:
			st = Stdout
		case 2:
			st = Stderr
		case 0: // stdin echo; not produced for logs, skip defensively
			st = ""
		default:
			return Frame{}, fmt.Errorf("invalid stream id %d", d.hdr[0])
		}
		if d.hdr[1] != 0 || d.hdr[2] != 0 || d.hdr[3] != 0 {
			return Frame{}, errors.New("corrupt frame header")
		}
		n := binary.BigEndian.Uint32(d.hdr[4:])
		if n > maxFrame {
			return Frame{}, fmt.Errorf("frame of %d bytes exceeds limit", n)
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(d.r, buf); err != nil {
			return Frame{}, io.ErrUnexpectedEOF
		}
		if st == "" {
			continue
		}
		f := Frame{Stream: st, Data: buf}
		if d.timestamps {
			if i := strings.IndexByte(string(buf[:min(len(buf), 40)]), ' '); i > 0 {
				if ts, err := time.Parse(time.RFC3339Nano, string(buf[:i])); err == nil {
					f.Time, f.Data = ts, buf[i+1:]
				}
			}
		}
		return f, nil
	}
}
