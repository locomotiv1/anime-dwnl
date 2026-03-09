package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/nstratos/go-myanimelist/mal"
	"github.com/urfave/cli/v3"
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

func main() {
	oauth2Client := getAuthClient()
	c := mal.NewClient(oauth2Client)

	app := &cli.Command{
		Name:  "anime",
		Usage: "A CLI tool to track and download your anime",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "List your currently watching Anime",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					err := animeList(c, ctx)
					if err != nil {
						return err
					}
					return nil
				},
			},
			{
				Name:  "download",
				Usage: "Download all of your missing episodes",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					err := fetchTorrent(c, ctx)
					if err != nil {
						return err
					}
					return nil
				},
			},
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}

// Notice we added trustedUploaders []string here!
func getTorrent(requestURL string, trustedUploaders []string) string {
	// 1. Send the HTTP Request
	req, err := http.NewRequest("GET", requestURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "Anime-Download-CLI/1.0")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	// 2. Read the response & Parse XML
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	var feed Rss
	if err := xml.Unmarshal(bodyBytes, &feed); err != nil {
		return ""
	}

	// 3. Filter for 1080p AND Trusted Uploaders
	for _, item := range feed.Items {
		// First, verify it is 1080p
		if strings.Contains(item.Title, "1080p") {
			// Then, check if the title contains ANY of our trusted groups
			for _, group := range trustedUploaders {
				// Using strings.Contains allows it to match "[SubsPlease]" even
				// if it's buried in a long title string.
				if strings.Contains(item.Title, group) {
					// We found a match! Return the magnet link immediately.
					return fmt.Sprintf("magnet:?xt=urn:btih:%s", item.InfoHash)
				}
			}
		}
	}

	return "" // Return blank if no matching torrents were found
}

func fetchTorrent(c *mal.Client, ctx context.Context) error {
	trustedGroups := []string{"SubsPlease", "Erai-raws", "Judas"}
	anime, _, err := c.User.AnimeList(ctx, "@me",
		mal.Fields{"list_status"},
		mal.AnimeStatusWatching,
		mal.SortAnimeListByListUpdatedAt,
	)
	if err != nil {
		return err
	}

	for _, item := range anime {
		currentEpisode := episodeCount(item.Anime.Title)
		episodesWatched := item.Status.NumEpisodesWatched

		if episodesWatched == 0 {
			searchQuery := item.Anime.Title
			requestURL := fmt.Sprintf("https://nyaa.si/?page=rss&q=%s&c=1_2&f=0&s=seeders&o=desc", url.QueryEscape(searchQuery))
			magnet := getTorrent(requestURL, trustedGroups)
			println(magnet)
		} else {
			for i := episodesWatched + 1; i <= currentEpisode; i++ {
				searchQuery := fmt.Sprintf("%s 0%d", item.Anime.Title, i) // it fucks up when u wanna search episodes with more than 9 episodes
				requestURL := fmt.Sprintf("https://nyaa.si/?page=rss&q=%s&c=1_2&f=0&s=seeders&o=desc", url.QueryEscape(searchQuery))
				magnet := getTorrent(requestURL, trustedGroups)
				println(magnet)
			}
		}
	}
	return nil
}

func animeList(c *mal.Client, ctx context.Context) error {
	anime, _, err := c.User.AnimeList(ctx, "@me",
		mal.Fields{"list_status"},
		mal.AnimeStatusWatching,
		mal.SortAnimeListByListUpdatedAt,
	)
	if err != nil {
		return err
	}

	if len(anime) == 0 {
		fmt.Println("You aren't watching anything right now!")
	} else {
		fmt.Println("\n Currently Watching:")
		fmt.Println("--------------------------------------------------")

		for _, item := range anime {
			currentEpisodes := episodeCount(item.Anime.Title)
			fmt.Printf("- %s (Ep: %d) / %d \n",
				item.Anime.Title,
				item.Status.NumEpisodesWatched,
				currentEpisodes,
			)
		}
		fmt.Println("--------------------------------------------------")
	}

	return nil
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

	resp, err := http.Post(
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

	var releasedEpisodes int

	if media.Status == "NOT_YET_RELEASED" {
		releasedEpisodes = 0
	} else if media.Status == "RELEASING" && media.NextAiringEpisode != nil {
		releasedEpisodes = media.NextAiringEpisode.Episode - 1
	} else {
		releasedEpisodes = media.Episodes
	}

	return releasedEpisodes
}
