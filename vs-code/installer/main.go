// main.go
//
// Cross-platform VS Code Custom Installer
// - Embeds settings.json, keybindings.json and extensions.txt (via //go:embed)
// - Interactive choices: apply settings, apply keybindings, install extensions
// - Creates backups (optional), writes files to user VS Code config dir
// - Installs extensions with timeout, retries and random backoff
// - Writes human-readable log to ~/vscode-custom-install.log (or %USERPROFILE% on Windows)
// - Flags: --yes (non-interactive accept all), --dry-run, --src <path>, --no-backup
//
// Usage:
//   go build -o vscode-installer main.go
//   ./vscode-installer           # interactive
//   ./vscode-installer --yes     # accept defaults (apply all)
//   ./vscode-installer --dry-run # show actions but do not perform writes/installs
//   ./vscode-installer --no-backup  # skip backup
//
// Put your custom files in ./data/ (settings.json, keybindings.json, extensions.txt) before building,
// or modify the embedded files below.

package main

import (
	"bufio"
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/pterm/pterm"
)

// ---------------------- EMBED your custom files here ----------------------
// Create a folder data/ with settings.json, keybindings.json and extensions.txt.
// If they are not present at build-time, embedded variables will be empty.

//go:embed data/settings.json
var embeddedSettings []byte

//go:embed data/keybindings.json
var embeddedKeybindings []byte

//go:embed data/extensions.txt
var embeddedExtensions []byte

// -------------------------------------------------------------------------

// configuration constants
const (
	logFileName       = "vscode-custom-install.log"
	backupPrefix      = "backup_"
	extensionsFile    = "extensions.txt"
	settingsFile      = "settings.json"
	keybindingsFile   = "keybindings.json"
	installTimeoutSec = 40              // timeout for single extension install
	retries           = 3               // attempts per extension
	minSleepMs        = 800             // min random sleep between installs (ms)
	maxSleepMs        = 2500            // max random sleep between installs (ms)
	listTimeoutSec    = 10              // timeout for code --list-extensions
)

// Installer holds runtime state
type Installer struct {
	baseDir      string // dir of exe (or src if --src)
	homeDir      string
	vscodeUser   string
	backupDir    string
	logPath      string
	codeCLIPath  string
	useEmbedded  bool // whether to use embedded files or external from baseDir
	dryRun       bool
	assumeYes    bool
	srcOverride  string // path provided with --src
	settingsData []byte
	keybindData  []byte
	extList      []string
	logger       *os.File
	skipBackup   bool
}

// NewInstaller builds Installer and prepares logging
func NewInstaller(dryRun, assumeYes bool, srcOverride string, skipBackup bool) (*Installer, error) {
	inst := &Installer{
		dryRun:      dryRun,
		assumeYes:   assumeYes,
		srcOverride: srcOverride,
		skipBackup:  skipBackup,
	}

	// baseDir: exe dir by default, or srcOverride when provided
	if srcOverride != "" {
		abs, err := filepath.Abs(srcOverride)
		if err != nil {
			return nil, fmt.Errorf("bad --src path: %w", err)
		}
		inst.baseDir = abs
		inst.useEmbedded = false
	} else {
		exe, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("cannot determine exe path: %w", err)
		}
		inst.baseDir = filepath.Dir(exe)
		// decide whether embedded resources are present
		if len(embeddedSettings) > 0 || len(embeddedKeybindings) > 0 || len(embeddedExtensions) > 0 {
			inst.useEmbedded = true
		} else {
			inst.useEmbedded = false
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home dir: %w", err)
	}
	inst.homeDir = home

	// determine vscode user config dir
	inst.vscodeUser = userVSCodeDir(home)
	if inst.vscodeUser == "" {
		return nil, fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}

	// prepare log path in home dir
	inst.logPath = filepath.Join(inst.homeDir, logFileName)
	logFile, err := os.OpenFile(inst.logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("cannot open log file %s: %w", inst.logPath, err)
	}
	inst.logger = logFile

	// prepare backup dir under vscode user dir (timestamped) — creation deferred until user confirms
	ts := time.Now().Format("2006-01-02_15-04-05")
	inst.backupDir = filepath.Join(inst.vscodeUser, backupPrefix+ts)

	return inst, nil
}

func (i *Installer) Close() {
	if i.logger != nil {
		i.logger.Close()
	}
}

