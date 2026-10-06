package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"vegasyncor/internal/api"
	"vegasyncor/internal/config"
	"vegasyncor/internal/i18n"
	"vegasyncor/internal/paths"
	"vegasyncor/internal/secrets"
	"vegasyncor/internal/transfer"
)

var stdin = bufio.NewReader(os.Stdin)

// readSecret reads a passphrase without echo (or a plain line when stdin is not a terminal).
func readSecret(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		return string(b), err
	}
	line, err := stdin.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func confirm(prompt string) bool {
	fmt.Fprint(os.Stderr, prompt)
	line, _ := stdin.ReadString('\n')
	a := strings.ToLower(strings.TrimSpace(line))
	return a == "y" || a == "yes" || a == "s" || a == "si" || a == "sì"
}

// defaultExportName is the suggested name of an export file.
func defaultExportName() string {
	host, _ := os.Hostname()
	if host == "" {
		host = "server"
	}
	return "vegasyncor-" + host + "-" + time.Now().Format("20060102-1504") + transfer.Extension
}

func exportConfig(args []string) error {
	file := defaultExportName()
	if len(args) > 0 {
		file = args[0]
	}
	if _, err := os.Stat(file); err == nil {
		return errors.New(Tf("%s already exists: choose another name", file))
	}
	fmt.Fprintln(os.Stderr, T("The export file contains the passwords of the connections: they are encrypted with the password you choose now, which will be asked when importing."))
	pass, err := readSecret(T("Password for the export file: "))
	if err != nil {
		return err
	}
	if err := transfer.CheckPassphrase(pass); err != nil {
		return err
	}
	again, err := readSecret(T("Repeat the password: "))
	if err != nil {
		return err
	}
	if again != pass {
		return errors.New(T("the passwords do not match"))
	}

	data, err := api.NewClient(paths.Socket()).Export(pass)
	if errors.Is(err, api.ErrNoDaemon) {
		data, err = exportOffline(pass)
	}
	if err != nil {
		return err
	}
	if err := writeNew(file, data); err != nil {
		return err
	}
	fmt.Println(Tf("Configuration exported to %s", file))
	return nil
}

// exportOffline reads the configuration directly when the service is not running.
func exportOffline(pass string) ([]byte, error) {
	cfg, err := config.Load(paths.ConfigFile())
	if err != nil {
		return nil, err
	}
	box, err := secrets.LoadOrCreate(paths.KeyFile())
	if err != nil {
		return nil, err
	}
	p, err := transfer.BuildPayload(cfg, box)
	if err != nil {
		return nil, err
	}
	host, _ := os.Hostname()
	return transfer.Encrypt(p, pass, host, version)
}

func writeNew(file string, data []byte) error {
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

func importConfig(args []string) error {
	var file string
	yes := false
	for _, a := range args {
		switch a {
		case "-y", "--yes":
			yes = true
		default:
			file = a
		}
	}
	if file == "" {
		return errors.New(T("usage: vegasyncor import <file> [--yes]"))
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	h, err := transfer.ReadHeader(data)
	if err != nil {
		return err
	}
	fmt.Println(Tf("Export of %s, made on %s.", h.Host, h.Created.Local().Format(i18n.DateTimeLayout())))
	if !yes && !confirm(T("The import replaces ALL connections, syncs and settings of this server. Continue? [y/N] ")) {
		return errors.New(T("import cancelled"))
	}
	pass, err := readSecret(T("Password of the export file: "))
	if err != nil {
		return err
	}

	res, err := api.NewClient(paths.Socket()).Import(data, pass)
	offline := errors.Is(err, api.ErrNoDaemon)
	if offline {
		res, err = importOffline(data, pass)
	}
	if err != nil {
		return err
	}
	fmt.Println(res.Summary())
	if offline {
		fmt.Println(T("The service is not running: the configuration was updated directly and is used at the next start."))
	}
	return nil
}

func importOffline(data []byte, pass string) (*api.ImportResponse, error) {
	p, h, err := transfer.Decrypt(data, pass)
	if err != nil {
		return nil, err
	}
	cur, err := config.Load(paths.ConfigFile())
	if err != nil {
		return nil, err
	}
	box, err := secrets.LoadOrCreate(paths.KeyFile())
	if err != nil {
		return nil, err
	}
	next, res, err := transfer.BuildConfig(p, cur, box)
	if err != nil {
		return nil, err
	}
	if err := next.Save(paths.ConfigFile()); err != nil {
		return nil, err
	}
	return &api.ImportResponse{Result: res, Host: h.Host, Created: h.Created}, nil
}
