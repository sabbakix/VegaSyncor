package cloud

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vegasyncor/internal/config"
	"vegasyncor/internal/s3test"
)

// The S3 tests run against internal/s3test. To run the generic ones against a real
// service (a bucket for tests only; everything happens in a new vegasyncor-test-*
// folder, removed at the end):
//
//	VEGASYNCOR_TEST_S3_BUCKET=... VEGASYNCOR_TEST_S3_ACCESS_KEY=... VEGASYNCOR_TEST_S3_SECRET_KEY=... \
//	VEGASYNCOR_TEST_S3_ENDPOINT=... VEGASYNCOR_TEST_S3_REGION=... go test ./internal/cloud -run S3

const (
	testKey    = "AKIATESTKEY"
	testSecret = `ab/c+d=e#f;g"h'i j` // everything the configuration file could trip on
	testBucket = "backups"
)

// fastRetries makes the failures of the tests quick; the real values are in rclone.go.
func fastRetries(t *testing.T) {
	t.Helper()
	copyR, checkR, sleep := S3CopyRetries, S3CheckRetries, S3RetriesSleep
	S3CopyRetries, S3CheckRetries, S3RetriesSleep = "1", "1", "0s"
	t.Cleanup(func() { S3CopyRetries, S3CheckRetries, S3RetriesSleep = copyR, checkR, sleep })
}

func newFake(t *testing.T) (*s3test.Server, *S3) {
	t.Helper()
	needRclone(t)
	fastRetries(t)
	srv := s3test.New(testKey, testSecret, testBucket)
	t.Cleanup(srv.Close)
	return srv, &S3{Endpoint: srv.URL, Bucket: testBucket, AccessKey: testKey, SecretKey: testSecret}
}

// s3Target returns the bucket for the generic tests and a folder to work in.
func s3Target(t *testing.T) (*S3, string) {
	t.Helper()
	needRclone(t)
	fastRetries(t)
	bucket := os.Getenv("VEGASYNCOR_TEST_S3_BUCKET")
	if bucket == "" {
		_, s := newFake(t)
		return s, "Backup"
	}
	s := &S3{Endpoint: os.Getenv("VEGASYNCOR_TEST_S3_ENDPOINT"), Region: os.Getenv("VEGASYNCOR_TEST_S3_REGION"), Bucket: bucket,
		AccessKey: os.Getenv("VEGASYNCOR_TEST_S3_ACCESS_KEY"), SecretKey: os.Getenv("VEGASYNCOR_TEST_S3_SECRET_KEY")}
	folder := "vegasyncor-test-" + config.NewID() + "/Backup"
	t.Cleanup(func() {
		c, err := NewConfig(os.TempDir(), Remote{S3: s}, "", nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.Close()
		if _, err := c.run(context.Background(), "purge", c.Abs(strings.Split(folder, "/")[0])); err != nil {
			t.Logf("cleanup of %s: %v", folder, err)
		}
	})
	return s, folder
}

func s3Config(t *testing.T, s *S3, folder string, crypt *Crypt) *Config {
	t.Helper()
	c, err := NewConfig(t.TempDir(), Remote{S3: s}, folder, crypt)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func syncLog(t *testing.T, c *Config, o Options) ([]string, error) {
	t.Helper()
	var log bytes.Buffer
	if o.RunStamp == "" {
		o.RunStamp = "2026-10-30_220000"
	}
	_, err := c.Sync(context.Background(), o, &log, nil)
	t.Log(log.String())
	return lines(log.String()), err
}

func cat(t *testing.T, c *Config, remote string) string {
	t.Helper()
	out, err := c.run(context.Background(), "cat", remote)
	if err != nil {
		t.Fatalf("cat %s: %v", remote, err)
	}
	return string(out)
}

func exists(t *testing.T, c *Config, remote string) bool {
	t.Helper()
	dir, name := filepath.Split(remote)
	out, err := c.run(context.Background(), "lsf", "--files-only", strings.TrimSuffix(dir, "/"))
	if err != nil {
		t.Fatalf("lsf %s: %v", dir, err)
	}
	return has(strings.Split(strings.TrimSpace(string(out)), "\n"), name)
}

// hashTree maps every file of a folder to the hash of its content and its time.
func hashTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		st, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		sum := sha256.Sum256(data)
		out[rel] = hex.EncodeToString(sum[:]) + " " + st.ModTime().Truncate(time.Second).UTC().Format(time.RFC3339)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sameTree(t *testing.T, got, want map[string]string) {
	t.Helper()
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%q: got %q, want %q", k, got[k], v)
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("unexpected file %q", k)
		}
	}
}

