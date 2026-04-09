use rand::distr::{Alphanumeric, SampleString};
use serde::{Deserialize, Serialize};
use std::fs::File;
use std::io::{self, Write};

#[derive(Debug, Deserialize, Serialize)]
pub struct TokenResponse {
    pub access_token: String,
    pub refresh_token: String,
    expires_in: i64,
    token_type: String,
}

pub async fn authorization() -> Result<(), Box<dyn std::error::Error>> {
    let client_id = "c85377fbeb0e1b83e9b8f4cebe024b7c";

    let code_verifier = Alphanumeric.sample_string(&mut rand::rng(), 128);
    let code_challenge = &code_verifier;

    let auth_url = format!(
        "https://myanimelist.net/v1/oauth2/authorize?response_type=code&client_id={}&code_challenge={}&code_challenge_method=plain",
        client_id, code_challenge
    );

    println!("=== MyAnimeList OAuth2 Authorization ===");
    println!("1. Open this URL in your browser:\n\n{}\n", auth_url);
    println!("2. Click 'Allow'");
    println!("3. You will be redirected to a page displaying an authorization code.");

    print!("\nPaste the authorization code here: ");
    io::stdout().flush()?;

    let mut auth_code = String::new();
    io::stdin().read_line(&mut auth_code)?;
    let auth_code = auth_code.trim();

    // Exchange the code + verifier for an Access Token
    let client = reqwest::Client::new();
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

    if res.status().is_success() {
        let token_data: TokenResponse = res.json().await?;

        let json_data = serde_json::to_string_pretty(&token_data)?;
        let mut file = File::create("token.json")?;
        file.write_all(json_data.as_bytes())?;

        println!("\n Successfully authenticated!");
    } else {
        let error_text = res.text().await?;
        println!("\n Failed to get token: {}", error_text);
    }

    Ok(())
}

pub async fn refresh_existing_token(
    refresh_token: &str,
) -> Result<TokenResponse, Box<dyn std::error::Error>> {
    println!("Access token expired. Refreshing the token...");

    let client_id = "c85377fbeb0e1b83e9b8f4cebe024b7c";
    let client = reqwest::Client::new();

    let params = [
        ("client_id", client_id),
        ("grant_type", "refresh_token"),
        ("refresh_token", refresh_token),
    ];

    let res = client
        .post("https://myanimelist.net/v1/oauth2/token")
        .form(&params)
        .send()
        .await?;

    if res.status().is_success() {
        let token_data: TokenResponse = res.json().await?;

        // Save the brand new token over the old one!
        let json_data = serde_json::to_string_pretty(&token_data)?;
        std::fs::write("token.json", json_data)?;

        println!("Token successfully refreshed and saved!");
        Ok(token_data)
    } else {
        let error_text = res.text().await?;
        Err(format!("Failed to refresh token: {}", error_text).into())
    }
}
