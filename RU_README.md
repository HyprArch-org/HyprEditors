# RU([EN](README.md)) — Быстрая установка (Русский)

## HyprEditors — инсталлятор кастома VS Code (коротко)

Небольшой самодостаточный инсталлятор: применяет мой `settings.json` и `keybindings.json` и ставит расширения. Payload встроен в бинарник — один исполняемый файл настраивает окружение.

### Скачать и запустить (одной командой)

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

**Windows (x64)** — специальный URL:

```powershell
Invoke-WebRequest -Uri "https://editors.hyprarch.ru/win-installer" -OutFile "$env:TEMP\installer.exe"
Start-Process "$env:TEMP\installer.exe" -Wait
```

> Подсказка: шаблон URL — `https://editors.hyprarch.ru/{linux,linux-arm64,macos,macos-arm64,win-installer}`. Меняй `linux` → `macos` → `linux-arm64` и т.д. для нужной платформы/архитектуры.

### Флаги (коротко)

- `--yes` — принять все вопросы (без интерактива)
- `--dry-run` — показать действия, не применять
- `--src /path` — использовать внешние файлы вместо встроенных
- `--no-backup` — пропустить бэкап

### Что делает (коротко)

- опционально создаёт бэкап `settings.json`/`keybindings.json` (`.../Code/User/backup_YYYY-MM-DD_HH-MM-SS`)
- опционально применяет `settings.json` и `keybindings.json` (встроенные или из `--src`)
- опционально ставит расширения из `extensions.txt` (`code --install-extension`) с ретраями/таймаутами/паузами
- пишет лог в `~/vscode-custom-install.log` (или `%USERPROFILE%` для Windows)

### Дополнительные инструкции

- Список расширений: [Extensions](vs-code/extensions.md)
- Таблица биндов: [Keybindings](vs-code/keybindings.md)
