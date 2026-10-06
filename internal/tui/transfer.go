package tui

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"vegasyncor/internal/api"
	"vegasyncor/internal/i18n"
	"vegasyncor/internal/transfer"
)

// Export and import of the whole configuration (Connections tab, keys E and I).
// The TUI reads and writes the file; the service encrypts and decrypts.

type exportMsg struct {
	file string
	err  error
}

type importMsg struct {
	res *api.ImportResponse
	err error
}

func defaultExportPath() string {
	host, _ := os.Hostname()
	if host == "" {
		host = "server"
	}
	name := "vegasyncor-" + host + "-" + time.Now().Format("20060102-1504") + transfer.Extension
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, name)
	}
	return name
}

func expandPath(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

func newExportForm() *form {
	fm := &form{Title: T("Export configuration"), Fields: []*field{
		section(T("File")),
		newText("file", T("Export file"), defaultExportPath(), "/root/vegasyncor.vsconf").
			withHelp(T("connections with their passwords, syncs and settings; copy this file to the new server and import it there")),
		section(T("Protection")),
		newPassword("pass", T("Password"), Tf("at least %d characters", transfer.MinPassphrase)).
			withHelp(T("encrypts the file (AES-256): it will be asked when importing; without it the file cannot be opened")),
		newPassword("pass2", T("Repeat password"), ""),
	}}
	fm.init()
	return fm
}

func newImportForm() *form {
	fm := &form{Title: T("Import configuration"), Fields: []*field{
		section(T("File")),
		newText("file", T("Export file"), "", "/root/vegasyncor-server.vsconf").
			withHelp(T("file made with Export on the old server; it replaces ALL connections, syncs and settings of this server")),
		newPassword("pass", T("Password"), "").
			withHelp(T("the password chosen when the file was exported")),
	}}
	fm.init()
	return fm
}

func (m *Model) saveTransferForm() tea.Cmd {
	fm, c := m.form, m.client
	file := expandPath(fm.val("file"))
	pass := fm.get("pass").Input.Value()
	if file == "" {
		fm.Err = T("enter the file name")
		return nil
	}
	switch m.formKind {
	case "export":
		if err := transfer.CheckPassphrase(pass); err != nil {
			fm.Err = err.Error()
			return nil
		}
		if pass != fm.get("pass2").Input.Value() {
			fm.Err = T("the passwords do not match")
			return nil
		}
		if _, err := os.Stat(file); err == nil {
			fm.Err = Tf("%s already exists: choose another name", file)
			return nil
		}
		m.saving = true
		return func() tea.Msg {
			data, err := c.Export(pass)
			if err == nil {
				err = writeNewFile(file, data)
			}
			return exportMsg{file: file, err: err}
		}
	case "import":
		data, err := os.ReadFile(file)
		if err != nil {
			fm.Err = err.Error()
			return nil
		}
		h, err := transfer.ReadHeader(data)
		if err != nil {
			fm.Err = err.Error()
			return nil
		}
		if pass == "" {
			fm.Err = T("enter the password")
			return nil
		}
		m.confirm = &confirmBox{
			text: Tf("Import the configuration of %s exported on %s?", h.Host, h.Created.Local().Format(i18n.DateTimeLayout())) +
				"\n" + T("ALL connections, syncs and settings of this server are replaced."),
			action: func() tea.Msg {
				res, err := c.Import(data, pass)
				return importMsg{res: res, err: err}
			},
		}
	}
	return nil
}

func writeNewFile(file string, data []byte) error {
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(file)
		return err
	}
	return f.Close()
}

func (m *Model) transferDone(msg tea.Msg) tea.Cmd {
	m.saving = false
	var err error
	switch msg := msg.(type) {
	case exportMsg:
		if err = msg.err; err == nil {
			m.form = nil
			m.info = &infoBox{title: T("Configuration exported"), body: Tf("File: %s", msg.file) + "\n\n" +
				T("Copy it to the new server, install VegaSyncor there and use Import (key I in the Connections tab, or: vegasyncor import <file>). Keep the file and its password safe.")}
			return nil
		}
	case importMsg:
		if err = msg.err; err == nil {
			m.form = nil
			m.info = &infoBox{title: T("Configuration imported"), body: msg.res.Summary()}
			return tea.Batch(m.fetchStatus(), m.fetchFirewall())
		}
	}
	if m.form != nil {
		m.form.Err = err.Error()
	} else {
		m.setFlash(err.Error(), true)
	}
	return nil
}
