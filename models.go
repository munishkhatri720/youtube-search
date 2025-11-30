package main

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/topi314/tint"
)

type YouTubeVisitorData struct {
	Context   map[string]any `json:"context"`
	CreatedAt time.Time      `json:"createdAt"`
}

func (v *YouTubeVisitorData) IsExpired() bool {
	return time.Since(v.CreatedAt) > 30*time.Minute
}

func (v *YouTubeVisitorData) VisitorID() string {
	clientContext := v.Context["client"].(map[string]any)
	id, ok := clientContext["visitorData"].(string)
	if !ok {
		return ""
	}
	return id
}

func NewYouTubeVisitor(context map[string]any) *YouTubeVisitorData {
	return &YouTubeVisitorData{
		Context:   context,
		CreatedAt: time.Now(),
	}
}

type Thumbnail struct {
	Url    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type YouTubeTrack struct {
	Title      string      `json:"title"`
	Author     string      `json:"author"`
	Identifier string      `json:"identifier"`
	Images     []Thumbnail `json:"images"`
	Length     int         `json:"length"`
	Uri        string      `json:"uri"`
	Type       string      `json:"type"`
	Views      string      `json:"views"`
	ChannelId  string      `json:"channel_id"`
	IsLive     bool        `json:"is_live"`
}

func parseDurationText(durationStr string) int {
	parts := strings.Split(durationStr, ":")
	hours, minutes, seconds := 0, 0, 0

	if len(parts) == 3 {
		hours, _ = strconv.Atoi(parts[0])
		minutes, _ = strconv.Atoi(parts[1])
		seconds, _ = strconv.Atoi(parts[2])
	} else if len(parts) == 2 {
		minutes, _ = strconv.Atoi(parts[0])
		seconds, _ = strconv.Atoi(parts[1])
	} else if len(parts) == 1 {
		seconds, _ = strconv.Atoi(parts[0])
	}
	totalSeconds := hours*3600 + minutes*60 + seconds
	return totalSeconds * 1000
}

func parseYouTubeTrack(item gjson.Result) (YouTubeTrack, error) {

	itemRenderer := item.Get("musicResponsiveListItemRenderer")
	if !itemRenderer.Exists() {
		return YouTubeTrack{}, fmt.Errorf("musicResponsiveListItemRenderer not found")
	}
	thumbnails := []Thumbnail{}
	thumbnailArray := itemRenderer.Get("thumbnail.musicThumbnailRenderer.thumbnail.thumbnails")
	if thumbnailArray.Exists() && thumbnailArray.IsArray() {
		for _, thumb := range thumbnailArray.Array() {
			thumbnails = append(thumbnails, Thumbnail{
				Url:    thumb.Get("url").String(),
				Width:  int(thumb.Get("width").Int()),
				Height: int(thumb.Get("height").Int()),
			})
		}
	}

	title := itemRenderer.Get("flexColumns.0.musicResponsiveListItemFlexColumnRenderer.text.runs.0.text").
		String()

	flexColumns := itemRenderer.Get("flexColumns").Array()

	length := ""
	views := ""
	author := ""
	if len(flexColumns) <= 2 {
		flexColumnsRuns := itemRenderer.Get(
			"flexColumns.1.musicResponsiveListItemFlexColumnRenderer.text.runs",
		).Array()
		for _, run := range flexColumnsRuns {
			text := run.Get("text").String()
			if strings.TrimSpace(text) == "•" {
				break
			}
			author += text
		}

		if len(flexColumnsRuns) >= 5 &&
			strings.TrimSpace(flexColumnsRuns[len(flexColumnsRuns)-2].Get("text").String()) == "•" {
			if strings.Contains(
				flexColumnsRuns[len(flexColumnsRuns)-3].Get("text").String(),
				"views",
			) {
				views = flexColumnsRuns[len(flexColumnsRuns)-3].Get("text").String()
			}

		}
		length = flexColumnsRuns[len(flexColumnsRuns)-1].Get("text").String()

	} else {
		authorAndLengthRuns := flexColumns[1].Get("musicResponsiveListItemFlexColumnRenderer.text.runs").Array()
		for _, run := range authorAndLengthRuns {
			text := run.Get("text").String()

			if strings.TrimSpace(text) == "•" {
				continue
			} else {
				author += text
			}
		}
		length = authorAndLengthRuns[len(authorAndLengthRuns)-1].Get("text").String()
		views = flexColumns[2].Get("musicResponsiveListItemFlexColumnRenderer.text.runs.0.text").String()
	}

	videoId := itemRenderer.Get("playlistItemData.videoId").String()
	uri := fmt.Sprintf("https://music.youtube.com/watch?v=%s", videoId)

	channelId := ""
	menuItems := itemRenderer.Get("menu.menuRenderer.items").Array()
outer:
	for _, menuItem := range menuItems {
		if nav := menuItem.Get("menuNavigationItemRenderer"); nav.Exists() {
			runs := nav.Get("text.runs")
			if runs.IsArray() {
				for _, run := range runs.Array() {
					if strings.ToLower(strings.TrimSpace(run.Get("text").String())) == "go to artist" {

						if cid := nav.Get("navigationEndpoint.browseEndpoint.browseId"); cid.Exists() && cid.String() != "" {
							channelId = cid.String()
							break outer
						}
					}
				}
			}
		}
	}

	lengthInt := parseDurationText(length)
	if lengthInt == 0 {
		return YouTubeTrack{}, fmt.Errorf("failed to parse duration: %v", length)
	}

	itemType := "song"
	if len(thumbnails) > 0 {
		thumbUrl := thumbnails[0].Url
		if strings.Contains(thumbUrl, "i.ytimg.com/vi/") {
			itemType = "video"
		}
	}
	track := YouTubeTrack{
		Title:      title,
		Author:     author,
		Identifier: videoId,
		Images:     thumbnails,
		Length:     lengthInt,
		Uri:        uri,
		Type:       itemType,
		Views:      views,
		ChannelId:  channelId,
	}

	return track, nil

}

func parseYouTubeSearchResults(data []byte) ([]YouTubeTrack, error) {
	result := gjson.GetBytes(
		data,
		"contents.tabbedSearchResultsRenderer.tabs.0.tabRenderer.content.sectionListRenderer.contents.0.musicShelfRenderer.contents",
	)
	if !result.Exists() {
		return nil, fmt.Errorf(
			"array of musicResponsiveListItemRenderer doesn't found in the data",
		)
	}

	if !result.IsArray() {
		return nil, fmt.Errorf(
			"expected musicShelfRenderer.contents to be an array but got : %v",
			result.Type.String(),
		)
	}
	tracks := make([]YouTubeTrack, 0)
	for _, item := range result.Array() {
		track, err := parseYouTubeTrack(item)
		if err != nil {
			slog.Debug("Skipping item due to error", tint.Err(err))
			continue
		}
		tracks = append(tracks, track)
	}
	return tracks, nil
}
