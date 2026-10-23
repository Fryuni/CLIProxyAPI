package logging

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"gocloud.dev/blob"
	_ "gocloud.dev/blob/fileblob"
	_ "gocloud.dev/blob/gcsblob"
	_ "gocloud.dev/blob/s3blob"
	"gocloud.dev/gcerrors"
)

// requestLogUploadInterval is how often the uploader retries logs left behind by failed uploads.
const requestLogUploadInterval = time.Minute

// completedRequestLogPattern matches finished request and error log file names produced by generateFilename
// and createUniqueLogFile, e.g. v1-responses-2025-12-23T195811-a1b2c3d4.log or ...T195811_1-a1b2c3d4.log.
var completedRequestLogPattern = regexp.MustCompile(`-\d{4}-\d{2}-\d{2}T\d{6}(_\d+)?-.+\.log$`)

// incompleteRequestLogTempPrefixes are the temp file prefixes used while a request log is being assembled.
var incompleteRequestLogTempPrefixes = []string{"request-body-", "response-body-", "request-log-"}

// requestLogUploader moves completed request logs from the local logs directory to a bucket.
type requestLogUploader struct {
	logsDir   string
	bucketURL string
	bucket    *blob.Bucket
	ctx       context.Context
	cancel    context.CancelFunc
	kick      chan struct{}
	stop      chan struct{}
	done      chan struct{}
}

