// Package api defines the messages exchanged between the service and the TUI,
// and an HTTP client over a Unix socket.
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

// Values of Run.Trigger.
const (
	TriggerScheduled = "scheduled"
	TriggerManual    = "manual"
)

// TriggerLabel returns the translated label of a trigger (also for history
// entries written by older versions in Italian).
func TriggerLabel(t string) string {
	switch t {
	case TriggerScheduled, "pianificato":
		return T("scheduled")
	case TriggerManual, "manuale":
		return T("manual")
	}
	return t
}

// Run is a job run (in progress or finished).
type Run struct {
	ID       string           `json:"id"`
	JobID    string           `json:"job_id"`
	JobName  string           `json:"job_name"`
	Trigger  string           `json:"trigger"` // TriggerScheduled | TriggerManual
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

// ConnectionView is a connection without the password.
type ConnectionView struct {
	config.Connection
	HasPassword bool `json:"has_password"`
}

// ConnectionInput is used to create/edit a connection.
// An empty password when editing keeps the existing one.
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
	// server time (with its time zone): schedules follow this clock
	ServerTime time.Time `json:"server_time"`
	ZoneAbbr   string    `json:"zone_abbr,omitempty"` // e.g. CEST
	ZoneName   string    `json:"zone_name,omitempty"` // e.g. Europe/Rome
	// Language of the service messages ("en", "it").
	Language string `json:"language,omitempty"`
}

// Settings are the global settings that can be changed from the TUI.
type Settings struct {
	Language string `json:"language"`
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

// Client talks to the service through the Unix socket.
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

// ErrNoDaemon reports that the service cannot be reached (use errors.Is).
var ErrNoDaemon = errors.New("service unreachable")

// noDaemonError carries a translated message and unwraps to ErrNoDaemon.
type noDaemonError struct{ msg string }

func (e *noDaemonError) Error() string { return e.msg }
func (e *noDaemonError) Unwrap() error { return ErrNoDaemon }

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
				return &noDaemonError{T("service unreachable") + ": " +
					T("permission denied on the socket (use sudo or add the user to the vegasyncor group)")}
			}
			return &noDaemonError{T("service unreachable") + " (systemctl status vegasyncor)"}
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e errorBody
		if json.NewDecoder(resp.Body).Decode(&e) == nil && e.Error != "" {
			return errors.New(e.Error)
		}
		return errors.New(Tf("error %s", resp.Status))
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

func (c *Client) SetLanguage(code string) error {
	return c.do("POST", "/api/settings", Settings{Language: code}, nil)
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
