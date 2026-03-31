package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
)

const query = `
query ($search: String) {
  Media (search: $search, type: ANIME) {
    title {
      romaji
      english
    }
    status
    episodes
    nextAiringEpisode {
      episode
    }
  }
}
`

// Reusable HTTP client
var httpClient = &http.Client{}

type GraphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables"`
}

type AniListResponse struct {
	Data struct {
		Media struct {
			Title struct {
				Romaji  string `json:"romaji"`
				English string `json:"english"`
			} `json:"title"`
			Status            string `json:"status"`
			Episodes          int    `json:"episodes"`
			NextAiringEpisode *struct {
				Episode int `json:"episode"`
			} `json:"nextAiringEpisode"`
		} `json:"Media"`
	} `json:"data"`
}

type Rss struct {
	Items []RssItem `xml:"channel>item"`
}

type RssItem struct {
	Title    string `xml:"title"`
	Link     string `xml:"link"`
	InfoHash string `xml:"infoHash"`
	Seeders  int    `xml:"seeders"`
}

func episodeCount(title string) int {
	requestBody := GraphQLRequest{
		Query: query,
		Variables: map[string]interface{}{
			"search": title,
		},
	}

	jsonBytes, err := json.Marshal(requestBody)
	if err != nil {
		log.Fatalf("Failed to marshal JSON: %v", err)
	}

	resp, err := httpClient.Post(
		"https://graphql.anilist.co",
		"application/json",
		bytes.NewBuffer(jsonBytes),
	)
	if err != nil {
		log.Fatalf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	var result AniListResponse
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		log.Fatalf("Failed to parse response: %v", err)
	}

	if result.Data.Media.Title.Romaji == "" && result.Data.Media.Title.English == "" {
		return 0
	}

	media := result.Data.Media
	if media.Status == "NOT_YET_RELEASED" {
		return 0
	} else if media.Status == "RELEASING" && media.NextAiringEpisode != nil {
		return media.NextAiringEpisode.Episode - 1
	}
	return media.Episodes
}

func getTorrent(searchQuery string, trustedUploaders []string) string {
	requestURL := fmt.Sprintf("https://nyaa.si/?page=rss&q=%s&c=1_2&f=0&s=seeders&o=desc", url.QueryEscape(searchQuery))
	req, err := http.NewRequest("GET", requestURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "Anime-Download-CLI/1.0")

	resp, err := httpClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	var feed Rss
	if err := xml.Unmarshal(bodyBytes, &feed); err != nil {
		return ""
	}

	for _, item := range feed.Items {
		if strings.Contains(item.Title, "1080p") {
			for _, group := range trustedUploaders {
				if strings.Contains(item.Title, group) {
					trackers := "&tr=http%3A%2F%2Fnyaa.tracker.wf%3A7777%2Fannounce&tr=udp%3A%2F%2Ftracker.opentrackr.org%3A1337%2Fannounce"
					return fmt.Sprintf("magnet:?xt=urn:btih:%s%s", item.InfoHash, trackers)
				}
			}
		}
	}
	return ""
}