// a source with what backups really contain, big files included: over the cutoff
// set here they go as multipart uploads, whose ETag is not the MD5 of the content.
func richSource(t *testing.T) string {
	t.Helper()
	t.Setenv("RCLONE_S3_UPLOAD_CUTOFF", "5M")
	t.Setenv("RCLONE_S3_CHUNK_SIZE", "5M")
	src := t.TempDir()
	big := make([]byte, 12<<20+123)
	if _, err := rand.Read(big); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(src, "big.bin"), string(big))
	write(t, filepath.Join(src, "empty.txt"), "")
	for _, name := range []string{"Contabilità/fattura n°1 (copia).pdf", "a b/c+d & e=f.txt", "per%20cent/x#y.txt",
		"deep/er/and/deeper/file.txt", "UPPER/Case.TXT", "upper/case.txt", "日本語/ファイル.txt", "with'quote/semi;colon.txt"} {
		write(t, filepath.Join(src, name), "content of "+name)
	}
	old := time.Date(2019, 3, 4, 5, 6, 7, 0, time.UTC)
	os.Chtimes(filepath.Join(src, "deep/er/and/deeper/file.txt"), old, old)
	return src
}

// What matters most: what comes back is what went up, bytes and dates.
func TestS3UploadRestoreIdentical(t *testing.T) {
	for _, crypt := range []*Crypt{nil, {Password: "correct horse", Names: false}, {Password: "correct horse", Names: true}} {
		name := "plain"
		if crypt != nil {
			name = fmt.Sprintf("encrypted-names-%v", crypt.Names)
		}
		t.Run(name, func(t *testing.T) {
			s, folder := s3Target(t)
			src := richSource(t)
			c := s3Config(t, s, folder, crypt)
			if ls, err := syncLog(t, c, Options{Src: src, Dst: c.Root, Mode: config.ModeMirrorArchive}); err != nil || len(ls) != 10 {
				t.Fatalf("upload: %v, %v", err, ls)
			}
			restore := t.TempDir()
			if ls, err := syncLog(t, c, Options{Src: c.Root, Dst: restore, Mode: config.ModeMirror}); err != nil || len(ls) != 10 {
				t.Fatalf("restore: %v, %v", err, ls)
			}
			sameTree(t, hashTree(t, restore), hashTree(t, src))
			// a second upload finds nothing to do
			if ls, err := syncLog(t, c, Options{Src: src, Dst: c.Root, Mode: config.ModeMirrorArchive}); err != nil || len(ls) != 0 {
				t.Errorf("second upload: %v, %v", err, ls)
			}
		})
	}
}

