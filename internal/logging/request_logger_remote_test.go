package logging

import (
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"gocloud.dev/blob"
)

func fileBucketURL(t *testing.T, dir string) string {
	t.Helper()
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(dir)}).String()
}

func bucketObjects(t *testing.T, bucketURL string) map[string]string {
	t.Helper()
	ctx := context.Background()
	bucket, errOpen := blob.OpenBucket(ctx, bucketURL)
	if errOpen != nil {
		t.Fatalf("open bucket: %v", errOpen)
	}
	defer func() { _ = bucket.Close() }()

	objects := make(map[string]string)
	iter := bucket.List(nil)
	for {
		obj, errNext := iter.Next(ctx)
		if errNext != nil {
			break
		}
		data, errRead := bucket.ReadAll(ctx, obj.Key)
		if errRead != nil {
			t.Fatalf("read %s: %v", obj.Key, errRead)
		}
		objects[obj.Key] = string(data)
	}
	return objects
}

func localNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, errRead := os.ReadDir(dir)
	if errRead != nil {
		t.Fatalf("read dir: %v", errRead)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if errWrite := os.WriteFile(path, []byte(content), 0o644); errWrite != nil {
		t.Fatalf("write %s: %v", path, errWrite)
	}
}

func TestStartupRemovesIncompleteAndUploadsCompletedLogs(t *testing.T) {
	logsDir := t.TempDir()
	bucketURL := fileBucketURL(t, t.TempDir())

	completed := "v1-responses-2025-12-23T195811-a1b2c3d4.log"
	completedError := "error-v1-chat-completions-2025-12-23T195811_1-deadbeef.log"
	reserved := "v1-responses-2025-12-23T195812-cafebabe.log"
	writeTestFile(t, filepath.Join(logsDir, completed), "complete log")
	writeTestFile(t, filepath.Join(logsDir, completedError), "complete error log")
	writeTestFile(t, filepath.Join(logsDir, reserved), "")
	writeTestFile(t, filepath.Join(logsDir, "request-body-1.tmp"), "partial")
	writeTestFile(t, filepath.Join(logsDir, "response-body-1.tmp"), "partial")
	writeTestFile(t, filepath.Join(logsDir, "request-log-1.tmp"), "partial")
	writeTestFile(t, filepath.Join(logsDir, "main.log"), "application log")
	writeTestFile(t, filepath.Join(logsDir, "main-2025-12-23T19-58-11.000.log"), "rotated application log")
	partsDir := filepath.Join(logsDir, "request-log-parts-api-response-123")
	if errMkdir := os.MkdirAll(partsDir, 0o755); errMkdir != nil {
		t.Fatalf("mkdir parts: %v", errMkdir)
	}
	writeTestFile(t, filepath.Join(partsDir, "part-1.tmp"), "partial")

	logger := NewFileRequestLogger(true, logsDir, "", 0)
	logger.RemoveIncompleteLogs()
	if errSet := logger.SetRemoteBucket(bucketURL); errSet != nil {
		t.Fatalf("SetRemoteBucket: %v", errSet)
	}
	logger.CloseRemoteBucket(context.Background())

	objects := bucketObjects(t, bucketURL)
	if len(objects) != 2 || objects[completed] != "complete log" || objects[completedError] != "complete error log" {
		t.Fatalf("unexpected bucket objects: %v", objects)
	}
	got := strings.Join(localNames(t, logsDir), ",")
	if got != "main-2025-12-23T19-58-11.000.log,main.log" {
		t.Fatalf("unexpected local files: %s", got)
	}
}

