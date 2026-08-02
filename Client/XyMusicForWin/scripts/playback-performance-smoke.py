"""Exercise authenticated playback, lyrics, and a virtualized long list locally.

The app remains served by Vite. Every API and audio request is intercepted in
the browser so this smoke test never contacts a configured music server.

Start Vite with ``npm run dev``, then run ``npm run test:performance-smoke``.
"""

from __future__ import annotations

import json
import math
import os
import re
import struct
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import parse_qs, urlparse

from playwright.sync_api import Page, Route, sync_playwright


APP_URL = os.environ.get("XYMUSIC_SMOKE_APP_URL", "http://127.0.0.1:1420")
ARTIFACTS = Path(os.environ.get("XYMUSIC_SMOKE_ARTIFACTS", "artifacts"))
REPORT_PATH = ARTIFACTS / "playback-performance-smoke.json"
SCREENSHOT_PATH = ARTIFACTS / "playback-performance-smoke.png"
LYRICS_SCREENSHOT_PATH = ARTIFACTS / "playback-performance-lyrics.png"
ACCESS_TOKEN = "performance-smoke-access"
TRACK_ID = "track-1"
ALBUM_ID = "album-1"
HISTORY_CURSOR = "history-page-2"


def make_track(index: int) -> dict[str, object]:
    return {
        "id": f"track-{index}",
        "title": f"Performance Track {index:03d}",
        "artists": [{"id": "artist-1", "name": "Performance Artist"}],
        "album": {"id": ALBUM_ID, "title": "Performance Album"},
        "artwork": None,
        "durationMs": 24_000,
        "isFavorite": False,
        "publishedAt": "2026-08-02T00:00:00.000Z",
    }


TRACKS = [make_track(index) for index in range(1, 101)]
FEATURED_ALBUM = {
    "id": ALBUM_ID,
    "title": "Performance Album",
    "artists": [{"id": "artist-1", "name": "Performance Artist"}],
    "cover": None,
    "releaseDate": "2026-08-02",
    "trackCount": len(TRACKS),
    "description": "Local performance smoke data.",
}


def positive_int_from_environment(name: str, default: int, *, maximum: int) -> int:
    value = os.environ.get(name)
    if value is None:
        return default
    try:
        parsed = int(value)
    except ValueError as error:
        raise ValueError(f"{name} must be an integer") from error
    if not 1 <= parsed <= maximum:
        raise ValueError(f"{name} must be between 1 and {maximum}")
    return parsed


def positive_float_from_environment(name: str, default: float) -> float:
    value = os.environ.get(name)
    if value is None:
        return default
    try:
        parsed = float(value)
    except ValueError as error:
        raise ValueError(f"{name} must be a number") from error
    if not math.isfinite(parsed) or parsed <= 0:
        raise ValueError(f"{name} must be a finite number greater than zero")
    return parsed


def cpu_throttle_rate_from_environment() -> float:
    value = os.environ.get("XYMUSIC_SMOKE_CPU_THROTTLE")
    if value is None:
        return 1
    try:
        parsed = float(value)
    except ValueError as error:
        raise ValueError("XYMUSIC_SMOKE_CPU_THROTTLE must be a number") from error
    if not math.isfinite(parsed) or not 1 <= parsed <= 8:
        raise ValueError("XYMUSIC_SMOKE_CPU_THROTTLE must be between 1 and 8")
    return parsed


LYRIC_LINE_COUNT = positive_int_from_environment("XYMUSIC_SMOKE_LYRIC_LINES", 12, maximum=2_000)
LYRIC_LINE_SPACING_SECONDS = positive_float_from_environment(
    "XYMUSIC_SMOKE_LYRIC_LINE_SPACING_SECONDS",
    2,
)
RAPID_LYRIC_LINE_INTERVAL_SECONDS = 0.25
CPU_THROTTLE_RATE = cpu_throttle_rate_from_environment()


def make_word_lrc() -> str:
    lines: list[str] = []
    word_spacing = LYRIC_LINE_SPACING_SECONDS / 4
    for index in range(LYRIC_LINE_COUNT):
        start = index * LYRIC_LINE_SPACING_SECONDS
        lines.append(
            f"[{timestamp(start)}]"
            f"<{timestamp(start)}>Frame"
            f"<{timestamp(start + word_spacing)}> stable"
            f"<{timestamp(start + word_spacing * 2)}> while"
            f"<{timestamp(start + word_spacing * 3)}> playing"
        )
    return "\n".join(lines)


