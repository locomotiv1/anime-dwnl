package main

import (
	"context"
	"fmt"
	"log"
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

type DownloadTask struct {
	Title  string
	Magnet string
}

type FileMoveTask struct {
	OldPath string
	NewPath string
}

func sanitizeFolder(name string) string {
	invalidChars := []string{"<", ">", ":", "\"", "/", "\\", "|", "?", "*"}
	for _, char := range invalidChars {
		name = strings.ReplaceAll(name, char, "")
	}
	return strings.TrimSpace(name)
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
					return animeList(c, ctx)
				},
			},
			{
				Name:  "download",
				Usage: "Download all of your missing episodes",
				Flags: []cli.Flag{
					&cli.IntFlag{
						Name:    "listing",
						Aliases: []string{"l"},
						Usage:   "Download all episodes from the corrensponding anime from 'anime list' function",
					},
					&cli.IntFlag{
						Name:    "count",
						Aliases: []string{"c"},
						Usage:   "Specify the maximum number of episodes to download, only applies to currently airing anime",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					listNum := int(cmd.Int("listing"))
					count := int(cmd.Int("count"))

					if listNum == 0 {
						fmt.Println("You are downloading your entire watch list, this might take a while")
						fmt.Println("Use -l <number> to download specific anime")
					}

					var wg sync.WaitGroup
					s := chin.New().WithWait(&wg)
					go s.Start()

					tasks, err := fetchTorrent(c, ctx, listNum, count)
					s.Stop()
					wg.Wait()

					if err != nil {
						return err
					}
					if len(tasks) == 0 {
						fmt.Println("No new episodes to download.")
						return nil
					}

					download(tasks)
					return nil
				},
			},
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}

func download(tasks []DownloadTask) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("could not find home directory: %v", err)
	}
	downloadFolder := filepath.Join(homeDir, "anime")

	clientConfig := torrent.NewDefaultClientConfig()
	clientConfig.DataDir = downloadFolder

	c, err := torrent.NewClient(clientConfig)
	if err != nil {
		log.Fatalf("Error creating torrent client: %v", err)
	}

	fmt.Printf("Queuing up %d episodes for download...\n", len(tasks))

	titleMap := make(map[string]string)
	for _, task := range tasks {
		t, err := c.AddMagnet(task.Magnet)
		if err != nil {
			log.Printf("Error adding magnet: %v\n", err)
			continue
		}
		titleMap[t.InfoHash().String()] = sanitizeFolder(task.Title)
		go func(currTorrent *torrent.Torrent) {
			<-currTorrent.GotInfo()
			currTorrent.DownloadAll()
		}(t)
	}

	// Use a done channel to cleanly exit the monitor loop
	done := make(chan struct{})
	go monitorProgress(c, done)

	c.WaitAll()
	close(done)
	fmt.Println("All torrents successfully downloaded!")

	// Figure out paths before closing the client
	moves := getMoveTasks(c, downloadFolder, titleMap)

	c.Close()

	organizeFiles(moves)
}

func monitorProgress(c *torrent.Client, done chan struct{}) {
	for {
		select {
		case <-done:
			return
		case <-time.After(3 * time.Second):
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
	}
}

func getMoveTasks(c *torrent.Client, downloadFolder string, titleMap map[string]string) []FileMoveTask {
	var moves []FileMoveTask

	for _, t := range c.Torrents() {
		if t.Info() == nil {
			continue
		}

		animeTitle := titleMap[t.InfoHash().String()]
		if animeTitle == "" {
			continue
		}

		animeFolder := filepath.Join(downloadFolder, animeTitle)
		os.MkdirAll(animeFolder, 0o755)

		oldPath := filepath.Join(downloadFolder, t.Info().Name)
		newPath := filepath.Join(animeFolder, t.Info().Name)

		if oldPath != newPath {
			moves = append(moves, FileMoveTask{OldPath: oldPath, NewPath: newPath})
		}
	}
	return moves
}

func organizeFiles(moves []FileMoveTask) {
	fmt.Println("Organizing files into folders...")
	for _, m := range moves {
		if err := os.Rename(m.OldPath, m.NewPath); err != nil {
			log.Printf("Failed to move %s: %v\n", m.OldPath, err)
		}
	}
	fmt.Println("Done organizing!")
}

func fetchTorrent(c *mal.Client, ctx context.Context, targetIndex int, count int) ([]DownloadTask, error) {
	trustedGroups := []string{"SubsPlease", "Erai-raws", "Judas"}
	var tasks []DownloadTask

	anime, _, err := c.User.AnimeList(ctx, "@me",
		mal.Fields{"list_status", "status"},
		mal.AnimeStatusWatching,
		mal.SortAnimeListByListUpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if targetIndex > len(anime) {
		return nil, fmt.Errorf("invalid list number")
	}

	for i, item := range anime {
		if targetIndex > 0 && i != (targetIndex-1) {
			continue
		}

		currentEpisode := episodeCount(item.Anime.Title)
		episodesWatched := item.Status.NumEpisodesWatched

		if episodesWatched == 0 && item.Anime.Status == "finished_airing" {
			if count > 0 {
				fmt.Printf("\n[Notice] '%s' has finished airing. Ignoring the episode count flag and downloading the full batch instead.\n", item.Anime.Title)
			}

			if magnet := getTorrent(item.Anime.Title, trustedGroups); magnet != "" {
				tasks = append(tasks, DownloadTask{Title: item.Anime.Title, Magnet: magnet})
			}

		} else if item.Anime.Status == "currently_airing" {
			endEpisode := currentEpisode
			if count > 0 && (episodesWatched+count) < currentEpisode {
				endEpisode = episodesWatched + count
			}

			for i := episodesWatched + 1; i <= endEpisode; i++ {
				searchQuery := fmt.Sprintf("%s %02d", item.Anime.Title, i)

				if magnet := getTorrent(searchQuery, trustedGroups); magnet != "" {
					tasks = append(tasks, DownloadTask{Title: item.Anime.Title, Magnet: magnet})
				}
			}
		} else if episodesWatched > 0 && item.Anime.Status == "finished_airing" {
			continue
		}
	}
	return tasks, nil
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
		return nil
	}

	fmt.Println("\n Currently Watching:")
	fmt.Println("--------------------------------------------------")

	for i, item := range anime {
		currentEpisodes := episodeCount(item.Anime.Title)
		fmt.Printf("%d - %s (Ep: %d) / %d \n",
			i+1,
			item.Anime.Title,
			item.Status.NumEpisodesWatched,
			currentEpisodes,
		)
	}
	fmt.Println("--------------------------------------------------")
	return nil
}
