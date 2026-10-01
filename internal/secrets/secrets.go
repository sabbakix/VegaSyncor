// Package secrets cifra le password delle connessioni con AES-256-GCM.
// La chiave master (32 byte casuali) è salvata in un file leggibile solo da root.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const prefix = "v1:"

type Box struct {
	aead cipher.AEAD
}

// LoadOrCreate legge la chiave master da path; se non esiste ne genera una nuova.
func LoadOrCreate(path string) (*Box, error) {
	key, err := readKey(path)
	if errors.Is(err, os.ErrNotExist) {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		enc := base64.StdEncoding.EncodeToString(key) + "\n"
		if err := os.WriteFile(path, []byte(enc), 0o600); err != nil {
			return nil, fmt.Errorf("scrittura chiave master: %w", err)
		}
	} else if err != nil {
		return nil, err
	}
	return New(key)
}

func readKey(path string) ([]byte, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("la chiave master %s è accessibile ad altri utenti (permessi %o): usare chmod 600", path, st.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("chiave master %s non valida", path)
	}
	return key, nil
}

func New(key []byte) (*Box, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Encrypt restituisce il testo cifrato in formato "v1:<base64(nonce|ciphertext)>".
// aad lega il segreto al suo contesto (es. ID connessione).
func (b *Box) Encrypt(plain, aad string) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := b.aead.Seal(nonce, nonce, []byte(plain), []byte(aad))
	return prefix + base64.StdEncoding.EncodeToString(out), nil
}

func (b *Box) Decrypt(enc, aad string) (string, error) {
	if enc == "" {
		return "", nil
	}
	if !strings.HasPrefix(enc, prefix) {
		return "", errors.New("formato segreto sconosciuto")
	}
	raw, err := base64.StdEncoding.DecodeString(enc[len(prefix):])
	if err != nil {
		return "", err
	}
	ns := b.aead.NonceSize()
	if len(raw) < ns {
		return "", errors.New("segreto troncato")
	}
	plain, err := b.aead.Open(nil, raw[:ns], raw[ns:], []byte(aad))
	if err != nil {
		return "", errors.New("impossibile decifrare la password (chiave master cambiata?)")
	}
	return string(plain), nil
}
