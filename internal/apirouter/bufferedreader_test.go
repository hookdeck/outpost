package apirouter

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"testing"
	"testing/iotest"
)

type closeTracker struct {
	io.Reader
	closed bool
}

func (c *closeTracker) Close() error {
	c.closed = true
	return nil
}

func TestBufferedReader_NewReadCloserWithRest(t *testing.T) {
	sizes := []int{0, 100, MaxRequestBodySize, MaxRequestBodySize + 1, MaxRequestBodySize + 2, 300 * 1024}

	for _, size := range sizes {
		t.Run(fmt.Sprintf("%d bytes", size), func(t *testing.T) {
			content := bytes.Repeat([]byte("abcdefghij"), size/10+1)[:size]
			source := &closeTracker{Reader: iotest.HalfReader(bytes.NewReader(content))}

			br, err := NewBufferedReader(source)
			if err != nil {
				t.Fatalf("NewBufferedReader() error = %v", err)
			}

			wantBuffered := min(size, MaxRequestBodySize+1)
			if got := len(br.Bytes()); got != wantBuffered {
				t.Errorf("buffered %d bytes, want %d", got, wantBuffered)
			}

			body := br.NewReadCloserWithRest(source)
			got, err := io.ReadAll(body)
			if err != nil {
				t.Fatalf("reading body error = %v", err)
			}
			if !bytes.Equal(got, content) {
				t.Errorf("body has %d bytes, want the %d bytes sent", len(got), size)
			}

			// The buffer stays available for logging after the body is read.
			if !bytes.Equal(br.Bytes(), content[:wantBuffered]) {
				t.Error("buffer changed after the body was read")
			}

			if err := body.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
			if !source.closed {
				t.Error("Close() did not close the original body")
			}
		})
	}

	t.Run("read error after the buffered prefix", func(t *testing.T) {
		readErr := errors.New("connection reset")
		prefix := bytes.Repeat([]byte("a"), MaxRequestBodySize+1)
		source := &closeTracker{Reader: io.MultiReader(bytes.NewReader(prefix), iotest.ErrReader(readErr))}

		br, err := NewBufferedReader(source)
		if err != nil {
			t.Fatalf("NewBufferedReader() error = %v", err)
		}

		got, err := io.ReadAll(br.NewReadCloserWithRest(source))
		if !errors.Is(err, readErr) {
			t.Errorf("reading body error = %v, want %v", err, readErr)
		}
		if !bytes.Equal(got, prefix) {
			t.Errorf("body has %d bytes before the error, want %d", len(got), len(prefix))
		}
	})
}
