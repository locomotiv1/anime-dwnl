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
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/adhocore/chin"
	"github.com/anacrolix/torrent"
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
					fmt.Println("You are downloading your entire watch list, this might take a while")
					fmt.Println("Use _ to download specific anime(s)")
					var wg sync.WaitGroup

					s := chin.New().WithWait(&wg)
					go s.Start()
					magnets, err := fetchTorrent(c, ctx)
					s.Stop()
					wg.Wait()
					if err != nil {
						return err
					}
					download(magnets)
					return nil
				},
			},
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}

func getTorrent(requestURL string, trustedUploaders []string) string {
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
					return fmt.Sprintf("magnet:?xt=urn:btih:%s", item.InfoHash)
				}
			}
		}
	}

	return ""
}

func download(magnets []string) {
	clientConfig := torrent.NewDefaultClientConfig()
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("could now find home directory %v", err)
	}
	downloadFolder := filepath.Join(homeDir, "anime")
	clientConfig.DataDir = downloadFolder

	c, err := torrent.NewClient(clientConfig)
	if err != nil {
		log.Fatalf("Error creating torrent client: %v", err)
	}
	defer c.Close()

	fmt.Printf("Queuing up %d episodes for download...\n", len(magnets))

	for _, magnetLink := range magnets {

		t, err := c.AddMagnet(magnetLink)
		if err != nil {
			log.Printf("Error adding magnet: %v\n", err)
			continue
		}

		// We use a 'goroutine' here.
		// This tells Go to fetch the metadata for torrents at the same time in the background.
		// If we didn't use 'go' here, the program would get stuck waiting for Episode 1
		// to find peers before it even tried to look for Episode 2!
		go func(currTorrent *torrent.Torrent) {
			<-currTorrent.GotInfo()
			currTorrent.DownloadAll()
		}(t)
	}
	go func() {
		for {
			time.Sleep(3 * time.Second)
			// This clears terminal screen so that this whole block of text wont be printed to the terminal every single time it updates
			fmt.Print("\033[H\033[2J")
			fmt.Println("\n--- Current Downloads ---")
			activeTorrents := c.Torrents()

			sort.Slice(activeTorrents, func(i, j int) bool {
				return activeTorrents[i].Name() < activeTorrents[j].Name()
			})

			for _, t := range activeTorrents {

				if t.Info() == nil {
					fmt.Printf("[Fetching Metadata] %s\n", t.Name())
					continue
				}

				completed := t.BytesCompleted()
				total := t.Info().TotalLength()

				if total > 0 {
					percent := float64(completed) / float64(total) * 100

					if percent == 100 {
						fmt.Printf("[ DONE ] %s\n", t.Info().Name)
					} else {
						fmt.Printf("[%5.1f%%] %s\n", percent, t.Info().Name)
					}
				}
			}
		}
	}()

	c.WaitAll()
	fmt.Println("All torrents successfully downloaded!")
}

func fetchTorrent(c *mal.Client, ctx context.Context) ([]string, error) {
	trustedGroups := []string{"SubsPlease", "Erai-raws", "Judas"}
	var magnets []string
	anime, _, err := c.User.AnimeList(ctx, "@me",
		mal.Fields{"list_status", "status"},
		mal.AnimeStatusWatching,
		mal.SortAnimeListByListUpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	for _, item := range anime {
		currentEpisode := episodeCount(item.Anime.Title)
		episodesWatched := item.Status.NumEpisodesWatched

		if episodesWatched == 0 && item.Anime.Status == "finished_airing" {
			searchQuery := item.Anime.Title
			requestURL := fmt.Sprintf("https://nyaa.si/?page=rss&q=%s&c=1_2&f=0&s=seeders&o=desc", url.QueryEscape(searchQuery))
			magnet := getTorrent(requestURL, trustedGroups)
			if magnet != "" {
				magnets = append(magnets, magnet)
			}
			// fmt.Printf("- %s -- %s\n", item.Anime.Title, magnet)

		} else if item.Anime.Status == "currently_airing" {
			for i := episodesWatched + 1; i <= currentEpisode; i++ {
				searchQuery := fmt.Sprintf("%s %02d", item.Anime.Title, i)
				requestURL := fmt.Sprintf("https://nyaa.si/?page=rss&q=%s&c=1_2&f=0&s=seeders&o=desc", url.QueryEscape(searchQuery))
				magnet := getTorrent(requestURL, trustedGroups)
				if magnet != "" {
					magnets = append(magnets, magnet)
				}
				// fmt.Printf("- %s (Ep: %d) -- %s\n", item.Anime.Title, i, magnet)
			}
		} else if episodesWatched > 0 && item.Anime.Status == "finished_airing" {
			continue
		}
	}
	return magnets, err
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
