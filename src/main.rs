use rand::distr::{Alphanumeric, SampleString};
use serde::Deserialize;
use std::io::{self, Write};

#[derive(Debug, Deserialize)]
struct TokenResponse {
    access_token: String,
    refresh_token: String,
    expires_in: i64,
    token_type: String,
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    // 1. Set your Client ID (from https://myanimelist.net/apiconfig)
    let client_id = "c85377fbeb0e1b83e9b8f4cebe024b7c";

    // 2. Generate a Code Verifier
    let code_verifier = Alphanumeric.sample_string(&mut rand::rng(), 128);
    let code_challenge = &code_verifier;

    // 3. Build the Authorization URL
    let auth_url = format!(
        "https://myanimelist.net/v1/oauth2/authorize?response_type=code&client_id={}&code_challenge={}&code_challenge_method=plain",
        client_id, code_challenge
    );

    println!("=== MyAnimeList OAuth2 Authorization ===");
    println!("1. Open this URL in your browser:\n\n{}\n", auth_url);
    println!("2. Click 'Allow'");
    println!("3. You will be redirected to a page displaying an authorization code.");

    // 4. Capture the authorization code
    print!("\nPaste the authorization code here: ");
    io::stdout().flush()?;

    let mut auth_code = String::new();
    io::stdin().read_line(&mut auth_code)?;
    let auth_code = auth_code.trim();

    // 5. Exchange the code + verifier for an Access Token
    let client = reqwest::Client::new();

    // THE FIX: .as_str() ensures all elements are strictly (&str, &str)
    let params = [
        ("client_id", client_id),
        ("code", auth_code),
        ("code_verifier", code_verifier.as_str()),
        ("grant_type", "authorization_code"),
    ];

    let res = client
        .post("https://myanimelist.net/v1/oauth2/token")
        .form(&params)
        .send()
        .await?;

    // 6. Handle the response
    if res.status().is_success() {
        let token_data: TokenResponse = res.json().await?;
        println!("\n✅ Successfully authenticated!");
        println!("Access Token: {}", token_data.access_token);
        println!("Refresh Token: {}", token_data.refresh_token);
        println!("Expires in: {} seconds", token_data.expires_in);
    } else {
        let error_text = res.text().await?;
        println!("\n❌ Failed to get token: {}", error_text);
    }

    Ok(())
}
