package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

type SearchType int

type ctxKey string

const VisitorDataContextKey ctxKey = "visitorData"

const (
	SearchTypeYouTube SearchType = iota
	SearchTypeYouTubeMusic
)

var innertubeContextPattern = regexp.MustCompile(
	`["']INNERTUBE_CONTEXT["']\s*:\s*({.*)\s*["']INNERTUBE_CONTEXT_CLIENT_NAME["']`,
)
var isrcPattern = regexp.MustCompile(`^[A-Z]{2}[A-Z0-9]{3}[0-9]{2}[0-9]{5}$`)

const (
	VideoIDRegex    = `(?P<v>[a-zA-Z0-9_-]{11})`
	PlaylistIDRegex = `(?P<list>(PL|UU)[a-zA-Z0-9_-]+)`
)

var (
	DirectVideoIDPattern = regexp.MustCompile("^" + VideoIDRegex + "$")
)

const YT_VIDEO_FILTER_PARAM = "EgWKAQIQAWoQEAMQBRAEEAkQChAVEBAQEQ%3D%3D"
const YT_SONG_FILTER_PARAM = "EgWKAQIIAWoQEAMQBRAEEAkQChAVEBAQEQ%3D%3D"
const YT_MUSIC_BASE_URL = "https://music.youtube.com"
const YT_BASE_URL = "https://www.youtube.com"
const INNERTUBE_SEARCH_API_URL = YT_MUSIC_BASE_URL + "/youtubei/v1/search?prettyPrint=false"

func (srv *Server) MakeSearchHandler(searchType SearchType) http.HandlerFunc {
	return func(writer http.ResponseWriter, req *http.Request) {
		query := req.FormValue("query")
		if strings.TrimSpace(query) == "" {
			http.Error(writer, "query parameter is required", http.StatusBadRequest)
			return
		}

		if isrcPattern.MatchString(query) || strings.HasPrefix(strings.ToLower(query), "isrc:") {
			if strings.HasPrefix(strings.ToLower(query), "isrc:") {
				query = strings.TrimSpace(query[5:])
			}
			searchType = SearchTypeYouTubeMusic
		}

		if DirectVideoIDPattern.MatchString(query) {
			videoId := DirectVideoIDPattern.FindStringSubmatch(query)[1]
			if utf8.RuneCountInString(videoId) > 11 {
				videoId = videoId[:11]
			}

			slog.Info("Direct video ID detected", "videoId", videoId)
			track, err := srv.LoadVideoMetadata(req.Context(), videoId)
			if err != nil || track.Identifier == "" {
				http.Error(
					writer,
					fmt.Sprintf("Error loading video metadata: %v", err),
					http.StatusInternalServerError,
				)
				return
			}
			writer.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(writer).Encode([]YouTubeTrack{track}); err != nil {
				http.Error(
					writer,
					fmt.Sprintf("Error encoding response: %v", err),
					http.StatusInternalServerError,
				)
				return
			}
			return

		}

		results, err := srv.searchFromYouTube(req.Context(), searchType, query)
		if err != nil {
			http.Error(
				writer,
				fmt.Sprintf("Error searching YouTube: %v", err),
				http.StatusInternalServerError,
			)
			return
		}

		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(results); err != nil {
			http.Error(
				writer,
				fmt.Sprintf("Error encoding response: %v", err),
				http.StatusInternalServerError,
			)
			return
		}

	}
}

func (srv *Server) fetchInnertubeContext(ctx context.Context) (*YouTubeVisitorData, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, YT_MUSIC_BASE_URL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := srv.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to perform request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	matches := innertubeContextPattern.FindSubmatch(respBody)
	if len(matches) < 2 {
		return nil, fmt.Errorf("failed to find INNERTUBE_CONTEXT in response")
	}

	var contextData map[string]any
	contextString := string(matches[1])
	contextString = strings.TrimRight(contextString, ",")
	if err := json.Unmarshal([]byte(contextString), &contextData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal INNERTUBE_CONTEXT: %w", err)
	}
	return NewYouTubeVisitor(contextData), nil
}

