package uploader

import (
	"encoding/json"
	"io"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// accessLogEntry is the JSON schema of one access-log line; field names match echo's default middleware.Logger format, so log parsers keep working.
type accessLogEntry struct {
	Time         string `json:"time"`
	ID           string `json:"id"`
	RemoteIP     string `json:"remote_ip"`
	Host         string `json:"host"`
	Method       string `json:"method"`
	URI          string `json:"uri"`
	UserAgent    string `json:"user_agent"`
	Status       int    `json:"status"`
	Error        string `json:"error"`
	Latency      int64  `json:"latency"`
	LatencyHuman string `json:"latency_human"`
	BytesIn      int64  `json:"bytes_in"`
	BytesOut     int64  `json:"bytes_out"`
}

// requestLogger writes one JSON access-log line per request to w.
func requestLogger(w io.Writer) echo.MiddlewareFunc {
	return middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogLatency:       true,
		LogRemoteIP:      true,
		LogHost:          true,
		LogMethod:        true,
		LogURI:           true,
		LogUserAgent:     true,
		LogStatus:        true,
		LogError:         true,
		LogContentLength: true,
		LogResponseSize:  true,
		LogRequestID:     true,
		HandleError:      true,
		LogValuesFunc: func(_ echo.Context, v middleware.RequestLoggerValues) error {
			// An empty or malformed Content-Length is logged as 0, like echo's default format.
			bytesIn, _ := strconv.ParseInt(v.ContentLength, 10, 64)

			entry := accessLogEntry{
				Time:         time.Now().Format(time.RFC3339Nano),
				ID:           v.RequestID,
				RemoteIP:     v.RemoteIP,
				Host:         v.Host,
				Method:       v.Method,
				URI:          v.URI,
				UserAgent:    v.UserAgent,
				Status:       v.Status,
				Latency:      v.Latency.Nanoseconds(),
				LatencyHuman: v.Latency.String(),
				BytesIn:      bytesIn,
				BytesOut:     v.ResponseSize,
			}
			if v.Error != nil {
				entry.Error = v.Error.Error()
			}

			// Encode emits the entry and its newline in a single Write, so concurrent lines never interleave.
			return json.NewEncoder(w).Encode(entry)
		},
	})
}
