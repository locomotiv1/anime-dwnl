use rand::distr::{Alphanumeric, SampleString};
use serde::{Deserialize, Serialize};
use std::fs::File;
use std::{
    io::{self, Write},
    path::Path,
};

#[derive(Debug, Deserialize, Serialize)]
struct TokenResponse {
    access_token: String,
    refresh_token: String,
    expires_in: i64,
    token_type: String,
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
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
        let path = Path::new("token.json");

        if path.exists() {
            println!("verification token already exists on your computer")
        } else {
            let json_data = serde_json::to_string_pretty(&token_data)?;
            let mut file = File::create("token.json")?;
            file.write_all(json_data.as_bytes())?;

            println!("\n Successfully authenticated!");
        }
    } else {
        let error_text = res.text().await?;
        println!("\n Failed to get token: {}", error_text);
    }

    Ok(())
}