func (srv *Server) LoadVideoMetadata(ctx context.Context, videoID string) (YouTubeTrack, error) {
	visitor := srv.RandomVisitor()

	vCtx := context.WithValue(
		ctx,
		VisitorDataContextKey,
		visitor.VisitorID(),
	)

	payload := map[string]any{
		"context": visitor.Context,
		"videoId": videoID,
	}

	reqBody, err := json.Marshal(payload)
	if err != nil {
		return YouTubeTrack{}, fmt.Errorf("failed to marshal video metadata payload: %w", err)
	}

	req, err := http.NewRequestWithContext(
		vCtx,
		http.MethodPost,
		YT_MUSIC_BASE_URL+"/youtubei/v1/player?prettyPrint=false",
		bytes.NewReader(reqBody),
	)
	if err != nil {
		return YouTubeTrack{}, fmt.Errorf("failed to create video metadata request: %w", err)
	}

	resp, err := srv.client.Do(req)
	if err != nil {
		return YouTubeTrack{}, fmt.Errorf("failed to perform video metadata request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return YouTubeTrack{}, fmt.Errorf(
			"video metadata request failed with status: %s",
			resp.Status,
		)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return YouTubeTrack{}, fmt.Errorf("failed to read video metadata response body: %w", err)
	}

	var respdata struct {
		VideoDetails struct {
			Title         string `json:"title"`
			Author        string `json:"author"`
			VideoID       string `json:"videoId"`
			LengthSeconds string `json:"lengthSeconds"`
			ViewCount     string `json:"viewCount"`
			ChannelID     string `json:"channelId"`
			IsLiveContent bool   `json:"isLiveContent"`
			Thumbnail     struct {
				Thumbnails []Thumbnail `json:"thumbnails"`
			} `json:"thumbnail"`
		} `json:"videoDetails"`
	}

	if err := json.Unmarshal(respBody, &respdata); err != nil {
		return YouTubeTrack{}, fmt.Errorf("failed to unmarshal video metadata response: %w", err)
	}

	lengthMS, _ := strconv.Atoi(respdata.VideoDetails.LengthSeconds)
	lengthMS = lengthMS * 1000
	track := YouTubeTrack{
		Title:      respdata.VideoDetails.Title,
		Author:     respdata.VideoDetails.Author,
		Identifier: respdata.VideoDetails.VideoID,
		Images:     respdata.VideoDetails.Thumbnail.Thumbnails,
		Length:     lengthMS,
		Uri:        YT_BASE_URL + "/watch?v=" + respdata.VideoDetails.VideoID,
		Type:       "video",
		Views:      respdata.VideoDetails.ViewCount,
		ChannelId:  respdata.VideoDetails.ChannelID,
		IsLive:     respdata.VideoDetails.IsLiveContent,
	}
	return track, nil
}

func (srv *Server) searchFromYouTube(
	ctx context.Context,
	searchType SearchType,
	query string,
) ([]YouTubeTrack, error) {
	if srv.db != nil {
		cacheKey := srv.createCacheKey(searchType, query)
		cachedData, err := srv.LookupCache(ctx, cacheKey)
		if err != nil {
			slog.Error("Failed to lookup cache", "error", err)
		} else if cachedData != nil {
			var result []YouTubeTrack
			if err := json.Unmarshal(cachedData, &result); err != nil {
				slog.Error("Failed to unmarshal cached search results", "error", err)
			} else {
				slog.Info("Returning cached search results", "key", cacheKey)
				return result, nil
			}
		}
	}
	visitor := srv.RandomVisitor()

	vCtx := context.WithValue(
		ctx,
		VisitorDataContextKey,
		visitor.VisitorID(),
	)

	payload := map[string]any{
		"context": visitor.Context,
		"query":   query,
	}

	switch searchType {
	case SearchTypeYouTube:
		payload["params"] = YT_VIDEO_FILTER_PARAM
	case SearchTypeYouTubeMusic:
		payload["params"] = YT_SONG_FILTER_PARAM
	}

	reqBody, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal search payload: %w", err)
	}

	req, err := http.NewRequestWithContext(
		vCtx,
		http.MethodPost,
		INNERTUBE_SEARCH_API_URL,
		bytes.NewReader(reqBody),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create search request: %w", err)
	}

	resp, err := srv.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to perform search request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search request failed with status: %s", resp.Status)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read search response body: %w", err)
	}

	parsed, err := parseYouTubeSearchResults(respBody)
	if err == nil && len(parsed) > 0 && srv.db != nil {
		cacheKey := srv.createCacheKey(searchType, query)
		if err := srv.StoreCache(vCtx, cacheKey, parsed); err != nil {
			slog.Error("Failed to store search results in cache", "error", err)
		} else {
			slog.Info("Stored search results in cache", "key", cacheKey)
		}
	}
	if searchType == SearchTypeYouTube && len(parsed) != 0 {
		for _, item := range parsed {
			item.Uri = "https://www.youtube.com/watch?v=" + item.Identifier
		}
	}
	return parsed, err
}
