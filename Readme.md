# YouTube Search API

A simple go api server to search videos/songs on YouTube & YouTube Music.

## Features

-  Search YouTube videos and YouTube Music tracks
-  SQLite caching to reduce rate limits
-  Youtube visitor data randomization
-  Configurable request timeouts
- Optional IPv6 rotation support (subnet should be multiple of 16)

## Installation

```bash
go build -o youtube-searchapi .
```

## Docker
```bash
docker compose up -d
```

## Configuration

Create a `config.yaml` file:

```yaml
server_addr: ":8080"
max_visitor_count: 2
request_timeout: 10

logging:
  level: "info"
  format: text
  no_color: false
  add_source: false

caching:
  enabled: true
  cache_dir: cache.db
  cache_max_limit: -1  # -1 for unlimited
```

## Usage

```bash
./youtube-searchapi -config config.yaml
```

## API Endpoints

### Search YouTube Videos
```
GET /api/youtube/search?query=<search_term>
```

### Search YouTube Music
```
GET /api/youtubemusic/search?query=<search_term>
```

## Example

```bash
curl "http://localhost:8080/api/youtube/search?query=lofi+beats"
```


