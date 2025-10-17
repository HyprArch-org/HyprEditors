# EN([RU](RU_README.md)) — Quick install (English)

## HyprEditors — VS Code custom installer (quick)

Small self-contained installer that applies my VS Code custom (`settings.json`, `keybindings.json`) and installs extensions. Binaries embed the payload so one executable sets up the whole environment.

### Download & run (one-liners)

**Linux x86_64**

```bash
curl -fsSL https://editors.hyprarch.ru/linux-installer -o /tmp/installer
chmod +x /tmp/installer
/tmp/installer
```

**Linux ARM64**

```bash
curl -fsSL https://editors.hyprarch.ru/linux-arm64-installer -o /tmp/installer
chmod +x /tmp/installer
/tmp/installer
```

**macOS (Apple Silicon)**

```bash
curl -fsSL https://editors.hyprarch.ru/macos-arm64-installer -o /tmp/installer
chmod +x /tmp/installer
/tmp/installer
```

**macOS (Intel)**

```bash
curl -fsSL https://editors.hyprarch.ru/macos-installer -o /tmp/installer
chmod +x /tmp/installer
/tmp/installer
```

**Windows (x64)** — single special URL:

```powershell
Invoke-WebRequest -Uri "https://editors.hyprarch.ru/win-installer" -OutFile "$env:TEMP\installer.exe"
Start-Process "$env:TEMP\installer.exe" -Wait
```

> Tip: URL pattern examples — replace platform/arch as needed:
> `https://editors.hyprarch.ru/{linux,linux-arm64,macos,macos-arm64,win-installer}`

### Flags (short)

- `--yes` — accept all prompts (non-interactive)
- `--dry-run` — show actions, don’t write/install
- `--src /path` — use external files instead of embedded
- `--no-backup` — skip creating backup

### What it does (short)

- optional backup of `settings.json` / `keybindings.json` (saved to `.../Code/User/backup_YYYY-MM-DD_HH-MM-SS`)
- optionally applies `settings.json` and `keybindings.json` (embedded or from `--src`)
- optionally installs extensions from `extensions.txt` using `code --install-extension` with retries/timeouts/pauses
- writes log to `~/vscode-custom-install.log` (or `%USERPROFILE%` on Windows)

### More (links)

- Extensions list & notes: [Extensions](vs-code/extensions.md)
- Keybindings reference: [Keybindings](vs-code/keybindings.md)
