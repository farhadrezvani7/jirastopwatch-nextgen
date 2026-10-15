"use strict";
// UI translations. English is the fallback for any missing key.
// Server errors arrive as keys ("err.badKey") or "key|detail"; unknown text (e.g. Jira's own messages) is shown as is.
const LANGS = {
  en: ["English", "English"], fa: ["فارسی", "Persian"], ar: ["العربية", "Arabic"], ur: ["اردو", "Urdu"],
  ckb: ["کوردیی ناوەندی", "Kurdish (Sorani)"], ps: ["پښتو", "Pashto"], he: ["עברית", "Hebrew"],
  tr: ["Türkçe", "Turkish"], az: ["Azərbaycanca", "Azerbaijani"], ru: ["Русский", "Russian"], uk: ["Українська", "Ukrainian"],
  es: ["Español", "Spanish"], pt: ["Português", "Portuguese"], fr: ["Français", "French"], de: ["Deutsch", "German"],
  it: ["Italiano", "Italian"], nl: ["Nederlands", "Dutch"], pl: ["Polski", "Polish"], cs: ["Čeština", "Czech"],
  sk: ["Slovenčina", "Slovak"], ro: ["Română", "Romanian"], el: ["Ελληνικά", "Greek"], hu: ["Magyar", "Hungarian"],
  bg: ["Български", "Bulgarian"], sr: ["Српски", "Serbian"], hr: ["Hrvatski", "Croatian"],
  sv: ["Svenska", "Swedish"], da: ["Dansk", "Danish"], fi: ["Suomi", "Finnish"], nb: ["Norsk bokmål", "Norwegian"],
  zh: ["中文", "Chinese"], ja: ["日本語", "Japanese"], ko: ["한국어", "Korean"],
  hi: ["हिन्दी", "Hindi"], bn: ["বাংলা", "Bengali"], ta: ["தமிழ்", "Tamil"],
  id: ["Bahasa Indonesia", "Indonesian"], ms: ["Bahasa Melayu", "Malay"], th: ["ไทย", "Thai"], vi: ["Tiếng Việt", "Vietnamese"],
  sw: ["Kiswahili", "Swahili"],
};
const RTL = new Set(["fa", "ar", "ur", "ckb", "ps", "he"]);
const dirOf = l => (RTL.has(l) ? "rtl" : "ltr");

// English is built in (the fallback). Every other language is web/lang/<code>.json, loaded on demand.
const STR = {};
// Key order for the compact tables below.
const KEYS = [
  "login.sub", "login.site", "login.mode", "mode.cloud", "mode.pat", "mode.basic", "id.email", "id.user",
  "secret.token", "secret.pat", "secret.pw", "ph.saved", "ph.secret", "hint.cloud", "hint.pat", "hint.basic",
  "login.remember", "login.rememberHint", "login.signin", "login.signing",
  "hdr.total", "hdr.pauseAll", "menu.settings", "menu.signOut", "set.pauseOthers",
  "add.placeholder", "add.btn", "empty.title", "empty.text",
  "timer.start", "timer.pause", "timer.editTip", "timer.log", "timer.logTip", "timer.removeTip", "timer.confirmRemove",
  "tab.recent", "tab.assigned", "tab.filters", "search.ph", "reload.tip", "list.none", "list.noFilters", "list.already",
  "edit.title", "edit.field", "btn.save", "btn.cancel",
  "log.title", "log.time", "log.started", "log.comment", "log.commentPh", "log.status", "log.keep", "log.btn", "log.posting", "log.done",
  "est.label", "est.auto", "est.leave", "est.new", "est.manual", "est.value",
  "set.title", "set.language", "set.country", "set.zone", "set.system", "set.tzNote", "set.commentMode",
  "cm.worklog", "cm.comment", "cm.both", "set.startTransitions", "set.startHint", "set.saved",
  "err.notSignedIn", "err.badKey", "err.badRequest", "err.fillAll", "err.saveSettings", "err.keyShape", "err.timeFormat",
  "err.timeZero", "err.badStart", "err.badFilter", "err.badSource", "err.timerNotFound", "err.badZone",
  "err.addrEmpty", "err.addrInvalid", "err.addrScheme", "err.unreachable", "err.rejected", "err.denied", "err.notfound", "err.http",
  "err.appGone", "warn.transition", "filter.ph", "filter.none", "timers.filter",
  "theme.label", "theme.light", "theme.dark", "theme.system",
  "timer.reset", "timer.moveUp", "timer.moveDown", "err.maxIssues",
  "set.lock", "lock.none", "lock.pause", "lock.resume", "set.exit", "exit.keep", "exit.pause", "exit.reset", "set.update", "upd.available", "upd.get", "mini.tip",
];
const EN_VALUES = [
  "Sign in to Jira to track time on issues.", "Jira address", "Sign in with", "Email + API token (Jira Cloud)",
  "Personal access token (Jira Server / Data Center)", "Username + password (Jira Server / Data Center)", "Email", "Username",
  "API token", "Personal access token", "Password", "Leave empty to use the saved one", "Paste it here",
  "Create one at {link}. Your account password won't work on Cloud.",
  "In Jira: your avatar → Profile → Personal Access Tokens → Create token.",
  "Use this if you don't have a token. Some servers lock the account after repeated wrong passwords.",
  "Remember me on this computer", "The secret is kept in your system's secure credential store (or, if there is none, in a file only you can read).", "Sign in", "Signing in…",
  "Total", "⏸ Pause all", "Settings", "Sign out", "Pause others when starting a timer",
  "Add issue by key, e.g. ABC-123 — or pick one from the list", "Add", "No issues yet", "Add an issue by key above or pick one from the list.",
  "Start", "Pause", "Click to edit time", "Log work", "Post worklog to Jira", "Remove from list", "This timer is running. Remove it anyway?",
  "Recent", "Assigned", "Filters", "Search issues…", "Refresh list", "No issues found.", "You have no favorite filters in Jira.", "{key} is already in your list",
  "Edit time", "Time spent (e.g. 1h 30m, 1.5h, 45m, 1:30)", "Save", "Cancel",
  "Log work", "Time spent", "Started", "Comment", "What did you work on? (optional)", "Change status after logging", "Keep current status ({status})", "Log work", "Posting…", "Logged {time} on {key}",
  "Remaining estimate", "Adjust automatically", "Leave unchanged", "Set to…", "Reduce by…", "Value (e.g. 2h)",
  "Settings", "Language", "Country (sets the time zone)", "Time zone", "System time zone", "Worklog start times use this zone ({offset}). It is now {time} there.", "Worklog comment",
  "Post as part of the worklog", "Post as a separate comment only", "Post as both worklog and comment", "When a timer starts, move the issue to status (one per line)", "Matches status names containing the text, e.g. In Progress", "Settings saved",
  "Not signed in", "Invalid issue key", "Bad request", "Please fill in all the fields", "Signed in, but couldn't save settings: {0}", "That doesn't look like an issue key (e.g. ABC-123)", "Time format: 1h 30m, 1.5h, 45m or 1:30",
  "Enter a time greater than zero, e.g. 1h 30m", "Invalid start time", "Bad filter", "Bad source", "Timer not found", "Unknown time zone",
  "Jira address is empty", "Jira address is not valid", "Jira address must start with https://", "Can't reach Jira: {0}", "Sign-in rejected: check your credentials", "Access denied", "Not found", "Jira returned HTTP {0}",
  "The app isn't running anymore", "Time logged, but the status change failed: {0}",
  "Search…", "No matches", "Filter your timers…",
  "Theme", "Light", "Dark", "Auto (system)",
  "Reset timer", "Move up", "Move down", "At most 20 issues",
  "When the computer locks", "Do nothing", "Pause running timers", "Pause, and resume when unlocked", "When the app closes", "Keep timers running", "Pause timers", "Reset all timers", "Check for updates", "Version {0} is available", "Download", "Mini window (always on top)",
];

