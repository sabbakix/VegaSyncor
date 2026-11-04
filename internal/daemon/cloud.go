package daemon

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"vegasyncor/internal/api"
	"vegasyncor/internal/cloud"
	"vegasyncor/internal/config"
	"vegasyncor/internal/paths"
	"vegasyncor/internal/syncer"
)

// Off-site copies with rclone: what OneDrive and S3 have in common.

// cloudName is the name of the service of a cloud location, for the messages.
func cloudName(locType string) string {
	if locType == config.LocS3 {
		return "S3"
	}
	return "OneDrive"
}

// cloudConfig prepares the rclone configuration for a cloud connection; dest is
// the folder of the job (the base of the encryption when crypt is set). release
// removes the configuration.
func (d *Daemon) cloudConfig(cfg *config.Config, connID, dest string, crypt *cloud.Crypt) (*cloud.Config, func(), error) {
	cn := cfg.Connection(connID)
	if cn == nil || cn.IsSMB() {
		return nil, nil, errors.New(Tf("connection %q not found", connID))
	}
	if err := cloud.Available(); err != nil {
		return nil, nil, err
	}
	if cn.IsS3() {
		return d.s3Bucket(cn, dest, crypt)
	}
	return d.oneDrive(cn, dest, crypt)
}

func (d *Daemon) s3Bucket(cn *config.Connection, dest string, crypt *cloud.Crypt) (*cloud.Config, func(), error) {
	secret, err := d.box.Decrypt(cn.PasswordEnc, cn.ID)
	if err != nil {
		return nil, nil, err
	}
	if secret == "" {
		return nil, nil, errors.New(Tf("S3 %s: the secret key is missing (edit the connection)", cn.Name))
	}
	if err := os.MkdirAll(paths.RuntimeDir(), 0o755); err != nil {
		return nil, nil, err
	}
	c, err := cloud.NewConfig(paths.RuntimeDir(), cloud.Remote{S3: &cloud.S3{
		Endpoint: cn.Endpoint, Region: cn.Region, Bucket: cn.Bucket, AccessKey: cn.Username, SecretKey: secret,
	}}, dest, crypt)
	if err != nil {
		return nil, nil, err
	}
	return c, c.Close, nil
}

// s3AccessError explains why a bucket cannot be listed.
func s3AccessError(cfg *config.Config, connID string, err error) error {
	bucket := "?"
	if cn := cfg.Connection(connID); cn != nil {
		bucket = cn.Bucket
	}
	if strings.Contains(err.Error(), "directory not found") || strings.Contains(err.Error(), "NoSuchBucket") {
		return errors.New(Tf("the S3 bucket %s does not exist (VegaSyncor never creates one)", bucket))
	}
	return errors.New(Tf("the S3 bucket %s cannot be read: %v", bucket, err))
}

// cryptOf returns the encryption settings of a job (nil if not encrypted).
func (d *Daemon) cryptOf(j config.Job) (*cloud.Crypt, error) {
	if !j.Encrypt {
		return nil, nil
	}
	pass, err := d.box.Decrypt(j.CryptPassEnc, config.CryptAAD(j.ID))
	if err != nil || pass == "" {
		return nil, errors.New(T("encryption: the password cannot be read (enter it again in the job)"))
	}
	return &cloud.Crypt{Password: pass, Names: j.EncryptNames}, nil
}

// --- runs ---

// syncCloud copies src to the cloud destination of j; it returns the phase to
// report when it fails.
func (d *Daemon) syncCloud(ctx context.Context, cfg *config.Config, j config.Job, r *api.Run, src string, lf io.Writer, logf func(string)) (syncer.Result, string, error) {
	name := cloudName(j.Dest.Type)
	d.setPhase(r, Tf("connecting to %s", name))
	c, release, err := d.cloudOf(cfg, j, j.Dest, logf)
	if err != nil {
		return syncer.Result{}, T("destination"), err
	}
	defer release()
	if j.Dest.Type == config.LocS3 {
		// before anything else: wrong keys or a wrong bucket stop the run here
		if err := c.CheckAccess(ctx, c.Raw); err != nil {
			return syncer.Result{}, T("destination"), s3AccessError(cfg, j.Dest.ConnectionID, err)
		}
	}

	var protect []string
	for _, l := range []*config.Location{j.Archive, j.LogDir} {
		if l != nil {
			if rel, ok := cfg.RelPath(*l, j.Dest); ok && rel != "" {
				protect = append(protect, rel)
			}
		}
	}
	archive := cloud.Join(c.Root, config.ArchiveDirName)
	if j.Mode == config.ModeMirrorArchive && j.Archive != nil {
		archive = c.Abs(j.Archive.Path) // same drive or bucket (checked when the job is saved)
	}

	d.setPhase(r, T("synchronizing"))
	opts := cloud.Options{
		Src: src, Dst: c.Root, Mode: j.Mode, Excludes: j.Excludes, DryRun: r.DryRun, BandwidthKBps: j.BandwidthKBps,
		Checksum: j.Checksum, Protect: protect, RunStamp: r.Start.Format(syncer.StampFormat),
	}
	if j.Mode == config.ModeMirrorArchive {
		opts.Archive = archive
	}
	res, err := c.Sync(ctx, opts, lf, func(p syncer.Progress) {
		d.mu.Lock()
		r.Progress = &p
		d.mu.Unlock()
	})
	if err != nil {
		return res, name, err
	}
	if j.Mode == config.ModeMirrorArchive && !r.DryRun && j.ArchiveDays > 0 {
		d.setPhase(r, T("cleaning up the archive"))
		removed, err := c.PruneArchive(ctx, archive, j.ArchiveDays, time.Now())
		if err != nil {
			logf(Tf("warning: archive cleanup: %v", err))
		} else if len(removed) > 0 {
			logf(Tf("archive: removed %d folders older than %d days", len(removed), j.ArchiveDays))
		}
	}
	return res, "", nil
}

