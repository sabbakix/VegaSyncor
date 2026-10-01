// Package api definisce i messaggi scambiati tra servizio e TUI
// e un client HTTP su socket Unix.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"vegasyncor/internal/config"
	"vegasyncor/internal/syncer"
)

const (
	StatusRunning   = "running"
	StatusOK        = "ok"
	StatusWarning   = "warning"
	StatusError     = "error"
	StatusCancelled = "cancelled"
	StatusSkipped   = "skipped"
)

// Run è un'esecuzione di un job (in corso o conclusa).
type Run struct {
	ID       string           `json:"id"`
	JobID    string           `json:"job_id"`
	JobName  string           `json:"job_name"`
	Trigger  string           `json:"trigger"` // "pianificato" | "manuale"
	DryRun   bool             `json:"dry_run"`
	Start    time.Time        `json:"start"`
	End      time.Time        `json:"end,omitempty"`
	Status   string           `json:"status"`
	Message  string           `json:"message,omitempty"`
	Stats    syncer.Stats     `json:"stats"`
	Progress *syncer.Progress `json:"progress,omitempty"`
	Phase    string           `json:"phase,omitempty"`
}

func (r *Run) Duration() time.Duration {
	if r.End.IsZero() {
		return time.Since(r.Start)
	}
	return r.End.Sub(r.Start)
}

// ConnectionView è una connessione senza la password.
type ConnectionView struct {
	config.Connection
	HasPassword bool `json:"has_password"`
}

// ConnectionInput è usato per creare/modificare una connessione.
// Password vuota in modifica = mantieni quella esistente.
type ConnectionInput struct {
	config.Connection
	Password string `json:"password,omitempty"`
}

type JobStatus struct {
	Job     config.Job `json:"job"`
	Source  string     `json:"source"`
	Dest    string     `json:"dest"`
	Current *Run       `json:"current,omitempty"`
	Last    *Run       `json:"last,omitempty"`
	Next    time.Time  `json:"next,omitempty"`
}

type Status struct {
	Version     string           `json:"version"`
	Started     time.Time        `json:"started"`
	Hostname    string           `json:"hostname"`
	MaxParallel int              `json:"max_parallel"`
	Jobs        []JobStatus      `json:"jobs"`
	Connections []ConnectionView `json:"connections"`
	Warnings    []string         `json:"warnings,omitempty"`
}

type BrowseRequest struct {
	Location config.Location `json:"location"`
}

type BrowseResponse struct {
	Path string   `json:"path"`
	Dirs []string `json:"dirs"`
}

type TestResult struct {
	OK      bool     `json:"ok"`
	Message string   `json:"message"`
	Shares  []string `json:"shares,omitempty"`
}

type errorBody struct {
	Error string `json:"error"`
}

// Client parla con il servizio tramite socket Unix.
type Client struct {
	http *http.Client
}

func NewClient(socket string) *Client {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}
	return &Client{http: &http.Client{Transport: tr, Timeout: 90 * time.Second}}
}

// ErrNoDaemon indica che il servizio non è raggiungibile.
var ErrNoDaemon = errors.New("servizio non raggiungibile")

func (c *Client) do(method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, "http://vegasyncor"+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		var ne *net.OpError
		if errors.As(err, &ne) {
			if errors.Is(err, os.ErrPermission) {
				return fmt.Errorf("%w: permesso negato sul socket (usare sudo o aggiungere l'utente al gruppo vegasyncor)", ErrNoDaemon)
			}
			return fmt.Errorf("%w (systemctl status vegasyncor)", ErrNoDaemon)
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e errorBody
		if json.NewDecoder(resp.Body).Decode(&e) == nil && e.Error != "" {
			return errors.New(e.Error)
		}
		return fmt.Errorf("errore %s", resp.Status)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (c *Client) Status() (*Status, error) {
	var s Status
	return &s, c.do("GET", "/api/status", nil, &s)
}

func (c *Client) SaveJob(j config.Job) (*config.Job, error) {
	var out config.Job
	if j.ID == "" {
		return &out, c.do("POST", "/api/jobs", j, &out)
	}
	return &out, c.do("PUT", "/api/jobs/"+url.PathEscape(j.ID), j, &out)
}

func (c *Client) DeleteJob(id string) error {
	return c.do("DELETE", "/api/jobs/"+url.PathEscape(id), nil, nil)
}

func (c *Client) SetJobEnabled(id string, enabled bool) error {
	return c.do("POST", "/api/jobs/"+url.PathEscape(id)+"/enabled", map[string]bool{"enabled": enabled}, nil)
}

func (c *Client) RunJob(id string, dry bool) error {
	q := ""
	if dry {
		q = "?dry=1"
	}
	return c.do("POST", "/api/jobs/"+url.PathEscape(id)+"/run"+q, nil, nil)
}

func (c *Client) CancelJob(id string) error {
	return c.do("POST", "/api/jobs/"+url.PathEscape(id)+"/cancel", nil, nil)
}

func (c *Client) SaveConnection(in ConnectionInput) (*ConnectionView, error) {
	var out ConnectionView
	if in.ID == "" {
		return &out, c.do("POST", "/api/connections", in, &out)
	}
	return &out, c.do("PUT", "/api/connections/"+url.PathEscape(in.ID), in, &out)
}

func (c *Client) DeleteConnection(id string) error {
	return c.do("DELETE", "/api/connections/"+url.PathEscape(id), nil, nil)
}

func (c *Client) TestConnection(id string) (*TestResult, error) {
	var out TestResult
	return &out, c.do("POST", "/api/connections/"+url.PathEscape(id)+"/test", nil, &out)
}

func (c *Client) Browse(loc config.Location) (*BrowseResponse, error) {
	var out BrowseResponse
	return &out, c.do("POST", "/api/browse", BrowseRequest{Location: loc}, &out)
}

func (c *Client) History(jobID string, limit int) ([]Run, error) {
	var out []Run
	q := url.Values{}
	if jobID != "" {
		q.Set("job", jobID)
	}
	q.Set("limit", fmt.Sprint(limit))
	return out, c.do("GET", "/api/history?"+q.Encode(), nil, &out)
}

func (c *Client) RunLog(runID string, maxBytes int) (string, error) {
	var out struct {
		Log string `json:"log"`
	}
	err := c.do("GET", fmt.Sprintf("/api/runs/%s/log?max=%d", url.PathEscape(runID), maxBytes), nil, &out)
	return out.Log, err
}

func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
