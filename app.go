package main

import (
	"context"
	"os"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"minimon/core"
)

// App is the Windows shell around core.Core, bound to the frontend by Wails.
type App struct {
	ctx context.Context
	c   *core.Core
}

func NewApp() *App {
	a := &App{}
	a.c = core.New(version, func(name string, v any) {
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, name, v)
		}
	})
	return a
}

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

func (a *App) domReady(ctx context.Context) {
	u := a.c.Cfg.UI
	if u.PosSaved {
		runtime.WindowSetPosition(ctx, u.WinX, u.WinY)
	}
	a.c.StartAll()
	go a.trackPosition()
}

func (a *App) savePos() {
	x, y := runtime.WindowGetPosition(a.ctx)
	a.c.Lock()
	u := &a.c.Cfg.UI
	changed := !u.PosSaved || x != u.WinX || y != u.WinY
	u.WinX, u.WinY, u.PosSaved = x, y, true
	a.c.Unlock()
	if changed {
		_ = a.c.Cfg.Save()
	}
}

func (a *App) trackPosition() {
	for range time.Tick(4 * time.Second) {
		a.savePos()
	}
}

func (a *App) beforeClose(ctx context.Context) bool { a.savePos(); return false }
func (a *App) shutdown(ctx context.Context)          { a.c.StopAll() }

// ---- bound methods ----

func (a *App) GetState() core.State                 { return a.c.GetState("windows") }
func (a *App) TestServer(in core.ServerInput) string { return a.c.TestServer(in) }
func (a *App) SaveServer(in core.ServerInput) string { return a.c.SaveServer(in) }
func (a *App) DeleteServer(id string)                { a.c.DeleteServer(id) }
func (a *App) MoveServer(id string, dir int)         { a.c.MoveServer(id, dir) }
func (a *App) SetServerSize(id, size string)         { a.c.SetServerSize(id, size) }
func (a *App) ToggleServer(id string)                { a.c.ToggleServer(id) }
func (a *App) TrustNewHostKey(id string)             { a.c.TrustNewHostKey(id) }

func (a *App) SaveUI(u core.UI) {
	a.c.SaveUI(u)
	runtime.WindowSetAlwaysOnTop(a.ctx, u.OnTop)
}

func (a *App) FitWindow(w, h int) {
	if w < 120 {
		w = 120
	}
	if h < 40 {
		h = 40
	}
	runtime.WindowSetSize(a.ctx, w, h)
	a.c.Lock()
	a.c.Cfg.UI.WinW, a.c.Cfg.UI.WinH = w, h
	a.c.Unlock()
}

func (a *App) PickKeyFile() string {
	p, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "Private key"})
	if err != nil || p == "" {
		return ""
	}
	st, err := os.Stat(p)
	if err != nil || st.Size() > 64*1024 {
		return ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return string(b)
}

func (a *App) Minimise() { runtime.WindowMinimise(a.ctx) }
func (a *App) Quit()     { runtime.Quit(a.ctx) }
