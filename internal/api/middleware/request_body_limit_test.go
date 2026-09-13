package middleware

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
)

func TestRequestBodyLimitRunsBeforeCapture(t *testing.T) {
	for _, knownLength := range []bool{true, false} {
		router := gin.New()
		router.Use(RequestBodyLimit("/v1/audio/transcriptions", 64))
		captured := false
		router.Use(func(c *gin.Context) {
			captured = true
			_, err := captureRequestInfo(c, true)
			var limitErr *http.MaxBytesError
			if !errors.As(err, &limitErr) {
				t.Errorf("capture error = %v", err)
			}
			c.Next()
		})
		router.POST("/v1/audio/transcriptions", func(c *gin.Context) {
			_, err := io.ReadAll(c.Request.Body)
			var limitErr *http.MaxBytesError
			if !errors.As(err, &limitErr) {
				t.Errorf("handler lost size error: %v", err)
			}
			c.Status(http.StatusRequestEntityTooLarge)
		})
		req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", bytes.NewReader(bytes.Repeat([]byte("a"), 4096)))
		if !knownLength {
			req.ContentLength = -1
		}
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusRequestEntityTooLarge || captured == knownLength {
			t.Fatalf("knownLength=%v status=%d captured=%v", knownLength, rr.Code, captured)
		}
	}
}

func TestRequestBodyLimitBoundsDecodedLogCapture(t *testing.T) {
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer encoder.Close()
	compressed := encoder.EncodeAll(bytes.Repeat([]byte("a"), 4096), nil)
	router := gin.New()
	router.Use(RequestBodyLimit("/v1/audio/transcriptions", 64))
	router.POST("/v1/audio/transcriptions", func(c *gin.Context) {
		info, errCapture := captureRequestInfo(c, true)
		if errCapture != nil {
			t.Fatal(errCapture)
		}
		if !bytes.HasPrefix(info.Body, bytes.Repeat([]byte("a"), 64)) || len(info.Body) > 128 {
			t.Errorf("captured %d decoded bytes", len(info.Body))
		}
		raw, errRead := io.ReadAll(c.Request.Body)
		if errRead != nil || !bytes.Equal(raw, compressed) {
			t.Errorf("original compressed body not preserved: %v", errRead)
		}
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", bytes.NewReader(compressed))
	req.Header.Set("Content-Encoding", "zstd")
	router.ServeHTTP(httptest.NewRecorder(), req)
}