// log both to stdout (pretty) and to logfile
func (i *Installer) logf(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	// write with timestamp to log file
	if i.logger != nil {
		t := time.Now().Format("2006-01-02 15:04:05")
		fmt.Fprintln(i.logger, t+" "+msg)
	}
	// also print compact info via pterm
	pterm.Info.Println(msg)
}

// warn (yellow)
func (i *Installer) warnf(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	if i.logger != nil {
		t := time.Now().Format("2006-01-02 15:04:05")
		fmt.Fprintln(i.logger, t+" WARNING: "+msg)
	}
	pterm.Warning.Println(msg)
}

// error (red)
func (i *Installer) errorf(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	if i.logger != nil {
		t := time.Now().Format("2006-01-02 15:04:05")
		fmt.Fprintln(i.logger, t+" ERROR: "+msg)
	}
	pterm.Error.Println(msg)
}

// ----------------------------------------------------------------------------
// Utilities
// ----------------------------------------------------------------------------

func userVSCodeDir(home string) string {
	switch runtime.GOOS {
	case "windows":
		app := os.Getenv("APPDATA")
		if app == "" {
			// fallback
			return filepath.Join(home, "AppData", "Roaming", "Code", "User")
		}
		return filepath.Join(app, "Code", "User")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Code", "User")
	default:
		return filepath.Join(home, ".config", "Code", "User")
	}
}

// findCodeCLI tries various candidates for the 'code' CLI
func findCodeCLI() (string, error) {
	candidates := []string{
		"code", "code-insiders", "code.cmd", "code.exe", "codium", "codium.exe",
	}
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			return p, nil
		}
	}
	return "", errors.New("code CLI not found in PATH")
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	// ensure parent dir exists
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// writeBytes writes data to dst (creates parent dir). used for embedded payloads.
func writeBytes(dst string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

func readLinesFromString(s string) []string {
	var res []string
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		res = append(res, line)
	}
	return res
}

func readLinesFromFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var res []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		res = append(res, line)
	}
	return res, sc.Err()
}

// list installed extensions via code CLI (with timeout)
func listInstalledExtensions(codeCLI string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), listTimeoutSec*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, codeCLI, "--list-extensions")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var res []string
	for _, l := range strings.Split(string(out), "\n") {
		if t := strings.TrimSpace(l); t != "" {
			res = append(res, t)
		}
	}
	return res, nil
}

// case-insensitive contains for installed set
func installedContains(set []string, ext string) bool {
	le := strings.ToLower(ext)
	for _, s := range set {
		if strings.ToLower(s) == le {
			return true
		}
	}
	return false
}