def timestamp(seconds: float) -> str:
    minutes, remainder = divmod(seconds, 60)
    return f"{int(minutes):02d}:{remainder:05.2f}"


WORD_LRC = make_word_lrc()


def make_silent_wav(duration_seconds: int = 26, sample_rate: int = 8_000) -> bytes:
    sample_count = duration_seconds * sample_rate
    pcm = b"\x00\x00" * sample_count
    byte_rate = sample_rate * 2
    return b"".join(
        [
            b"RIFF",
            struct.pack("<I", 36 + len(pcm)),
            b"WAVEfmt ",
            struct.pack("<IHHIIHH", 16, 1, 1, sample_rate, byte_rate, 2, 16),
            b"data",
            struct.pack("<I", len(pcm)),
            pcm,
        ]
    )


AUDIO_BYTES = make_silent_wav()


class LocalApi:
    def __init__(self) -> None:
        self.requests: list[dict[str, object]] = []
        self.contract_failures: list[str] = []

    def handle(self, route: Route) -> None:
        request = route.request
        parsed = urlparse(request.url)
        path = parsed.path
        method = request.method
        query = parse_qs(parsed.query)
        self.requests.append({"method": method, "path": path, "query": query})

        if path != "/api/v1/auth/login" and path != "/api/v1/oss/perf.wav":
            authorization = request.headers.get("authorization")
            if authorization != f"Bearer {ACCESS_TOKEN}":
                self.contract_failures.append(f"Missing bearer token for {method} {path}")

        if method == "POST" and path == "/api/v1/auth/login":
            body = self._json_body(request, "login")
            if body.get("username") != "performance" or body.get("password") != "performance":
                self.contract_failures.append("Login request did not contain the smoke credentials")
            device = body.get("device")
            if not isinstance(device, dict) or not device.get("installationId") or device.get("platform") != "WINDOWS":
                self.contract_failures.append("Login request did not contain a valid Windows device payload")
            self._json(
                route,
                {
                    "user": {
                        "id": "performance-user",
                        "username": "performance",
                        "displayName": "Performance Smoke",
                        "bio": None,
                        "role": "USER",
                        "version": 1,
                    },
                    "tokens": {"accessToken": ACCESS_TOKEN, "refreshToken": "performance-smoke-refresh"},
                },
            )
            return

        if method == "GET" and path == "/api/v1/tracks":
            if query.get("albumId") == [ALBUM_ID]:
                self._json(route, {"items": TRACKS, "nextCursor": None})
            else:
                self._json(route, {"items": [], "nextCursor": None})
            return

        if method == "GET" and path == "/api/v1/albums":
            self._json(route, {"items": [FEATURED_ALBUM], "nextCursor": None})
            return

        if method == "GET" and path == "/api/v1/playlists":
            self._json(route, {"items": [], "nextCursor": None})
            return

        if method == "POST" and path == "/api/v1/albums/random":
            if self._json_body(request, "random albums").get("limit") != 5:
                self.contract_failures.append("Random albums request did not ask for five items")
            self._json(route, {"items": []})
            return

        if method == "POST" and path == "/api/v1/tracks/random":
            if self._json_body(request, "random tracks").get("limit") != 10:
                self.contract_failures.append("Random tracks request did not ask for ten items")
            self._json(route, {"items": TRACKS[:10]})
            return

        if method == "GET" and path == "/api/v1/library/history":
            if query.get("limit") != ["50"]:
                self.contract_failures.append(f"History request used an unexpected limit: {query}")
            if "cursor" not in query:
                self._json(route, {"items": [{"track": track} for track in TRACKS[:50]], "nextCursor": HISTORY_CURSOR})
            elif query.get("cursor") == [HISTORY_CURSOR]:
                self._json(route, {"items": [{"track": track} for track in TRACKS[50:]], "nextCursor": None})
            else:
                self.contract_failures.append(f"History request used an unexpected cursor: {query}")
                self._json(route, {"items": [], "nextCursor": None})
            return

        if method == "POST" and re.fullmatch(r"/api/v1/tracks/track-\d+/playback", path):
            body = self._json_body(request, "playback grant")
            if body.get("preferredQuality") != "AUTO" or body.get("acceptedCodecs") != ["aac", "mp3", "flac", "opus"]:
                self.contract_failures.append("Playback grant request did not contain the expected quality and codecs")
            self._json(
                route,
                {
                    "url": "/api/v1/oss/perf.wav",
                    "expiresAt": "2099-01-01T00:00:00.000Z",
                    "selectedQuality": "AUTO",
                },
            )
            return

        if method == "PUT" and re.fullmatch(r"/api/v1/library/history/track-\d+", path):
            body = self._json_body(request, "playback history")
            if not isinstance(body.get("playbackSessionId"), str) or not body.get("playbackSessionId"):
                self.contract_failures.append("Playback history request did not contain a session id")
            if not isinstance(body.get("positionMs"), (int, float)) or body["positionMs"] < 0:
                self.contract_failures.append("Playback history request did not contain a valid position")
            if body.get("event") not in {"STARTED", "PAUSED", "ENDED"}:
                self.contract_failures.append("Playback history request did not contain a valid event")
            if not request.headers.get("idempotency-key"):
                self.contract_failures.append("Playback history request did not contain an idempotency key")
            route.fulfill(status=204)
            return

        if method == "GET" and path == f"/api/v1/tracks/{TRACK_ID}":
            detail = {
                **TRACKS[0],
                "lyric": {
                    "id": "lyric-1",
                    "trackId": TRACK_ID,
                    "language": "en",
                    "format": "LRC",
                    "timing": "WORD",
                    "content": WORD_LRC,
                    "trackVersion": 1,
                    "updatedAt": "2026-08-02T00:00:00.000Z",
                },
            }
            self._json(route, detail)
            return

        if method == "GET" and path == "/api/v1/oss/perf.wav":
            self._audio(route)
            return

        self.contract_failures.append(f"Unexpected API request: {method} {path}?{parsed.query}")
        self._json(route, {"error": "not mocked"}, status=404)

    def _json_body(self, request, endpoint: str) -> dict[str, object]:
        try:
            value = json.loads(request.post_data or "")
        except json.JSONDecodeError:
            self.contract_failures.append(f"{endpoint} request body was not valid JSON")
            return {}
        if not isinstance(value, dict):
            self.contract_failures.append(f"{endpoint} request body was not an object")
            return {}
        return value

    @staticmethod
    def _json(route: Route, value: object, status: int = 200) -> None:
        route.fulfill(
            status=status,
            content_type="application/json; charset=utf-8",
            body=json.dumps(value),
        )

    @staticmethod
    def _audio(route: Route) -> None:
        range_header = route.request.headers.get("range", "")
        match = re.fullmatch(r"bytes=(\d+)-(\d*)", range_header)
        if match:
            start = min(int(match.group(1)), len(AUDIO_BYTES) - 1)
            end = min(int(match.group(2)) if match.group(2) else len(AUDIO_BYTES) - 1, len(AUDIO_BYTES) - 1)
            body = AUDIO_BYTES[start:end + 1]
            route.fulfill(
                status=206,
                body=body,
                headers={
                    "Content-Type": "audio/wav",
                    "Accept-Ranges": "bytes",
                    "Content-Range": f"bytes {start}-{end}/{len(AUDIO_BYTES)}",
                    "Content-Length": str(len(body)),
                },
            )
            return
        route.fulfill(
            status=200,
            body=AUDIO_BYTES,
            headers={
                "Content-Type": "audio/wav",
                "Accept-Ranges": "bytes",
                "Content-Length": str(len(AUDIO_BYTES)),
            },
        )


