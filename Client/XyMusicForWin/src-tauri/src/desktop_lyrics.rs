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

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct DesktopLyricsState {
    requested_visible: bool,
    locked: bool,
    hidden_by_fullscreen: bool,
    fullscreen_behavior: FullscreenBehavior,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct DesktopLyricsWindowState {
    revision: u64,
    requested_visible: bool,
    visible: bool,
    locked: bool,
    hidden_by_fullscreen: bool,
    fullscreen_behavior: FullscreenBehavior,
}

#[derive(Debug, Default)]
struct DesktopLyricsManagerState {
    desired: DesktopLyricsState,
    applied: Option<DesktopLyricsState>,
    revision: u64,
}

pub struct DesktopLyricsManager(Mutex<DesktopLyricsManagerState>);

impl Default for DesktopLyricsManager {
    fn default() -> Self {
        Self(Mutex::new(DesktopLyricsManagerState::default()))
    }
}

fn public_state(state: DesktopLyricsState, revision: u64) -> DesktopLyricsWindowState {
    DesktopLyricsWindowState {
        revision,
        requested_visible: state.requested_visible,
        visible: state.requested_visible && !state.hidden_by_fullscreen,
        locked: state.locked,
        hidden_by_fullscreen: state.hidden_by_fullscreen,
        fullscreen_behavior: state.fullscreen_behavior,
    }
}

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
struct NativeApplyPlan {
    ignore_cursor_events: Option<bool>,
    visible: Option<bool>,
    state_event: Option<DesktopLyricsWindowState>,
}

impl NativeApplyPlan {
    fn is_empty(self) -> bool {
        self.ignore_cursor_events.is_none() && self.visible.is_none() && self.state_event.is_none()
    }
}

fn native_apply_plan(
    applied: Option<DesktopLyricsState>,
    desired: DesktopLyricsState,
    revision: u64,
) -> NativeApplyPlan {
    let desired_public = public_state(desired, revision);
    let applied_public = applied.map(|state| public_state(state, revision));

    NativeApplyPlan {
        ignore_cursor_events: match applied {
            Some(previous) if previous.locked == desired.locked => None,
            _ => Some(desired.locked),
        },
        visible: match applied_public {
            Some(previous) if previous.visible == desired_public.visible => None,
            _ => Some(desired_public.visible),
        },
        state_event: if applied_public == Some(desired_public) {
            None
        } else {
            Some(desired_public)
        },
    }
}

fn apply(app: &AppHandle, plan: NativeApplyPlan) -> Result<(), String> {
    if plan.ignore_cursor_events.is_some() || plan.visible.is_some() {
        let window = app
            .get_webview_window(LYRICS_WINDOW)
            .ok_or_else(|| "desktop lyrics window is unavailable".to_string())?;
        if let Some(locked) = plan.ignore_cursor_events {
            window
                .set_ignore_cursor_events(locked)
                .map_err(|error| error.to_string())?;
        }
        if let Some(visible) = plan.visible {
            if visible {
                window.show().map_err(|error| error.to_string())?;
            } else {
                window.hide().map_err(|error| error.to_string())?;
            }
        }
    }
    if let Some(state) = plan.state_event {
        app.emit(WINDOW_STATE_EVENT, state)
            .map_err(|error| error.to_string())?;
    }
    Ok(())
}

fn update(
    app: &AppHandle,
    change: impl FnOnce(&mut DesktopLyricsState),
) -> Result<DesktopLyricsWindowState, String> {
    let manager = app.state::<DesktopLyricsManager>();
    let mut manager_state = manager
        .0
        .lock()
        .map_err(|_| "desktop lyrics state lock is poisoned")?;
    let mut desired = manager_state.desired;
    change(&mut desired);
    let revision = if desired == manager_state.desired {
        manager_state.revision
    } else {
        manager_state.revision.saturating_add(1)
    };

    let plan = native_apply_plan(manager_state.applied, desired, revision);
    if !plan.is_empty() {
        apply(app, plan)?;
        manager_state.applied = Some(desired);
    }
    manager_state.desired = desired;
    manager_state.revision = revision;
    Ok(public_state(desired, revision))
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
    use super::{
        DesktopLyricsState, FullscreenBehavior, NativeApplyPlan, native_apply_plan, public_state,
    };

    #[test]
    fn fullscreen_hiding_preserves_requested_visibility() {
        let state = DesktopLyricsState {
            requested_visible: true,
            hidden_by_fullscreen: true,
            fullscreen_behavior: FullscreenBehavior::Hide,
            ..DesktopLyricsState::default()
        };
        let public = public_state(state, 3);
        assert_eq!(public.revision, 3);
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

    #[test]
    fn unchanged_applied_state_schedules_no_native_window_work() {
        let state = DesktopLyricsState {
            requested_visible: true,
            locked: true,
            hidden_by_fullscreen: false,
            fullscreen_behavior: FullscreenBehavior::Show,
        };

        assert_eq!(
            native_apply_plan(Some(state), state, 4),
            NativeApplyPlan::default()
        );
    }

    #[test]
    fn fullscreen_visibility_change_does_not_repeat_cursor_configuration() {
        let applied = DesktopLyricsState {
            requested_visible: true,
            locked: true,
            hidden_by_fullscreen: false,
            fullscreen_behavior: FullscreenBehavior::Hide,
        };
        let desired = DesktopLyricsState {
            hidden_by_fullscreen: true,
            ..applied
        };

        let plan = native_apply_plan(Some(applied), desired, 5);
        assert_eq!(plan.ignore_cursor_events, None);
        assert_eq!(plan.visible, Some(false));
        assert_eq!(plan.state_event, Some(public_state(desired, 5)));
    }
}
