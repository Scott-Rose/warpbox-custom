// Actions — POST endpoints for runtime operations accessible from the
// landing page as buttons (e.g., resync metadata, clear RAM cache, toggle log level).
package server

import (
	"log/slog"
	"net/http"

	"github.com/ben/warpbox/internal/config"
)

// ActionFunc is a callback that an action button triggers.
type ActionFunc func() error

// Server actions config, wired from main.go.
var actionResync  ActionFunc
var actionClearCache ActionFunc

// SetActions configures the action callbacks used by the /actions/ handlers.
func SetActions(resync, clearCache ActionFunc) {
	actionResync = resync
	actionClearCache = clearCache
}

// handleActions dispatches POST requests to the appropriate action handler.
func (s *Server) handleActions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	switch r.URL.Path {
	case "/actions/resync":
		s.handleResync(w, r)
	case "/actions/clearcache":
		s.handleClearCache(w, r)
	case "/actions/loglevel":
		s.handleLogLevel(w, r)
	default:
		http.NotFound(w, r)
	}
}

// handleResync triggers an immediate metadata sync from TorBox.
func (s *Server) handleResync(w http.ResponseWriter, r *http.Request) {
	if actionResync == nil {
		http.Error(w, "Resync not configured", http.StatusInternalServerError)
		return
	}

	slog.Info("action: resync triggered from landing page")
	go func() {
		if err := actionResync(); err != nil {
			slog.Error("action: resync failed", "error", err)
		}
	}()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Resync triggered\n"))
}

// handleClearCache evicts all cached chunks from the RAM buffer.
func (s *Server) handleClearCache(w http.ResponseWriter, r *http.Request) {
	if actionClearCache == nil {
		http.Error(w, "Clear cache not configured", http.StatusInternalServerError)
		return
	}

	slog.Info("action: clear cache triggered from landing page")
	if err := actionClearCache(); err != nil {
		slog.Error("action: clear cache failed", "error", err)
		http.Error(w, "Clear cache failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Cache cleared\n"))
}

// handleLogLevel changes the runtime log level and persists it to config.yml.
// Accepts form value "level=debug|info|warn|error".
func (s *Server) handleLogLevel(w http.ResponseWriter, r *http.Request) {
	newLevel := r.FormValue("level")
	if newLevel == "" {
		http.Error(w, "Missing 'level' parameter", http.StatusBadRequest)
		return
	}

	// Validate and parse the level.
	parsedLevel, err := config.ParseLevel(newLevel)
	if err != nil {
		http.Error(w, "Invalid level: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Persist to config file.
	cfgPath := s.ConfigPath()
	if cfgPath != "" {
		if err := config.UpdateLogLevel(cfgPath, newLevel); err != nil {
			slog.Error("action: failed to persist log level to config", "path", cfgPath, "error", err)
			http.Error(w, "Failed to persist log level", http.StatusInternalServerError)
			return
		}
	}

	// Atomically swap the log level at runtime via LevelVar.
	// This takes effect immediately for all slog handlers that reference it.
	s.cfg.LevelVar.Set(parsedLevel)

	slog.Info("action: log level changed", "level", newLevel)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Log level changed to " + newLevel + "\n"))
}