func TestS3Modes(t *testing.T) {
	for _, mode := range config.Modes {
		t.Run(mode, func(t *testing.T) {
			s, folder := s3Target(t)
			c := s3Config(t, s, folder, nil)
			first := t.TempDir()
			write(t, filepath.Join(first, "a.txt"), "old")
			write(t, filepath.Join(first, "gone.txt"), "deleted at the source")
			if _, err := syncLog(t, c, Options{Src: first, Dst: c.Root, Mode: config.ModeMirror}); err != nil {
				t.Fatal(err)
			}
			src := t.TempDir()
			write(t, filepath.Join(src, "a.txt"), "new version")
			write(t, filepath.Join(src, "sub", "b.txt"), "b")
			write(t, filepath.Join(src, "Thumbs.db"), "x")
			future := time.Now().Add(time.Hour)
			os.Chtimes(filepath.Join(src, "a.txt"), future, future)
			gone, a := Join(c.Root, "gone.txt"), Join(c.Root, "a.txt")
			arch := Join(c.Root, config.ArchiveDirName+"/2026-10-30_220000")
			opts := Options{Src: src, Dst: c.Root, Mode: mode}

			// the dry run reports the same as the run, and changes nothing
			o := opts
			o.DryRun = true
			ls, err := syncLog(t, c, o)
			if err != nil {
				t.Fatal(err)
			}
			wantDel := mode != config.ModeAdditive
			if !has(ls, ">f+++++++++ sub/b.txt") || !has(ls, ">f.st...... a.txt") || has(ls, "*deleting   gone.txt") != wantDel {
				t.Errorf("dry run lines %v", ls)
			}
			if cat(t, c, a) != "old" || !exists(t, c, gone) {
				t.Fatal("the dry run changed the destination")
			}

			if ls, err = syncLog(t, c, opts); err != nil {
				t.Fatal(err)
			}
			if !has(ls, ">f+++++++++ sub/b.txt") || !has(ls, ">f.st...... a.txt") || has(ls, ">f+++++++++ Thumbs.db") ||
				has(ls, "*deleting   a.txt") || has(ls, "*deleting   gone.txt") != wantDel || len(ls) != map[bool]int{true: 3, false: 2}[wantDel] {
				t.Errorf("lines %v", ls)
			}
			if cat(t, c, a) != "new version" || cat(t, c, Join(c.Root, "sub/b.txt")) != "b" {
				t.Error("files not copied")
			}
			if exists(t, c, gone) == wantDel {
				t.Errorf("gone.txt present = %v in mode %s", !wantDel, mode)
			}
			if mode == config.ModeMirrorArchive {
				if cat(t, c, Join(arch, "gone.txt")) != "deleted at the source" || cat(t, c, Join(arch, "a.txt")) != "old" {
					t.Error("previous versions not archived")
				}
			}

			// a second run changes nothing and leaves the archive alone
			opts.RunStamp = "2026-10-31_220000"
			if ls, err = syncLog(t, c, opts); err != nil || len(ls) != 0 {
				t.Errorf("second run: %v %v", err, ls)
			}
			if mode == config.ModeMirrorArchive {
				if cat(t, c, Join(arch, "a.txt")) != "old" {
					t.Error("archive changed by the mirror")
				}
				removed, err := c.PruneArchive(context.Background(), Join(c.Root, config.ArchiveDirName), 1, time.Date(2026, 11, 5, 0, 0, 0, 0, time.Local))
				if err != nil || len(removed) != 1 || cat(t, c, a) != "new version" {
					t.Errorf("prune: %v %v", removed, err)
				}
			}
		})
	}
}

// Same size and same date, different content: only Compare contents sees it.
func TestS3Checksum(t *testing.T) {
	s, folder := s3Target(t)
	c := s3Config(t, s, folder, nil)
	src := t.TempDir()
	when := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	write(t, filepath.Join(src, "f.txt"), "AAAA")
	os.Chtimes(filepath.Join(src, "f.txt"), when, when)
	if _, err := syncLog(t, c, Options{Src: src, Dst: c.Root, Mode: config.ModeMirror}); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(src, "f.txt"), "BBBB")
	os.Chtimes(filepath.Join(src, "f.txt"), when, when)
	if ls, err := syncLog(t, c, Options{Src: src, Dst: c.Root, Mode: config.ModeMirror}); err != nil || len(ls) != 0 {
		t.Errorf("without checksum: %v %v", err, ls)
	}
	if _, err := syncLog(t, c, Options{Src: src, Dst: c.Root, Mode: config.ModeMirror, Checksum: true}); err != nil {
		t.Fatal(err)
	}
	if got := cat(t, c, Join(c.Root, "f.txt")); got != "BBBB" {
		t.Errorf("with checksum: %q", got)
	}
}

// On S3 a missing folder is an empty listing, not an error: what the service uses
// to refuse restoring from nothing.
func TestS3MissingFolderLooksEmpty(t *testing.T) {
	s, folder := s3Target(t)
	c := s3Config(t, s, folder+"/does-not-exist", nil)
	if err := c.CheckAccess(context.Background(), c.Raw); err != nil {
		t.Errorf("CheckAccess: %v", err)
	}
	if empty, err := c.IsEmpty(context.Background(), c.Raw); err != nil || !empty {
		t.Errorf("IsEmpty = %v, %v", empty, err)
	}
	// only archived files: nothing to restore
	src := t.TempDir()
	write(t, filepath.Join(src, "f"), "1")
	if _, err := syncLog(t, c, Options{Src: src, Dst: Join(c.Root, config.ArchiveDirName+"/2026"), Mode: config.ModeAdditive}); err != nil {
		t.Fatal(err)
	}
	if empty, err := c.IsEmpty(context.Background(), c.Raw); err != nil || !empty {
		t.Errorf("IsEmpty with only the archive = %v, %v", empty, err)
	}
}

