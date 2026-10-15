package uploader

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serveLogged serves one request through requestLogger, followed by any extra middleware,
// and returns the decoded log line and the response.
func serveLogged(t *testing.T, req *http.Request, h echo.HandlerFunc, mw ...echo.MiddlewareFunc) (map[string]any, *httptest.ResponseRecorder) {
	t.Helper()

	var buf bytes.Buffer

	e := echo.New()
	e.Use(requestLogger(&buf))
	e.Use(mw...)
	e.Any("/x", h)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	var got map[string]any

	require.NoError(t, json.Unmarshal(buf.Bytes(), &got), "log line must be valid JSON: %q", buf.String())

	return got, rec
}

func TestRequestLogger(t *testing.T) {
	tests := []struct {
		name       string
		handler    echo.HandlerFunc
		wantStatus int
		wantError  string
	}{
		{
			name:       "success",
			handler:    func(c echo.Context) error { return c.String(http.StatusOK, "ok") },
			wantStatus: http.StatusOK,
		},
		{
			name:       "http error",
			handler:    func(echo.Context) error { return echo.NewHTTPError(http.StatusNotFound, "missing") },
			wantStatus: http.StatusNotFound,
			wantError:  "code=404, message=missing",
		},
		{
			name:       "plain error is logged as 500",
			handler:    func(echo.Context) error { return errors.New("boom") },
			wantStatus: http.StatusInternalServerError,
			wantError:  "boom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/x?a=1", nil)
			req.Header.Set("User-Agent", "test-agent")

			got, rec := serveLogged(t, req, tt.handler)

			assert.EqualValues(t, tt.wantStatus, got["status"])
			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Equal(t, tt.wantError, got["error"])
			assert.Equal(t, http.MethodGet, got["method"])
			assert.Equal(t, "/x?a=1", got["uri"])
			assert.Equal(t, "test-agent", got["user_agent"])

			for _, k := range []string{"time", "id", "remote_ip", "host", "latency", "latency_human", "bytes_in", "bytes_out"} {
				assert.Contains(t, got, k)
			}
		})
	}
}

func TestRequestLoggerBytesIn(t *testing.T) {
	tests := []struct {
		name string
		body string
		want float64
	}{
		{name: "no body", want: 0},
		{name: "with body", body: "hello world", want: 11},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body io.Reader
			if tt.body != "" {
				body = strings.NewReader(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/x", body)
			if tt.body != "" {
				req.Header.Set(echo.HeaderContentLength, strconv.Itoa(len(tt.body)))
			}

			got, _ := serveLogged(t, req, func(c echo.Context) error { return c.NoContent(http.StatusOK) })

			assert.Equal(t, tt.want, got["bytes_in"], "bytes_in must be a JSON number")
		})
	}
}

func TestRequestLoggerLogsRejectedBodyLimit(t *testing.T) {
	body := "more than one byte"
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentLength, strconv.Itoa(len(body)))

	got, rec := serveLogged(t, req,
		func(c echo.Context) error { return c.NoContent(http.StatusOK) },
		middleware.BodyLimit("1B"))

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.EqualValues(t, http.StatusRequestEntityTooLarge, got["status"])
	assert.NotEmpty(t, got["error"])
}
