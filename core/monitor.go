package core

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// remoteScript runs on the server inside the single SSH session. It uses only
// shell builtins (read/echo/case), so no process is spawned per sample: the
// whole cost on the server is reading three small /proc files once a tick.
const remoteScript = `export LC_ALL=C
W="__IF__"
[ -z "$W" ] && while read -r i d _; do [ "$d" = 00000000 ] && { W=$i; break; }; done < /proc/net/route
[ -z "$W" ] && while read -r l; do case $l in *:*) i=${l%%:*}; [ "$i" != lo ] && { W=$i; break; };; esac; done < /proc/net/dev
echo "IF $W"
exec 3<> <(:) 2>/dev/null
while :; do
 read -r _ a b c d e f g h _ < /proc/stat
 t=0; v=0; while read -r k x _; do case $k in MemTotal:) t=$x;; MemAvailable:) v=$x; break;; esac; done < /proc/meminfo
 r=0; s=0; while read -r l; do case $l in "$W:"*) set -- ${l#*:}; r=$1; s=$9; break;; esac; done < /proc/net/dev
 echo "$a $b $c $d $e $f $g $h $t $v $r $s"
 read -t __I__ -u 3 _ 2>/dev/null; [ $? -gt 128 ] || sleep __I__
done`

type Stat struct {
	ID       string  `json:"id"`
	Rx       float64 `json:"rx"` // bytes/s
	Tx       float64 `json:"tx"`
	CPU      float64 `json:"cpu"` // 0..100
	RAM      float64 `json:"ram"` // 0..100
	MemUsed  uint64  `json:"memUsed"`
	MemTotal uint64  `json:"memTotal"`
	Iface    string  `json:"iface"`
	TS       int64   `json:"ts"`
}

type Status struct {
	ID    string `json:"id"`
	State string `json:"state"` // connecting | online | error | off
	Msg   string `json:"msg"`
}

type worker struct {
	cancel context.CancelFunc
	done   chan struct{}
}

type Monitor struct {
	mu      sync.Mutex
	workers map[string]*worker
	emitS   func(Stat)
	emitSt  func(Status)
	onKey   func(id, key string)
	last    sync.Map // id -> Status
}

func NewMonitor(emitS func(Stat), emitSt func(Status), onKey func(id, key string)) *Monitor {
	return &Monitor{workers: map[string]*worker{}, emitS: emitS, emitSt: emitSt, onKey: onKey}
}

func (m *Monitor) status(s Status) {
	m.last.Store(s.ID, s)
	m.emitSt(s)
}

func (m *Monitor) Start(s Server, interval int) {
	m.Stop(s.ID)
	if s.Disabled {
		m.status(Status{ID: s.ID, State: "off"})
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &worker{cancel: cancel, done: make(chan struct{})}
	m.mu.Lock()
	m.workers[s.ID] = w
	m.mu.Unlock()
	go func() {
		defer close(w.done)
		backoff := 3 * time.Second
		for {
			m.status(Status{ID: s.ID, State: "connecting"})
			start := time.Now()
			err := m.run(ctx, &s, interval)
			if ctx.Err() != nil {
				return
			}
			msg := "disconnected"
			if err != nil {
				msg = err.Error()
			}
			m.status(Status{ID: s.ID, State: "error", Msg: msg})
			if time.Since(start) > time.Minute {
				backoff = 3 * time.Second
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 60*time.Second {
				backoff *= 2
			}
		}
	}()
}

func (m *Monitor) Stop(id string) {
	m.mu.Lock()
	w := m.workers[id]
	delete(m.workers, id)
	m.mu.Unlock()
	if w != nil {
		w.cancel()
		select {
		case <-w.done:
		case <-time.After(3 * time.Second):
		}
	}
}

func (m *Monitor) StopAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.workers))
	for id := range m.workers {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.Stop(id)
	}
}

func sshConfig(s *Server, onKey func(string)) (*ssh.ClientConfig, error) {
	var auths []ssh.AuthMethod
	secret := unseal(s.Secret)
	switch s.Auth {
	case "key":
		if secret == "" {
			return nil, errors.New("no private key")
		}
		var signer ssh.Signer
		var err error
		if p := unseal(s.Pass); p != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(secret), []byte(p))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(secret))
		}
		if err != nil {
			return nil, fmt.Errorf("bad key: %v", err)
		}
		auths = append(auths, ssh.PublicKeys(signer))
	default:
		auths = append(auths, ssh.Password(secret),
			ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
				a := make([]string, len(qs))
				for i := range a {
					a[i] = secret
				}
				return a, nil
			}))
	}
	known := s.HostKey
	return &ssh.ClientConfig{
		User:    s.User,
		Auth:    auths,
		Timeout: 12 * time.Second,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			got := base64.StdEncoding.EncodeToString(key.Marshal())
			if known == "" {
				onKey(got)
				return nil
			}
			if got != known {
				return errors.New("HOSTKEY_CHANGED")
			}
			return nil
		},
	}, nil
}