func TestS3WrongPassword(t *testing.T) {
	s, folder := s3Target(t)
	src := t.TempDir()
	write(t, filepath.Join(src, "a.txt"), "secret content")
	up := s3Config(t, s, folder, &Crypt{Password: "right one"})
	if _, err := syncLog(t, up, Options{Src: src, Dst: up.Root, Mode: config.ModeMirror}); err != nil {
		t.Fatal(err)
	}
	down := s3Config(t, s, folder, &Crypt{Password: "wrong one"})
	restore := t.TempDir()
	write(t, filepath.Join(restore, "local.txt"), "must survive")
	if _, err := syncLog(t, down, Options{Src: down.Root, Dst: restore, Mode: config.ModeMirror}); err == nil || !strings.Contains(err.Error(), "cannot be decrypted") {
		t.Errorf("err = %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(restore, "local.txt")); string(b) != "must survive" {
		t.Error("the local copy was touched by a failed restore")
	}
}

// --- what only the test server can show ---

// The secret reaches rclone intact (signatures are verified), and another secret is refused.
func TestS3Credentials(t *testing.T) {
	_, s := newFake(t)
	c := s3Config(t, s, "", nil)
	if err := c.CheckAccess(context.Background(), c.Abs("")); err != nil {
		t.Fatalf("right secret: %v", err)
	}
	bad := *s
	bad.SecretKey = "wrong"
	err := s3Config(t, &bad, "", nil).CheckAccess(context.Background(), c.Abs(""))
	if err == nil || !strings.Contains(err.Error(), "SignatureDoesNotMatch") {
		t.Errorf("wrong secret: %v", err)
	}
	bad = *s
	bad.AccessKey = "OTHER"
	err = s3Config(t, &bad, "", nil).CheckAccess(context.Background(), c.Abs(""))
	if err == nil || !strings.Contains(err.Error(), "InvalidAccessKeyId") {
		t.Errorf("wrong key: %v", err)
	}
}

// A mistyped bucket is an error with a readable message, and it is never created.
func TestS3MissingBucket(t *testing.T) {
	srv, s := newFake(t)
	s.Bucket = "typo"
	c := s3Config(t, s, "Backup", nil)
	if err := c.CheckAccess(context.Background(), c.Raw); err == nil || !strings.Contains(err.Error(), "directory not found") {
		t.Errorf("CheckAccess: %v", err)
	}
	src := t.TempDir()
	write(t, filepath.Join(src, "a.txt"), "x")
	_, err := syncLog(t, c, Options{Src: src, Dst: c.Root, Mode: config.ModeMirror})
	if err == nil || !strings.Contains(err.Error(), "NoSuchBucket") || strings.Contains(err.Error(), "status code") {
		t.Errorf("sync: %v", err)
	}
	if err := c.Mkdir(context.Background(), c.Abs("New")); err != nil {
		t.Errorf("mkdir: %v", err)
	}
	if srv.HasBucket("typo") {
		t.Error("the bucket was created")
	}
}

// A failed upload never leads to deletions: the old copy stays until the new one is complete.
func TestS3FailedUploadDeletesNothing(t *testing.T) {
	for _, mode := range []string{config.ModeMirror, config.ModeMirrorArchive} {
		t.Run(mode, func(t *testing.T) {
			srv, s := newFake(t)
			srv.Put(testBucket, "Backup/gone.txt", []byte("the only copy"))
			srv.FailPut = func(key string) bool { return strings.HasSuffix(key, "bad.txt") }
			src := t.TempDir()
			write(t, filepath.Join(src, "good.txt"), "ok")
			write(t, filepath.Join(src, "bad.txt"), "fails")
			c := s3Config(t, s, "Backup", nil)
			var log bytes.Buffer
			res, err := c.Sync(context.Background(), Options{Src: src, Dst: c.Root, Mode: mode, RunStamp: "2026-10-30_220000"}, &log, nil)
			if err == nil && res.Warning == "" {
				t.Fatalf("the failure was not reported\n%s", log.String())
			}
			if srv.Get(testBucket, "Backup/gone.txt") == nil {
				t.Errorf("deleted after a failed upload\n%s", log.String())
			}
			if strings.Contains(log.String(), "*deleting") {
				t.Errorf("deletion reported\n%s", log.String())
			}
			if srv.Get(testBucket, "Backup/bad.txt") != nil {
				t.Error("partial object left")
			}
			// the next run, with the service back, completes the job
			srv.FailPut = nil
			if _, err := c.Sync(context.Background(), Options{Src: src, Dst: c.Root, Mode: mode, RunStamp: "2026-10-31_220000"}, io.Discard, nil); err != nil {
				t.Fatal(err)
			}
			if srv.Get(testBucket, "Backup/bad.txt") == nil || srv.Get(testBucket, "Backup/gone.txt") != nil {
				t.Errorf("second run: %v", srv.Keys(testBucket))
			}
		})
	}
}

// An overwritten file must keep a copy: in the archive before the new one arrives.
func TestS3FailedOverwriteKeepsOldVersion(t *testing.T) {
	srv, s := newFake(t)
	srv.Put(testBucket, "Backup/a.txt", []byte("old"))
	srv.FailPut = func(key string) bool { return key == "Backup/a.txt" }
	src := t.TempDir()
	write(t, filepath.Join(src, "a.txt"), "new version")
	c := s3Config(t, s, "Backup", nil)
	_, err := c.Sync(context.Background(), Options{Src: src, Dst: c.Root, Mode: config.ModeMirrorArchive, RunStamp: "2026-10-30_220000"}, io.Discard, nil)
	if err == nil {
		t.Error("failure not reported")
	}
	cur, arch := srv.Get(testBucket, "Backup/a.txt"), srv.Get(testBucket, "Backup/"+config.ArchiveDirName+"/2026-10-30_220000/a.txt")
	if (cur == nil || string(cur.Data) != "old") && (arch == nil || string(arch.Data) != "old") {
		t.Errorf("old version lost: %v", srv.Keys(testBucket))
	}
}

// A listing that fails is not an empty folder: nothing is deleted on either side.
func TestS3FailedListingDeletesNothing(t *testing.T) {
	srv, s := newFake(t)
	srv.Put(testBucket, "Backup/a.txt", []byte("in the bucket"))
	c := s3Config(t, s, "Backup", nil)
	srv.FailList = true

	restore := t.TempDir()
	write(t, filepath.Join(restore, "local.txt"), "must survive")
	if _, err := c.Sync(context.Background(), Options{Src: c.Root, Dst: restore, Mode: config.ModeMirror}, io.Discard, nil); err == nil {
		t.Error("restore: failure not reported")
	}
	if _, err := os.Stat(filepath.Join(restore, "local.txt")); err != nil {
		t.Error("restore deleted a local file after a failed listing")
	}
	if _, err := c.IsEmpty(context.Background(), c.Raw); err == nil {
		t.Error("a failed listing was taken for an empty folder")
	}

	src := t.TempDir()
	write(t, filepath.Join(src, "other.txt"), "x")
	if _, err := c.Sync(context.Background(), Options{Src: src, Dst: c.Root, Mode: config.ModeMirror}, io.Discard, nil); err == nil {
		t.Error("upload: failure not reported")
	}
	if srv.Get(testBucket, "Backup/a.txt") == nil {
		t.Error("upload deleted a file after a failed listing")
	}
}

// Damage in the bucket is noticed when restoring: for a whole object and for the
// parts of a multipart one, whose hash rclone keeps in the metadata.
func TestS3RestoreDetectsCorruption(t *testing.T) {
	srv, s := newFake(t)
	src := richSource(t)
	c := s3Config(t, s, "Backup", nil)
	if _, err := syncLog(t, c, Options{Src: src, Dst: c.Root, Mode: config.ModeMirror}); err != nil {
		t.Fatal(err)
	}
	big := srv.Get(testBucket, "Backup/big.bin")
	if big.Meta["md5chksum"] == "" || !strings.Contains(big.ETag, "-") {
		t.Fatalf("big.bin was not a multipart upload with its hash: %v %s", big.Meta, big.ETag)
	}
	if srv.PendingUploads() != 0 {
		t.Errorf("%d multipart uploads left open", srv.PendingUploads())
	}
	for _, key := range []string{"Backup/big.bin", "Backup/deep/er/and/deeper/file.txt"} {
		srv.Get(testBucket, key).Data[3] ^= 0xff
		restore := t.TempDir()
		var log bytes.Buffer
		res, err := c.Sync(context.Background(), Options{Src: c.Root, Dst: restore, Mode: config.ModeMirror}, &log, nil)
		if err == nil && res.Warning == "" {
			t.Errorf("%s: damage not reported\n%s", key, log.String())
		}
		if !strings.Contains(log.String(), "corrupted") {
			t.Errorf("%s: not reported as corrupted\n%s", key, log.String())
		}
		if _, err := os.Stat(filepath.Join(restore, strings.TrimPrefix(key, "Backup/"))); err == nil {
			t.Errorf("%s: the damaged file was written", key)
		}
		srv.Get(testBucket, key).Data[3] ^= 0xff
	}
}

// Unchanged files are not sent again, and the listing does not grow with the folders.
func TestS3Requests(t *testing.T) {
	srv, s := newFake(t)
	src := t.TempDir()
	for i := range 20 {
		write(t, filepath.Join(src, fmt.Sprintf("dir%d", i), "f.txt"), "x")
	}
	c := s3Config(t, s, "Backup", nil)
	if _, err := syncLog(t, c, Options{Src: src, Dst: c.Root, Mode: config.ModeMirrorArchive}); err != nil {
		t.Fatal(err)
	}
	before := len(srv.Requests())
	if _, err := syncLog(t, c, Options{Src: src, Dst: c.Root, Mode: config.ModeMirrorArchive}); err != nil {
		t.Fatal(err)
	}
	puts, lists := 0, 0
	for _, r := range srv.Requests()[before:] {
		switch {
		case strings.HasPrefix(r, "PUT ") || strings.HasPrefix(r, "DELETE "):
			puts++
		case strings.HasPrefix(r, "GET "+testBucket+"/?"):
			lists++
		}
	}
	if puts != 0 || lists > 2 {
		t.Errorf("second run: %d writes, %d listings", puts, lists)
	}
}

func TestS3ConfigFile(t *testing.T) {
	needRclone(t)
	s := &S3{Region: "eu-west-1", Bucket: "b", AccessKey: "k", SecretKey: "s3cret"}
	c, err := NewConfig(t.TempDir(), Remote{S3: s}, "Backup/X", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(c.path)
	for _, want := range []string{"type = s3", "provider = AWS", "no_check_bucket = true", "env_auth = false", "region = eu-west-1", "secret_access_key = s3cret"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("missing %q in\n%s", want, raw)
		}
	}
	if strings.Contains(string(raw), "endpoint") {
		t.Errorf("endpoint for AWS:\n%s", raw)
	}
	if c.Root != "od:b/Backup/X" || c.Abs("") != "od:b" || c.Token() != "" {
		t.Errorf("root %q abs %q token %q", c.Root, c.Abs(""), c.Token())
	}
	if st, _ := os.Stat(c.path); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode())
	}
	c.Close()
	if _, err := os.Stat(c.path); err == nil {
		t.Error("configuration with the secret not removed")
	}
	for _, bad := range []S3{{Bucket: "b", AccessKey: "k", SecretKey: "s\nnew_section = 1"}, {Bucket: "b\n", AccessKey: "k", SecretKey: "s"},
		{Bucket: "b", AccessKey: "k"}, {AccessKey: "k", SecretKey: "s"}} {
		if _, err := NewConfig(t.TempDir(), Remote{S3: &bad}, "", nil); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestLastLineSkipsContinuation(t *testing.T) {
	out := "2026/11/04 18:13:51 Failed to sync: NoSuchBucket: The specified bucket does not exist\n\tstatus code: 404, request id: , host id: \n"
	if got := lastLine(out); got != "Failed to sync: NoSuchBucket: The specified bucket does not exist" {
		t.Errorf("lastLine = %q", got)
	}
}

// A mirror works inside its folder: neighbours in the bucket, even with a name
// that starts the same, are not its business.
func TestS3MirrorStaysInItsFolder(t *testing.T) {
	srv, s := newFake(t)
	for _, k := range []string{"Backup2/keep.txt", "Backup.old/keep.txt", "other/keep.txt", "keep.txt", "backup/keep.txt", "Backup/gone.txt"} {
		srv.Put(testBucket, k, []byte("x"))
	}
	src := t.TempDir()
	write(t, filepath.Join(src, "a.txt"), "a")
	c := s3Config(t, s, "Backup", nil)
	if _, err := syncLog(t, c, Options{Src: src, Dst: c.Root, Mode: config.ModeMirror}); err != nil {
		t.Fatal(err)
	}
	want := "Backup.old/keep.txt Backup/a.txt Backup2/keep.txt backup/keep.txt keep.txt other/keep.txt"
	if got := strings.Join(srv.Keys(testBucket), " "); got != want {
		t.Errorf("keys %s", got)
	}
}

// With encryption the archived versions can be read back, and the cleanup finds them.
func TestS3EncryptedArchive(t *testing.T) {
	s, folder := s3Target(t)
	c := s3Config(t, s, folder, &Crypt{Password: "correct horse", Names: true})
	src := t.TempDir()
	write(t, filepath.Join(src, "a.txt"), "version 1")
	write(t, filepath.Join(src, "gone.txt"), "deleted later")
	if _, err := syncLog(t, c, Options{Src: src, Dst: c.Root, Mode: config.ModeMirrorArchive}); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(src, "a.txt"), "version 2 is longer")
	os.Remove(filepath.Join(src, "gone.txt"))
	ls, err := syncLog(t, c, Options{Src: src, Dst: c.Root, Mode: config.ModeMirrorArchive, RunStamp: "2026-10-31_220000"})
	if err != nil || !has(ls, ">f.st...... a.txt") || !has(ls, "*deleting   gone.txt") {
		t.Fatalf("%v %v", err, ls)
	}
	arch := Join(c.Root, config.ArchiveDirName)
	if cat(t, c, Join(arch, "2026-10-31_220000/a.txt")) != "version 1" || cat(t, c, Join(arch, "2026-10-31_220000/gone.txt")) != "deleted later" {
		t.Error("archived versions not readable")
	}
	if cat(t, c, Join(c.Root, "a.txt")) != "version 2 is longer" {
		t.Error("current version")
	}
	removed, err := c.PruneArchive(context.Background(), arch, 1, time.Date(2026, 11, 5, 0, 0, 0, 0, time.Local))
	if err != nil || len(removed) != 1 || cat(t, c, Join(c.Root, "a.txt")) != "version 2 is longer" {
		t.Errorf("prune %v %v", removed, err)
	}
}

