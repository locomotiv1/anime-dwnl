package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"
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
					&cli.StringFlag{
						Name:    "dir",
						Aliases: []string{"d"},
						Usage:   "Set a custom download directory path (default: ~/anime/)",
					},
					&cli.StringFlag{
						Name:    "quality",
						Aliases: []string{"q"},
						Usage:   "Set the video quality (e.g., 720p, 1080p, 480p)",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					listNum := int(cmd.Int("listing"))
					count := int(cmd.Int("count"))
					customDir := cmd.String("dir")
					quality := cmd.String("quality")

					if listNum == 0 {
						fmt.Println("You are downloading your entire watch list, this might take a while")
						fmt.Println("Use -l <number> to download specific anime")
						fmt.Print("\nDo you want to continue? [y/N]: ")

						reader := bufio.NewReader(os.Stdin)
						input, _ := reader.ReadString('\n')
						input = strings.TrimSpace(strings.ToLower(input))

						if input != "y" && input != "yes" {
							fmt.Println("Download cancelled.")
							return nil
						}
					}

					var wg sync.WaitGroup
					s := chin.New().WithWait(&wg)
					go s.Start()

					tasks, err := fetchTorrent(c, ctx, listNum, count, quality)
					s.Stop()
					wg.Wait()

					if err != nil {
						return err
					}
					if len(tasks) == 0 {
						fmt.Println("No new episodes to download.")
						return nil
					}

					download(tasks, customDir)
					return nil
				},
			},
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}

func download(tasks []DownloadTask, customDir string) {
	var downloadFolder string

	if customDir != "" {
		downloadFolder = customDir
	} else {
		// Fallback to the default ~/anime/
		homeDir, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("could not find home directory: %v", err)
		}
		downloadFolder = filepath.Join(homeDir, "anime")
	}

	if err := os.MkdirAll(downloadFolder, 0o755); err != nil {
		log.Fatalf("Could not create download directory: %v", err)
	}

	clientConfig := torrent.NewDefaultClientConfig()
	clientConfig.DataDir = downloadFolder

	c, err := torrent.NewClient(clientConfig)
	if err != nil {
		log.Fatalf("Error creating torrent client: %v", err)
	}

	fmt.Printf("Queuing up %d episodes for download in %s...\n", len(tasks), downloadFolder)

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

	done := make(chan struct{})
	go monitorProgress(c, done)

	c.WaitAll()
	close(done)
	fmt.Println("All torrents successfully downloaded!")

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

func fetchTorrent(c *mal.Client, ctx context.Context, targetIndex int, count int, quality string) ([]DownloadTask, error) {
	trustedGroups := []string{"SubsPlease", "Erai-raws", "Judas"}
	var tasks []DownloadTask

	watching, _, err := c.User.AnimeList(ctx, "@me",
		mal.Fields{"list_status", "status"},
		mal.AnimeStatusWatching,
		mal.SortAnimeListByListUpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	planToWatch, _, err := c.User.AnimeList(ctx, "@me",
		mal.Fields{"list_status", "status"},
		mal.AnimeStatusPlanToWatch,
		mal.SortAnimeListByListUpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	anime := append(watching, planToWatch...)

	if targetIndex > len(anime) {
		return nil, fmt.Errorf("invalid list number")
	}

	for i, item := range anime {
		if targetIndex > 0 && i != (targetIndex-1) {
			continue
		}

		currentEpisode := episodeCount(item.Anime.Title)
		episodesWatched := item.Status.NumEpisodesWatched

		if item.Anime.Status == "finished_airing" {
			if count > 0 {
				fmt.Printf("\n[Notice] '%s' has finished airing. Ignoring the episode count flag and downloading the full batch instead.\n", item.Anime.Title)
			}

			if magnet := getTorrent(item.Anime.Title, trustedGroups, quality); magnet != "" {
				tasks = append(tasks, DownloadTask{Title: item.Anime.Title, Magnet: magnet})
			}

		} else if item.Anime.Status == "currently_airing" {
			endEpisode := currentEpisode
			if count > 0 && (episodesWatched+count) < currentEpisode {
				endEpisode = episodesWatched + count
			}

			for i := episodesWatched + 1; i <= endEpisode; i++ {
				searchQuery := fmt.Sprintf("%s %02d", item.Anime.Title, i)

				if magnet := getTorrent(searchQuery, trustedGroups, quality); magnet != "" {
					tasks = append(tasks, DownloadTask{Title: item.Anime.Title, Magnet: magnet})
				}
			}
		}
	}
	return tasks, nil
}

func animeList(c *mal.Client, ctx context.Context) error {
	watching, _, err := c.User.AnimeList(ctx, "@me",
		mal.Fields{"list_status", "status"},
		mal.AnimeStatusWatching,
		mal.SortAnimeListByListUpdatedAt,
	)
	if err != nil {
		return err
	}

	planToWatch, _, err := c.User.AnimeList(ctx, "@me",
		mal.Fields{"list_status", "status"},
		mal.AnimeStatusPlanToWatch,
		mal.SortAnimeListByListUpdatedAt,
	)
	if err != nil {
		return err
	}

	anime := append(watching, planToWatch...)

	if len(anime) == 0 {
		fmt.Println("You aren't watching anything right now, and your plan-to-watch list is empty!")
		return nil
	}

	fmt.Println("\n Anime List")
	fmt.Println("--------------------------------------------------")

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)

	for i, item := range anime {
		currentEpisodes := episodeCount(item.Anime.Title)
		cleanStatus := strings.ReplaceAll(item.Anime.Status, "_", " ")
		userListStatus := strings.ReplaceAll(string(item.Status.Status), "_", " ")
		url := fmt.Sprintf("https://myanimelist.net/anime/%d", item.Anime.ID)

		title := fmt.Sprintf("\033]8;;%s\033\\%s\033]8;;\033\\", url, item.Anime.Title)

		fmt.Fprintf(w, "%d - %s\t (Ep: %d) / %d\t [%s]\t (%s)\n",
			i+1,
			title,
			item.Status.NumEpisodesWatched,
			currentEpisodes,
			cleanStatus,
			userListStatus,
		)
	}

	w.Flush()
	fmt.Println("--------------------------------------------------")
	return nil
}
