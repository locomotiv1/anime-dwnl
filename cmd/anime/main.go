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

type DownloadTask struct {
	Title  string
	Magnet string
}

// Removes illegal characters from Anime titles so folder creation doesn't crash on Windows
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
				Flags: []cli.Flag{
					&cli.IntFlag{
						Name:    "listing",
						Aliases: []string{"l"},
						Usage:   "Download all episodes from the corrensponding anime from 'anime list' function",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					listNum := int(cmd.Int("listing"))
					if listNum == 0 {
						fmt.Println("You are downloading your entire watch list, this might take a while")
						fmt.Println("Use -l <number> to download specific anime")
					}
					var wg sync.WaitGroup
					s := chin.New().WithWait(&wg)
					go s.Start()

					tasks, err := fetchTorrent(c, ctx, listNum)
					s.Stop()
					wg.Wait()
					if err != nil {
						return err
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
					trackers := "&tr=http%3A%2F%2Fnyaa.tracker.wf%3A7777%2Fannounce&tr=udp%3A%2F%2Ftracker.opentrackr.org%3A1337%2Fannounce"
					return fmt.Sprintf("magnet:?xt=urn:btih:%s%s", item.InfoHash, trackers)
				}
			}
		}
	}

	return ""
}

func download(tasks []DownloadTask) {
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

	fmt.Printf("Queuing up %d episodes for download...\n", len(tasks))

	// create a map to link a torrent's unique hash back to its Anime Title
	titleMap := make(map[string]string)

	for _, task := range tasks {
		t, err := c.AddMagnet(task.Magnet)
		if err != nil {
			log.Printf("Error adding magnet: %v\n", err)
			continue
		}

		// Store the clean anime title in our map using the torrent's hash as the key
		titleMap[t.InfoHash().String()] = sanitizeFolder(task.Title)

		go func(currTorrent *torrent.Torrent) {
			<-currTorrent.GotInfo()
			currTorrent.DownloadAll()
		}(t)
	}

	go func() {
		for {
			time.Sleep(3 * time.Second)
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

	fmt.Println("Organizing files into folders...")
	for _, t := range c.Torrents() {
		if t.Info() == nil {
			continue
		}

		animeTitle, exists := titleMap[t.InfoHash().String()]
		if !exists || animeTitle == "" {
			continue
		}

		animeFolder := filepath.Join(downloadFolder, animeTitle)
		os.MkdirAll(animeFolder, 0o755)

		oldPath := filepath.Join(downloadFolder, t.Info().Name)
		newPath := filepath.Join(animeFolder, t.Info().Name)

		if oldPath != newPath {
			err := os.Rename(oldPath, newPath)
			if err != nil {
				log.Printf("Failed to move %s: %v\n", t.Info().Name, err)
			}
		}
	}
	fmt.Println("Done organizing!")
}

func fetchTorrent(c *mal.Client, ctx context.Context, targetIndex int) ([]DownloadTask, error) {
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
		return nil, fmt.Errorf("Invalid list number")
	}

	for i, item := range anime {
		if targetIndex > 0 && i != (targetIndex-1) {
			continue
		}
		currentEpisode := episodeCount(item.Anime.Title)
		episodesWatched := item.Status.NumEpisodesWatched

		if episodesWatched == 0 && item.Anime.Status == "finished_airing" {
			searchQuery := item.Anime.Title
			requestURL := fmt.Sprintf("https://nyaa.si/?page=rss&q=%s&c=1_2&f=0&s=seeders&o=desc", url.QueryEscape(searchQuery))
			magnet := getTorrent(requestURL, trustedGroups)
			if magnet != "" {
				tasks = append(tasks, DownloadTask{Title: item.Anime.Title, Magnet: magnet})
			}

		} else if item.Anime.Status == "currently_airing" {
			for i := episodesWatched + 1; i <= currentEpisode; i++ {
				searchQuery := fmt.Sprintf("%s %02d", item.Anime.Title, i)
				requestURL := fmt.Sprintf("https://nyaa.si/?page=rss&q=%s&c=1_2&f=0&s=seeders&o=desc", url.QueryEscape(searchQuery))
				magnet := getTorrent(requestURL, trustedGroups)
				if magnet != "" {
					tasks = append(tasks, DownloadTask{Title: item.Anime.Title, Magnet: magnet})
				}
			}
		} else if episodesWatched > 0 && item.Anime.Status == "finished_airing" {
			continue
		}
	}
	return tasks, err
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
