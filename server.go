package main

import (
	"context"
	"database/sql"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type Server struct {
	srv        *http.Server
	client     *HttpClient
	visitors   []*YouTubeVisitorData
	ticker     *time.Ticker
	Cfg        *Config
	mu         sync.RWMutex
	faultCount int
	db         *sql.DB
}

func (srv *Server) RandomVisitor(ctx context.Context, isYouTube bool) *YouTubeVisitorData {
	srv.mu.RLock()
	needNew := len(srv.visitors) < srv.Cfg.MaxVisitorCount &&
		srv.faultCount < srv.Cfg.MaxVisitorCount*4
	currentCount := len(srv.visitors)
	srv.mu.RUnlock()

	if needNew {
		slog.Info("Fetching new visitor data", "current_count", currentCount)
		visitor, err := srv.fetchInnertubeContext(ctx, isYouTube)
		if err == nil {
			idx := visitor.VisitorID()
			if len(visitor.VisitorID()) > 50 {
				idx = visitor.VisitorID()[:50] + "..."
			}
			slog.Info(
				"Fetched new visitor data",
				slog.Any("visitor", idx),
				slog.Any("isYouTube", visitor.IsYouTube),
			)
			srv.mu.Lock()
			srv.visitors = append(srv.visitors, visitor)
			srv.mu.Unlock()
			return visitor
		}
		srv.mu.Lock()
		srv.faultCount++
		srv.mu.Unlock()
		slog.Error("Failed to fetch visitor data", "error", err, "fault_count", srv.faultCount)
	}

	srv.mu.RLock()
	defer srv.mu.RUnlock()

	var filtered []*YouTubeVisitorData
	for _, v := range srv.visitors {
		if v.IsYouTube == isYouTube {
			filtered = append(filtered, v)
		}
	}

	if len(filtered) == 0 {
		return nil
	}

	randomIndex := rand.IntN(len(filtered))
	return filtered[randomIndex]
}

func (srv *Server) RotateVisitors(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			slog.Info("Stopping visitor rotation")
			return
		case <-srv.ticker.C:
			// Collect expired visitors with read lock
			srv.mu.RLock()
			if len(srv.visitors) == 0 {
				srv.mu.RUnlock()
				continue
			}

			type expiredVisitor struct {
				index     int
				isYouTube bool
				idx       string
			}
			var expiredList []expiredVisitor
			for i, visitor := range srv.visitors {
				if visitor.IsExpired() {
					idx := visitor.VisitorID()
					if len(visitor.VisitorID()) > 50 {
						idx = visitor.VisitorID()[:50] + "..."
					}
					expiredList = append(expiredList, expiredVisitor{
						index:     i,
						isYouTube: visitor.IsYouTube,
						idx:       idx,
					})
				}
			}
			srv.mu.RUnlock()

			// Fetch new visitors OUTSIDE the lock
			for _, expired := range expiredList {
				slog.Info("Rotating expired visitor data", slog.Any("visitor", expired.idx))
				newVisitor, err := srv.fetchInnertubeContext(ctx, expired.isYouTube)
				if err != nil {
					slog.Error("Failed to fetch new visitor data", "error", err)
				} else {
					srv.mu.Lock()
					if expired.index < len(srv.visitors) {
						srv.visitors[expired.index] = newVisitor
						slog.Info("Rotated visitor data", slog.Any("visitor", newVisitor.VisitorID()))
					}
					srv.mu.Unlock()
				}
			}
		}
	}
}

func (srv *Server) ConnectDb(ctx context.Context) error {
	slog.Info("Connecting to database", "path", srv.Cfg.Caching.CacheDir)
	conn, err := sql.Open("sqlite", srv.Cfg.Caching.CacheDir)
	if err != nil {
		return err
	}

	if err := conn.PingContext(ctx); err != nil {
		return err
	}

	slog.Info("Connected to database successfully")

	_, _ = conn.Exec(
		`PRAGMA journal_mode = WAL; PRAGMA synchronous = NORMAL; PRAGMA busy_timeout = 5000;`,
	)

	schema := `
	CREATE TABLE IF NOT EXISTS caches (
		key TEXT PRIMARY KEY,
		value BLOB,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_caches_key ON caches (key);`

	_, err = conn.Exec(schema)
	if err != nil {
		return err
	}

	go srv.EnforceCacheLimit(ctx)

	srv.db = conn
	return nil
}

func (srv *Server) Start(ctx context.Context) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/youtube/search", srv.MakeSearchHandler(SearchTypeYouTube))
	mux.HandleFunc("/api/youtubemusic/search", srv.MakeSearchHandler(SearchTypeYouTubeMusic))
	srv.srv = &http.Server{
		BaseContext: func(l net.Listener) context.Context {
			return ctx
		},
		Addr:    srv.Cfg.ServerAddr,
		Handler: PanicRecovery(RequestLogger(mux)),
	}
	go func() {
		if err := srv.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			panic(err)
		}
	}()
}

func (srv *Server) Stop(ctx context.Context) error {
	if srv.srv == nil {
		return nil
	}
	return srv.srv.Shutdown(ctx)
}
