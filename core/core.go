package core

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
)

// Emitter delivers events ("stat", "status") to the UI.
type Emitter func(name string, payload any)

// Core is the platform-independent app: config, servers and monitors.
// The desktop (Wails) and mobile (gomobile) shells both wrap it.
type Core struct {
	Cfg     *Config
	Mon     *Monitor
	Version string
	mu      sync.Mutex
}

type ServerView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	User      string `json:"user"`
	Auth      string `json:"auth"`
	Iface     string `json:"iface"`
	Size      string `json:"size"`
	Disabled  bool   `json:"disabled"`
	HasSecret bool   `json:"hasSecret"`
}

type ServerInput struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
	User   string `json:"user"`
	Auth   string `json:"auth"`
	Secret string `json:"secret"`
	Pass   string `json:"pass"`
	Iface  string `json:"iface"`
}

type State struct {
	UI       UI           `json:"ui"`
	Servers  []ServerView `json:"servers"`
	Statuses []Status     `json:"statuses"`
	Version  string       `json:"version"`
	Platform string       `json:"platform"`
}

func New(version string, emit Emitter) *Core {
	c := &Core{Cfg: LoadConfig(), Version: version}
	c.Mon = NewMonitor(
		func(s Stat) { emit("stat", s) },
		func(s Status) { emit("status", s) },
		func(id, key string) {
			c.mu.Lock()
			for i := range c.Cfg.Servers {
				if c.Cfg.Servers[i].ID == id && c.Cfg.Servers[i].HostKey == "" {
					c.Cfg.Servers[i].HostKey = key
				}
			}
			c.mu.Unlock()
			_ = c.Cfg.Save()
		})
	return c
}

func (c *Core) StartAll() {
	c.mu.Lock()
	servers := append([]Server(nil), c.Cfg.Servers...)
	iv := c.Cfg.UI.Interval
	c.mu.Unlock()
	for _, s := range servers {
		c.Mon.Start(s, iv)
	}
}

func (c *Core) StopAll() { c.Mon.StopAll() }

func (c *Core) Lock()   { c.mu.Lock() }
func (c *Core) Unlock() { c.mu.Unlock() }

func view(s Server) ServerView {
	return ServerView{ID: s.ID, Name: s.Name, Host: s.Host, Port: s.Port, User: s.User, Auth: s.Auth,
		Iface: s.Iface, Size: s.Size, Disabled: s.Disabled, HasSecret: s.Secret != ""}
}

func (c *Core) GetState(platform string) State {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := State{UI: c.Cfg.UI, Version: c.Version, Platform: platform, Servers: []ServerView{}, Statuses: []Status{}}
	for _, s := range c.Cfg.Servers {
		st.Servers = append(st.Servers, view(s))
		if v, ok := c.Mon.last.Load(s.ID); ok {
			st.Statuses = append(st.Statuses, v.(Status))
		}
	}
	return st
}

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (c *Core) fromInput(in ServerInput) (Server, error) {
	in.Host = strings.TrimSpace(in.Host)
	in.User = strings.TrimSpace(in.User)
	if in.Host == "" || in.User == "" {
		return Server{}, errors.New("host and user are required")
	}
	if in.Port <= 0 || in.Port > 65535 {
		in.Port = 22
	}
	if in.Auth != "key" {
		in.Auth = "password"
	}
	var s Server
	c.mu.Lock()
	for _, x := range c.Cfg.Servers {
		if x.ID == in.ID && in.ID != "" {
			s = x
		}
	}
	c.mu.Unlock()
	if s.ID == "" {
		s.ID = newID()
	}
	if s.Host != in.Host || s.Port != in.Port {
		s.HostKey = ""
	}
	if s.Auth != in.Auth && in.Secret == "" {
		s.Secret = ""
	}
	s.Name, s.Host, s.Port, s.User, s.Auth, s.Iface = strings.TrimSpace(in.Name), in.Host, in.Port, in.User, in.Auth, strings.TrimSpace(in.Iface)
	if s.Name == "" {
		s.Name = in.Host
	}
	if in.Secret != "" {
		s.Secret = seal(in.Secret)
		s.Pass = seal(in.Pass)
	}
	if s.Secret == "" {
		return Server{}, errors.New("password or key is required")
	}
	return s, nil
}

func (c *Core) TestServer(in ServerInput) string {
	s, err := c.fromInput(in)
	if err != nil {
		return err.Error()
	}
	iface, _, err := Test(s)
	if err != nil {
		return err.Error()
	}
	return "OK " + iface
}

