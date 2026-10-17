package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// ---------------------------------------------------------------------------
// 1. Config
// ---------------------------------------------------------------------------

const (
	// addr is the single port the app listens on. This is the port that the
	// Cloudflare Quick Tunnel will be pointed at.
	addr = ":3333"

	// readHeaderTimeout caps how long the server waits for request headers.
	readHeaderTimeout = 5 * time.Second

	// writeTimeout caps how long a handler may take to write a response.
	writeTimeout = 10 * time.Second

	// shutdownTimeout is how long in-flight requests get to finish on Ctrl-C.
	shutdownTimeout = 5 * time.Second
)

// ---------------------------------------------------------------------------
// 2. Types
// ---------------------------------------------------------------------------

// Message is the JSON request body accepted by POST /json.
type Message struct {
	Name string `json:"name"`
	Body string `json:"body"`
}

// APIResponse is the success envelope returned by JSON handlers.
type APIResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Body    string `json:"body"`
}

// APIError is the uniform error envelope returned by every failing path.
type APIError struct {
	Status string `json:"status"`
	Error  string `json:"error"`
	Detail string `json:"detail,omitempty"`
}

// ---------------------------------------------------------------------------
// 3. Helpers
// ---------------------------------------------------------------------------

// writeJSON sets the JSON content type, writes the status code, and encodes v.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("could not encode response: %s", err)
	}
}

// writeError returns the uniform error envelope used across the API.
func writeError(w http.ResponseWriter, code int, errMsg, detail string) {
	writeJSON(w, code, APIError{
		Status: "error",
		Error:  errMsg,
		Detail: detail,
	})
}

// ---------------------------------------------------------------------------
// 4. Handlers
// ---------------------------------------------------------------------------

// getRoot serves the plain-text landing page at the exact root path.
func getRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "This is my website!\n")
}

// getHello serves a plain-text greeting, kept as a contrast to the JSON routes.
func getHello(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "Hello, HTTP!\n")
}

// handleHealth serves a JSON liveness probe, useful to confirm that a tunnel
// actually reaches the application.
func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleJSON decodes a Message, validates it, and returns a success envelope.
func handleJSON(w http.ResponseWriter, r *http.Request) {
	var msg Message

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&msg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload", err.Error())
		return
	}

	msg.Name = strings.TrimSpace(msg.Name)
	msg.Body = strings.TrimSpace(msg.Body)
	if msg.Name == "" || msg.Body == "" {
		writeError(w, http.StatusUnprocessableEntity, "validation failed", "name and body must not be empty")
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Status:  "success",
		Message: "Received message from " + msg.Name,
		Body:    msg.Body,
	})
}

// routes maps each known path to the method it accepts. It is used by the
// fallback handler to answer wrong-method requests with 405 + Allow instead of
// a plain 404 (the catch-all pattern suppresses ServeMux's automatic 405).
var routes = map[string]string{
	"/":       http.MethodGet,
	"/hello":  http.MethodGet,
	"/health": http.MethodGet,
	"/json":   http.MethodPost,
}

// notFound is the catch-all fallback. It returns 405 for a known path reached
// with the wrong method, and a JSON 404 envelope for everything else.
func notFound(w http.ResponseWriter, r *http.Request) {
	if allowed, ok := routes[r.URL.Path]; ok {
		w.Header().Set("Allow", allowed)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", allowed+" only")
		return
	}
	writeError(w, http.StatusNotFound, "not found", r.URL.Path)
}

// ---------------------------------------------------------------------------
// 5. Router
// ---------------------------------------------------------------------------

// newMux builds an explicit ServeMux using method-aware patterns (Go 1.22+)
// with a JSON fallback for unmatched paths.
func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", getRoot)
	mux.HandleFunc("GET /hello", getHello)
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("POST /json", handleJSON)
	mux.HandleFunc("/", notFound)
	return mux
}

// ---------------------------------------------------------------------------
// 6. Server
// ---------------------------------------------------------------------------

// newServer wires the mux and timeouts into an http.Server.
func newServer(handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout:      writeTimeout,
	}
}

// ---------------------------------------------------------------------------
// 7. main
// ---------------------------------------------------------------------------

func main() {
	server := newServer(newMux())

	// Stop listening when the process receives SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("listening on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("error starting server: %s", err)
		}
	}()

	<-ctx.Done()
	log.Print("shutting down gracefully")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		fmt.Printf("error during shutdown: %s\n", err)
	}
}
