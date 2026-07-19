use tauri::{
    App, AppHandle, Manager,
    menu::MenuBuilder,
    tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
};

const MAIN_WINDOW: &str = "main";
const TRAY_ID: &str = "xymusic-tray";
const SHOW_MENU_ID: &str = "tray-show";
const EXIT_MENU_ID: &str = "tray-exit";

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum TrayMenuAction {
    Show,
    Exit,
}

fn menu_action(id: &str) -> Option<TrayMenuAction> {
    match id {
        SHOW_MENU_ID => Some(TrayMenuAction::Show),
        EXIT_MENU_ID => Some(TrayMenuAction::Exit),
        _ => None,
    }
}

pub fn install(app: &App) -> tauri::Result<()> {
    let menu = MenuBuilder::new(app)
        .text(SHOW_MENU_ID, "打开 XyMusic")
        .separator()
        .text(EXIT_MENU_ID, "退出")
        .build()?;
    let mut builder = TrayIconBuilder::with_id(TRAY_ID)
        .menu(&menu)
        .show_menu_on_left_click(false)
        .tooltip("XyMusic")
        .on_menu_event(|app, event| match menu_action(event.id().as_ref()) {
            Some(TrayMenuAction::Show) => {
                let _ = show_main_window(app);
            }
            Some(TrayMenuAction::Exit) => app.exit(0),
            None => {}
        })
        .on_tray_icon_event(|tray, event| {
            if matches!(
                event,
                TrayIconEvent::Click {
                    button: MouseButton::Left,
                    button_state: MouseButtonState::Up,
                    ..
                }
            ) {
                let _ = show_main_window(tray.app_handle());
            }
        });
    if let Some(icon) = app.default_window_icon().cloned() {
        builder = builder.icon(icon);
    }
    builder.build(app)?;
    Ok(())
}

pub fn show_main_window(app: &AppHandle) -> Result<(), String> {
    let window = app
        .get_webview_window(MAIN_WINDOW)
        .ok_or_else(|| "main window is unavailable".to_string())?;
    window.show().map_err(|error| error.to_string())?;
    window.set_focus().map_err(|error| error.to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn tray_identifiers_remain_distinct() {
        assert_ne!(SHOW_MENU_ID, EXIT_MENU_ID);
        assert_ne!(TRAY_ID, SHOW_MENU_ID);
    }

    #[test]
    fn tray_menu_actions_are_strictly_mapped() {
        assert_eq!(menu_action(SHOW_MENU_ID), Some(TrayMenuAction::Show));
        assert_eq!(menu_action(EXIT_MENU_ID), Some(TrayMenuAction::Exit));
        assert_eq!(menu_action("unknown"), None);
    }
}