// SetRemoteBucket configures the bucket that completed request logs are uploaded to.
// The URL uses gocloud.dev syntax (gs://, s3://, file://). An empty URL disables uploads
// and keeps logs locally. Changing the URL stops the previous uploader before starting the new one.
func (l *FileRequestLogger) SetRemoteBucket(bucketURL string) error {
	if l == nil {
		return nil
	}
	bucketURL = strings.TrimSpace(bucketURL)

	l.uploaderMu.Lock()
	defer l.uploaderMu.Unlock()

	if l.uploader != nil && l.uploader.bucketURL == bucketURL {
		return nil
	}
	if l.uploader != nil {
		// Abort the old uploader without draining; files it leaves behind are picked up by the next one.
		canceled, cancel := context.WithCancel(context.Background())
		cancel()
		l.uploader.close(canceled)
		l.uploader = nil
	}
	if bucketURL == "" {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	bucket, errOpen := blob.OpenBucket(ctx, bucketURL)
	if errOpen != nil {
		cancel()
		return fmt.Errorf("failed to open request log bucket: %w", errOpen)
	}
	uploader := &requestLogUploader{
		logsDir:   l.logsDir,
		bucketURL: bucketURL,
		bucket:    bucket,
		ctx:       ctx,
		cancel:    cancel,
		kick:      make(chan struct{}, 1),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	l.uploader = uploader
	go uploader.run()
	return nil
}

// CloseRemoteBucket uploads pending request logs and stops the uploader.
// Uploads still running when ctx is done are abandoned; their files stay local
// and are uploaded on the next start.
func (l *FileRequestLogger) CloseRemoteBucket(ctx context.Context) {
	if l == nil {
		return
	}
	l.uploaderMu.Lock()
	defer l.uploaderMu.Unlock()
	if l.uploader != nil {
		l.uploader.close(ctx)
		l.uploader = nil
	}
}

// notifyLogCompleted wakes the uploader after a request log file is finalized.
func (l *FileRequestLogger) notifyLogCompleted() {
	if l == nil {
		return
	}
	l.uploaderMu.Lock()
	uploader := l.uploader
	l.uploaderMu.Unlock()
	if uploader == nil {
		return
	}
	select {
	case uploader.kick <- struct{}{}:
	default:
	}
}

// RemoveIncompleteLogs deletes request log artifacts left behind by requests that were
// still being logged when a previous process stopped. It must run before this process
// starts serving requests, since every incomplete artifact is assumed to be orphaned.
func (l *FileRequestLogger) RemoveIncompleteLogs() {
	if l == nil {
		return
	}
	entries, errRead := os.ReadDir(l.logsDir)
	if errRead != nil {
		if !os.IsNotExist(errRead) {
			log.WithError(errRead).Warn("failed to scan request logs directory")
		}
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(l.logsDir, name)
		if entry.IsDir() {
			if strings.HasPrefix(name, "request-log-parts-") {
				if errRemove := os.RemoveAll(path); errRemove != nil {
					log.WithError(errRemove).Warnf("failed to remove incomplete request log parts: %s", name)
				}
			}
			continue
		}
		if !isIncompleteRequestLogFile(entry) {
			continue
		}
		if errRemove := os.Remove(path); errRemove != nil && !os.IsNotExist(errRemove) {
			log.WithError(errRemove).Warnf("failed to remove incomplete request log: %s", name)
		}
	}
}

func isIncompleteRequestLogFile(entry os.DirEntry) bool {
	name := entry.Name()
	if strings.HasSuffix(name, ".tmp") {
		for _, prefix := range incompleteRequestLogTempPrefixes {
			if strings.HasPrefix(name, prefix) {
				return true
			}
		}
		return false
	}
	// An empty log is a reserved name whose content was never moved in.
	if !completedRequestLogPattern.MatchString(name) {
		return false
	}
	info, errInfo := entry.Info()
	return errInfo == nil && info.Size() == 0
}

func (u *requestLogUploader) run() {
	defer close(u.done)
	ticker := time.NewTicker(requestLogUploadInterval)
	defer ticker.Stop()

	u.uploadCompleted()
	for {
		select {
		case <-u.kick:
		case <-ticker.C:
		case <-u.stop:
			u.uploadCompleted()
			return
		}
		u.uploadCompleted()
	}
}

// close drains pending uploads, waiting until ctx is done, then releases the bucket.
func (u *requestLogUploader) close(ctx context.Context) {
	close(u.stop)
	select {
	case <-u.done:
	case <-ctx.Done():
		u.cancel()
		<-u.done
	}
	u.cancel()
	if errClose := u.bucket.Close(); errClose != nil {
		log.WithError(errClose).Warn("failed to close request log bucket")
	}
}

// uploadCompleted uploads every completed request log in the logs directory and deletes each uploaded file.
func (u *requestLogUploader) uploadCompleted() {
	entries, errRead := os.ReadDir(u.logsDir)
	if errRead != nil {
		if !os.IsNotExist(errRead) {
			log.WithError(errRead).Warn("failed to scan request logs directory for upload")
		}
		return
	}
	for _, entry := range entries {
		if u.ctx.Err() != nil {
			return
		}
		name := entry.Name()
		if entry.IsDir() || !completedRequestLogPattern.MatchString(name) {
			continue
		}
		// Empty files are names reserved by requests that are still being logged.
		if info, errInfo := entry.Info(); errInfo != nil || info.Size() == 0 {
			continue
		}
		path := filepath.Join(u.logsDir, name)
		if errUpload := u.upload(path, name); errUpload != nil {
			if errors.Is(errUpload, os.ErrNotExist) {
				continue
			}
			log.WithError(errUpload).Warnf("failed to upload request log: %s", name)
			continue
		}
		if errRemove := os.Remove(path); errRemove != nil && !os.IsNotExist(errRemove) {
			log.WithError(errRemove).Warnf("failed to remove uploaded request log: %s", name)
		}
	}
}

// upload stores the log at path under name without replacing existing objects. Local names are unique only
// within one directory at one time, so when instances share a bucket or a name is reused after an earlier
// upload, the log is stored under the next free sequenced name instead.
func (u *requestLogUploader) upload(path, name string) error {
	file, errOpen := os.Open(path)
	if errOpen != nil {
		return errOpen
	}
	defer func() {
		if errClose := file.Close(); errClose != nil {
			log.WithError(errClose).Warn("failed to close request log file after upload")
		}
	}()

	key := name
	for seq := 1; ; seq++ {
		errUpload := u.uploadAs(file, key)
		if gcerrors.Code(errUpload) != gcerrors.FailedPrecondition {
			return errUpload
		}
		if seq > 1000 {
			return fmt.Errorf("too many conflicting remote request logs for %s", name)
		}
		if _, errSeek := file.Seek(0, io.SeekStart); errSeek != nil {
			return errSeek
		}
		key = sequencedLogFilename(name, seq)
	}
}

func (u *requestLogUploader) uploadAs(file io.Reader, key string) error {
	ctx, cancel := context.WithCancel(u.ctx)
	defer cancel()
	writer, errWriter := u.bucket.NewWriter(ctx, key, &blob.WriterOptions{ContentType: "text/plain; charset=utf-8", IfNotExist: true})
	if errWriter != nil {
		return errWriter
	}
	if _, errCopy := io.Copy(writer, file); errCopy != nil {
		// Canceling before Close aborts the write so a partial object is not committed.
		cancel()
		_ = writer.Close()
		return errCopy
	}
	return writer.Close()
}
