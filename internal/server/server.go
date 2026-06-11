// Package server implements the WebDAV HTTP handler for Warpbox.
//
// It handles PROPFIND (directory listing), GET with Range (streaming),
// HEAD, and OPTIONS methods. All reads go through the throttle → cache →
// metadata pipeline.
package server

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ben/warpbox/internal/cache"
	"github.com/ben/warpbox/internal/metadata"
	"github.com/ben/warpbox/internal/throttle"
	"github.com/ben/warpbox/internal/torbox"
)

// SyncStatusFunc is a callback that returns the current sync status.
type SyncStatusFunc func() metadata.SyncStatus

// negativeCacheEntry tracks a failed CDN URL fetch so we don't hammer TorBox
// on rapid retries from Plex/jellyfin. Entries expire after their TTL.
type negativeCacheEntry struct {
	err       error
	expiresAt time.Time
}

// torrentFailureTracker counts failures for a single torrent within a sliding
// window. Once the threshold is exceeded, the torrent is marked "stale" and
// all CDN URL fetches are skipped until the next metadata sync.
type torrentFailureTracker struct {
	failures   []time.Time
	staleUntil time.Time
}

// Server is the Warpbox WebDAV server.
type Server struct {
	cfg        Config
	store      *metadata.Store
	cache      *cache.Buffer
	torBox     *torbox.Client
	queue      *throttle.Queue
	root       string
	mux        *http.ServeMux
	startTime  time.Time
	syncStatus SyncStatusFunc

	// Negative cache: key = "torrentID:fileID", value = error + expiry.
	// Protects against Plex's tight retry loop burning API quota on known-bad files.
	negativeCache   map[string]*negativeCacheEntry
	negativeCacheMu sync.Mutex

	// Circuit breaker: per-torrent failure tracking.
	// Marked stale after maxTorrentFailures in the sliding window.
	torrentFailures   map[int64]*torrentFailureTracker
	torrentFailuresMu sync.Mutex

		// Stop channel for periodic cleanup goroutines.
	cleanupStopCh chan struct{}
	// Configurable map size limits.
	negativeCacheMaxEntries  int
	circuitBreakerMaxEntries int
}

// Config holds the server-specific configuration.
type Config struct {
	ListenAddr         string
	WebDAVRoot         string
	CDNTtlMinutes       int  // How long to cache CDN URLs (0 = disable)
	CDNURLAutoRepair    bool // Auto-repair stale CDN URLs by re-fetching from TorBox
	CDNURLRepairRetries int  // Max repair retries per request (0 = no retries)
	Version            string // Build version, injected at compile time
	MaxRAMMB           int    // For landing page display
	ChunkSizeMB        int    // For landing page display
	TTLSeconds         int    // For landing page display
	EvictionStrategy   string // For landing page display
	RequestsPerMinute  int    // For landing page display
	LogFormat          string // For landing page display
	LogLevel           string // For landing page display
	SyncIntervalMinute int    // For landing page display

	// CDN URL fetch retry settings.
	CDNURLRetryBackoff int // Backoff base in seconds; default 1
	CDNURLRetryCount   int // Max retry attempts; default 3

	// Negative cache TTL in seconds.
	NegativeCacheTTLSeconds int // default 30

	// Circuit breaker settings.
	CircuitBreakerFailures  int // Max failures in window; default 5
	CircuitBreakerWindowSec int // Sliding window seconds; default 60
	CircuitBreakerStaleMin  int // Stale duration minutes; default 5

	// Memory management settings.
	NegativeCacheMaxEntries  int // Max entries in negative cache; default 5000
	CircuitBreakerMaxEntries int // Max entries in circuit breaker; default 2000
	CleanupIntervalSeconds   int // How often to sweep expired entries; default 60
}