func TestLogRequestUploadsAndRemovesLocalFile(t *testing.T) {
	logsDir := t.TempDir()
	bucketURL := fileBucketURL(t, t.TempDir())

	logger := NewFileRequestLogger(true, logsDir, "", 0)
	if errSet := logger.SetRemoteBucket(bucketURL); errSet != nil {
		t.Fatalf("SetRemoteBucket: %v", errSet)
	}
	errLog := logger.LogRequest("/v1/responses", "POST", map[string][]string{"Content-Type": {"application/json"}}, []byte(`{"input":"hello"}`), 200, nil, []byte(`{"output":"world"}`), nil, nil, nil, nil, nil, "req-12345678", time.Now(), time.Now())
	if errLog != nil {
		t.Fatalf("LogRequest: %v", errLog)
	}
	logger.CloseRemoteBucket(context.Background())

	objects := bucketObjects(t, bucketURL)
	if len(objects) != 1 {
		t.Fatalf("expected one uploaded log, got %v", objects)
	}
	for key, content := range objects {
		if !strings.HasPrefix(key, "v1-responses-") || !strings.HasSuffix(key, "-12345678.log") {
			t.Fatalf("unexpected object key %q", key)
		}
		if !strings.Contains(content, `{"input":"hello"}`) || !strings.Contains(content, `{"output":"world"}`) {
			t.Fatalf("uploaded log is missing request or response body:\n%s", content)
		}
	}
	if names := localNames(t, logsDir); len(names) != 0 {
		t.Fatalf("expected local logs to be removed, got %v", names)
	}
}

func TestStreamingLogUploadsAndRemovesLocalFile(t *testing.T) {
	logsDir := t.TempDir()
	bucketURL := fileBucketURL(t, t.TempDir())

	logger := NewFileRequestLogger(true, logsDir, "", 0)
	if errSet := logger.SetRemoteBucket(bucketURL); errSet != nil {
		t.Fatalf("SetRemoteBucket: %v", errSet)
	}
	writer, errStart := logger.LogStreamingRequest("/v1/chat/completions", "POST", nil, []byte(`{"stream":true}`), "req-87654321")
	if errStart != nil {
		t.Fatalf("LogStreamingRequest: %v", errStart)
	}
	if errStatus := writer.WriteStatus(200, nil); errStatus != nil {
		t.Fatalf("WriteStatus: %v", errStatus)
	}
	writer.WriteChunkAsync([]byte("data: chunk\n\n"))
	if errClose := writer.Close(); errClose != nil {
		t.Fatalf("Close: %v", errClose)
	}
	logger.CloseRemoteBucket(context.Background())

	objects := bucketObjects(t, bucketURL)
	if len(objects) != 1 {
		t.Fatalf("expected one uploaded log, got %v", objects)
	}
	for _, content := range objects {
		if !strings.Contains(content, "data: chunk") {
			t.Fatalf("uploaded log is missing streamed chunk:\n%s", content)
		}
	}
	if names := localNames(t, logsDir); len(names) != 0 {
		t.Fatalf("expected local logs to be removed, got %v", names)
	}
}

func TestWriteLogFileAtomicallyLeavesNothingOnFailure(t *testing.T) {
	dir := t.TempDir()
	errWrite := writeLogFileAtomically(dir, "v1-responses-2025-12-23T195811-a1b2c3d4.log", func(w io.Writer) error {
		_, _ = w.Write([]byte("partial"))
		return errors.New("boom")
	})
	if errWrite == nil {
		t.Fatal("expected write error")
	}
	if names := localNames(t, dir); len(names) != 0 {
		t.Fatalf("expected no files after failed write, got %v", names)
	}
}

func TestLogsStayLocalWithoutBucket(t *testing.T) {
	logsDir := t.TempDir()
	logger := NewFileRequestLogger(true, logsDir, "", 0)
	errLog := logger.LogRequest("/v1/responses", "POST", nil, []byte("{}"), 200, nil, []byte("{}"), nil, nil, nil, nil, nil, "req-1", time.Now(), time.Now())
	if errLog != nil {
		t.Fatalf("LogRequest: %v", errLog)
	}
	names := localNames(t, logsDir)
	if len(names) != 1 || !completedRequestLogPattern.MatchString(names[0]) {
		t.Fatalf("expected one completed local log, got %v", names)
	}
}

