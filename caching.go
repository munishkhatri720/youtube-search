package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"
)

func (srv *Server) createCacheKey(searchType SearchType, query string) string {
	query = strings.ToLower(strings.TrimSpace(query))
	data := map[string]any{
		"search_type": searchType,
		"query":       query,
	}
	encoded := url.Values{}
	for k, v := range data {
		encoded.Set(k, fmt.Sprintf("%v", v))
	}
	return encoded.Encode()
}

func (srv *Server) EnforceCacheLimit(ctx context.Context) error {
	if srv.db != nil {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		slog.Info("Started cache cleanup ticker")
		for {
			select {
			case <-ctx.Done():
				slog.Info("Stopped cache cleanup ticker")
				return nil

			case <-ticker.C:
				var count int
				err := srv.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM caches").Scan(&count)
				if err != nil {
					slog.Error("Failed to get cache count", "error", err)
					continue
				}
				slog.Info("Current cache count", "count", count)
				if srv.Cfg.Caching.CacheMaxLimit < 0 {
					continue
				}
				if int64(count) <= srv.Cfg.Caching.CacheMaxLimit {
					continue
				}
				toDelete := int64(count) - srv.Cfg.Caching.CacheMaxLimit
				slog.Info("Deleting old cache", "to_delete", toDelete)

				_, err = srv.db.ExecContext(
					ctx,
					`DELETE FROM caches WHERE key IN (SELECT key FROM caches ORDER BY timestamp ASC LIMIT ?)`,
					toDelete,
				)
				if err != nil {
					slog.Error("Failed to delete old cache entries", "error", err)
					continue
				}

			}
		}

	}
	return nil
}

func (srv *Server) StoreCache(ctx context.Context, key string, data []YouTubeTrack) error {
	value, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if srv.db != nil {
		_, err := srv.db.ExecContext(ctx,
			"INSERT OR REPLACE INTO caches (key, value) VALUES (?, ?)",
			key,
			value,
		)
		if err != nil {
			return err
		}
		slog.Info("Stored cache entry", "key", key)
		return nil

	}
	return nil

}

func (srv *Server) LookupCache(ctx context.Context, key string) ([]byte, error) {
	if srv.db != nil {
		var data []byte
		err := srv.db.QueryRowContext(ctx, "SELECT value FROM caches WHERE key = ?", key).
			Scan(&data)
		if err != nil {
			if err == sql.ErrNoRows {
				return nil, nil
			}
			return nil, err
		}
		slog.Info("Cache hit", "key", key)
		return data, nil
	}
	return nil, nil
}
func (srv *Server) clearCache(ctx context.Context) error {
	if srv.db != nil {
		_, err := srv.db.ExecContext(ctx, "DELETE FROM caches")
		if err != nil {
			return err
		}
		slog.Info("Cleared all cache entries")
		return nil
	}
	return nil
}
