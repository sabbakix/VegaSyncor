package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Sign-in to OneDrive without a browser on the server. "rclone authorize" listens
// on 127.0.0.1:53682 of the server; its /auth page redirects to the Microsoft
// sign-in, whose address is shown to the user. After signing in on any device the
// browser is sent to http://localhost:53682/?code=…, which fails there (it is not
// the server): the user pastes that address and the service forwards it to the
// listener. rclone then exchanges the code for the token (with its own app ID and
// secret, never handled here) and prints it.

// ListenAddr is where "rclone authorize" listens.
const ListenAddr = "127.0.0.1:53682"

// Auth is a sign-in in progress.
type Auth struct {
	cmd  *exec.Cmd
	out  *lockedBuffer
	done chan struct{} // closed when rclone exits
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

var authLinkRe = regexp.MustCompile(`http://127\.0\.0\.1:53682/auth\?state=[A-Za-z0-9_-]+`)

// noRedirect is an HTTP client that returns redirects instead of following them.
var noRedirect = &http.Client{
	Timeout:       15 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// StartAuth starts a sign-in and returns the Microsoft address to open.
func StartAuth(tmpDir string) (*Auth, string, error) {
	if err := Available(); err != nil {
		return nil, "", err
	}
	a := &Auth{out: &lockedBuffer{}, done: make(chan struct{})}
	a.cmd = exec.Command(Binary, "authorize", "onedrive", "--auth-no-open-browser")
	a.cmd.Env = append(os.Environ(), "LC_ALL=C", "RCLONE_CONFIG="+tmpDir+"/rclone-auth.conf")
	a.cmd.Stdout, a.cmd.Stderr = a.out, a.out
	if err := a.cmd.Start(); err != nil {
		return nil, "", err
	}
	go func() {
		a.cmd.Wait()
		close(a.done)
	}()

	deadline := time.Now().Add(15 * time.Second)
	var local string
	for local == "" {
		select {
		case <-a.done:
			return nil, "", fmt.Errorf("rclone authorize: %s", lastLine(a.out.String()))
		case <-time.After(100 * time.Millisecond):
		}
		local = authLinkRe.FindString(a.out.String())
		if local == "" && time.Now().After(deadline) {
			a.Cancel()
			return nil, "", errors.New("rclone authorize: no sign-in link")
		}
	}
	resp, err := noRedirect.Get(local)
	if err != nil {
		a.Cancel()
		return nil, "", err
	}
	resp.Body.Close()
	link := resp.Header.Get("Location")
	if !strings.HasPrefix(link, "https://") {
		a.Cancel()
		return nil, "", fmt.Errorf("rclone authorize: unexpected answer %s", resp.Status)
	}
	return a, link, nil
}

// Cancel stops the sign-in.
func (a *Auth) Cancel() {
	if a != nil && a.cmd.Process != nil {
		a.cmd.Process.Kill()
	}
}

func (a *Auth) exited() bool {
	select {
	case <-a.done:
		return true
	default:
		return false
	}
}

// Finish completes the sign-in with the address the browser landed on (or just
// its "?code=…&state=…" part) and returns the token (JSON).
func (a *Auth) Finish(ctx context.Context, pasted string) (string, error) {
	// the browser may have reached the listener by itself (browser on the same
	// machine, or WSL forwarding localhost from Windows): rclone already has the
	// token, nothing needs to be pasted
	if a.exited() {
		return parseToken(a.out.String())
	}
	if strings.TrimSpace(pasted) == "" {
		return "", errors.New(T("paste the address shown by the browser after the sign-in"))
	}
	q, err := callbackQuery(pasted)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", "http://"+ListenAddr+"/?"+q, nil)
	if err != nil {
		return "", err
	}
	resp, err := noRedirect.Do(req)
	if err != nil {
		// the listener is gone: rclone may be finishing a sign-in received directly
		select {
		case <-a.done:
			return parseToken(a.out.String())
		case <-time.After(30 * time.Second):
			return "", errors.New(T("the sign-in is no longer active: start it again"))
		}
	}
	page, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	resp.Body.Close()
	if strings.Contains(string(page), "Failure") || strings.Contains(string(page), "doesn&#39;t match") {
		a.Cancel()
		return "", errors.New(T("this address belongs to another sign-in: start it again and use the new link"))
	}
	select {
	case <-a.done:
	case <-time.After(60 * time.Second):
		a.Cancel()
		return "", errors.New(T("no answer from Microsoft: try again"))
	case <-ctx.Done():
		a.Cancel()
		return "", ctx.Err()
	}
	return parseToken(a.out.String())
}

// callbackQuery extracts code and state from what the user pasted.
func callbackQuery(pasted string) (string, error) {
	pasted = strings.TrimSpace(pasted)
	if i := strings.Index(pasted, "?"); i >= 0 {
		pasted = pasted[i+1:]
	}
	v, err := url.ParseQuery(pasted)
	if err == nil && v.Get("error") != "" {
		desc := v.Get("error_description")
		if desc == "" {
			desc = v.Get("error")
		}
		return "", errors.New(T("sign-in refused by Microsoft:") + " " + desc)
	}
	if err != nil || v.Get("code") == "" || v.Get("state") == "" {
		return "", errors.New(T("paste the whole address shown by the browser after the sign-in (it starts with http://localhost:53682/?code=)"))
	}
	return url.Values{"code": {v.Get("code")}, "state": {v.Get("state")}}.Encode(), nil
}

// parseToken extracts the token printed by "rclone authorize".
func parseToken(out string) (string, error) {
	const start, end = "--->", "<---End paste"
	i, j := strings.Index(out, start), strings.Index(out, end)
	if i < 0 || j < i {
		msg := lastLine(out)
		if k := strings.Index(msg, "Fatal error: "); k >= 0 {
			msg = msg[k+len("Fatal error: "):]
		}
		return "", fmt.Errorf("%s %s", T("sign-in failed:"), msg)
	}
	tok := strings.TrimSpace(out[i+len(start) : j])
	var t struct {
		AccessToken string `json:"access_token"`
	}
	if json.Unmarshal([]byte(tok), &t) != nil || t.AccessToken == "" {
		return "", errors.New(T("sign-in failed: invalid token"))
	}
	return tok, nil
}

// GraphURL is the Microsoft Graph endpoint of the default drive of the user.
var GraphURL = "https://graph.microsoft.com/v1.0/me/drive"

// Drive describes the OneDrive of the signed-in user.
type Drive struct {
	ID    string `json:"drive_id"`
	Type  string `json:"drive_type"`
	Owner string `json:"account"`
}

// DriveInfo asks Microsoft Graph which drive the token gives access to.
func DriveInfo(ctx context.Context, token string) (Drive, error) {
	var d Drive
	var t struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal([]byte(token), &t); err != nil || t.AccessToken == "" {
		return d, errors.New(T("sign-in failed: invalid token"))
	}
	req, err := http.NewRequestWithContext(ctx, "GET", GraphURL, nil)
	if err != nil {
		return d, err
	}
	req.Header.Set("Authorization", "Bearer "+t.AccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return d, err
	}
	defer resp.Body.Close()
	var body struct {
		ID        string `json:"id"`
		DriveType string `json:"driveType"`
		Owner     struct {
			User struct {
				DisplayName string `json:"displayName"`
			} `json:"user"`
		} `json:"owner"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return d, fmt.Errorf("Microsoft Graph: %s", resp.Status)
	}
	if resp.StatusCode != 200 || body.ID == "" {
		msg := body.Error.Message
		if msg == "" {
			msg = resp.Status
		}
		return d, fmt.Errorf("Microsoft Graph: %s", msg)
	}
	return Drive{ID: body.ID, Type: body.DriveType, Owner: body.Owner.User.DisplayName}, nil
}
