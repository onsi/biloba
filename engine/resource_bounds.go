package engine

import (
	"bytes"
	"context"
	"fmt"
	"io"
)

const boundedReadChunkSize = 64 << 10

type contentLimitError struct{ limit int64 }

func (e *contentLimitError) Error() string {
	return fmt.Sprintf("content size exceeds limit %d", e.limit)
}

func readBounded(ctx context.Context, reader io.Reader, maxBytes int64) ([]byte, error) {
	var body bytes.Buffer
	chunk := make([]byte, boundedReadChunkSize)
	remaining := maxBytes + 1
	for remaining > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		readSize := min(int64(len(chunk)), remaining)
		n, err := reader.Read(chunk[:readSize])
		if n > 0 {
			_, _ = body.Write(chunk[:n])
			remaining -= int64(n)
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if n == 0 {
			return nil, io.ErrNoProgress
		}
	}
	if int64(body.Len()) > maxBytes {
		return nil, &contentLimitError{limit: maxBytes}
	}
	return body.Bytes(), nil
}
