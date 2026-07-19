use serde::{Deserialize, Serialize};
use std::sync::Mutex;
use tauri::{AppHandle, Emitter, Manager};

const MEDIA_ACTION_EVENT: &str = "desktop://media-action";

#[derive(Clone, Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct DesktopMediaMetadata {
    title: String,
    artist: Option<String>,
    album: Option<String>,
    artwork_url: Option<String>,
}

#[derive(Clone, Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct DesktopPlaybackState {
    status: String,
    position: Option<f64>,
    duration: Option<f64>,
}

#[derive(Clone, Debug, Serialize)]
struct MediaActionPayload {
    action: &'static str,
    #[serde(skip_serializing_if = "Option::is_none")]
    position: Option<f64>,
}

#[cfg(windows)]
mod platform {
    use super::*;
    use windows::{
        Foundation::{TimeSpan, TypedEventHandler, Uri},
        Media::Playback::MediaPlayer,
        Media::{
            MediaPlaybackStatus, MediaPlaybackType, PlaybackPositionChangeRequestedEventArgs,
            SystemMediaTransportControls, SystemMediaTransportControlsButton,
            SystemMediaTransportControlsButtonPressedEventArgs,
            SystemMediaTransportControlsTimelineProperties,
        },
        Storage::Streams::RandomAccessStreamReference,
    };

    pub struct WindowsMediaSession {
        _player: MediaPlayer,
        controls: SystemMediaTransportControls,
        button_token: i64,
        position_token: i64,
    }

    impl WindowsMediaSession {
        pub fn new(app: &AppHandle) -> Result<Self, String> {
            let player = MediaPlayer::new().map_err(win_error)?;
            let controls = player.SystemMediaTransportControls().map_err(win_error)?;
            if let Ok(manager) = player.CommandManager() {
                manager.SetIsEnabled(false).map_err(win_error)?;
            }
            controls.SetIsPlayEnabled(true).map_err(win_error)?;
            controls.SetIsPauseEnabled(true).map_err(win_error)?;
            controls.SetIsStopEnabled(true).map_err(win_error)?;
            controls.SetIsPreviousEnabled(true).map_err(win_error)?;
            controls.SetIsNextEnabled(true).map_err(win_error)?;

            let button_app = app.clone();
            let button_token = controls
                .ButtonPressed(&TypedEventHandler::<
                    SystemMediaTransportControls,
                    SystemMediaTransportControlsButtonPressedEventArgs,
                >::new(move |_sender, args| {
                    if let Ok(button) = args.ok().and_then(|args| args.Button()) {
                        let action = if button == SystemMediaTransportControlsButton::Play {
                            Some("play")
                        } else if button == SystemMediaTransportControlsButton::Pause {
                            Some("pause")
                        } else if button == SystemMediaTransportControlsButton::Stop {
                            Some("stop")
                        } else if button == SystemMediaTransportControlsButton::Previous {
                            Some("previous")
                        } else if button == SystemMediaTransportControlsButton::Next {
                            Some("next")
                        } else {
                            None
                        };
                        if let Some(action) = action {
                            let _ = button_app.emit(
                                MEDIA_ACTION_EVENT,
                                MediaActionPayload {
                                    action,
                                    position: None,
                                },
                            );
                        }
                    }
                    Ok(())
                }))
                .map_err(win_error)?;

            let position_app = app.clone();
            let position_token = controls
                .PlaybackPositionChangeRequested(&TypedEventHandler::<
                    SystemMediaTransportControls,
                    PlaybackPositionChangeRequestedEventArgs,
                >::new(move |_sender, args| {
                    if let Ok(position) =
                        args.ok().and_then(|args| args.RequestedPlaybackPosition())
                    {
                        let _ = position_app.emit(
                            MEDIA_ACTION_EVENT,
                            MediaActionPayload {
                                action: "seek",
                                position: Some(position.Duration as f64 / 10_000_000.0),
                            },
                        );
                    }
                    Ok(())
                }))
                .map_err(win_error)?;

            Ok(Self {
                _player: player,
                controls,
                button_token,
                position_token,
            })
        }

        pub fn update_metadata(&self, metadata: DesktopMediaMetadata) -> Result<(), String> {
            let updater = self.controls.DisplayUpdater().map_err(win_error)?;
            updater
                .SetType(MediaPlaybackType::Music)
                .map_err(win_error)?;
            let music = updater.MusicProperties().map_err(win_error)?;
            music.SetTitle(&metadata.title.into()).map_err(win_error)?;
            music
                .SetArtist(&metadata.artist.unwrap_or_default().into())
                .map_err(win_error)?;
            music
                .SetAlbumTitle(&metadata.album.unwrap_or_default().into())
                .map_err(win_error)?;
            if let Some(url) = metadata
                .artwork_url
                .filter(|value| !value.trim().is_empty())
            {
                if let Ok(uri) = Uri::CreateUri(&url.into()) {
                    if let Ok(thumbnail) = RandomAccessStreamReference::CreateFromUri(&uri) {
                        updater.SetThumbnail(&thumbnail).map_err(win_error)?;
                    }
                }
            }
            updater.Update().map_err(win_error)?;
            self.controls.SetIsEnabled(true).map_err(win_error)
        }

