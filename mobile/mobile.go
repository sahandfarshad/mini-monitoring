// Package mobile is the gomobile-bound API for the Android shell.
// The Android app shows the same HTML frontend in a WebView and forwards
// every call through Call, receiving events through Listener.
package mobile

import (
	"sync"

	"minimon/core"
)

// Listener receives events ("stat", "status") as JSON.
type Listener interface {
	OnEvent(name string, json string)
}

var (
	mu sync.Mutex
	c  *core.Core
)

// Start loads the config from dir and starts monitoring.
func Start(dir, version string, l Listener) {
	mu.Lock()
	defer mu.Unlock()
	if c != nil {
		return
	}
	core.ConfigDir = dir
	c = core.New(version, func(name string, v any) {
		b, err := jsonMarshal(v)
		if err == nil {
			l.OnEvent(name, string(b))
		}
	})
	go c.StartAll()
}

// Pause stops all SSH sessions (app in background); Resume restarts them.
func Pause() {
	if c != nil {
		c.StopAll()
	}
}

func Resume() {
	if c != nil {
		go c.StartAll()
	}
}

// Call runs a frontend method; args is a JSON array, the result is JSON.
func Call(method, args string) string {
	if c == nil {
		return "null"
	}
	r, err := c.Dispatch(method, args, "android")
	if err != nil {
		return "null"
	}
	return r
}