func (c *Core) SaveServer(in ServerInput) string {
	s, err := c.fromInput(in)
	if err != nil {
		return err.Error()
	}
	c.mu.Lock()
	found := false
	for i := range c.Cfg.Servers {
		if c.Cfg.Servers[i].ID == s.ID {
			s.Size, s.Disabled = c.Cfg.Servers[i].Size, c.Cfg.Servers[i].Disabled
			c.Cfg.Servers[i] = s
			found = true
		}
	}
	if !found {
		c.Cfg.Servers = append(c.Cfg.Servers, s)
	}
	iv := c.Cfg.UI.Interval
	c.mu.Unlock()
	if err := c.Cfg.Save(); err != nil {
		return err.Error()
	}
	go c.Mon.Start(s, iv)
	return ""
}

func (c *Core) DeleteServer(id string) {
	c.mu.Lock()
	out := c.Cfg.Servers[:0]
	for _, s := range c.Cfg.Servers {
		if s.ID != id {
			out = append(out, s)
		}
	}
	c.Cfg.Servers = out
	c.mu.Unlock()
	_ = c.Cfg.Save()
	go c.Mon.Stop(id)
}

func (c *Core) MoveServer(id string, dir int) {
	c.mu.Lock()
	for i, s := range c.Cfg.Servers {
		j := i + dir
		if s.ID == id && j >= 0 && j < len(c.Cfg.Servers) {
			c.Cfg.Servers[i], c.Cfg.Servers[j] = c.Cfg.Servers[j], c.Cfg.Servers[i]
			break
		}
	}
	c.mu.Unlock()
	_ = c.Cfg.Save()
}

func (c *Core) edit(id string, f func(*Server)) {
	c.mu.Lock()
	for i := range c.Cfg.Servers {
		if c.Cfg.Servers[i].ID == id {
			f(&c.Cfg.Servers[i])
		}
	}
	c.mu.Unlock()
	_ = c.Cfg.Save()
}

func (c *Core) SetServerSize(id, size string) { c.edit(id, func(s *Server) { s.Size = size }) }

func (c *Core) restart(id string, f func(*Server)) {
	var srv Server
	c.edit(id, func(s *Server) { f(s); srv = *s })
	if srv.ID != "" {
		go c.Mon.Start(srv, c.Cfg.UI.Interval)
	}
}

func (c *Core) ToggleServer(id string)    { c.restart(id, func(s *Server) { s.Disabled = !s.Disabled }) }
func (c *Core) TrustNewHostKey(id string) { c.restart(id, func(s *Server) { s.HostKey = "" }) }

// SaveUI stores look-and-feel settings; window geometry is kept as is.
func (c *Core) SaveUI(u UI) {
	c.mu.Lock()
	old := c.Cfg.UI
	u.WinX, u.WinY, u.PosSaved, u.WinW, u.WinH = old.WinX, old.WinY, old.PosSaved, old.WinW, old.WinH
	if u.Interval < 1 {
		u.Interval = 1
	}
	if u.Interval > 60 {
		u.Interval = 60
	}
	c.Cfg.UI = u
	servers := append([]Server(nil), c.Cfg.Servers...)
	c.mu.Unlock()
	_ = c.Cfg.Save()
	if old.Interval != u.Interval {
		go func() {
			for _, s := range servers {
				c.Mon.Start(s, u.Interval)
			}
		}()
	}
}

// Dispatch runs a UI call given as method name + JSON array of arguments and
// returns the JSON result. Used by shells without native bindings (Android).
func (c *Core) Dispatch(method, args, platform string) (string, error) {
	var a []json.RawMessage
	if args != "" {
		if err := json.Unmarshal([]byte(args), &a); err != nil {
			return "", err
		}
	}
	str := func(i int) string {
		var s string
		if i < len(a) {
			_ = json.Unmarshal(a[i], &s)
		}
		return s
	}
	num := func(i int) int {
		var n int
		if i < len(a) {
			_ = json.Unmarshal(a[i], &n)
		}
		return n
	}
	in := func() ServerInput {
		var v ServerInput
		if len(a) > 0 {
			_ = json.Unmarshal(a[0], &v)
		}
		return v
	}
	var res any
	switch method {
	case "GetState":
		res = c.GetState(platform)
	case "TestServer":
		res = c.TestServer(in())
	case "SaveServer":
		res = c.SaveServer(in())
	case "DeleteServer":
		c.DeleteServer(str(0))
	case "MoveServer":
		c.MoveServer(str(0), num(1))
	case "SetServerSize":
		c.SetServerSize(str(0), str(1))
	case "ToggleServer":
		c.ToggleServer(str(0))
	case "TrustNewHostKey":
		c.TrustNewHostKey(str(0))
	case "SaveUI":
		var u UI
		if len(a) > 0 {
			_ = json.Unmarshal(a[0], &u)
		}
		c.SaveUI(u)
	default:
		return "null", nil // window-only calls are no-ops elsewhere
	}
	b, err := json.Marshal(res)
	return string(b), err
}