STR.en = Object.fromEntries(KEYS.map((k, i) => [k, EN_VALUES[i]]));
// Added later; languages without these keys show the English text.
Object.assign(STR.en, {
  "menu.update": "Check for updates", "upd.checking": "Checking for updates…", "upd.uptodate": "You have the latest version ({0}).",
  "upd.title": "Update available", "upd.ask": "Version {latest} is available (you have {current}). Replace the current version with the new one and restart now? Running timers keep counting.",
  "upd.now": "Update now", "upd.later": "Not now", "upd.installing": "Downloading and installing…",
  "upd.done": "Updated to {0}. The app is restarting…",
  "log.jiraZone": "Jira shows times in your profile time zone ({zone}); this moment is {time} there.",
  "err.updCheck": "Could not check for updates: {0}", "err.updInstall": "The update failed: {0}",
  "err.updNoWrite": "This copy can't update itself (it is installed in a protected folder). Download the new version here:",
});

let lang = "en";
function pickLang(saved) {
  if (saved && LANGS[saved]) return saved;
  const alias = {no: "nb", nn: "nb", tl: "en", fil: "en"};
  for (const l of navigator.languages || [navigator.language || "en"]) {
    let c = l.toLowerCase().split("-")[0];
    c = alias[c] || c;
    if (l.toLowerCase().startsWith("ku") ) c = "ckb";
    if (LANGS[c]) return c;
  }
  return "en";
}
async function loadLang(l) {
  if (STR[l]) return;
  try { STR[l] = await (await fetch(`/lang/${l}.json`)).json(); } catch { STR[l] = {}; } // missing keys fall back to English
}
async function setLang(l) {
  l = LANGS[l] ? l : "en";
  await loadLang(l);
  lang = l;
  document.documentElement.lang = lang;
  document.documentElement.dir = dirOf(lang);
}
// Lower-cases and strips accents so "turkce" finds "Türkçe".
const norm = s => String(s).normalize("NFD").replace(/[\u0300-\u036f]/g, "").toLowerCase();
// t("key", {a: 1}) fills {a}. Unknown keys return the text unchanged.
function t(key, vars) {
  let s = (STR[lang] && STR[lang][key]) ?? STR.en[key];
  if (s === undefined) return key;
  if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, v);
  return s;
}
// Translates a server message: "err.key" or "err.key|detail"; anything else (Jira's own text) is passed through.
function tr(msg) {
  const i = msg.indexOf("|");
  const key = i < 0 ? msg : msg.slice(0, i);
  if (STR.en[key] === undefined) return msg;
  return t(key, {0: i < 0 ? "" : msg.slice(i + 1)});
}
