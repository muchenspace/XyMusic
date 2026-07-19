mod credentials;
mod desktop_lyrics;
mod media;
mod window;

use tauri::{Manager, WindowEvent};

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(
            tauri_plugin_log::Builder::default()
                .level(log::LevelFilter::Info)
                .build(),
        )
        .plugin(tauri_plugin_notification::init())
        .manage(desktop_lyrics::DesktopLyricsManager::default())
        .manage(window::MiniModeState::default())
        .setup(|app| {
            app.manage(media::MediaSessionState::initialize(app.handle())?);
            if let Some(window) = app.get_webview_window("desktop-lyrics") {
                window
                    .set_ignore_cursor_events(false)
                    .map_err(|error| error.to_string())?;
            }
            Ok(())
        })
        .on_window_event(|window, event| {
            if let WindowEvent::CloseRequested { api, .. } = event {
                if window.label() == "main" {
                    return;
                }
                if window.label() == "desktop-lyrics" {
                    api.prevent_close();
                    let _ = desktop_lyrics::set_desktop_lyrics_visible(
                        window.app_handle().clone(),
                        false,
                    );
                    return;
                }
            }
            if window.label() == "main" && matches!(event, WindowEvent::Destroyed) {
                window.app_handle().exit(0);
            }
            if window.label() == "main" && matches!(event, WindowEvent::Resized(_)) {
                let _ = desktop_lyrics::synchronize_fullscreen(window.app_handle());
            }
        })
        .invoke_handler(tauri::generate_handler![
            credentials::credential_read,
            credentials::credential_write,
            credentials::credential_delete,
            desktop_lyrics::get_desktop_lyrics_window_state,
            desktop_lyrics::set_desktop_lyrics_visible,
            desktop_lyrics::toggle_desktop_lyrics_visible,
            desktop_lyrics::set_desktop_lyrics_locked,
            desktop_lyrics::set_desktop_lyrics_fullscreen_behavior,
            media::update_media_metadata,
            media::update_media_playback,
            media::clear_media_session,
            window::exit_application,
            window::set_mini_mode,
        ])
        .run(tauri::generate_context!())
        .expect("error while running XyMusic desktop application");
}
