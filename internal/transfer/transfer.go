// Package transfer exports and imports the whole VegaSyncor configuration
// (connections with their passwords, syncs, settings) as a file encrypted with a
// passphrase, to move the service to another server.
//
// The file is a JSON envelope; the payload is encrypted with AES-256-GCM using a
// key derived from the passphrase with Argon2id. The envelope header is used as
// additional authenticated data, so it cannot be altered either. Connection
// passwords travel in clear text only inside the encrypted payload: the master key
// of the server is never exported.
package transfer

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"time"

	"golang.org/x/crypto/argon2"

	"vegasyncor/internal/config"
)

const (
	Format  = "vegasyncor-export"
	Version = 1
	// Extension suggested for export files.
	Extension = ".vsconf"
	// MinPassphrase is the minimum length of the passphrase.
	MinPassphrase = 8
)

// KDF parameters (Argon2id), stored in the file so they can change in the future.
type KDF struct {
	Name    string `json:"name"` // "argon2id"
	Salt    []byte `json:"salt"`
	Time    uint32 `json:"time"`
	Memory  uint32 `json:"memory_kib"`
	Threads uint8  `json:"threads"`
}

// Header is the clear-text part of the file.
type Header struct {
	Format  string    `json:"format"`
	Version int       `json:"version"`
	Created time.Time `json:"created"`
	Host    string    `json:"host,omitempty"`
	App     string    `json:"app_version,omitempty"`
	KDF     KDF       `json:"kdf"`
	Cipher  string    `json:"cipher"` // "aes-256-gcm"
}

type envelope struct {
	Header
	Nonce []byte `json:"nonce"`
	Data  []byte `json:"data"`
}

// Payload is the encrypted content.
type Payload struct {
	Config config.Config `json:"config"` // PasswordEnc fields are empty
	// Passwords of the connections, by connection ID (clear text inside the payload).
	Passwords map[string]string `json:"passwords"`
}

// ErrPassphrase is returned (use errors.Is) when the passphrase is wrong or the
// file was altered.
var ErrPassphrase = errors.New("wrong password or damaged file")

type passphraseError struct{}

func (passphraseError) Error() string        { return T("wrong password or damaged file") }
func (passphraseError) Is(target error) bool { return target == ErrPassphrase }

// CheckPassphrase validates a new passphrase for an export.
func CheckPassphrase(p string) error {
	if len([]rune(p)) < MinPassphrase {
		return errors.New(Tf("the password must be at least %d characters long", MinPassphrase))
	}
	return nil
}

func deriveKey(pass string, k KDF) []byte {
	return argon2.IDKey([]byte(pass), k.Salt, k.Time, k.Memory, k.Threads, 32)
}

// Encrypt produces the export file for payload, protected by passphrase.
func Encrypt(p Payload, passphrase, host, appVersion string) ([]byte, error) {
	if err := CheckPassphrase(passphrase); err != nil {
		return nil, err
	}
	plain, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	nonce := make([]byte, 12)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	h := Header{
		Format: Format, Version: Version, Created: time.Now().UTC().Truncate(time.Second),
		Host: host, App: appVersion, Cipher: "aes-256-gcm",
		KDF: KDF{Name: "argon2id", Salt: salt, Time: 3, Memory: 64 * 1024, Threads: 4},
	}
	aad, err := json.Marshal(h)
	if err != nil {
		return nil, err
	}
	gcm, err := newGCM(deriveKey(passphrase, h.KDF))
	if err != nil {
		return nil, err
	}
	env := envelope{Header: h, Nonce: nonce, Data: gcm.Seal(nil, nonce, plain, aad)}
	return json.MarshalIndent(env, "", "  ")
}

// ReadHeader returns the clear-text header of an export file (no passphrase
// needed), checking that the file can be imported by this version.
func ReadHeader(data []byte) (*Header, error) {
	env, err := parse(data)
	if err != nil {
		return nil, err
	}
	return &env.Header, nil
}

func parse(data []byte) (*envelope, error) {
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil || env.Format != Format {
		return nil, errors.New(T("this is not a VegaSyncor export file"))
	}
	if env.Version != Version || env.Cipher != "aes-256-gcm" || env.KDF.Name != "argon2id" {
		return nil, errors.New(T("export file made by a newer VegaSyncor version: update this server first"))
	}
	return &env, nil
}

// Decrypt opens an export file with passphrase.
func Decrypt(data []byte, passphrase string) (*Payload, *Header, error) {
	env, err := parse(data)
	if err != nil {
		return nil, nil, err
	}
	k := env.KDF
	if len(k.Salt) < 16 || k.Time < 1 || k.Time > 20 || k.Memory < 8*1024 || k.Memory > 1024*1024 || k.Threads < 1 {
		return nil, nil, errors.New(T("this is not a VegaSyncor export file"))
	}
	aad, err := json.Marshal(env.Header)
	if err != nil {
		return nil, nil, err
	}
	gcm, err := newGCM(deriveKey(passphrase, k))
	if err != nil {
		return nil, nil, err
	}
	if len(env.Nonce) != gcm.NonceSize() {
		return nil, nil, errors.New(T("this is not a VegaSyncor export file"))
	}
	plain, err := gcm.Open(nil, env.Nonce, env.Data, aad)
	if err != nil {
		return nil, nil, passphraseError{}
	}
	var p Payload
	if err := json.Unmarshal(plain, &p); err != nil {
		return nil, nil, err
	}
	return &p, &env.Header, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