// cloudOf prepares the cloud side of a job (source or destination, loc), with the
// encryption of the job, and writes the encryption mode to the log.
func (d *Daemon) cloudOf(cfg *config.Config, j config.Job, loc config.Location, logf func(string)) (*cloud.Config, func(), error) {
	crypt, err := d.cryptOf(j)
	if err != nil {
		return nil, nil, err
	}
	c, release, err := d.cloudConfig(cfg, loc.ConnectionID, loc.Path, crypt)
	if err != nil {
		return nil, nil, err
	}
	if crypt != nil {
		names := T("contents only")
		if crypt.Names {
			names = T("contents and names")
		}
		logf(Tf("Encryption:  on (%s)", names))
	}
	return c, release, nil
}

// --- connection test, browsing ---

func (d *Daemon) testOneDrive(ctx context.Context, cfg *config.Config, id string) api.TestResult {
	c, release, err := d.cloudConfig(cfg, id, "", nil)
	if err != nil {
		return api.TestResult{OK: false, Message: err.Error()}
	}
	defer release()
	s, err := c.About(ctx)
	if err != nil {
		return api.TestResult{OK: false, Message: err.Error()}
	}
	return api.TestResult{OK: true, Message: Tf("access granted: %s free of %s", api.HumanBytes(s.Free), api.HumanBytes(s.Total))}
}

// testS3 lists the bucket: S3 has no quota to ask for, and writing a test object
// into a bucket holding backups is not something a test should do.
func (d *Daemon) testS3(ctx context.Context, cfg *config.Config, id string) api.TestResult {
	c, release, err := d.cloudConfig(cfg, id, "", nil)
	if err != nil {
		return api.TestResult{OK: false, Message: err.Error()}
	}
	defer release()
	if err := c.CheckAccess(ctx, c.Abs("")); err != nil {
		return api.TestResult{OK: false, Message: s3AccessError(cfg, id, err).Error()}
	}
	return api.TestResult{OK: true, Message: Tf("the bucket %s can be read (writing is checked by the first copy)", cfg.Connection(id).Bucket)}
}

func (d *Daemon) browseCloud(ctx context.Context, loc config.Location) (api.BrowseResponse, error) {
	p, err := config.CleanSubPath(loc.Path)
	if err != nil {
		return api.BrowseResponse{}, err
	}
	d.mu.Lock()
	cfg := cloneConfig(d.cfg)
	d.mu.Unlock()
	c, release, err := d.cloudConfig(cfg, loc.ConnectionID, "", nil)
	if err != nil {
		return api.BrowseResponse{}, err
	}
	defer release()
	dirs, err := c.ListDirs(ctx, c.Abs(p))
	if err != nil {
		return api.BrowseResponse{}, err
	}
	sort.Slice(dirs, func(a, b int) bool { return strings.ToLower(dirs[a]) < strings.ToLower(dirs[b]) })
	return api.BrowseResponse{Path: p, Dirs: dirs}, nil
}

// mkdirCloud creates a folder; on S3 nothing is created until the first copy puts
// a file in it, so only the path is returned.
func (d *Daemon) mkdirCloud(ctx context.Context, loc config.Location, name string) (string, error) {
	p, err := config.CleanSubPath(loc.Path)
	if err != nil {
		return "", err
	}
	d.mu.Lock()
	cfg := cloneConfig(d.cfg)
	d.mu.Unlock()
	c, release, err := d.cloudConfig(cfg, loc.ConnectionID, "", nil)
	if err != nil {
		return "", err
	}
	defer release()
	dirs, err := c.ListDirs(ctx, c.Abs(p))
	if err != nil {
		return "", err
	}
	for _, x := range dirs {
		if strings.EqualFold(x, name) {
			return "", errAlreadyExists
		}
	}
	newPath := strings.TrimPrefix(path.Join(p, name), "/")
	return newPath, c.Mkdir(ctx, c.Abs(newPath))
}

var errAlreadyExists = errors.New("exists")
