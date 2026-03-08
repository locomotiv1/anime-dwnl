package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

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
					err := download(c, ctx)
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

func download(c *mal.Client, ctx context.Context) error {
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

		for i := episodesWatched + 1; i <= currentEpisode; i++ {
			fmt.Println(i)
		}
	}

	// cfg := torrent.NewDefaultClientConfig()
	// cfg.DataDir = "/home/kacper/anime"
	//
	// client, err := torrent.NewClient(cfg)
	// if err != nil {
	// 	log.Fatalf("Error creating torrent client: %v", err)
	// }
	// defer client.Close()
	//
	// infoHashHex := "68ba5c2f758f0b5d1106e00e855b382dd5ce124c"
	//
	// trackers := "&tr=http%3A%2F%2Fnyaa.tracker.wf%3A7777%2Fannounce&tr=udp%3A%2F%2Ftracker.coppersurfer.tk%3A6969%2Fannounce"
	//
	// magnetURI := fmt.Sprintf("magnet:?xt=urn:btih:%s%s", infoHashHex, trackers)
	//
	// t, err := client.AddMagnet(magnetURI)
	// if err != nil {
	// 	log.Fatalf("Error adding magnet: %v", err)
	// }
	//
	// fmt.Println("Fetching torrent metadata (waiting for DHT/Trackers)...")
	//
	// // Wait for the client to get the torrent info (metadata)
	// <-t.GotInfo()
	// fmt.Printf("Metadata acquired! Name: %s\n", t.Info().Name)
	//
	// t.DownloadAll()
	//
	// go func() {
	// 	for {
	// 		bytesCompleted := t.BytesCompleted()
	// 		totalLength := t.Info().TotalLength()
	// 		percentage := float64(bytesCompleted) / float64(totalLength) * 100
	//
	// 		fmt.Printf("\rDownloading: %.2f%% (%d / %d bytes)", percentage, bytesCompleted, totalLength)
	//
	// 		if bytesCompleted == totalLength {
	// 			break
	// 		}
	// 		time.Sleep(2 * time.Second)
	// 	}
	// }()
	//
	// // 7. Block the main thread until the specific torrent finishes
	// client.WaitAll()
	// fmt.Println("\nDownload complete!")
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
