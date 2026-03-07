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

func saveToken(file string, token *oauth2.Token) {
	f, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}

func generateVerifier() string {
	b := make([]byte, 64)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// getAuthClient handles the MAL login process and returns an authenticated HTTP client.
func getAuthClient() *http.Client {
	godotenv.Load()
	clientID := os.Getenv("MAL_CLIENT_ID")
	if clientID == "" {
		log.Fatal("MAL_CLIENT_ID is missing from your .env file!")
	}

	oauthConfig := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: os.Getenv("MAL_CLIENT_SECRET"), // MAL doesn't strictly need the secret for PKCE, but it's good to have
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://myanimelist.net/v1/oauth2/authorize",
			TokenURL: "https://myanimelist.net/v1/oauth2/token",
		},
		RedirectURL: "http://localhost:8080/callback",
	}

	tokenFile := "token.json"
	token, err := tokenFromFile(tokenFile)
	if err != nil {
		fmt.Println("No saved login found. Starting authentication...")

		tokenChan := make(chan *oauth2.Token)
		server := &http.Server{Addr: ":8080"}
		codeVerifier := generateVerifier()

		http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
			code := r.URL.Query().Get("code")
			t, err := oauthConfig.Exchange(context.Background(), code, oauth2.SetAuthURLParam("code_verifier", codeVerifier))
			if err != nil {
				http.Error(w, "Failed to exchange token", http.StatusInternalServerError)
				return
			}
			fmt.Fprintf(w, "Login successful! You can close this browser window and return to your terminal.")
			tokenChan <- t
		})

		authURL := oauthConfig.AuthCodeURL(
			"random-state",
			oauth2.SetAuthURLParam("code_challenge", codeVerifier),
			oauth2.SetAuthURLParam("code_challenge_method", "plain"),
		)

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

		token = <-tokenChan
		server.Shutdown(context.Background())
		saveToken(tokenFile, token)

		fmt.Println("Successfully logged in and saved credentials!")
	}

	return oauthConfig.Client(context.Background(), token)
}
