<div align="center">

# JiraStopWatch NextGen

**A fast, modern time tracker for Jira — for Windows and Linux.** (macOS support is built but not available yet: see [macOS](#macos).)

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

**Responsive** — the layout adapts down to phone-width windows; the issue list becomes a slide-over panel.

<img src="docs/screenshots/responsive.png" alt="Responsive layout: light, dark, and the slide-over issue list" width="640">

**Service desk queues** — pick a queue and its issues appear, ready to become timers.

<img src="docs/screenshots/queues.png" alt="The Queues tab" width="640">

**My work** — your week, one column per day, straight from Jira.

<img src="docs/screenshots/mywork.png" alt="My work: a week of worklogs, one column per day" width="640">

---

## Install (two clicks)

You only need a Chromium-based browser that is already on almost every computer: **Edge** (pre-installed on Windows 10/11), **Chrome**, **Brave** or **Chromium**. No runtime, no admin rights, no terminal.

Download the file for your system from the [**Releases**](https://github.com/farhadrezvani7/jirastopwatch-nextgen/releases/latest) page:

### Windows
1. Download `jirastopwatch-nextgen-windows-amd64-setup.exe` (use `…-arm64-setup.exe` on ARM PCs).
2. Double-click it → **Next** → **Finish**. You get a Start Menu entry and a desktop shortcut; remove it any time from *Settings → Apps*. To pin it to the taskbar, pin the Start Menu entry (not the open window, which Windows attributes to the browser).
   The first time, SmartScreen may say *“Windows protected your PC”* because the file is not code-signed: click **More info → Run anyway**.

### macOS
> ⚠️ **macOS is not available for now.** The macOS groundwork is in the project (a `.app` bundle, signing in the release workflow, the build itself), but running it on a Mac needs an **Apple Developer account** (a paid, yearly membership) to sign and notarize the app so macOS lets people open it — and we don't have one yet. Until we do, **only Windows and Linux users can install** JiraStopWatch NextGen. The macOS files attached to releases are provided as-is for developers and are not supported. Once an Apple account is available, macOS support will be switched on and announced here.

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

### Updating
Click the profile icon (top right) → **Check for updates**. The profile window also shows the version you are running. If a newer release exists, confirm and the app downloads it, verifies its checksum, replaces itself and restarts, with no uninstalling and no manual download. Running timers must be paused or logged first (the dialog offers *Pause all & update*); paused timers keep their time. After an update the old window closes by itself, together with all of its helper processes, and the new version opens its own. Copies installed system-wide (e.g. from a `.deb`/`.rpm` in `/usr/bin`) ask for your administrator password (polkit/`pkexec`) to replace the file; if `pkexec` isn't available the dialog links to the download page instead.

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
4. Add issues by key (`ABC-123`), by pasting a Jira link, or by picking them from *Recent / Assigned / Filters / All* (and *Queues* if your Jira has Service Management). Press ▶ to start, **Log work** to book the time; click the pin to keep a card on top, or press and hold a card to drag it.

---

## What's new compared with the original jirastopwatch

The original is a C# / WinForms / .NET Framework desktop app for Windows. NextGen keeps its core workflow and improves almost everything around it.

| | Original jirastopwatch | **JiraStopWatch NextGen** |
|---|---|---|
| **Platforms** | Windows only | **Windows and Linux** (x64 and ARM); macOS pending an Apple Developer account |
| **Technology** | C#, WinForms, .NET Framework, RestSharp | **Go**, one self-contained binary, embedded web UI |
| **Install** | MSI installer + .NET runtime | **Download one file and run it** — no installer, no runtime |
| **Footprint** *(measured on Linux)* | — | ~6.5 MB binary · server ready in ~40 ms · ~10 MB RAM for the server process (the browser window is extra) |
| **Interface** | WinForms window | **Redesigned** modern UI with **light & dark themes**, live clock |
| **Languages** | English | **41 languages**, searchable, with full **right-to-left** layout (Persian, Arabic, Urdu, Hebrew, Kurdish, Pashto) |
| **Time zone** | Computer's local time | **Pick a country (all 247) → worklogs use its time zone** — no more manual +3:30 maths |
| **Sign-in** | Username + API token | **Cloud** (email + API token), **Server/DC personal access token**, or username + password |
| **Jira Cloud compatibility** | Needs patches for current Cloud (removed search API, silent login redirects) | Built in: v3 search endpoint, login-redirect / HTML-login detection, URL clean-up |
| **Search** | Jira issue search | Jira issue search **plus** searchable language / country / zone pickers, favourite-filter search and a filter box for your own timers |
| **Log work** | Time, comment, estimate | Same, **plus** change the issue's status in the same step and see the remaining estimate (and what *Auto* will leave) |
| **Local safety** | — | Server binds to `127.0.0.1` only, per-launch secret token, DNS-rebinding (Host) check |
| **Data files** | Binary-serialised settings | Human-readable JSON, written atomically, owner-only permissions |
| **Tests** | NUnit | Go tests, run by CI on **Windows and Linux** (and macOS, for the groundwork) |

### Everything from the original that is kept
Multiple stopwatches · reorder with ▲▼ · pause others when one starts · edit time · remaining-estimate adjustment (auto / leave / set to / reduce by) · post the note as worklog comment, separate comment, or both · move an issue to e.g. *In Progress* when its timer starts · pause on lock · choose what happens on exit · keyboard shortcuts · optional update notice · max. 20 issues · Apache 2.0.

---

## Features

- ⏱ **Timers that never lose time** — they keep counting while the window is closed and are restored on the next start. Unsent notes are kept too.
- 🌍 **Time zone by country** — all 247 countries; multi-zone countries (US, Russia, Brazil, …) get a zone picker. Worklog start time and the header clock follow it. The default is your computer's own zone, and times are sent to Jira with an explicit UTC offset, so they are right wherever you are.
- 🗣 **41 languages** — English, فارسی, العربية, اردو, کوردی, پښتو, עברית, Türkçe, Azərbaycanca, Русский, Українська, Español, Português, Français, Deutsch, Italiano, Nederlands, Polski, Čeština, Slovenčina, Română, Ελληνικά, Magyar, Български, Српски, Hrvatski, Svenska, Dansk, Suomi, Norsk, 中文, 日本語, 한국어, हिन्दी, বাংলা, தமிழ், Bahasa Indonesia, Bahasa Melayu, ไทย, Tiếng Việt, Kiswahili. Choosing a country suggests its main language.
- 🎨 **Light & dark themes** — switch from the header, the sign-in page or Settings; *Auto* follows your OS.
- 🔎 **Search where you need it** — language, country and zone pickers (accent-insensitive; countries match English, localized and native names), your favourite Jira filters, your timer list, and Jira issues.
- 🔒 **Pause on lock** — pause running timers when the computer locks and optionally resume on unlock (Linux with systemd-logind, Windows, macOS).
- 🔗 **Paste a Jira link** — `Ctrl+V` anywhere adds the issue from a `/browse/ABC-123` or board URL.
- 🕒 **Jira server-clock aware** — Jira stores the exact moment, but its reports group days by the *server's* time zone. If that differs from yours (and from your Jira profile), the app tells you and lets you opt in to writing your local clock time on the server's clock so reports land on the right day; switch back in Settings any time (the app also offers it once the server's zone is fixed).
- 🔑 **Stays signed in** — your token is kept in the system credential store and the app signs you in on launch.
- 📊 **Logged today** — the header shows everything you have logged in Jira today (all issues, in your chosen time zone) and each card shows what you logged on that issue today, so even many worklogs on one ticket add up at a glance. The number comes from Jira itself, so it is right even if you installed the app in the middle of the day. Next to it, *Not logged yet* shows the time still sitting on your timer cards (hidden when there is none).
- 📌 **Pin cards** — the pin icon next to an issue key keeps that card above all the others. Pinning is stored only in this app (not in Jira) and survives restarts. Pinned cards stay in their own block: new, started and dragged cards never push past them, ▲▼ and drag-and-drop only reorder within the block, and removing a pinned card asks first.
- 🗂 **Service desk queues** — if your Jira has Jira Service Management, a *Queues* tab appears: pick a service project, then a queue, and its issues are listed (with counts and *Load more*), ready to add as timers. It uses Jira's standard service-desk API, so it works on Jira Cloud and on Server/Data Center, with all three sign-in methods. Jira without Service Management, or an account without agent access, simply doesn't show the tab; a project you can't open shows Jira's own message. Inside a queue, a saved filter or a JQL result, the search box narrows the list you are looking at (the ‹ button goes back to the queues); on *Recent*, *Assigned* and *All* it searches all of Jira.
- 🌐 **Choose the browser for Jira links** — in *Settings → Open Jira issues in* pick any browser installed on your computer (found automatically on Windows and Linux), so issue links open where you are already signed in to Jira. The default is your system browser; if the chosen one is uninstalled the default is used instead. Only pages of your own Jira site are ever opened this way.
- ⤴️ **Started timers jump to the top** — optional (Settings): a timer that starts moves to the top of the list by itself, so you never reorder by hand.
- 🌍 **Tested on both flavours of Jira** — every feature is exercised against a live **Jira Cloud** site (email + API token) and a live **Jira Server 8.15** (personal access token, and username + password), in addition to the automated tests that run for all three sign-in methods. Pasting any Jira page address (even a long `…/jira/for-you?tab=…` link, or `http://` on Cloud) is enough to sign in.
- 🗓 **My work** — profile window → *My work* shows your own worklogs for a week, one column per day with its Gregorian date, the day's total, and every worklog (issue key, summary, time, comment) exactly as Jira has it: click one to edit or delete it, **+** adds time on that day. Everything is read from Jira each time (and again after every change); a day belongs to the date written in the worklog's *started* text, the same rule Jira and Tempo use, so the numbers match Tempo's week view. The first day of the week follows your language and can be changed. It uses Jira's standard worklog API, so it works on Jira Cloud and Server/Data Center with all three sign-in methods, with or without Tempo.
- 🛡 **Steady on bad networks** — reads from Jira are retried when the connection drops, the reply is cut off or Jira answers *429/502/503/504*; anything that changes data (logging, editing, deleting) is sent exactly once, so nothing is ever booked twice. If a few issues can't be read, *My work* still shows the rest and tells you which ones are missing. Every failure is shown as a clear message in your language instead of a raw technical error.
- ✍️ **Log time manually** — profile window → *Log time manually*: pick any issue, a date and time, a length, a comment (and optionally a status change and estimate) and book it without any timer.
- 🔀 **Wrong card? Log to another issue** — the log dialog has a *Log to issue* picker (your other timers, or search every issue). The time is booked on the issue you choose, its remaining estimate and status list are those of that issue, and the card you started by mistake is the one that is used up. *Log & start again* then continues on the issue you chose.
- 🔁 **Log & start again** — a button in the log dialog books the worklog and immediately restarts the same issue's timer, no searching needed.
- ✋ **Drag and drop** — press and hold a card, then drag it to a new place (▲▼ and `Ctrl+↑/↓` still work).
- 🎛 **Look, your way** — in *Settings* pick the base font size (10–16 px, previewed live) and switch soft drop shadows on or off (off by default).
- 🔍 **All issues + advanced search (JQL)** — the *All* tab lists every issue you can see in Jira, from any team, newest first, with *Load more* paging. In that tab the funnel icon opens a JQL box, e.g. `status = Develop AND resolution = Unresolved AND labels = Flutter ORDER BY updated DESC`. Jira runs it as you, so permissions apply and syntax errors are shown as Jira words them; your last query is remembered.
- 👤 **Profile window** — the header shows just a profile icon; click it to see who is signed in, toggle *pause others*, open Settings, check for updates or sign out.
- 📱 **Responsive & compact** — the layout adapts down to phone-width windows (the issue list becomes a slide-over panel), and slimmer cards and a one-row header fit more without scrolling. The issue panel can be collapsed on wide windows too.
- 🔔 **Tray icon** — on Windows and on Linux desktops with a tray: quick access to the window, running-timer tooltip, pause all, quit (see [Tray icon](#tray-icon)).
- ⬆️ **One-click updates** — *Check for updates* downloads, verifies and installs the new version for you.

### Keyboard shortcuts (same as the original)
| Keys | Action | Keys | Action |
|---|---|---|---|
| `Ctrl+P` | Start / pause | `Ctrl+↑` / `Ctrl+↓` | Move issue |
| `Ctrl+L` | Log work | `Ctrl+O` | Open in Jira |
| `Ctrl+E` | Edit time | `Ctrl+I` / `Ctrl+N` | Focus “add issue” |
| `Ctrl+R` | Reset timer | `Ctrl+C` | Copy issue key |
| `Ctrl+Del` | Remove | `Ctrl+V` | Add pasted key / link |

---

## Your time is safe
- **Crash or power cut** — timers are saved to disk the moment they change, written so that a power failure can't leave an empty file (the data is flushed first and the previous copy is kept as a backup that is used automatically if the file is ever damaged). Running timers are stored as a start time, so the time keeps counting while the app is closed normally. After a crash or power cut the app records that it was last alive (every 20 seconds); on the next start it **asks you** whether the computer was off — *Rewind to that moment and pause* or *Keep counting* — and never changes a timer by itself, so the hours the computer was off are only removed when you confirm it.
- **Internet drops while logging** — nothing is lost: the timer and your unsent note stay as they were and you can log again. If Jira had already saved the entry before the connection broke, logging again recognises the identical entry (same start minute and length) and does not book it twice. The worklog is posted first and a separate comment after it, so a failure can never duplicate a comment or lose time.
- **No internet at start-up** — *Continue offline* opens your timers; Jira is reconnected automatically when it is back.
- **Only one copy at a time** — the running copy holds an operating-system lock (released automatically if it dies), so launching the app again just brings up the running copy and two copies can never overwrite each other's timers.
- **Updates** — the app won't update while a timer is running (pause or log it first; the dialog offers *Pause all & update*), and an update interrupted by a power cut leaves the old version in place.
- **Disk full / read-only folder** — you are told right away that timers could not be saved. A clock that is set back never removes time you have already counted. Your sign-in secret is never left behind in backup copies.

## Privacy & security
- Your data goes **only to your own Jira site** — nothing else is contacted (the update check, which asks GitHub for the latest release, is off unless a release repo is configured at build time and can be switched off in Settings; a new version is only downloaded after you confirm).
- The local web server listens on `127.0.0.1` on a random port and requires a per-launch secret, so other websites and other users of the machine cannot talk to it.
- If **Remember me** is ticked, the token is kept in your operating system's credential store: Windows Credential Manager, the macOS Keychain, or the Linux Secret Service (GNOME Keyring / KWallet; needs the `secret-tool` command from `libsecret-tools`). If none is available, it falls back to a file in the app's config folder that only your user can read, **not** encrypted. Old versions' plain-text tokens are moved into the credential store automatically. On shared computers leave *Remember me* off.
- Config and timers live in `~/.config/jirastopwatch-nextgen` (Linux), `%AppData%\jirastopwatch-nextgen` (Windows) or `~/Library/Application Support/jirastopwatch-nextgen` (macOS).

## Not included (compared with the original)
The diagnostic log file, and a tray/menu-bar icon on macOS (Windows and Linux have one: see below).

## Tray icon
On Windows and on Linux desktops with a status-notifier tray (KDE, XFCE, Cinnamon, GNOME with the AppIndicator extension) the app adds a notification-area icon. Click it to open the window; the tooltip shows the running timer, and the menu has *Open*, *Pause all timers* and *Quit*. With a tray icon, closing the window keeps the app (and your timers) running in the tray; use *Quit* to exit. Without a tray, closing the window quits as before.

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