// New creates a new WebDAV server.
func New(cfg Config, store *metadata.Store, c *cache.Buffer, torBox *torbox.Client, queue *throttle.Queue) *Server {
	s := &Server{
		cfg:       cfg,
		store:     store,
		cache:     c,
		torBox:    torBox,
		queue:     queue,
		root:      cfg.WebDAVRoot,
		mux:       http.NewServeMux(),
		startTime: time.Now(),

		negativeCache:          make(map[string]*negativeCacheEntry),
		torrentFailures:        make(map[int64]*torrentFailureTracker),
		cleanupStopCh:          make(chan struct{}),
		negativeCacheMaxEntries:  cfg.NegativeCacheMaxEntries,
		circuitBreakerMaxEntries: cfg.CircuitBreakerMaxEntries,
	}
	s.registerRoutes()
	s.startCleanupLoop()
	return s
}

// startCleanupLoop runs a periodic background goroutine that sweeps expired
// entries from the negative cache and circuit breaker maps. This prevents
// unbounded memory growth from entries that are never looked up again.
func (s *Server) startCleanupLoop() {
	interval := time.Duration(s.cfg.CleanupIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 60 * time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.sweepNegativeCache()
				s.sweepCircuitBreaker()
			case <-s.cleanupStopCh:
				return
			}
		}
	}()
}

// StopCleanup stops the periodic cleanup goroutine. Intended for tests.
func (s *Server) StopCleanup() {
	close(s.cleanupStopCh)
}

// sweepNegativeCache removes expired entries. If the map exceeds
// maxNegativeCacheEntries, the oldest entries are also evicted.
func (s *Server) sweepNegativeCache() {
	s.negativeCacheMu.Lock()
	defer s.negativeCacheMu.Unlock()

	now := time.Now()

	// Remove expired entries.
	for k, v := range s.negativeCache {
		if now.After(v.expiresAt) {
			delete(s.negativeCache, k)
		}
	}

	// If still over limit, evict oldest entries.
	if len(s.negativeCache) > s.negativeCacheMaxEntries {
		over := len(s.negativeCache) - s.negativeCacheMaxEntries
		// Collect keys sorted by expiry (oldest first).
		type kv struct {
			key       string
			expiresAt time.Time
		}
		sorted := make([]kv, 0, len(s.negativeCache))
		for k, v := range s.negativeCache {
			sorted = append(sorted, kv{key: k, expiresAt: v.expiresAt})
		}
		// Simple bubble of oldest to front — small O(n) is fine for cleanup.
		for i := 0; i < len(sorted) && i < over; i++ {
			oldest := i
			for j := i + 1; j < len(sorted); j++ {
				if sorted[j].expiresAt.Before(sorted[oldest].expiresAt) {
					oldest = j
				}
			}
			sorted[i], sorted[oldest] = sorted[oldest], sorted[i]
		}
		for i := 0; i < over; i++ {
			delete(s.negativeCache, sorted[i].key)
		}
		slog.Debug("swept negative cache",
			"remaining", len(s.negativeCache),
			"evicted", over,
			"max", s.negativeCacheMaxEntries,
		)
	}
}

// sweepCircuitBreaker removes trackers where the stale period has expired.
// If the map exceeds maxCircuitBreakerEntries, the oldest are evicted.
func (s *Server) sweepCircuitBreaker() {
	s.torrentFailuresMu.Lock()
	defer s.torrentFailuresMu.Unlock()

	now := time.Now()

	// Remove trackers whose stale period has expired.
	for k, v := range s.torrentFailures {
		if !v.staleUntil.IsZero() && now.After(v.staleUntil) {
			delete(s.torrentFailures, k)
		}
	}

	// If still over limit, evict oldest.
	if len(s.torrentFailures) > s.circuitBreakerMaxEntries {
		over := len(s.torrentFailures) - s.circuitBreakerMaxEntries
		type kv struct {
			key       int64
			staleUntil time.Time
		}
		sorted := make([]kv, 0, len(s.torrentFailures))
		for k, v := range s.torrentFailures {
			sorted = append(sorted, kv{key: k, staleUntil: v.staleUntil})
		}
		// Sort by staleUntil ascending — those already expired or oldest first.
		for i := 0; i < len(sorted) && i < over; i++ {
			oldest := i
			for j := i + 1; j < len(sorted); j++ {
				if sorted[j].staleUntil.Before(sorted[oldest].staleUntil) {
					oldest = j
				}
			}
			sorted[i], sorted[oldest] = sorted[oldest], sorted[i]
		}
		for i := 0; i < over; i++ {
			delete(s.torrentFailures, sorted[i].key)
		}
		slog.Debug("swept circuit breaker",
			"remaining", len(s.torrentFailures),
			"evicted", over,
			"max", s.circuitBreakerMaxEntries,
		)
	}
}

