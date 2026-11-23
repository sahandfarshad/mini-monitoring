package core

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Server is one monitored machine. Secret (password or private key) is stored
// encrypted with Windows DPAPI, so only this Windows user can read it.
type Server struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Auth     string `json:"auth"` // "password" or "key"
	Secret   string `json:"secret,omitempty"`
	Pass     string `json:"pass,omitempty"` // key passphrase (encrypted)
	Iface    string `json:"iface"`          // empty = default route interface
	Size     string `json:"size"`           // "", "s", "m", "l" ("" = global)
	HostKey  string `json:"hostKey,omitempty"`
	Disabled bool   `json:"disabled"`
}

// UI holds every adjustable look-and-feel setting.
type UI struct {
	Size      string  `json:"size"`      // s | m | l
	ScaleS    float64 `json:"scaleS"`    // multipliers per size
	ScaleM    float64 `json:"scaleM"`
	ScaleL    float64 `json:"scaleL"`
	Opacity   float64 `json:"opacity"`   // 0.3 .. 1
	History   int     `json:"history"`   // seconds shown in chart
	Interval  int     `json:"interval"`  // seconds between samples
	Bits      bool    `json:"bits"`      // show Mbit/s instead of MB/s
	OnTop     bool    `json:"onTop"`
	RxColor   string  `json:"rxColor"`
	TxColor   string  `json:"txColor"`
	CPUColor  string  `json:"cpuColor"`
	RAMColor  string  `json:"ramColor"`
	BgColor   string  `json:"bgColor"`
	ShowTitle bool    `json:"showTitle"`
	WinX      int     `json:"winX"`
	WinY      int     `json:"winY"`
	WinW      int     `json:"winW"`
	WinH      int     `json:"winH"`
	PosSaved  bool    `json:"posSaved"`
}

type Config struct {
	UI      UI       `json:"ui"`
	Servers []Server `json:"servers"`
}

var cfgMu sync.Mutex

func defaultUI() UI {
	return UI{Size: "m", ScaleS: 1, ScaleM: 1, ScaleL: 1, Opacity: 0.92, History: 60, Interval: 1,
		Bits: true, OnTop: true, RxColor: "#34d399", TxColor: "#60a5fa", CPUColor: "#f59e0b",
		RAMColor: "#a78bfa", BgColor: "#11151c", ShowTitle: true, WinW: 330, WinH: 160}
}

// ConfigDir overrides where config.json lives (mobile sets its app files dir).
var ConfigDir string

func configPath() string {
	dir := ConfigDir
	if dir == "" {
		d, err := os.UserConfigDir()
		if err != nil {
			d = "."
		}
		dir = filepath.Join(d, "MiniMonitor")
	}
	_ = os.MkdirAll(dir, 0o700)
	return filepath.Join(dir, "config.json")
}

func LoadConfig() *Config {
	c := &Config{UI: defaultUI()}
	b, err := os.ReadFile(configPath())
	if err == nil {
		_ = json.Unmarshal(b, c)
	}
	d := defaultUI()
	if c.UI.Size == "" {
		c.UI.Size = d.Size
	}
	if c.UI.ScaleS <= 0 {
		c.UI.ScaleS = 1
	}
	if c.UI.ScaleM <= 0 {
		c.UI.ScaleM = 1
	}
	if c.UI.ScaleL <= 0 {
		c.UI.ScaleL = 1
	}
	if c.UI.Opacity <= 0 {
		c.UI.Opacity = d.Opacity
	}
	if c.UI.History <= 0 {
		c.UI.History = d.History
	}
	if c.UI.Interval <= 0 {
		c.UI.Interval = 1
	}
	if c.UI.WinW < 120 {
		c.UI.WinW = d.WinW
	}
	if c.UI.WinH < 40 {
		c.UI.WinH = d.WinH
	}
	for _, k := range []*string{&c.UI.RxColor, &c.UI.TxColor, &c.UI.CPUColor, &c.UI.RAMColor, &c.UI.BgColor} {
		if *k == "" {
			c.UI = mergeColors(c.UI, d)
			break
		}
	}
	return c
}

func mergeColors(u, d UI) UI {
	if u.RxColor == "" {
		u.RxColor = d.RxColor
	}
	if u.TxColor == "" {
		u.TxColor = d.TxColor
	}
	if u.CPUColor == "" {
		u.CPUColor = d.CPUColor
	}
	if u.RAMColor == "" {
		u.RAMColor = d.RAMColor
	}
	if u.BgColor == "" {
		u.BgColor = d.BgColor
	}
	return u
}

func (c *Config) Save() error {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	b, _ := json.MarshalIndent(c, "", "  ")
	tmp := configPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, configPath())
}

func seal(plain string) string {
	if plain == "" {
		return ""
	}
	enc, err := protect([]byte(plain))
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(enc)
}

func unseal(s string) string {
	if s == "" {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return ""
	}
	p, err := unprotect(raw)
	if err != nil {
		return ""
	}
	return string(p)
}
