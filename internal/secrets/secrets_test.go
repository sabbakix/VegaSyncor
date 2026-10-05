package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "k")
	b, err := LoadOrCreate(p)
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Errorf("key permissions %o", st.Mode().Perm())
	}
	enc, err := b.Encrypt("P@ss=word,ù", "c1")
	if err != nil {
		t.Fatal(err)
	}
	b2, err := LoadOrCreate(p)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := b2.Decrypt(enc, "c1"); err != nil || got != "P@ss=word,ù" {
		t.Fatalf("decrypt: %q %v", got, err)
	}
	if _, err := b2.Decrypt(enc, "c2"); err == nil {
		t.Error("decrypted with a different context")
	}
	os.Chmod(p, 0o644)
	if _, err := LoadOrCreate(p); err == nil {
		t.Error("key with loose permissions accepted")
	}
}
