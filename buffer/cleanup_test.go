package buffer

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuffer_responseCleanup(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 2048)
	tests := []struct {
		name     string
		method   string
		limit    int64
		abort    bool
		retry    string
		code     int
		attempts int
		body     []byte
	}{
		{name: "success", method: http.MethodPost, code: http.StatusOK, attempts: 1, body: payload},
		{name: "panic", method: http.MethodPost, abort: true, attempts: 1},
		{name: "limit", method: http.MethodPost, limit: 3072, code: http.StatusInternalServerError, attempts: 1},
		{name: "head", method: http.MethodHead, code: http.StatusOK, attempts: 1},
		{name: "retry", method: http.MethodPost, retry: "ResponseCode() == 502 && Attempts() <= 1", code: http.StatusOK, attempts: 2, body: payload},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("TMPDIR", dir)

			attempts := 0
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts++

				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				assert.Equal(t, payload, body)

				if test.retry != "" && attempts == 1 {
					w.WriteHeader(http.StatusBadGateway)
				} else {
					w.WriteHeader(http.StatusOK)
				}

				_, err = w.Write(payload)
				require.NoError(t, err)

				files, err := os.ReadDir(dir)
				require.NoError(t, err)
				require.NotEmpty(t, files)

				if test.abort {
					panic(http.ErrAbortHandler)
				}

				if test.limit > 0 {
					_, _ = w.Write(payload)
				}
			})

			options := []Option{MemRequestBodyBytes(1024), MemResponseBodyBytes(1024), MaxResponseBodyBytes(test.limit)}
			if test.retry != "" {
				options = append(options, Retry(test.retry))
			}

			middleware, err := New(next, options...)
			require.NoError(t, err)

			request := httptest.NewRequest(test.method, "http://example.test/", bytes.NewReader(payload))
			recorder := httptest.NewRecorder()

			if test.abort {
				assert.PanicsWithValue(t, http.ErrAbortHandler, func() {
					middleware.ServeHTTP(recorder, request)
				})
			} else {
				middleware.ServeHTTP(recorder, request)
				assert.Equal(t, test.code, recorder.Code)
			}

			if test.body != nil {
				assert.Equal(t, test.body, recorder.Body.Bytes())
			}

			assert.Equal(t, test.attempts, attempts)

			files, err := os.ReadDir(dir)
			require.NoError(t, err)
			assert.Empty(t, files)
		})
	}
}
