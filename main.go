package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/nstratos/go-myanimelist/mal"
	"github.com/urfave/cli/v3"
)

func main() {
	oauth2Client := getAuthClient()
	c := mal.NewClient(oauth2Client)

	cmd := &cli.Command{
		Name:  "anime",
		Usage: "Download and list your currently watching anime",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			// Pass the client 'c' and the context 'ctx' into our function
			err := animeList(c, ctx)
			if err != nil {
				return err
			}
			return nil
		},
	}

	// 3. Run the CLI tool
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}

// animeList takes the mal.Client and context, and returns an error if something breaks
func animeList(c *mal.Client, ctx context.Context) error {
	// Fetch the anime using c.User.AnimeList without the extra wrappers!
	anime, _, err := c.User.AnimeList(ctx, "@me",
		mal.Fields{"list_status"},
		mal.AnimeStatusWatching,
		mal.SortAnimeListByListUpdatedAt,
	)
	if err != nil {
		return err // Return the error so the main function knows it failed
	}

	if len(anime) == 0 {
		fmt.Println("You aren't watching anything right now!")
	} else {
		fmt.Println("\n Currently Watching:")
		fmt.Println("--------------------------------------------------")

		for _, item := range anime {
			fmt.Printf("- %s (Ep: %d)\n",
				item.Anime.Title,
				item.Status.NumEpisodesWatched,
			)
		}
		fmt.Println("--------------------------------------------------")
	}

	return nil
}