func dial(ctx context.Context, s *Server, onKey func(string)) (*ssh.Client, error) {
	cc, err := sshConfig(s, onKey)
	if err != nil {
		return nil, err
	}
	port := s.Port
	if port == 0 {
		port = 22
	}
	addr := net.JoinHostPort(s.Host, strconv.Itoa(port))
	d := net.Dialer{Timeout: 12 * time.Second, KeepAlive: 30 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cc)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return ssh.NewClient(c, chans, reqs), nil
}

// Test checks that the server is reachable and readable, returning the
// detected interface on success.
func Test(s Server) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var hk string
	cl, err := dial(ctx, &s, func(k string) { hk = k })
	if err != nil {
		return "", "", err
	}
	defer cl.Close()
	sess, err := cl.NewSession()
	if err != nil {
		return "", "", err
	}
	defer sess.Close()
	out, err := sess.Output(`while read -r i d _; do [ "$d" = 00000000 ] && { echo "$i"; break; }; done < /proc/net/route; [ -r /proc/stat ] && echo OK`)
	if err != nil && len(out) == 0 {
		return "", "", err
	}
	o := strings.TrimSpace(string(out))
	if !strings.HasSuffix(o, "OK") {
		return "", "", errors.New("cannot read /proc (Linux only)")
	}
	return strings.TrimSpace(strings.TrimSuffix(o, "OK")), hk, nil
}

func (m *Monitor) run(ctx context.Context, s *Server, interval int) error {
	cl, err := dial(ctx, s, func(k string) {
		s.HostKey = k
		m.onKey(s.ID, k)
	})
	if err != nil {
		return err
	}
	defer cl.Close()
	go func() { <-ctx.Done(); cl.Close() }()

	sess, err := cl.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()
	out, err := sess.StdoutPipe()
	if err != nil {
		return err
	}
	script := strings.ReplaceAll(remoteScript, "__I__", strconv.Itoa(interval))
	script = strings.ReplaceAll(script, "__IF__", safeIface(s.Iface))
	if err := sess.Start("exec bash -c " + shellQuote(script)); err != nil {
		return err
	}

	// watchdog: if no sample arrives for a while, drop the connection
	alive := make(chan struct{}, 1)
	go func() {
		limit := time.Duration(interval)*3*time.Second + 15*time.Second
		t := time.NewTimer(limit)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-alive:
				if !t.Stop() {
					select {
					case <-t.C:
					default:
					}
				}
				t.Reset(limit)
			case <-t.C:
				cl.Close()
				return
			}
		}
	}()

	var (
		prevTot, prevIdle uint64
		prevRx, prevTx    uint64
		prevT             time.Time
		have, online      bool
		iface             string
	)
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "IF ") {
			iface = strings.TrimSpace(line[3:])
			continue
		}
		f := strings.Fields(line)
		if len(f) != 12 {
			continue
		}
		var v [12]uint64
		for i := range f {
			v[i], _ = strconv.ParseUint(f[i], 10, 64)
		}
		now := time.Now()
		st := Stat{ID: s.ID, TS: now.UnixMilli(), Iface: iface}
		var tot uint64
		for _, x := range v[:8] {
			tot += x
		}
		idle := v[3] + v[4]
		first := !have
		if v[8] > 0 {
			used := v[8] - v[9]
			st.MemUsed, st.MemTotal = used*1024, v[8]*1024
			st.RAM = 100 * float64(used) / float64(v[8])
		}
		if have {
			if tot > prevTot {
				st.CPU = 100 * (1 - float64(idle-prevIdle)/float64(tot-prevTot))
				if st.CPU < 0 {
					st.CPU = 0
				}
			}
			if dt := now.Sub(prevT).Seconds(); dt > 0 && v[10] >= prevRx && v[11] >= prevTx {
				st.Rx = float64(v[10]-prevRx) / dt
				st.Tx = float64(v[11]-prevTx) / dt
			}
		}
		prevTot, prevIdle, prevRx, prevTx, prevT, have = tot, idle, v[10], v[11], now, true
		select {
		case alive <- struct{}{}:
		default:
		}
		if !online {
			online = true
			m.status(Status{ID: s.ID, State: "online"})
		}
		if !first {
			m.emitS(st)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return errors.New("connection closed")
}

func safeIface(n string) string {
	var b strings.Builder
	for _, r := range n {
		if r == '.' || r == '_' || r == '-' || r == '@' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
