package daemon

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"vegasyncor/internal/api"
	"vegasyncor/internal/transfer"
)

// The service only encrypts and decrypts: the client reads and writes the file,
// so nothing is written as root to a path chosen through the socket.

func (d *Daemon) handleExport(w http.ResponseWriter, r *http.Request) {
	var in api.ExportRequest
	if err := decode(r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	if err := transfer.CheckPassphrase(in.Passphrase); err != nil {
		fail(w, 400, err)
		return
	}
	d.mu.Lock()
	p, err := transfer.BuildPayload(d.cfg, d.box)
	d.mu.Unlock()
	if err != nil {
		fail(w, 500, err)
		return
	}
	host, _ := os.Hostname()
	data, err := transfer.Encrypt(p, in.Passphrase, host, d.Version)
	if err != nil {
		fail(w, 500, err)
		return
	}
	slog.Info("configuration exported", "connections", len(p.Config.Connections), "jobs", len(p.Config.Jobs))
	reply(w, api.ExportResponse{Data: data})
}

func (d *Daemon) handleImport(w http.ResponseWriter, r *http.Request) {
	var in api.ImportRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 32<<20)).Decode(&in); err != nil {
		fail(w, 400, err)
		return
	}
	p, h, err := transfer.Decrypt(in.Data, in.Passphrase)
	if err != nil {
		fail(w, 400, err)
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.running) > 0 {
		fail(w, 409, errors.New(T("some syncs are running: wait for them to finish or stop them before importing")))
		return
	}
	next, res, err := transfer.BuildConfig(p, d.cfg, d.box)
	if err != nil {
		fail(w, 400, err)
		return
	}
	prev := d.cfg
	d.cfg = next
	if err := d.saveConfigLocked(prev); err != nil {
		fail(w, 500, err)
		return
	}
	d.next = map[string]time.Time{}
	if cap(d.sem) != next.MaxParallel {
		d.sem = make(chan struct{}, next.MaxParallel) // nothing is running
	}
	setLanguage(next.Language)
	slog.Info("configuration imported", "from", h.Host, "created", h.Created,
		"connections", res.Connections, "jobs", res.Jobs)
	go d.fwRefresh(true) // the connection hosts changed
	reply(w, api.ImportResponse{Result: res, Host: h.Host, Created: h.Created})
}
