use std::sync::Mutex;
use std::sync::mpsc::sync_channel;
use tauri::{AppHandle, LogicalSize, Manager, PhysicalPosition, PhysicalSize, Size, WebviewWindow};

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

#[derive(Clone, Copy, Debug)]
struct TrueFullscreenSnapshot {
    maximized: bool,
}

#[derive(Default)]
pub struct TrueFullscreenState(Mutex<Option<TrueFullscreenSnapshot>>);

#[tauri::command]
pub fn hide_main_window(app: AppHandle) -> Result<(), String> {
    let window = app
        .get_webview_window(MAIN_WINDOW)
        .ok_or_else(|| "main window is unavailable".to_string())?;
    window.hide().map_err(|error| error.to_string())
}

#[tauri::command]
pub fn toggle_main_window_maximize(app: AppHandle) -> Result<(), String> {
    let window = app
        .get_webview_window(MAIN_WINDOW)
        .ok_or_else(|| "main window is unavailable".to_string())?;
    if window.is_fullscreen().map_err(|error| error.to_string())? {
        return Ok(());
    }
    if window.is_maximized().map_err(|error| error.to_string())? {
        window.unmaximize().map_err(|error| error.to_string())
    } else {
        window.maximize().map_err(|error| error.to_string())
    }
}

#[tauri::command]
pub fn toggle_main_window_fullscreen(app: AppHandle) -> Result<(), String> {
    let window = app
        .get_webview_window(MAIN_WINDOW)
        .ok_or_else(|| "main window is unavailable".to_string())?;
    let fullscreen_state = app.state::<TrueFullscreenState>();

    if window.is_fullscreen().map_err(|error| error.to_string())? {
        let restore_maximized = fullscreen_state
            .0
            .lock()
            .map_err(|_| "true fullscreen state lock is poisoned")?
            .as_ref()
            .is_some_and(|snapshot| snapshot.maximized);
        let result = run_on_main_window_thread(&window, move |window| {
            window
                .set_fullscreen(false)
                .map_err(|error| error.to_string())?;
            window
                .set_resizable(true)
                .map_err(|error| error.to_string())?;
            if restore_maximized {
                window.maximize().map_err(|error| error.to_string())?;
            }
            Ok(())
        });
        if result.is_ok() {
            fullscreen_state
                .0
                .lock()
                .map_err(|_| "true fullscreen state lock is poisoned")?
                .take();
        }
        return result;
    }

    let mini_mode_active = {
        let state = app.state::<MiniModeState>();
        let snapshot = state
            .0
            .lock()
            .map_err(|_| "mini-mode state lock is poisoned")?;
        snapshot.is_some()
    };
    if mini_mode_active {
        return Err("true fullscreen is unavailable in mini mode".to_string());
    }

    let was_maximized = window.is_maximized().map_err(|error| error.to_string())?;
    let result = run_on_main_window_thread(&window, move |window| {
        let transition = (|| {
            if was_maximized {
                window.unmaximize().map_err(|error| error.to_string())?;
            }
            window
                .set_resizable(false)
                .map_err(|error| error.to_string())?;
            window
                .set_fullscreen(true)
                .map_err(|error| error.to_string())?;
            Ok(())
        })();
        if transition.is_err() {
            let _ = window.set_resizable(true);
            if was_maximized {
                let _ = window.maximize();
            }
        }
        transition
    });
    if result.is_ok() {
        *fullscreen_state
            .0
            .lock()
            .map_err(|_| "true fullscreen state lock is poisoned")? =
            Some(TrueFullscreenSnapshot {
                maximized: was_maximized,
            });
    }
    result
}

// tao applies fullscreen bounds on its window thread. Waiting for this callback keeps
// the maximize/fullscreen operations ordered instead of exposing an intermediate state.
fn run_on_main_window_thread<F>(window: &WebviewWindow, operation: F) -> Result<(), String>
where
    F: FnOnce(&WebviewWindow) -> Result<(), String> + Send + 'static,
{
    let (sender, receiver) = sync_channel(1);
    let target = window.clone();
    window
        .run_on_main_thread(move || {
            let _ = sender.send(operation(&target));
        })
        .map_err(|error| error.to_string())?;
    receiver
        .recv()
        .map_err(|_| "window thread did not complete the mode transition".to_string())?
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

    if window.is_fullscreen().map_err(|error| error.to_string())? {
        return Err("mini mode cannot change while true fullscreen is active".to_string());
    }

    if enabled {
        if snapshot.is_some() {
            return Ok(());
        }
        *snapshot = Some(WindowSnapshot {
            size: window.outer_size().map_err(|error| error.to_string())?,
            position: window.outer_position().map_err(|error| error.to_string())?,
            maximized: window.is_maximized().map_err(|error| error.to_string())?,
        });
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