func TestUploadDoesNotReplaceExistingRemoteLog(t *testing.T) {
	logsDir := t.TempDir()
	bucketURL := fileBucketURL(t, t.TempDir())

	name := "v1-responses-2025-12-23T195811-a1b2c3d4.log"
	sequenced := "v1-responses-2025-12-23T195811_1-a1b2c3d4.log"
	bucket, errOpen := blob.OpenBucket(context.Background(), bucketURL)
	if errOpen != nil {
		t.Fatalf("open bucket: %v", errOpen)
	}
	if errWrite := bucket.WriteAll(context.Background(), name, []byte("other instance log"), nil); errWrite != nil {
		t.Fatalf("seed bucket: %v", errWrite)
	}
	_ = bucket.Close()
	writeTestFile(t, filepath.Join(logsDir, name), "local log")

	logger := NewFileRequestLogger(true, logsDir, "", 0)
	if errSet := logger.SetRemoteBucket(bucketURL); errSet != nil {
		t.Fatalf("SetRemoteBucket: %v", errSet)
	}
	logger.CloseRemoteBucket(context.Background())

	objects := bucketObjects(t, bucketURL)
	if len(objects) != 2 || objects[name] != "other instance log" || objects[sequenced] != "local log" {
		t.Fatalf("unexpected bucket objects: %v", objects)
	}
	if names := localNames(t, logsDir); len(names) != 0 {
		t.Fatalf("expected local logs to be removed, got %v", names)
	}
}

func TestSetRemoteBucketRejectsLogsDirectory(t *testing.T) {
	logsDir := t.TempDir()
	logger := NewFileRequestLogger(true, logsDir, "", 0)
	defer logger.CloseRemoteBucket(context.Background())

	sameDir := fileBucketURL(t, logsDir)
	for _, bucketURL := range []string{sameDir, sameDir + "?prefix=archived-", fileBucketURL(t, filepath.Join(logsDir, "sub")) + "?prefix=../"} {
		if errSet := logger.SetRemoteBucket(bucketURL); errSet == nil {
			t.Fatalf("expected %s to be rejected", bucketURL)
		}
	}
	alias := filepath.Join(t.TempDir(), "logs-alias")
	if errLink := os.Symlink(logsDir, alias); errLink == nil {
		if errSet := logger.SetRemoteBucket(fileBucketURL(t, alias)); errSet == nil {
			t.Fatal("expected a symlink to the logs directory to be rejected")
		}
	} else {
		t.Logf("skipping symlink case: %v", errLink)
	}
	if errSet := logger.SetRemoteBucket(sameDir + "?prefix=archive/"); errSet != nil {
		t.Fatalf("expected a subdirectory prefix to be accepted: %v", errSet)
	}
}

func TestCompletedLogKeepsDirectCreateMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not meaningful on Windows")
	}
	dir := t.TempDir()
	errWrite := writeLogFileAtomically(dir, "v1-responses-2025-12-23T195811-a1b2c3d4.log", func(w io.Writer) error {
		_, errWrite := w.Write([]byte("complete"))
		return errWrite
	})
	if errWrite != nil {
		t.Fatalf("writeLogFileAtomically: %v", errWrite)
	}
	info, errStat := os.Stat(filepath.Join(dir, "v1-responses-2025-12-23T195811-a1b2c3d4.log"))
	if errStat != nil {
		t.Fatalf("stat: %v", errStat)
	}
	// Compare with a file created the way logs were created before atomic finalization.
	reference, _, errReference := createUniqueLogFile(dir, "reference-2025-12-23T195811-a1b2c3d4.log")
	if errReference != nil {
		t.Fatalf("create reference: %v", errReference)
	}
	referenceInfo, errReferenceStat := reference.Stat()
	_ = reference.Close()
	if errReferenceStat != nil {
		t.Fatalf("stat reference: %v", errReferenceStat)
	}
	if mode, want := info.Mode().Perm(), referenceInfo.Mode().Perm(); mode != want {
		t.Fatalf("expected mode %o, got %o", want, mode)
	}
}
