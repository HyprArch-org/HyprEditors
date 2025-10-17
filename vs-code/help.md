# Build the HyprEditors Installer — Quick Reference (EN)

Short, no-frills commands to build the VS Code custom installer for different platforms.

> Run from `vs-code/installer` (where `main.go` and `data/` live).

---

## Single-platform builds

- **Linux (x86_64)**

```bash
GOOS=linux GOARCH=amd64 go build -o dist/installer-linux main.go
```

- **Linux (ARM64)**

```bash
GOOS=linux GOARCH=arm64 go build -o dist/installer-linux-arm64 main.go
```

- **Windows (x64)**

```bash
GOOS=windows GOARCH=amd64 go build -o dist/installer.exe main.go
```

- **macOS (Intel)**

```bash
GOOS=darwin GOARCH=amd64 go build -o dist/installer-macos main.go
```

- **macOS (Apple Silicon)**

```bash
GOOS=darwin GOARCH=arm64 go build -o dist/installer-macos-arm64 main.go
```

---

## Build all (quick script)

Create `build-all.sh` and run it:

```bash
#!/usr/bin/env bash
set -e
mkdir -p dist
GOOS=linux GOARCH=amd64  go build -o dist/installer-linux main.go
GOOS=linux GOARCH=arm64   go build -o dist/installer-linux-arm64 main.go
GOOS=windows GOARCH=amd64 go build -o dist/installer.exe main.go
GOOS=darwin GOARCH=amd64  go build -o dist/installer-macos main.go
GOOS=darwin GOARCH=arm64  go build -o dist/installer-macos-arm64 main.go
echo "Builds saved to ./dist"
```

Make executable and run:

```bash
chmod +x build-all.sh
./build-all.sh
```

---

## Notes (very short)

- Ensure `data/` contains `settings.json`, `keybindings.json`, `extensions.txt` if you want them embedded.
- Run `go mod tidy` before building if dependencies changed.
- For cross-compiling macOS on Linux/Windows, consider using a macOS build runner or CI (macOS toolchain required for some cases).