def start_frame_sampler(page: Page, name: str) -> None:
    page.evaluate(
        """name => {
          const samplers = window.__xymusicFrameSamplers ??= {};
          const samples = [];
          let previous = performance.now();
          const tick = now => {
            samples.push(now - previous);
            previous = now;
            samplers[name].frame = requestAnimationFrame(tick);
          };
          samplers[name] = { samples, frame: requestAnimationFrame(tick) };
        }""",
        name,
    )


def stop_frame_sampler(page: Page, name: str) -> dict[str, float | int]:
    return page.evaluate(
        """name => {
          const sampler = window.__xymusicFrameSamplers?.[name];
          if (!sampler) return { count: 0, p95: 0, max: 0, over50: 0 };
          cancelAnimationFrame(sampler.frame);
          const samples = sampler.samples.slice(1).sort((left, right) => left - right);
          const percentile = ratio => samples.length ? samples[Math.min(samples.length - 1, Math.floor((samples.length - 1) * ratio))] : 0;
          return {
            count: samples.length,
            p95: Number(percentile(0.95).toFixed(2)),
            max: Number((samples.at(-1) ?? 0).toFixed(2)),
            over50: samples.filter(value => value > 50).length,
          };
        }""",
        name,
    )


def sample_current_word_progress(page: Page, count: int = 8) -> list[float]:
    return page.evaluate(
        """async sampleCount => {
          const readProgress = () => {
            const word = document.querySelector('.lyrics-view .lyric-word.is-current');
            const progress = Number.parseFloat(word?.style.getPropertyValue('--lyric-word-progress') ?? '');
            return Number.isFinite(progress) ? progress : -1;
          };
          const samples = [];
          for (let index = 0; index < sampleCount; index += 1) {
            await new Promise(requestAnimationFrame);
            samples.push(readProgress());
          }
          return samples;
        }""",
        count,
    )