// CacheStats returns the buffer's current stats.
func (s *Server) CacheStats() (entries, usedRAM, maxRAM int) {
	return s.cache.Stats()
}

// NegativeCacheSize returns the current number of entries in the negative cache.
func (s *Server) NegativeCacheSize() int {
	s.negativeCacheMu.Lock()
	defer s.negativeCacheMu.Unlock()
	return len(s.negativeCache)
}

// CircuitBreakerSize returns the current number of entries in the circuit breaker.
func (s *Server) CircuitBreakerSize() int {
	s.torrentFailuresMu.Lock()
	defer s.torrentFailuresMu.Unlock()
	return len(s.torrentFailures)
}

// versionHeader returns an HTTP middleware that sets the Server header.
func (s *Server) versionHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "warpbox/"+s.cfg.Version)
		next.ServeHTTP(w, r)
	})
}

// registerRoutes sets up the HTTP handlers for WebDAV methods,
// the HTML browser, branded landing page, and embedded favicon/logo.
func (s *Server) registerRoutes() {
	handler := s.versionHeader(http.HandlerFunc(s.handleWebDAV))
	s.mux.Handle(s.root+"/", handler)
	s.mux.Handle(s.root, handler)

	// Human-browsable HTML directory listing at /http/
	s.mux.Handle("/http/", s.versionHeader(http.HandlerFunc(s.handleHTTP)))
	s.mux.Handle("/http", s.versionHeader(http.HandlerFunc(s.handleHTTP)))

	// Infuse WebDAV endpoint (same content, different URL path).
	s.mux.Handle("/infuse/", s.versionHeader(http.HandlerFunc(s.handleWebDAV)))
	s.mux.Handle("/infuse", s.versionHeader(http.HandlerFunc(s.handleWebDAV)))

	// Log viewer.
	s.mux.Handle("/logs/", s.versionHeader(http.HandlerFunc(s.handleLogs)))
	s.mux.Handle("/logs", s.versionHeader(http.HandlerFunc(s.handleLogs)))

	// Action endpoints (POST-only).
	s.mux.Handle("/actions/", s.versionHeader(http.HandlerFunc(s.handleActions)))

	s.mux.Handle("/", s.versionHeader(http.HandlerFunc(s.handleLanding)))
	s.mux.HandleFunc("/warpbox.svg", s.handleLogo)
	s.mux.HandleFunc("/favicon.ico", s.handleLogo)
}

// handleWebDAV dispatches WebDAV methods to the appropriate handler.
// If the request comes via /infuse/, rewrite the path to the configured
// WebDAV root so the sub-handlers work without modification.
func (s *Server) handleWebDAV(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/infuse") {
		r.URL.Path = strings.Replace(r.URL.Path, "/infuse", s.root, 1)
	}

	switch r.Method {
	case http.MethodOptions:
		s.handleOptions(w, r)
	case http.MethodGet:
		s.handleGet(w, r)
	case http.MethodHead:
		s.handleHead(w, r)
	case "PROPFIND":
		s.handlePropfind(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// SetSyncStatus configures the callback for reading sync worker status.
func (s *Server) SetSyncStatus(fn SyncStatusFunc) {
	s.syncStatus = fn
}

// handleOptions responds with WebDAV capabilities.
func (s *Server) handleOptions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("DAV", "1")
	w.Header().Set("Allow", "OPTIONS, GET, HEAD, PROPFIND")
	w.WriteHeader(http.StatusOK)
}

// Start begins listening on the configured address.
func (s *Server) Start(ctx context.Context) error {
	slog.Info("webdav server listening", "addr", s.cfg.ListenAddr)
	return http.ListenAndServe(s.cfg.ListenAddr, s.mux)
}