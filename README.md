<div align="center">

# JiraStopWatch NextGen

**A fast, modern, cross-platform time tracker for Jira — Windows, macOS and Linux.**

[![CI](https://github.com/farhadrezvani7/jirastopwatch-nextgen/actions/workflows/ci.yml/badge.svg)](https://github.com/farhadrezvani7/jirastopwatch-nextgen/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

[English](README.md) · [فارسی](README.fa.md)

<img src="docs/screenshots/app-dark.png" alt="JiraStopWatch NextGen, dark theme" width="780">

</div>

JiraStopWatch NextGen is built on the core of the open-source [**jirastopwatch**](https://github.com/jirastopwatch/jirastopwatch) project (the Windows desktop stopwatch for logging time on Jira issues). It keeps everything that made the original useful, and then goes further: it has been **upgraded, redesigned, and rewritten in Go** so it is lighter, starts instantly, and runs natively on **Windows, macOS and Linux**.

Start a timer on an issue, work, press **Log work** — the time lands in Jira as a worklog, in *your* time zone, in *your* language.

| Dark | Light | Right-to-left (Persian) |
|---|---|---|
| <img src="docs/screenshots/app-dark.png" width="260"> | <img src="docs/screenshots/app-light.png" width="260"> | <img src="docs/screenshots/app-rtl-fa.png" width="260"> |

---

## Install (two clicks)

You only need a Chromium-based browser that is already on almost every computer: **Edge** (pre-installed on Windows 10/11), **Chrome**, **Brave** or **Chromium**. No runtime, no admin rights, no terminal.

Download the file for your system from the [**Releases**](https://github.com/farhadrezvani7/jirastopwatch-nextgen/releases/latest) page:

### Windows
1. Download `jirastopwatch-nextgen-windows-amd64-setup.exe` (use `…-arm64-setup.exe` on ARM PCs).
2. Double-click it → **Next** → **Finish**. You get a Start Menu entry and a desktop shortcut; remove it any time from *Settings → Apps*.
   The first time, SmartScreen may say *“Windows protected your PC”* because the file is not code-signed: click **More info → Run anyway**.

### macOS
1. Download `jirastopwatch-nextgen-macos-arm64.zip` (Apple Silicon: M1–M4) or `…-macos-amd64.zip` (Intel).
2. Open the zip and drag **Jira StopWatch NextGen** into **Applications**.
3. First launch only: because the app is not notarized, macOS blocks it. Open it once, then go to **System Settings → Privacy & Security → Open Anyway**. (On older macOS: right-click the app → **Open**.)

### Linux
- **Ubuntu / Debian / Mint:** download `jirastopwatch-nextgen-linux-amd64.deb` (or `…-arm64.deb`), double-click it and press **Install** in the software center.
- **Fedora / openSUSE:** download the `.rpm` the same way.
- **Any distro, one line** (installs for your user only, no root):
  ```bash
  curl -fsSL https://raw.githubusercontent.com/farhadrezvani7/jirastopwatch-nextgen/main/install.sh | bash
  ```
  Afterwards search for **Jira StopWatch NextGen** in your applications menu. The terminal is only needed for this one command; the app runs on its own.
- Already downloaded the plain `jirastopwatch-nextgen-linux-*` binary? `chmod +x` it and run it with `--install`; remove again with `--uninstall`.

> No Chromium-based browser? The app opens in your default browser instead and quits when you close the tab.

### Build from source (any OS)
```bash
git clone https://github.com/farhadrezvani7/jirastopwatch-nextgen.git
cd jirastopwatch-nextgen
go build -o jirastopwatch-nextgen .        # needs Go 1.24+
./jirastopwatch-nextgen
```
On Windows run `.\install.ps1` to also get a Start Menu shortcut; `./build-all.sh` cross-compiles every platform and builds the installers into `dist/` (needs `nfpm`, `makensis` and `zip` for the installers).

---

## First sign-in (1 minute)
1. Enter your Jira address (e.g. `yourcompany.atlassian.net`).
2. Get a token:
   - **Jira Cloud:** open <https://id.atlassian.com/manage-profile/security/api-tokens> → *Create API token*. Sign in with your email + that token.
   - **Jira Server / Data Center:** your avatar → *Profile* → *Personal Access Tokens* → *Create token* (or use username + password).
3. Leave **Remember me** ticked (the default) and the app signs you in automatically next time; untick it on a shared computer.
4. Add issues by key (`ABC-123`), by pasting a Jira link, or by picking them from *Recent / Assigned / Filters*.

---

## What's new compared with the original jirastopwatch

The original is a C# / WinForms / .NET Framework desktop app for Windows. NextGen keeps its core workflow and improves almost everything around it.

| | Original jirastopwatch | **JiraStopWatch NextGen** |
|---|---|---|
| **Platforms** | Windows only | **Windows, macOS, Linux** (x64 and ARM) |
| **Technology** | C#, WinForms, .NET Framework, RestSharp | **Go**, one self-contained binary, embedded web UI |
| **Install** | MSI installer + .NET runtime | **Download one file and run it** — no installer, no runtime |
| **Footprint** *(measured on Linux)* | — | ~6.5 MB binary · server ready in ~40 ms · ~10 MB RAM for the server process (the browser window is extra) |
| **Interface** | WinForms window | **Redesigned** modern UI with **light & dark themes**, live clock, mini window |
| **Languages** | English | **41 languages**, searchable, with full **right-to-left** layout (Persian, Arabic, Urdu, Hebrew, Kurdish, Pashto) |
| **Time zone** | Computer's local time | **Pick a country (all 247) → worklogs use its time zone** — no more manual +3:30 maths |
| **Sign-in** | Username + API token | **Cloud** (email + API token), **Server/DC personal access token**, or username + password |
| **Jira Cloud compatibility** | Needs patches for current Cloud (removed search API, silent login redirects) | Built in: v3 search endpoint, login-redirect / HTML-login detection, URL clean-up |
| **Search** | Jira issue search | Jira issue search **plus** searchable language / country / zone pickers, favourite-filter search and a filter box for your own timers |
| **Log work** | Time, comment, estimate | Same, **plus** change the issue's status in the same step and see the remaining estimate (and what *Auto* will leave) |
| **Local safety** | — | Server binds to `127.0.0.1` only, per-launch secret token, DNS-rebinding (Host) check |
| **Data files** | Binary-serialised settings | Human-readable JSON, written atomically, owner-only permissions |
| **Tests** | NUnit | Go tests, run by CI on **Windows, macOS and Linux** |

### Everything from the original that is kept
Multiple stopwatches · pause others when one starts · edit time · remaining-estimate adjustment (auto / leave / set to / reduce by) · post the note as worklog comment, separate comment, or both · move an issue to e.g. *In Progress* when its timer starts · pause on lock · choose what happens on exit · keyboard shortcuts · optional update notice · max. 20 issues · Apache 2.0.

---

## Features

- ⏱ **Timers that never lose time** — they keep counting while the window is closed and are restored on the next start. Unsent notes are kept too.
- 🌍 **Time zone by country** — all 247 countries; multi-zone countries (US, Russia, Brazil, …) get a zone picker. Worklog start time and the header clock follow it.
- 🗣 **41 languages** — English, فارسی, العربية, اردو, کوردی, پښتو, עברית, Türkçe, Azərbaycanca, Русский, Українська, Español, Português, Français, Deutsch, Italiano, Nederlands, Polski, Čeština, Slovenčina, Română, Ελληνικά, Magyar, Български, Српски, Hrvatski, Svenska, Dansk, Suomi, Norsk, 中文, 日本語, 한국어, हिन्दी, বাংলা, தமிழ், Bahasa Indonesia, Bahasa Melayu, ไทย, Tiếng Việt, Kiswahili. Choosing a country suggests its main language.
- 🎨 **Light & dark themes** — switch from the header, the sign-in page or Settings; *Auto* follows your OS.
- 🔎 **Search where you need it** — language, country and zone pickers (accent-insensitive; countries match English, localized and native names), your favourite Jira filters, your timer list, and Jira issues.
- 🪟 **Mini window** — a small always-on-top floating timer with play/pause (Chrome/Edge 116+).
- 🔒 **Pause on lock** — pause running timers when the computer locks and optionally resume on unlock (Linux with systemd-logind, Windows, macOS).
- 🔗 **Paste a Jira link** — `Ctrl+V` anywhere adds the issue from a `/browse/ABC-123` or board URL.

### Keyboard shortcuts (same as the original)
| Keys | Action | Keys | Action |
|---|---|---|---|
| `Ctrl+P` | Start / pause | `Ctrl+↑` / `Ctrl+↓` | Move issue |
| `Ctrl+L` | Log work | `Ctrl+O` | Open in Jira |
| `Ctrl+E` | Edit time | `Ctrl+I` / `Ctrl+N` | Focus “add issue” |
| `Ctrl+R` | Reset timer | `Ctrl+C` | Copy issue key |
| `Ctrl+Del` | Remove | `Ctrl+V` | Add pasted key / link |

---

## Privacy & security
- Your data goes **only to your own Jira site** — nothing else is contacted (the update check is off unless a release repo is configured at build time, and can be switched off in Settings).
- The local web server listens on `127.0.0.1` on a random port and requires a per-launch secret, so other websites and other users of the machine cannot talk to it.
- If **Remember me** is ticked, the token is kept in your operating system's credential store: Windows Credential Manager, the macOS Keychain, or the Linux Secret Service (GNOME Keyring / KWallet; needs the `secret-tool` command from `libsecret-tools`). If none is available, it falls back to a file in the app's config folder that only your user can read, **not** encrypted. Old versions' plain-text tokens are moved into the credential store automatically. On shared computers leave *Remember me* off.
- Config and timers live in `~/.config/jirastopwatch-nextgen` (Linux), `%AppData%\jirastopwatch-nextgen` (Windows) or `~/Library/Application Support/jirastopwatch-nextgen` (macOS).

## Not included (compared with the original)
System-tray icon / minimise-to-tray and the diagnostic log file. The app runs in a browser window, which cannot create a tray icon; the mini window and the running time in the window title cover the same need.

## Development
```bash
go test ./...                 # unit tests
go run . -no-open             # print the URL instead of opening a window
VERSION=1.0.0 UPDATE_REPO=farhadrezvani7/jirastopwatch-nextgen ./build-all.sh   # release binaries + SHA256SUMS in dist/
```
- **Add or fix a language:** each language is `web/lang/<code>.json` (same keys as the English text in `web/i18n.js`). Add the code to `LANGS` in `web/i18n.js`; `go test` verifies that every declared language has every key. Missing keys fall back to English.
- **Releases:** pushing a tag like `v1.0.0` makes GitHub Actions build all platforms and publish them.

## About the author
Built and maintained by **Farhad Rezvani** — mobile developer (Flutter is my main craft), also working with Go, Rust and C#, and with SQL and PostgreSQL databases.
GitHub: [@farhadrezvani7](https://github.com/farhadrezvani7) · Telegram: [@farhad_rezvani](https://t.me/farhad_rezvani)

## Support & upgrades
Having a problem, or need an upgrade or a new feature in this project? Message me on Telegram: **[@farhad_rezvani](https://t.me/farhad_rezvani)**. You can also open an [issue](https://github.com/farhadrezvani7/jirastopwatch-nextgen/issues) on GitHub.

## License & credits
Licensed under the **Apache License 2.0** (see [LICENSE](LICENSE) and [NOTICE](NOTICE)).

Based on [jirastopwatch](https://github.com/jirastopwatch/jirastopwatch) — © 2016 Carsten Gehling, © 2020 Dan Tulloh, © 2023 Y. Meyer-Norwood and contributors. Thank you for the original idea and the excellent core.

“Jira” and “Atlassian” are trademarks of Atlassian. This project is not affiliated with or endorsed by Atlassian.