def track_lyric_auto_follow_scrolls(page: Page) -> None:
    page.evaluate(
        """() => {
          const calls = [];
          const original = HTMLElement.prototype.scrollIntoView;
          window.__xymusicLyricAutoFollowScrolls = calls;
          HTMLElement.prototype.scrollIntoView = function(options) {
            if (this.classList.contains('lyric-line')) {
              const behavior = options && typeof options === 'object' ? options.behavior ?? 'auto' : 'auto';
              calls.push(behavior);
            }
            return original.call(this, options);
          };
        }"""
    )


def lyric_auto_follow_scrolls(page: Page) -> dict[str, int]:
    return page.evaluate(
        """() => {
          const calls = window.__xymusicLyricAutoFollowScrolls ?? [];
          return calls.reduce((counts, behavior) => {
            counts[behavior] = (counts[behavior] ?? 0) + 1;
            return counts;
          }, {});
        }"""
    )


def scroll_virtual_list(page: Page) -> dict[str, object]:
    return page.evaluate(
        """async () => {
          const group = document.querySelector('.track-row-group');
          if (!group) throw new Error('Track row group is unavailable');
          let scroller = group.parentElement;
          while (scroller && !['auto', 'scroll'].includes(getComputedStyle(scroller).overflowY)) scroller = scroller.parentElement;
          const target = scroller ?? document.scrollingElement;
          if (!target) throw new Error('Scrollable parent is unavailable');
          const maximum = Math.max(0, target.scrollHeight - target.clientHeight);
          await new Promise(resolve => {
            let step = 0;
            const steps = 72;
            const tick = () => {
              target.scrollTop = maximum * (step / steps);
              target.dispatchEvent(new Event('scroll'));
              step += 1;
              if (step <= steps) requestAnimationFrame(tick);
              else resolve();
            };
            requestAnimationFrame(tick);
          });
          await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
          return { tagName: target.tagName, maximum, scrollTop: target.scrollTop };
        }"""
    )


