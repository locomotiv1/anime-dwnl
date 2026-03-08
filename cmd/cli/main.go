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

	cmd := &cli.Command{
		Name:  "anime",
		Usage: "Download and list your currently watching anime",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			err := animeList(c, ctx)
			if err != nil {
				return err
			}
			return nil
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
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
		currentEpisodes := episodeCount()

		for _, item := range anime {
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

func episodeCount() int {
	searchTerm := "Jujutsu Kaisen: Shimetsu Kaiyuu - Zenpen"

	requestBody := GraphQLRequest{
		Query: query,
		Variables: map[string]interface{}{
			"search": searchTerm,
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