// random sleep between min and max (milliseconds)
func randSleep(minMs, maxMs int) {
	if maxMs <= minMs {
		time.Sleep(time.Duration(minMs) * time.Millisecond)
		return
	}
	ms := minMs + rand.Intn(maxMs-minMs+1)
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

// run a command with combined output and timeout
func runCommandWithTimeout(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// ----------------------------------------------------------------------------
// Interactive helpers
// ----------------------------------------------------------------------------

func askYesNoDefaultYes(reader *bufio.Reader, question string, defaultYes bool) (bool, error) {
	if defaultYes {
		fmt.Printf("%s [Y/n]: ", question)
	} else {
		fmt.Printf("%s [y/N]: ", question)
	}
	text, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return defaultYes, nil
	}
	first := strings.ToLower(string(text[0]))
	return first == "y", nil
}

func chooseExtensionsInteractive(reader *bufio.Reader, all []string) ([]string, error) {
	// simple interactive chooser: show enumerated list and allow:
	// - "all" or "a" to choose all
	// - comma-separated numbers like "1,3,5-7"
	// - "none" or blank to skip
	fmt.Println("Список расширений (краткий):")
	for idx, ex := range all {
		fmt.Printf("  %3d) %s\n", idx+1, ex)
	}
	fmt.Println()
	fmt.Println("Варианты ввода:")
	fmt.Println("  all             — установить все")
	fmt.Println("  none / пусто    — пропустить установку")
	fmt.Println("  1,3,5-7         — установить перечисленные номера")
	fmt.Print("Выберите (all/none/числа): ")

	txt, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	txt = strings.TrimSpace(txt)
	if txt == "" || strings.EqualFold(txt, "none") {
		return []string{}, nil
	}
	if strings.EqualFold(txt, "all") || strings.EqualFold(txt, "a") {
		return all, nil
	}

	// parse selection
	parts := strings.Split(txt, ",")
	var sel []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.Contains(p, "-") {
			rng := strings.SplitN(p, "-", 2)
			if len(rng) != 2 {
				continue
			}
			start := parseIntOrZero(rng[0]) - 1
			end := parseIntOrZero(rng[1]) - 1
			if start < 0 || end < 0 || start >= len(all) || end >= len(all) || end < start {
				continue
			}
			for k := start; k <= end; k++ {
				sel = append(sel, all[k])
			}
		} else {
			idx := parseIntOrZero(p) - 1
			if idx >= 0 && idx < len(all) {
				sel = append(sel, all[idx])
			}
		}
	}
	// dedupe
	m := make(map[string]struct{})
	var out []string
	for _, s := range sel {
		if _, ok := m[s]; !ok {
			m[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out, nil
}

func parseIntOrZero(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	var v int
	fmt.Sscanf(s, "%d", &v)
	return v
}

// ----------------------------------------------------------------------------
// Main installer steps
// ----------------------------------------------------------------------------

func (i *Installer) preparePayloads() error {
	// if useEmbedded, load embedded variables; otherwise read files from baseDir
	if i.useEmbedded {
		i.settingsData = embeddedSettings
		i.keybindData = embeddedKeybindings
		i.extList = readLinesFromString(string(embeddedExtensions))
	} else {
		// load files from baseDir
		settingsPath := filepath.Join(i.baseDir, settingsFile)
		keybindPath := filepath.Join(i.baseDir, keybindingsFile)
		extPath := filepath.Join(i.baseDir, extensionsFile)

		if exists(settingsPath) {
			b, err := os.ReadFile(settingsPath)
			if err != nil {
				return fmt.Errorf("cannot read %s: %w", settingsPath, err)
			}
			i.settingsData = b
		}

		if exists(keybindPath) {
			b, err := os.ReadFile(keybindPath)
			if err != nil {
				return fmt.Errorf("cannot read %s: %w", keybindPath, err)
			}
			i.keybindData = b
		}

		if exists(extPath) {
			lines, err := readLinesFromFile(extPath)
			if err != nil {
				return fmt.Errorf("cannot read %s: %w", extPath, err)
			}
			i.extList = lines
		}
	}
	return nil
}

func (i *Installer) ensureCodeCLI() error {
	// try to find code CLI
	c, err := findCodeCLI()
	if err != nil {
		return err
	}
	i.codeCLIPath = c
	return nil
}

// makeBackup creates backup dir and copies existing settings/keybindings
// Respects dry-run and skipBackup flags.
func (i *Installer) makeBackup() error {
	if i.skipBackup {
		i.logf("Backup skipped by user (--no-backup).")
		return nil
	}
	if i.dryRun {
		i.logf("DRY-RUN: would create backup dir %s and copy existing files", i.backupDir)
		return nil
	}
	// create backup dir
	if err := os.MkdirAll(i.backupDir, 0o755); err != nil {
		return err
	}
	// copy existing settings and keybindings if present
	for _, nm := range []string{settingsFile, keybindingsFile} {
		src := filepath.Join(i.vscodeUser, nm)
		if exists(src) {
			dst := filepath.Join(i.backupDir, nm)
			if err := copyFile(src, dst); err != nil {
				i.warnf("cannot backup %s: %v", nm, err)
			} else {
				i.logf("backup: %s -> %s", src, dst)
			}
		} else {
			i.logf("no existing %s to backup", nm)
		}
	}
	return nil
}

func (i *Installer) applySettings() error {
	if len(i.settingsData) == 0 {
		i.warnf("settings.json payload is empty — пропускаю")
		return nil
	}
	dst := filepath.Join(i.vscodeUser, settingsFile)
	if i.dryRun {
		i.logf("DRY-RUN: would write %s (%d bytes)", dst, len(i.settingsData))
		return nil
	}
	if err := writeBytes(dst, i.settingsData); err != nil {
		return fmt.Errorf("cannot write settings.json: %w", err)
	}
	i.logf("Applied settings.json -> %s", dst)
	return nil
}

func (i *Installer) applyKeybindings() error {
	if len(i.keybindData) == 0 {
		i.warnf("keybindings.json payload is empty — пропускаю")
		return nil
	}
	dst := filepath.Join(i.vscodeUser, keybindingsFile)
	if i.dryRun {
		i.logf("DRY-RUN: would write %s (%d bytes)", dst, len(i.keybindData))
		return nil
	}
	if err := writeBytes(dst, i.keybindData); err != nil {
		return fmt.Errorf("cannot write keybindings.json: %w", err)
	}
	i.logf("Applied keybindings.json -> %s", dst)
	return nil
}

// installExtensionsInteractive handles interactive selection then installs
func (i *Installer) installExtensionsInteractive(reader *bufio.Reader) error {
	if len(i.extList) == 0 {
		i.warnf("extensions list is empty — nothing to install")
		return nil
	}
	// ask whether install all or choose
	if i.assumeYes {
		i.logf("Assume-yes mode: installing all extensions")
		return i.installExtensions(i.extList)
	}
	// ask user
	apply, err := askYesNoDefaultYes(reader, fmt.Sprintf("Установить %d расширений?", len(i.extList)), true)
	if err != nil {
		return err
	}
	if !apply {
		i.logf("User declined to install extensions")
		return nil
	}

	// choose all or subset
	choice, err := askYesNoDefaultYes(reader, "Установить все расширения (yes) или выбрать подмножество (no)?", true)
	if err != nil {
		return err
	}
	var toInstall []string
	if choice {
		toInstall = i.extList
	} else {
		selected, err := chooseExtensionsInteractive(reader, i.extList)
		if err != nil {
			return err
		}
		toInstall = selected
	}

	if len(toInstall) == 0 {
		i.logf("No extensions selected to install.")
		return nil
	}
	return i.installExtensions(toInstall)
}

// installExtensions installs the provided extension IDs with retries/timeouts
func (i *Installer) installExtensions(toInstall []string) error {
	// need code CLI
	if err := i.ensureCodeCLI(); err != nil {
		return fmt.Errorf("code CLI not found: %w", err)
	}

	// get installed list once
	installed, err := listInstalledExtensions(i.codeCLIPath)
	if err != nil {
		i.warnf("cannot list installed extensions: %v — continuing without dedupe", err)
	}

	total := len(toInstall)
	pbar, _ := pterm.DefaultProgressbar.WithTotal(total).WithTitle("Installing extensions").Start()
	for idx, ext := range toInstall {
		pbar.UpdateTitle(fmt.Sprintf("[%d/%d] %s", idx+1, total, ext))
		// skip if already installed
		if installed != nil && installedContains(installed, ext) {
			i.logf("Already installed, skipping: %s", ext)
			pbar.Increment()
			continue
		}
		// attempt install with retries
		success := false
		var lastOut string
		for attempt := 1; attempt <= retries; attempt++ {
			if i.dryRun {
				i.logf("DRY-RUN: would run: %s --install-extension %s", i.codeCLIPath, ext)
				success = true
				break
			}
			i.logf("Installing %s (attempt %d/%d)", ext, attempt, retries)
			out, err := runCommandWithTimeout(time.Second*installTimeoutSec, i.codeCLIPath, "--install-extension", ext, "--force")
			lastOut = out
			if err == nil {
				i.logf("Installed: %s", ext)
				success = true
				// update installed slice to contain ext
				installed = append(installed, ext)
				break
			}
			// detect timeout
			if errors.Is(err, context.DeadlineExceeded) {
				i.warnf("Timeout installing %s (attempt %d)", ext, attempt)
			} else {
				i.warnf("Error installing %s: %v", ext, err)
			}
			// small backoff before retry
			randSleep(1200, 2200)
		}
		if !success {
			i.errorf("Failed to install %s after %d attempts. Last output:\n%s", ext, retries, lastOut)
		}
		pbar.Increment()
		// random pause to avoid Hammering Marketplace
		randSleep(minSleepMs, maxSleepMs)
	}
	pbar.Stop()
	return nil
}

// ----------------------------------------------------------------------------
// Main
// ----------------------------------------------------------------------------

func main() {
	rand.Seed(time.Now().UnixNano())

	// CLI flags
	var (
		flagYes     = flag.Bool("yes", false, "Assume 'yes' for all questions (non-interactive)")
		flagDry     = flag.Bool("dry-run", false, "Dry run - show actions but don't write files or install extensions")
		flagSrc     = flag.String("src", "", "Use external folder with settings.json/keybindings.json/extensions.txt instead of embedded payloads")
		flagNoBackup = flag.Bool("no-backup", false, "Don't create backup of existing user settings (skip backup)")
		flagHelp    = flag.Bool("help", false, "Show help")
	)
	flag.Parse()
	if *flagHelp {
		flag.Usage()
		return
	}

	// pretty header
	pterm.DefaultBigText.WithLetters(pterm.NewLettersFromString("HYPR • VS CODE")).Render()
	fmt.Println()
	pterm.DefaultSection.Println("VS Code Custom Installer — interactive, cross-platform")
	fmt.Println()

	installer, err := NewInstaller(*flagDry, *flagYes, *flagSrc, *flagNoBackup)
	if err != nil {
		pterm.Fatal.Println("Cannot initialize installer:", err)
		return
	}
	defer installer.Close()

	// prepare payloads (embedded or external)
	if err := installer.preparePayloads(); err != nil {
		installer.errorf("Failed to prepare payloads: %v", err)
		// continue, because maybe user only wants to install extensions (which may be present)
	}

	// banner
	installer.logf("Target VS Code user config: %s", installer.vscodeUser)
	installer.logf("Backup dir will be: %s", installer.backupDir)
	installer.logf("Log file: %s", installer.logPath)

	// interactive flow
	reader := bufio.NewReader(os.Stdin)

	// ensure code CLI presence (we will only error out when needed)
	_ = installer.ensureCodeCLI() // not fatal yet

	// Ask whether to create backup (new behavior)
	doBackup := false
	if installer.assumeYes && !installer.skipBackup {
		// auto backup by default when --yes and not explicitly skipped
		doBackup = true
	} else if installer.skipBackup {
		doBackup = false
	} else {
		ask, _ := askYesNoDefaultYes(reader, "Создать бэкап текущих настроек перед изменением?", true)
		doBackup = ask
	}

	if doBackup {
		installer.logf("Backup: creating backup directory and saving existing settings.")
		if !installer.dryRun {
			if err := os.MkdirAll(installer.backupDir, 0o755); err != nil {
				installer.errorf("Cannot create backup dir: %v", err)
			}
		}
		if err := installer.makeBackup(); err != nil {
			installer.warnf("Backup step failed: %v", err)
		}
	} else {
		installer.logf("User chose to skip backup.")
	}

	// Ask 3 questions (settings, keybinds, extensions)
	applySettings := false
	applyKeybinds := false
	installExts := false

	if installer.assumeYes {
		applySettings = true
		applyKeybinds = true
		installExts = true
	} else {
		ok, _ := askYesNoDefaultYes(reader, "Применить settings.json?", true)
		applySettings = ok
		ok2, _ := askYesNoDefaultYes(reader, "Применить keybindings.json?", true)
		applyKeybinds = ok2
		ok3, _ := askYesNoDefaultYes(reader, "Установить расширения из списка?", true)
		installExts = ok3
	}

	// apply settings
	if applySettings {
		if err := installer.applySettings(); err != nil {
			installer.errorf("Failed to apply settings: %v", err)
		}
	} else {
		installer.logf("Skipped applying settings.json")
	}

	// apply keybindings
	if applyKeybinds {
		if err := installer.applyKeybindings(); err != nil {
			installer.errorf("Failed to apply keybindings: %v", err)
		}
	} else {
		installer.logf("Skipped applying keybindings.json")
	}

	// install extensions
	if installExts {
		// if payload extList empty but external src provided with no extensions file, warn
		if len(installer.extList) == 0 {
			installer.warnf("No extensions found in payload (embedded or src). Nothing to install.")
		} else {
			if installer.assumeYes {
				installer.installExtensions(installer.extList)
			} else {
				if err := installer.installExtensionsInteractive(reader); err != nil {
					installer.errorf("Extensions installation failed: %v", err)
				}
			}
		}
	} else {
		installer.logf("Skipped installing extensions")
	}

	// finish
	pterm.Success.Println("All done — installer finished.")
	installer.logf("Finished at %s", time.Now().Format(time.RFC3339))
	installer.logf("Backup dir: %s", installer.backupDir)
	installer.logf("Log file: %s", installer.logPath)
}
