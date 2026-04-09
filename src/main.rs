use crate::auth::authorization;
use std::path::Path;

mod auth;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let path = Path::new("token.json");

    if !path.exists() {
        authorization().await?;
    }
    Ok(())
}
