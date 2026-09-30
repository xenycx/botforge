package console

import (
	"bytes"
	"encoding/binary"
	"io"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

func frame(stream byte, payload string) []byte {
	h := make([]byte, 8)
	h[0] = stream
	binary.BigEndian.PutUint32(h[4:], uint32(len(payload)))
	return append(h, payload...)
}

func TestDecoderSeparatesStreamsAndParsesTimestamps(t *testing.T) {
	var b bytes.Buffer
	b.Write(frame(1, "2026-09-30T02:19:36.161234567Z hello out\n"))
	b.Write(frame(2, "2026-09-30T02:19:37.000000000Z oops err\n"))
	b.Write(frame(1, "no timestamp here"))
	d := NewDecoder(&b, true)
	f, err := d.Next()
	if err != nil || f.Stream != Stdout || string(f.Data) != "hello out\n" || f.Time.Nanosecond() != 161234567 {
		t.Fatalf("%+v %v", f, err)
	}
	f, _ = d.Next()
	if f.Stream != Stderr || string(f.Data) != "oops err\n" || !f.Time.Equal(time.Date(2026, 9, 30, 2, 19, 37, 0, time.UTC)) {
		t.Fatalf("%+v", f)
	}
	f, _ = d.Next()
	if f.Stream != Stdout || string(f.Data) != "no timestamp here" || !f.Time.IsZero() {
		t.Fatalf("%+v", f)
	}
	if _, err := d.Next(); err != io.EOF {
		t.Fatal(err)
	}
}

func TestDecoderIsCorrectAcrossArbitraryReadBoundaries(t *testing.T) {
	var b bytes.Buffer
	var want []string
	for i := 0; i < 50; i++ {
		s := strings.Repeat("x", i*7) + "\n"
		want = append(want, s)
		b.Write(frame(byte(1+i%2), s))
	}
	for name, r := range map[string]io.Reader{
		"onebyte": iotest.OneByteReader(bytes.NewReader(b.Bytes())),
		"half":    iotest.HalfReader(bytes.NewReader(b.Bytes())),
		"dataerr": iotest.DataErrReader(bytes.NewReader(b.Bytes())),
	} {
		d := NewDecoder(r, false)
		for i, w := range want {
			f, err := d.Next()
			if err != nil || string(f.Data) != w || f.Stream != Stream([]string{"stdout", "stderr"}[i%2]) {
				t.Fatalf("%s frame %d: %v %q", name, i, err, f.Data)
			}
		}
		if _, err := d.Next(); err != io.EOF {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestDecoderRejectsCorruptAndTruncatedInput(t *testing.T) {
	cases := map[string][]byte{
		"bad stream":     append([]byte{9, 0, 0, 0, 0, 0, 0, 1}, 'x'),
		"nonzero pad":    append([]byte{1, 1, 0, 0, 0, 0, 0, 1}, 'x'),
		"huge":           {1, 0, 0, 0, 0xff, 0xff, 0xff, 0xff},
		"truncated hdr":  {1, 0, 0},
		"truncated body": frame(1, "hello")[:9],
	}
	for name, in := range cases {
		if _, err := NewDecoder(bytes.NewReader(in), false).Next(); err == nil || err == io.EOF {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestDecoderSkipsStdinFramesAndBinaryData(t *testing.T) {
	var b bytes.Buffer
	b.Write(frame(0, "echo"))
	b.Write(frame(1, "\x00\xff\xfebinary"))
	f, err := NewDecoder(&b, false).Next()
	if err != nil || f.Stream != Stdout || !bytes.HasSuffix(f.Data, []byte("binary")) {
		t.Fatalf("%+v %v", f, err)
	}
}
