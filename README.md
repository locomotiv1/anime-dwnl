# Anime CLI

A completely self-contained, terminal-based application to track your MyAnimeList (MAL) currently watching list and automatically download your missing episodes. 

Built with Go, this tool directly integrates a BitTorrent client, meaning you don't need any external torrenting software (like qBittorrent or Transmission) to download.

## Installation

Make sure you have [Go installed](https://go.dev/doc/install) on your system. 

To install the CLI globally, open your terminal and run:

```bash
go install github.com/locomotiv1/anime-cli/cmd/anime@latest
```

*(Note: Ensure your Go `bin` directory is added to your system's `PATH` so you can run the `anime` command from any directory).*

## Project philosphy
This started out as my personal project so it has some assumption that you may or may not like

- If anime has finished airing and you have somes episodes watched this assumes you have files downloaded somewhere on your computer that you can watch and it will not download anything

- If anime has finished airing and you have 0 episodes watched it will download the batch of an entire season

## Usage
The first time you run a command, your browser will open asking you to securely authenticate with MyAnimeList. Your login token is saved locally to your user config directory, so you only have to do this once.

### List your Anime
View your "Currently Watching" list

```bash
anime list
```

### Download Missing Episodes
Download all missing episodes for **every** anime on your currently watching list.
*(Warning: This might take a while if you have a lot of missing episodes)*

```bash
anime download
```

### Download a Specific Anime
Download missing episodes for a specific anime based on its number from the `anime list` command. 

```bash
anime download -l 1
```

### Limit the Number of Episodes
If you only want to download a few episodes at a time (instead of catching up completely), you can use the `-c` (or `--count`) flag to specify a maximum number of episodes. 

*(Note: This flag only applies to currently airing anime. If a show has already finished airing, the CLI will bypass this limit and download the full season batch instead.)*

```bash
anime download -l 1 -c 3
```

By default, the CLI will create a folder called `anime` in your user's home directory and download all video files there.
* **Windows:** `C:\Users\YourName\anime\`
* **Mac/Linux:** `~/anime/`

## ⚠️ Disclaimer
This tool scrapes public RSS feeds and utilizes the BitTorrent protocol. Users are responsible for ensuring that their downloads comply with their local copyright laws. This project is for educational purposes.