        pub fn update_playback(&self, state: DesktopPlaybackState) -> Result<(), String> {
            let status = match state.status.as_str() {
                "playing" => MediaPlaybackStatus::Playing,
                "paused" => MediaPlaybackStatus::Paused,
                "stopped" => MediaPlaybackStatus::Stopped,
                _ => return Err("playback status must be playing, paused, or stopped".into()),
            };
            self.controls.SetPlaybackStatus(status).map_err(win_error)?;
            if let (Some(position), Some(duration)) = (state.position, state.duration) {
                if position.is_finite()
                    && duration.is_finite()
                    && position >= 0.0
                    && duration >= 0.0
                {
                    let timeline =
                        SystemMediaTransportControlsTimelineProperties::new().map_err(win_error)?;
                    let position = position.min(duration);
                    timeline.SetStartTime(seconds(0.0)).map_err(win_error)?;
                    timeline.SetMinSeekTime(seconds(0.0)).map_err(win_error)?;
                    timeline.SetPosition(seconds(position)).map_err(win_error)?;
                    timeline
                        .SetMaxSeekTime(seconds(duration))
                        .map_err(win_error)?;
                    timeline.SetEndTime(seconds(duration)).map_err(win_error)?;
                    self.controls
                        .UpdateTimelineProperties(&timeline)
                        .map_err(win_error)?;
                }
            }
            self.controls.SetIsEnabled(true).map_err(win_error)
        }

        pub fn clear(&self) -> Result<(), String> {
            self.controls
                .DisplayUpdater()
                .and_then(|updater| updater.ClearAll())
                .map_err(win_error)?;
            self.controls
                .SetPlaybackStatus(MediaPlaybackStatus::Closed)
                .map_err(win_error)?;
            self.controls.SetIsEnabled(false).map_err(win_error)
        }
    }

    impl Drop for WindowsMediaSession {
        fn drop(&mut self) {
            let _ = self.controls.RemoveButtonPressed(self.button_token);
            let _ = self
                .controls
                .RemovePlaybackPositionChangeRequested(self.position_token);
            let _ = self.controls.SetIsEnabled(false);
        }
    }

    fn seconds(value: f64) -> TimeSpan {
        TimeSpan {
            Duration: (value * 10_000_000.0).round() as i64,
        }
    }

    fn win_error(error: windows::core::Error) -> String {
        error.to_string()
    }
}

#[cfg(not(windows))]
mod platform {
    use super::*;

    pub struct WindowsMediaSession;

    impl WindowsMediaSession {
        pub fn new(_app: &AppHandle) -> Result<Self, String> {
            Ok(Self)
        }
        pub fn update_metadata(&self, _metadata: DesktopMediaMetadata) -> Result<(), String> {
            Ok(())
        }
        pub fn update_playback(&self, _state: DesktopPlaybackState) -> Result<(), String> {
            Ok(())
        }
        pub fn clear(&self) -> Result<(), String> {
            Ok(())
        }
    }
}

pub struct MediaSessionState(Mutex<Option<platform::WindowsMediaSession>>);

impl MediaSessionState {
    pub fn initialize(app: &AppHandle) -> Result<Self, String> {
        Ok(Self(Mutex::new(Some(platform::WindowsMediaSession::new(
            app,
        )?))))
    }

    fn with_session<T>(
        &self,
        operation: impl FnOnce(&platform::WindowsMediaSession) -> Result<T, String>,
    ) -> Result<T, String> {
        let session = self
            .0
            .lock()
            .map_err(|_| "media-session state lock is poisoned")?;
        operation(
            session
                .as_ref()
                .ok_or_else(|| "media session is unavailable".to_string())?,
        )
    }
}

#[tauri::command]
pub fn update_media_metadata(app: AppHandle, metadata: DesktopMediaMetadata) -> Result<(), String> {
    app.state::<MediaSessionState>()
        .with_session(|session| session.update_metadata(metadata))
}

#[tauri::command]
pub fn update_media_playback(app: AppHandle, state: DesktopPlaybackState) -> Result<(), String> {
    app.state::<MediaSessionState>()
        .with_session(|session| session.update_playback(state))
}

#[tauri::command]
pub fn clear_media_session(app: AppHandle) -> Result<(), String> {
    app.state::<MediaSessionState>()
        .with_session(platform::WindowsMediaSession::clear)
}
