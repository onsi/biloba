package protocol

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// MaxFrameSize bounds one protocol message before allocating its payload.
const MaxFrameSize = 16 << 20

// MalformedFrameError reports a frame whose header and payload were read in full but whose body
// did not decode.  It is the one framing failure that does not desync the stream - the reader is
// still sitting on a frame boundary - so a server can answer it and keep serving instead of
// tearing down every session sharing the pipe.  Every other error from Read means the bytes
// themselves are untrustworthy.
type MalformedFrameError struct{ Err error }

func (e *MalformedFrameError) Error() string { return "decode protocol frame: " + e.Err.Error() }
func (e *MalformedFrameError) Unwrap() error { return e.Err }

// FramedReader reads 4-byte little-endian length-prefixed JSON messages.
type FramedReader struct {
	reader io.Reader
}

func NewFramedReader(reader io.Reader) *FramedReader {
	return &FramedReader{reader: reader}
}

func (r *FramedReader) Read(value any) error {
	var header [4]byte
	if _, err := io.ReadFull(r.reader, header[:]); err != nil {
		return err
	}
	length := binary.LittleEndian.Uint32(header[:])
	if length == 0 {
		return fmt.Errorf("protocol frame is empty")
	}
	if length > MaxFrameSize {
		return fmt.Errorf("protocol frame is %d bytes; maximum is %d", length, MaxFrameSize)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r.reader, payload); err != nil {
		return fmt.Errorf("read protocol frame payload: %w", err)
	}
	if err := json.Unmarshal(payload, value); err != nil {
		return &MalformedFrameError{Err: err}
	}
	return nil
}

// FramedWriter writes complete frames atomically with respect to other callers.
type FramedWriter struct {
	writer io.Writer
	mu     sync.Mutex
}

func NewFramedWriter(writer io.Writer) *FramedWriter {
	return &FramedWriter{writer: writer}
}

func (w *FramedWriter) Write(value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode protocol frame: %w", err)
	}
	if len(payload) > MaxFrameSize {
		return fmt.Errorf("protocol frame is %d bytes; maximum is %d", len(payload), MaxFrameSize)
	}
	var header [4]byte
	binary.LittleEndian.PutUint32(header[:], uint32(len(payload)))

	w.mu.Lock()
	defer w.mu.Unlock()
	if err := writeAll(w.writer, header[:]); err != nil {
		return fmt.Errorf("write protocol frame header: %w", err)
	}
	if err := writeAll(w.writer, payload); err != nil {
		return fmt.Errorf("write protocol frame payload: %w", err)
	}
	return nil
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}
