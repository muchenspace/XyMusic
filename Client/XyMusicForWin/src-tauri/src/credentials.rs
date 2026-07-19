use keyring::{Entry, Error as KeyringError};

const SERVICE_NAME: &str = "com.xymusic.desktop";

fn validate_key(key: &str) -> Result<&str, String> {
    let key = key.trim();
    if key.is_empty() || key.len() > 128 {
        return Err("credential key must contain 1 to 128 characters".into());
    }
    Ok(key)
}

fn entry(key: &str) -> Result<Entry, String> {
    Entry::new(SERVICE_NAME, validate_key(key)?).map_err(|error| error.to_string())
}

#[tauri::command]
pub fn credential_read(key: String) -> Result<Option<String>, String> {
    match entry(&key)?.get_password() {
        Ok(value) => Ok(Some(value)),
        Err(KeyringError::NoEntry) => Ok(None),
        Err(error) => Err(error.to_string()),
    }
}

#[tauri::command]
pub fn credential_write(key: String, value: String) -> Result<(), String> {
    if value.len() > 32 * 1024 {
        return Err("credential payload exceeds 32 KiB".into());
    }
    entry(&key)?
        .set_password(&value)
        .map_err(|error| error.to_string())
}

#[tauri::command]
pub fn credential_delete(key: String) -> Result<(), String> {
    match entry(&key)?.delete_credential() {
        Ok(()) | Err(KeyringError::NoEntry) => Ok(()),
        Err(error) => Err(error.to_string()),
    }
}

#[cfg(test)]
mod tests {
    use super::{credential_delete, credential_read, credential_write, validate_key};

    #[test]
    fn credential_keys_are_bounded() {
        assert_eq!(validate_key(" session ").unwrap(), "session");
        assert!(validate_key("").is_err());
        assert!(validate_key(&"x".repeat(129)).is_err());
    }

    #[cfg(windows)]
    #[test]
    fn windows_credential_manager_round_trip() {
        let key = format!("xymusic.test.{}", std::process::id());
        let value = "temporary-test-credential";
        credential_delete(key.clone()).unwrap();
        credential_write(key.clone(), value.into()).unwrap();
        assert_eq!(
            credential_read(key.clone()).unwrap().as_deref(),
            Some(value)
        );
        credential_delete(key.clone()).unwrap();
        assert_eq!(credential_read(key).unwrap(), None);
    }
}