// Cancelling in the middle of an upload: the old copy is intact, no half file is
// visible, and the next run finishes the job.
func TestS3CancelledUpload(t *testing.T) {
	srv, s := newFake(t)
	t.Setenv("RCLONE_S3_UPLOAD_CUTOFF", "5M")
	t.Setenv("RCLONE_S3_CHUNK_SIZE", "5M")
	srv.Put(testBucket, "Backup/big.bin", []byte("the previous version"))
	srv.Put(testBucket, "Backup/gone.txt", []byte("still needed"))
	src := t.TempDir()
	big := make([]byte, 30<<20)
	rand.Read(big)
	write(t, filepath.Join(src, "big.bin"), string(big))
	c := s3Config(t, s, "Backup", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	_, err := c.Sync(ctx, Options{Src: src, Dst: c.Root, Mode: config.ModeMirror, BandwidthKBps: 2048}, io.Discard, nil)
	if err != context.Canceled {
		t.Fatalf("err = %v", err)
	}
	if o := srv.Get(testBucket, "Backup/big.bin"); o == nil || string(o.Data) != "the previous version" {
		t.Error("the old copy was damaged by the cancelled upload")
	}
	if srv.Get(testBucket, "Backup/gone.txt") == nil {
		t.Error("deleted by the cancelled run")
	}
	t.Logf("multipart uploads left open after the cancel: %d", srv.PendingUploads())
	if _, err := c.Sync(context.Background(), Options{Src: src, Dst: c.Root, Mode: config.ModeMirror}, io.Discard, nil); err != nil {
		t.Fatal(err)
	}
	if o := srv.Get(testBucket, "Backup/big.bin"); o == nil || !bytes.Equal(o.Data, big) || srv.Get(testBucket, "Backup/gone.txt") != nil {
		t.Error("the next run did not complete the job")
	}
}

// Folders without files do not count as content: some services keep them after
// their files are deleted, and the web consoles create them as marker objects. A
// restore from such a folder would otherwise mirror nothing over the local copy.
func TestS3EmptyFoldersAreNotContent(t *testing.T) {
	s, folder := s3Target(t)
	c := s3Config(t, s, folder, nil)
	src := t.TempDir()
	write(t, filepath.Join(src, "a.txt"), "x")
	write(t, filepath.Join(src, "sub", "deep", "b.txt"), "y")
	if _, err := syncLog(t, c, Options{Src: src, Dst: c.Root, Mode: config.ModeAdditive}); err != nil {
		t.Fatal(err)
	}
	if empty, err := c.IsEmpty(context.Background(), c.Raw); err != nil || empty {
		t.Fatalf("with files: IsEmpty = %v, %v", empty, err)
	}
	if err := os.Remove(filepath.Join(src, "a.txt")); err != nil {
		t.Fatal(err)
	}
	// a file only in a subfolder is content
	if _, err := c.run(context.Background(), "deletefile", Join(c.Root, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if empty, err := c.IsEmpty(context.Background(), c.Raw); err != nil || empty {
		t.Fatalf("file in a subfolder: IsEmpty = %v, %v", empty, err)
	}
	if _, err := c.run(context.Background(), "delete", c.Raw); err != nil {
		t.Fatal(err)
	}
	if empty, err := c.IsEmpty(context.Background(), c.Raw); err != nil || !empty {
		t.Errorf("no files left: IsEmpty = %v, %v", empty, err)
	}
}

func TestS3FolderMarkerObjects(t *testing.T) {
	srv, s := newFake(t)
	srv.Put(testBucket, "Backup/", nil)
	srv.Put(testBucket, "Backup/sub/", nil)
	c := s3Config(t, s, "Backup", nil)
	if empty, err := c.IsEmpty(context.Background(), c.Raw); err != nil || !empty {
		t.Errorf("only marker objects: IsEmpty = %v, %v", empty, err)
	}
}

// A copy with no progress is stopped with a clear error, and nothing is deleted.
func TestS3StallStops(t *testing.T) {
	srv, s := newFake(t)
	old := S3StallTimeout
	S3StallTimeout = 3 * time.Second
	defer func() { S3StallTimeout = old }()
	srv.Put(testBucket, "Backup/gone.txt", []byte("still needed"))
	h := make(chan struct{})
	defer close(h)
	srv.Hang = h
	src := t.TempDir()
	write(t, filepath.Join(src, "a.txt"), "x")
	c := s3Config(t, s, "Backup", nil)
	var log bytes.Buffer
	start := time.Now()
	_, err := c.Sync(context.Background(), Options{Src: src, Dst: c.Root, Mode: config.ModeMirror}, &log, nil)
	if err == nil || !strings.Contains(err.Error(), "no progress") {
		t.Errorf("err = %v\n%s", err, log.String())
	}
	if d := time.Since(start); d > 30*time.Second {
		t.Errorf("stopped after %v", d)
	}
	if srv.Get(testBucket, "Backup/gone.txt") == nil {
		t.Error("deleted by a stopped copy")
	}
}

// The arguments that keep a failure from taking hours reach rclone on S3 only.
func TestS3RetrySettings(t *testing.T) {
	_, s := newFake(t)
	S3CopyRetries, S3RetriesSleep = "7", "1s"
	src := t.TempDir()
	write(t, filepath.Join(src, "a"), "x")
	c := s3Config(t, s, "Backup", nil)
	var log bytes.Buffer
	if _, err := c.Sync(context.Background(), Options{Src: src, Dst: c.Root, Mode: config.ModeMirror}, &log, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), "--low-level-retries 7 --retries-sleep 1s") {
		t.Errorf("S3: %s", log.String())
	}
	local, err := NewConfig(t.TempDir(), Remote{DevRoot: t.TempDir()}, "x", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	log.Reset()
	if _, err := local.Sync(context.Background(), Options{Src: src, Dst: local.Root, Mode: config.ModeMirror}, &log, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(log.String(), "low-level-retries") || strings.Contains(log.String(), "fast-list") {
		t.Errorf("OneDrive changed: %s", log.String())
	}
}

// Large downloads are logged differently by rclone ("Multi-thread Copied"): they
// must be counted like the others, or a restore reports 0 files copied.
func TestS3LargeDownloadCounted(t *testing.T) {
	_, s := newFake(t)
	t.Setenv("RCLONE_MULTI_THREAD_CUTOFF", "1M")
	src := t.TempDir()
	big := make([]byte, 9<<20)
	if _, err := rand.Read(big); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(src, "big.bin"), string(big))
	c := s3Config(t, s, "Backup", nil)
	if _, err := syncLog(t, c, Options{Src: src, Dst: c.Root, Mode: config.ModeMirror}); err != nil {
		t.Fatal(err)
	}
	restore := t.TempDir()
	var log bytes.Buffer
	res, err := c.Sync(context.Background(), Options{Src: c.Root, Dst: restore, Mode: config.ModeMirror}, &log, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Stats.FilesTransferred != 1 || !has(lines(log.String()), ">f+++++++++ big.bin") {
		t.Errorf("stats %+v\n%s", res.Stats, log.String())
	}
	// the same when it replaces a local file
	write(t, filepath.Join(restore, "big.bin"), "old")
	old := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(restore, "big.bin"), old, old)
	log.Reset()
	if res, err = c.Sync(context.Background(), Options{Src: c.Root, Dst: restore, Mode: config.ModeMirror}, &log, nil); err != nil {
		t.Fatal(err)
	}
	if res.Stats.FilesTransferred != 1 || !has(lines(log.String()), ">f.st...... big.bin") {
		t.Errorf("replace: stats %+v\n%s", res.Stats, log.String())
	}
	sameTree(t, hashTree(t, restore), hashTree(t, src))
}
