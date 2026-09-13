package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
)

// ReadRequestBody reads the incoming request body and decodes supported
// Content-Encoding values before handlers inspect JSON fields.
func ReadRequestBody(c *gin.Context) ([]byte, error) {
	return ReadRequestBodyWithLimit(c, 0)
}

// ReadRequestBodyWithLimit bounds the wire body and every Content-Encoding decoding
// step. A nonpositive limit preserves the unbounded behavior of ReadRequestBody.
func ReadRequestBodyWithLimit(c *gin.Context, limit int64) ([]byte, error) {
	if c.Request.Body == nil {
		return nil, nil
	}
	if limit > 0 && c.Request.ContentLength > limit {
		return nil, &http.MaxBytesError{Limit: limit}
	}
	raw, err := readBodyWithLimit(c.Request.Body, limit)
	if err != nil {
		return nil, err
	}

	encoding := ""
	if c != nil && c.Request != nil {
		encoding = strings.TrimSpace(c.Request.Header.Get("Content-Encoding"))
	}
	if encoding == "" || strings.EqualFold(encoding, "identity") {
		return raw, nil
	}

	decoded, err := decodeRequestBodyWithLimit(raw, encoding, limit)
	if err != nil {
		var limitErr *http.MaxBytesError
		if !errors.As(err, &limitErr) && json.Valid(raw) {
			return raw, nil
		}
		return nil, err
	}
	return decoded, nil
}

func decodeRequestBody(raw []byte, encoding string) ([]byte, error) {
	return decodeRequestBodyWithLimit(raw, encoding, 0)
}

func decodeRequestBodyWithLimit(raw []byte, encoding string, limit int64) ([]byte, error) {
	parts := strings.Split(encoding, ",")
	body := raw
	for i := len(parts) - 1; i >= 0; i-- {
		enc := strings.ToLower(strings.TrimSpace(parts[i]))
		switch enc {
		case "", "identity":
			continue
		case "zstd":
			decoded, err := decodeZstdRequestBodyWithLimit(body, limit)
			if err != nil {
				return nil, err
			}
			body = decoded
		default:
			return nil, fmt.Errorf("unsupported request content encoding: %s", enc)
		}
	}
	return body, nil
}

func decodeZstdRequestBody(raw []byte) ([]byte, error) {
	return decodeZstdRequestBodyWithLimit(raw, 0)
}

func decodeZstdRequestBodyWithLimit(raw []byte, limit int64) ([]byte, error) {
	var options []zstd.DOption
	if limit > 0 {
		options = append(options, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(uint64(max(limit, 1<<20))))
	}
	decoder, err := zstd.NewReader(bytes.NewReader(raw), options...)
	if err != nil {
		return nil, fmt.Errorf("failed to create zstd request decoder: %w", err)
	}
	defer decoder.Close()

	decoded, err := readBodyWithLimit(decoder, limit)
	if err != nil {
		if limit > 0 && (errors.Is(err, zstd.ErrDecoderSizeExceeded) || errors.Is(err, zstd.ErrWindowSizeExceeded)) {
			return nil, &http.MaxBytesError{Limit: limit}
		}
		return nil, fmt.Errorf("failed to decode zstd request body: %w", err)
	}
	return decoded, nil
}

func readBodyWithLimit(reader io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		return io.ReadAll(reader)
	}
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, &http.MaxBytesError{Limit: limit}
	}
	return body, nil
}
