package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/joho/godotenv"
	"golang.org/x/oauth2"
)

var (
	oauthConfig  *oauth2.Config
	codeVerifier string
)

// generateVerifier creates a random string for PKCE security
func generateVerifier() string {
	b := make([]byte, 64)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// tokenFromFile reads a saved token from a local JSON file
func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

// saveToken saves the token to a local JSON file for future runs
func saveToken(file string, token *oauth2.Token) {
	fmt.Printf("Saving credential file to: %s\n", file)
	f, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}

func main() {
	// 1. Load the .env file
	err := godotenv.Load()
	if err != nil {
		log.Println("Warning: No .env file found or error reading it.")
	}

	// 2. Configure OAuth2 for MyAnimeList
	oauthConfig = &oauth2.Config{
		ClientID:     os.Getenv("MAL_CLIENT_ID"),
		ClientSecret: os.Getenv("MAL_CLIENT_SECRET"),
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://myanimelist.net/v1/oauth2/authorize",
			TokenURL: "https://myanimelist.net/v1/oauth2/token",
		},
		RedirectURL: "http://localhost:8080/callback",
	}

	if oauthConfig.ClientID == "" {
		log.Fatal("MAL_CLIENT_ID is missing from your .env file!")
	}

	tokenFile := "token.json"

	// 3. Try to load the token from the file first
	token, err := tokenFromFile(tokenFile)
	if err != nil {
		// IF WE ARE HERE: No token file exists. Start the browser login process.
		fmt.Println("No saved login found. Starting authentication...")

		tokenChan := make(chan *oauth2.Token)
		server := &http.Server{Addr: ":8080"}

		// Temporary callback route
		http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
			code := r.URL.Query().Get("code")
			if code == "" {
				http.Error(w, "Code not found", http.StatusBadRequest)
				return
			}

			// Exchange code for token, using the PKCE verifier
			t, err := oauthConfig.Exchange(context.Background(), code, oauth2.SetAuthURLParam("code_verifier", codeVerifier))
			if err != nil {
				http.Error(w, "Failed to exchange token", http.StatusInternalServerError)
				return
			}

			fmt.Fprintf(w, "Login successful! You can close this browser window and return to your terminal.")
			tokenChan <- t // Send the token to wake up the main CLI thread
		})

		// Generate PKCE verifier and Auth URL
		codeVerifier = generateVerifier()
		authURL := oauthConfig.AuthCodeURL(
			"random-state",
			oauth2.SetAuthURLParam("code_challenge", codeVerifier),
			oauth2.SetAuthURLParam("code_challenge_method", "plain"),
		)

		// Start the server in the background
		go func() {
			if err := server.ListenAndServe(); err != http.ErrServerClosed {
				log.Fatalf("HTTP server error: %v", err)
			}
		}()

		fmt.Println("==================================================")
		fmt.Println("Please open this link in your browser to log in:")
		fmt.Println("\n" + authURL + "\n")
		fmt.Println("Waiting for you to log in...")
		fmt.Println("==================================================")

		// Pause the CLI until we receive the token from the web server
		token = <-tokenChan

		// Shut down the temporary server
		server.Shutdown(context.Background())

		// Save the new token to a file
		saveToken(tokenFile, token)
		fmt.Println("✅ Successfully logged in and saved credentials!")
	}

	// 4. Create an authenticated client.
	// (Go handles refreshing the token automatically behind the scenes if it's expired!)
	client := oauthConfig.Client(context.Background(), token)

	// 5. Test the API! Let's fetch the logged-in user's profile.
	resp, err := client.Get("https://api.myanimelist.net/v2/users/@me")
	if err != nil {
		log.Fatalf("Failed to fetch user: %v", err)
	}
	defer resp.Body.Close()

	var user map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&user)
}
