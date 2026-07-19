use serde::Serialize;
use std::sync::Mutex;
use tauri::{AppHandle, Emitter, Manager};

const MAIN_WINDOW: &str = "main";
const LYRICS_WINDOW: &str = "desktop-lyrics";
const WINDOW_STATE_EVENT: &str = "desktop://lyrics-window-state";

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Serialize)]
#[serde(rename_all = "camelCase")]
pub enum FullscreenBehavior {
    #[default]
    Show,
    Hide,
}

impl TryFrom<&str> for FullscreenBehavior {
    type Error = String;

    fn try_from(value: &str) -> Result<Self, Self::Error> {
        match value {
            "show" => Ok(Self::Show),
            "hide" => Ok(Self::Hide),
            _ => Err("fullscreen behavior must be 'show' or 'hide'".into()),
        }
    }
}

#[derive(Clone, Copy, Debug, Default)]
pub struct DesktopLyricsState {
    requested_visible: bool,
    locked: bool,
    hidden_by_fullscreen: bool,
    fullscreen_behavior: FullscreenBehavior,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct DesktopLyricsWindowState {
    requested_visible: bool,
    visible: bool,
    locked: bool,
    hidden_by_fullscreen: bool,
    fullscreen_behavior: FullscreenBehavior,
}

pub struct DesktopLyricsManager(pub Mutex<DesktopLyricsState>);

impl Default for DesktopLyricsManager {
    fn default() -> Self {
        Self(Mutex::new(DesktopLyricsState::default()))
    }
}

fn public_state(state: DesktopLyricsState) -> DesktopLyricsWindowState {
    DesktopLyricsWindowState {
        requested_visible: state.requested_visible,
        visible: state.requested_visible && !state.hidden_by_fullscreen,
        locked: state.locked,
        hidden_by_fullscreen: state.hidden_by_fullscreen,
        fullscreen_behavior: state.fullscreen_behavior,
    }
}

fn apply(app: &AppHandle, state: DesktopLyricsState) -> Result<DesktopLyricsWindowState, String> {
    let public = public_state(state);
    let window = app
        .get_webview_window(LYRICS_WINDOW)
        .ok_or_else(|| "desktop lyrics window is unavailable".to_string())?;
    window
        .set_ignore_cursor_events(state.locked)
        .map_err(|error| error.to_string())?;
    if public.visible {
        window.show().map_err(|error| error.to_string())?;
    } else {
        window.hide().map_err(|error| error.to_string())?;
    }
    app.emit(WINDOW_STATE_EVENT, public)
        .map_err(|error| error.to_string())?;
    Ok(public)
}

fn update(
    app: &AppHandle,
    change: impl FnOnce(&mut DesktopLyricsState),
) -> Result<DesktopLyricsWindowState, String> {
    let manager = app.state::<DesktopLyricsManager>();
    let state = {
        let mut state = manager
            .0
            .lock()
            .map_err(|_| "desktop lyrics state lock is poisoned")?;
        change(&mut state);
        *state
    };
    apply(app, state)
}

pub fn synchronize_fullscreen(app: &AppHandle) -> Result<DesktopLyricsWindowState, String> {
    let fullscreen = app
        .get_webview_window(MAIN_WINDOW)
        .and_then(|window| window.is_fullscreen().ok())
        .unwrap_or(false);
    update(app, |state| {
        state.hidden_by_fullscreen =
            fullscreen && state.fullscreen_behavior == FullscreenBehavior::Hide;
    })
}

#[tauri::command]
pub fn get_desktop_lyrics_window_state(app: AppHandle) -> Result<DesktopLyricsWindowState, String> {
    synchronize_fullscreen(&app)
}

#[tauri::command]
pub fn set_desktop_lyrics_visible(
    app: AppHandle,
    visible: bool,
) -> Result<DesktopLyricsWindowState, String> {
    update(&app, |state| state.requested_visible = visible)
}

#[tauri::command]
pub fn toggle_desktop_lyrics_visible(app: AppHandle) -> Result<DesktopLyricsWindowState, String> {
    update(&app, |state| {
        state.requested_visible = !state.requested_visible
    })
}

#[tauri::command]
pub fn set_desktop_lyrics_locked(
    app: AppHandle,
    locked: bool,
) -> Result<DesktopLyricsWindowState, String> {
    update(&app, |state| state.locked = locked)
}

#[tauri::command]
pub fn set_desktop_lyrics_fullscreen_behavior(
    app: AppHandle,
    behavior: String,
) -> Result<DesktopLyricsWindowState, String> {
    let behavior = FullscreenBehavior::try_from(behavior.as_str())?;
    let fullscreen = app
        .get_webview_window(MAIN_WINDOW)
        .and_then(|window| window.is_fullscreen().ok())
        .unwrap_or(false);
    update(&app, |state| {
        state.fullscreen_behavior = behavior;
        state.hidden_by_fullscreen = fullscreen && behavior == FullscreenBehavior::Hide;
    })
}

#[cfg(test)]
mod tests {
    use super::{DesktopLyricsState, FullscreenBehavior, public_state};

    #[test]
    fn fullscreen_hiding_preserves_requested_visibility() {
        let state = DesktopLyricsState {
            requested_visible: true,
            hidden_by_fullscreen: true,
            fullscreen_behavior: FullscreenBehavior::Hide,
            ..DesktopLyricsState::default()
        };
        let public = public_state(state);
        assert!(public.requested_visible);
        assert!(!public.visible);
        assert!(public.hidden_by_fullscreen);
    }

    #[test]
    fn fullscreen_behavior_is_strictly_parsed() {
        assert_eq!(
            FullscreenBehavior::try_from("show").unwrap(),
            FullscreenBehavior::Show
        );
        assert_eq!(
            FullscreenBehavior::try_from("hide").unwrap(),
            FullscreenBehavior::Hide
        );
        assert!(FullscreenBehavior::try_from("invalid").is_err());
    }
}
