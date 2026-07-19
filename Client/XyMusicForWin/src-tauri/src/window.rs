use std::sync::Mutex;
use tauri::{AppHandle, LogicalSize, Manager, PhysicalPosition, PhysicalSize, Size};

const MAIN_WINDOW: &str = "main";
const NORMAL_MIN_WIDTH: f64 = 1000.0;
const NORMAL_MIN_HEIGHT: f64 = 640.0;
const MINI_WIDTH: f64 = 420.0;
const MINI_HEIGHT: f64 = 156.0;

#[derive(Clone, Copy, Debug)]
struct WindowSnapshot {
    size: PhysicalSize<u32>,
    position: PhysicalPosition<i32>,
    maximized: bool,
}

#[derive(Default)]
pub struct MiniModeState(Mutex<Option<WindowSnapshot>>);

#[tauri::command]
pub fn hide_main_window(app: AppHandle) -> Result<(), String> {
    let window = app
        .get_webview_window(MAIN_WINDOW)
        .ok_or_else(|| "main window is unavailable".to_string())?;
    window.hide().map_err(|error| error.to_string())
}

#[tauri::command]
pub fn set_mini_mode(app: AppHandle, enabled: bool) -> Result<(), String> {
    let window = app
        .get_webview_window(MAIN_WINDOW)
        .ok_or_else(|| "main window is unavailable".to_string())?;
    let state = app.state::<MiniModeState>();
    let mut snapshot = state
        .0
        .lock()
        .map_err(|_| "mini-mode state lock is poisoned")?;

    if enabled {
        if snapshot.is_some() {
            return Ok(());
        }
        *snapshot = Some(WindowSnapshot {
            size: window.outer_size().map_err(|error| error.to_string())?,
            position: window.outer_position().map_err(|error| error.to_string())?,
            maximized: window.is_maximized().map_err(|error| error.to_string())?,
        });
        if window.is_fullscreen().map_err(|error| error.to_string())? {
            window
                .set_fullscreen(false)
                .map_err(|error| error.to_string())?;
        }
        if window.is_maximized().map_err(|error| error.to_string())? {
            window.unmaximize().map_err(|error| error.to_string())?;
        }
        window
            .set_min_size(None::<Size>)
            .map_err(|error| error.to_string())?;
        window
            .set_max_size(Some(LogicalSize::new(MINI_WIDTH, MINI_HEIGHT)))
            .map_err(|error| error.to_string())?;
        window
            .set_size(LogicalSize::new(MINI_WIDTH, MINI_HEIGHT))
            .map_err(|error| error.to_string())?;
        window
            .set_resizable(false)
            .map_err(|error| error.to_string())?;
        window
            .set_always_on_top(true)
            .map_err(|error| error.to_string())?;
        return Ok(());
    }

    let Some(previous) = snapshot.take() else {
        return Ok(());
    };
    window
        .set_always_on_top(false)
        .map_err(|error| error.to_string())?;
    window
        .set_resizable(true)
        .map_err(|error| error.to_string())?;
    window
        .set_max_size(None::<Size>)
        .map_err(|error| error.to_string())?;
    window
        .set_min_size(Some(LogicalSize::new(NORMAL_MIN_WIDTH, NORMAL_MIN_HEIGHT)))
        .map_err(|error| error.to_string())?;
    window
        .set_size(previous.size)
        .map_err(|error| error.to_string())?;
    window
        .set_position(previous.position)
        .map_err(|error| error.to_string())?;
    if previous.maximized {
        window.maximize().map_err(|error| error.to_string())?;
    }
    Ok(())
}
