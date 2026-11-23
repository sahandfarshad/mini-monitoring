# MiniMonitor

A tiny, always-on-top widget that shows live network speed, CPU and RAM of your Linux servers.

- **Agentless, near-zero load.** One SSH session per server runs a loop made only of shell builtins
  (no process is spawned per sample) and prints one line of 12 numbers (~60 bytes) each tick.
  About 9 KB/min from the server and 3 KB/min back at a 1 s interval; the interval is adjustable (1–60 s).
- **Password or private key.** Secrets are encrypted with Windows DPAPI (Windows) or kept in the app's private storage (Android).
- **Three sizes** (small one-liner, medium, large), each with its own scale; colors, opacity, chart span, units and always-on-top are adjustable.
- **Windows** (Go + Wails, WebView2) and **Android** (same Go core via gomobile, same HTML UI in a WebView).

## Use
- `+` adds a server; servers stack below each other.
- Right-click (long-press on Android) a server: edit, size, move, pause, delete. Double-click to edit.
- Top-bar buttons appear on hover.

## Build
Push a tag `vX.Y.Z`: GitHub Actions builds `MiniMonitor-X.Y.Z-windows-x64.zip` and `MiniMonitor-X.Y.Z.apk` and attaches them to the release.

Server requirements: Linux with `bash` and `/proc`.
