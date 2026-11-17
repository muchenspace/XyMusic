const APP_COMMANDS: &[&str] = &[
    "credential_read",
    "credential_write",
    "credential_delete",
    "get_desktop_lyrics_window_state",
    "set_desktop_lyrics_visible",
    "toggle_desktop_lyrics_visible",
    "set_desktop_lyrics_locked",
    "set_desktop_lyrics_fullscreen_behavior",
    "update_media_metadata",
    "update_media_playback",
    "clear_media_session",
    "hide_main_window",
    "toggle_main_window_maximize",
    "toggle_main_window_fullscreen",
    "set_mini_mode",
];

fn main() {
    tauri_build::try_build(
        tauri_build::Attributes::new()
            .app_manifest(tauri_build::AppManifest::new().commands(APP_COMMANDS)),
    )
    .expect("failed to run tauri-build");
}
