package main

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"sync"
	"time"
)

type Server struct {
	srv        *http.Server
	client     *HttpClient
	visitors   []*YouTubeVisitorData
	ticker     *time.Ticker
	Cfg        *Config
	mu         sync.Mutex
	faultCount int
}

func (srv *Server) RandomVisitor() *YouTubeVisitorData {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.visitors) < srv.Cfg.MaxVisitorCount && srv.faultCount < srv.Cfg.MaxVisitorCount*2 {
		slog.Info("Fetching new visitor data", "current_count", len(srv.visitors))
		visitor, err := srv.fetchInnertubeContext(context.Background())
		if err == nil {
			slog.Info("Fetched new visitor data", slog.Any("visitor", visitor.VisitorID()))
			srv.visitors = append(srv.visitors, visitor)
			return visitor
		}
		srv.faultCount++
		slog.Error("Failed to fetch visitor data", "error", err, "fault_count", srv.faultCount)
	}

	randomIndex := rand.IntN(len(srv.visitors))
	return srv.visitors[randomIndex]
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
		Handler: mux,
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