def assert_frame_data(name: str, metric: dict[str, float | int]) -> None:
    if metric["count"] < 30:
        raise AssertionError(f"{name} frame sampling returned too few frames: {metric}")
    if metric["p95"] > 50:
        raise AssertionError(f"{name} frame sampling p95 exceeded 50 ms: {metric}")
    if metric["over50"] > max(2, int(metric["count"]) // 20):
        raise AssertionError(f"{name} frame sampling had too many frames above 50 ms: {metric}")


def require(condition: bool, message: str) -> None:
    if not condition:
        raise AssertionError(message)


def verify_required_requests(api: LocalApi) -> None:
    observed = {(str(item["method"]), str(item["path"])) for item in api.requests}
    required = {
        ("POST", "/api/v1/auth/login"),
        ("POST", f"/api/v1/tracks/{TRACK_ID}/playback"),
        ("GET", "/api/v1/oss/perf.wav"),
        ("PUT", f"/api/v1/library/history/{TRACK_ID}"),
        ("GET", f"/api/v1/tracks/{TRACK_ID}"),
        ("GET", "/api/v1/library/history"),
    }
    missing = sorted(required - observed)
    if missing:
        api.contract_failures.append(f"Required API requests were not observed: {missing}")
    history_queries = [item["query"] for item in api.requests if item["path"] == "/api/v1/library/history"]
    expected_history_queries = [
        {"limit": ["50"]},
        {"limit": ["50"], "cursor": [HISTORY_CURSOR]},
    ]
    if history_queries != expected_history_queries:
        api.contract_failures.append(f"History pagination requests were unexpected: {history_queries}")


def main() -> None:
    ARTIFACTS.mkdir(parents=True, exist_ok=True)
    app_url = urlparse(APP_URL)
    if app_url.scheme not in {"http", "https"} or not app_url.hostname:
        raise ValueError("XYMUSIC_SMOKE_APP_URL must be an absolute HTTP(S) URL")
    server_port = app_url.port or (443 if app_url.scheme == "https" else 80)
    report: dict[str, object] = {
        "appUrl": APP_URL,
        "cpuThrottleRate": CPU_THROTTLE_RATE,
        "startedAt": datetime.now(timezone.utc).isoformat(),
        "metrics": {},
        "virtualization": {},
        "consoleErrors": [],
        "pageErrors": [],
        "requestFailures": [],
        "contractFailures": [],
    }
    api = LocalApi()

    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True)
        try:
            context = browser.new_context(viewport={"width": 1440, "height": 900}, device_scale_factor=1)
            page = context.new_page()
            if CPU_THROTTLE_RATE > 1:
                context.new_cdp_session(page).send(
                    "Emulation.setCPUThrottlingRate",
                    {"rate": CPU_THROTTLE_RATE},
                )
            console_errors = report["consoleErrors"]
            page_errors = report["pageErrors"]
            request_failures = report["requestFailures"]
            assert isinstance(console_errors, list)
            assert isinstance(page_errors, list)
            assert isinstance(request_failures, list)
            page.on("console", lambda message: console_errors.append(message.text) if message.type == "error" else None)
            page.on("pageerror", lambda error: page_errors.append(str(error)))
            page.on("requestfailed", lambda request: request_failures.append(f"{request.method} {request.url}: {request.failure}"))
            page.route("**/api/v1/**", api.handle)

            page.goto(APP_URL, wait_until="networkidle", timeout=20_000)
            page.locator(".login-card").wait_for(state="visible", timeout=10_000)
            page.locator(".server-protocol select").select_option(app_url.scheme)
            page.locator(".server-host input").fill(app_url.hostname)
            page.locator(".server-port input").fill(str(server_port))
            page.locator('input[autocomplete="username"]').fill("performance")
            page.locator('input[autocomplete="current-password"]').fill("performance")
            page.locator("button.login-submit").click()
            page.locator(".random-track-card .random-track-main").first.wait_for(timeout=15_000)

            require(
                not page.evaluate("document.documentElement.scrollWidth > document.documentElement.clientWidth"),
                "Authenticated desktop view has horizontal overflow",
            )

            page.locator(".random-track-card .random-track-main").first.click()
            page.locator(".player-bar").wait_for(timeout=10_000)
            progress_input = page.locator(".player-bar .progress-row input")
            progress_input.wait_for(timeout=10_000)
            page.wait_for_function(
                """() => {
                  const input = document.querySelector('.player-bar .progress-row input');
                  return input instanceof HTMLInputElement && Number(input.value) > 0.15;
                }""",
                timeout=15_000,
            )
            report["playbackProgress"] = {
                "value": progress_input.input_value(),
                "ariaValueText": progress_input.get_attribute("aria-valuetext"),
            }
            start_frame_sampler(page, "playback")
            page.wait_for_timeout(2_000)
            playback_frames = stop_frame_sampler(page, "playback")
            assert_frame_data("playback", playback_frames)
            report["metrics"] = {"playbackFrames": playback_frames}

            track_lyric_auto_follow_scrolls(page)
            page.locator(".now-playing").click()
            page.locator(".lyrics-view").wait_for(timeout=10_000)
            page.locator(".lyrics-view .lyric-word").first.wait_for(timeout=10_000)
            rendered_lyric_lines = page.locator(".lyrics-view .lyric-line").count()
            require(
                rendered_lyric_lines == LYRIC_LINE_COUNT,
                f"Lyrics view rendered an unexpected number of lines: {rendered_lyric_lines}",
            )
            page.wait_for_function("() => Boolean(document.querySelector('.lyrics-view .lyric-word.is-current'))", timeout=8_000)
            word_progress_samples = sample_current_word_progress(page)
            visible_word_progress_samples = [
                progress for progress in word_progress_samples if 0 <= progress <= 100
            ]
            require(
                len(visible_word_progress_samples) >= 2
                and max(visible_word_progress_samples) - min(visible_word_progress_samples) >= 1,
                f"Word lyric progress did not advance during playback: {word_progress_samples}",
            )
            start_frame_sampler(page, "lyrics")
            page.wait_for_timeout(2_000)
            lyrics_frames = stop_frame_sampler(page, "lyrics")
            assert_frame_data("lyrics", lyrics_frames)
            auto_follow_scrolls = lyric_auto_follow_scrolls(page)
            if LYRIC_LINE_SPACING_SECONDS < RAPID_LYRIC_LINE_INTERVAL_SECONDS:
                require(
                    auto_follow_scrolls.get("auto", 0) > 0,
                    f"Dense lyrics did not use instant auto-follow positioning: {auto_follow_scrolls}",
                )
                require(
                    auto_follow_scrolls.get("smooth", 0) <= 3,
                    f"Dense lyrics repeatedly restarted smooth auto-follow scrolling: {auto_follow_scrolls}",
                )
            page.screenshot(path=str(LYRICS_SCREENSHOT_PATH), full_page=False)
            report["metrics"] = {"playbackFrames": playback_frames, "lyricsFrames": lyrics_frames}
            report["lyrics"] = {
                "configuredLines": LYRIC_LINE_COUNT,
                "renderedLines": rendered_lyric_lines,
                "lineSpacingSeconds": LYRIC_LINE_SPACING_SECONDS,
                "wordProgressSamples": word_progress_samples,
                "visibleWordProgressSamples": visible_word_progress_samples,
                "autoFollowScrolls": auto_follow_scrolls,
            }

            page.keyboard.press("Escape")
            page.locator(".lyrics-view").wait_for(state="hidden", timeout=5_000)
            page.locator(".sidebar .nav-item").nth(1).click()
            page.locator('.track-table[aria-rowcount="51"]').wait_for(timeout=10_000)
            page.evaluate(
                """() => {
                  const main = document.querySelector('main.main-view');
                  if (!(main instanceof HTMLElement)) throw new Error('Main scroll container is unavailable');
                  main.scrollTop = Math.max(0, main.scrollHeight - main.clientHeight);
                }"""
            )
            table = page.locator('.track-table[aria-rowcount="101"]')
            table.wait_for(timeout=10_000)
            virtual_group = page.locator('.track-row-group[data-virtualized="true"]')
            virtual_group.wait_for(timeout=5_000)
            initial_rows = virtual_group.locator(".track-row").count()
            require(0 < initial_rows <= 40, f"Virtualized list rendered an unbounded initial row count: {initial_rows}")

            page.wait_for_timeout(250)
            start_frame_sampler(page, "virtualScroll")
            scroll_info = scroll_virtual_list(page)
            virtual_frames = stop_frame_sampler(page, "virtualScroll")
            assert_frame_data("virtual-scroll", virtual_frames)
            rendered_indices = virtual_group.locator(".track-row").evaluate_all(
                "rows => rows.map(row => Number(row.getAttribute('aria-rowindex'))).filter(Number.isFinite)"
            )
            require(any(index > 80 for index in rendered_indices), f"Virtual list did not advance after scrolling: {rendered_indices}")
            require(
                not page.evaluate("document.documentElement.scrollWidth > document.documentElement.clientWidth"),
                "Virtualized long-list view has horizontal overflow",
            )
            report["metrics"] = {
                "playbackFrames": playback_frames,
                "lyricsFrames": lyrics_frames,
                "virtualScrollFrames": virtual_frames,
            }
            report["virtualization"] = {
                "initialRenderedRows": initial_rows,
                "renderedRowIndicesAfterScroll": rendered_indices,
                "scroll": scroll_info,
            }
            page.screenshot(path=str(SCREENSHOT_PATH), full_page=True)
            page.wait_for_timeout(300)

            verify_required_requests(api)
            report["contractFailures"] = api.contract_failures
            require(not api.contract_failures, "; ".join(api.contract_failures))
            require(not console_errors, f"Browser console errors: {console_errors}")
            require(not page_errors, f"Browser page errors: {page_errors}")
            require(not request_failures, f"Browser request failures: {request_failures}")
        except Exception as error:
            report["failure"] = str(error)
            raise
        finally:
            report["finishedAt"] = datetime.now(timezone.utc).isoformat()
            report["apiRequests"] = api.requests
            report["contractFailures"] = api.contract_failures
            REPORT_PATH.write_text(json.dumps(report, ensure_ascii=True, indent=2), encoding="utf-8")
            browser.close()


if __name__ == "__main__":
    main()
