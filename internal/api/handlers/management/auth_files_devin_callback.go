package management

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

// Each login owns a plaintext loopback listener, independently of the main
// server's TLS configuration and bind address. The caller closes it on completion.
func startDevinOAuthCallback(authDir, expectedState string) (string, func(), error) {
	listener, errListen := net.Listen("tcp4", "127.0.0.1:0")
	if errListen != nil {
		return "", nil, fmt.Errorf("listen for Devin OAuth callback: %w", errListen)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		query := r.URL.Query()
		state := strings.TrimSpace(query.Get("state"))
		code := strings.TrimSpace(query.Get("code"))
		errMessage := strings.TrimSpace(query.Get("error"))
		if errMessage == "" {
			errMessage = strings.TrimSpace(query.Get("error_description"))
		}
		if state != expectedState || (code == "" && errMessage == "") {
			http.Error(w, "invalid OAuth callback", http.StatusBadRequest)
			return
		}
		if _, errWrite := WriteOAuthCallbackFileForPendingSession(authDir, "devin", state, code, errMessage); errWrite != nil {
			http.Error(w, "invalid or expired OAuth callback", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "Authentication callback received. You can close this window.")
	})
	server := &http.Server{Handler: mux}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if errServe := server.Serve(listener); errServe != nil && !errors.Is(errServe, http.ErrServerClosed) {
			log.WithError(errServe).Warn("Devin OAuth callback server stopped unexpectedly")
		}
	}()
	stop := func() {
		// Let an accepted browser callback finish writing its response before
		// shutting down the listener at the end of credential acquisition.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if errShutdown := server.Shutdown(ctx); errShutdown != nil {
			if errClose := server.Close(); errClose != nil {
				log.WithError(errClose).Warn("failed to close Devin OAuth callback server")
			}
		}
		<-done
	}
	return "http://" + listener.Addr().String() + "/callback", stop, nil
}
