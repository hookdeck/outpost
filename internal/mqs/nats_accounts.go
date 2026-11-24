package mqs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

// accountMetaFile is the on-disk representation of a single tenant's
// NATS account inside AccountsDir.
//
// Layout under AccountsDir:
//
//	<account-name>/
//	  user.creds       NATS .creds (JWT + NKey seed) — default credentials path
//	  meta.yaml        this struct
//
// CredentialsFile may be overridden in meta.yaml (absolute or relative
// to the account directory). The default is "user.creds".
type accountMetaFile struct {
	Name            string `yaml:"name"`
	CredentialsFile string `yaml:"credentials_file"`
	Stream          string `yaml:"stream"`
	Consumer        string `yaml:"consumer"`
	TenantID        string `yaml:"tenant_id"`
}

// loadAccountsFromDir scans dir for tenant subdirectories that contain a
// meta.yaml file and returns the list of accounts. Subdirectories without
// a meta.yaml are skipped (still being written, perhaps).
func loadAccountsFromDir(dir string) ([]NATSPublishAccountConfig, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read accounts_dir %q: %w", dir, err)
	}
	var accounts []NATSPublishAccountConfig
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		accountDir := filepath.Join(dir, e.Name())
		metaPath := filepath.Join(accountDir, "meta.yaml")
		if _, err := os.Stat(metaPath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				// Subdirectory without meta.yaml: still being provisioned
				// or simply not an outpost account. Silently skip.
				continue
			}
			return nil, fmt.Errorf("stat meta for account %q: %w", e.Name(), err)
		}
		acc, err := loadAccountMeta(metaPath, accountDir, e.Name())
		if err != nil {
			return nil, fmt.Errorf("account %q: %w", e.Name(), err)
		}
		accounts = append(accounts, acc)
	}
	return accounts, nil
}

func loadAccountMeta(metaPath, accountDir, dirName string) (NATSPublishAccountConfig, error) {
	body, err := os.ReadFile(metaPath)
	if err != nil {
		return NATSPublishAccountConfig{}, err
	}
	var meta accountMetaFile
	if err := yaml.Unmarshal(body, &meta); err != nil {
		return NATSPublishAccountConfig{}, fmt.Errorf("parse meta.yaml: %w", err)
	}

	name := meta.Name
	if name == "" {
		name = dirName
	}

	creds := meta.CredentialsFile
	if creds == "" {
		// Convention: <accountDir>/user.creds when present. If absent the
		// account runs with no credentials (valid for trusted-network or
		// token-via-URL setups).
		candidate := filepath.Join(accountDir, "user.creds")
		if _, err := os.Stat(candidate); err == nil {
			creds = candidate
		}
	} else if !filepath.IsAbs(creds) {
		creds = filepath.Join(accountDir, creds)
	}

	return NATSPublishAccountConfig{
		Name:            name,
		CredentialsFile: creds,
		Stream:          meta.Stream,
		Consumer:        meta.Consumer,
		TenantID:        meta.TenantID,
	}, nil
}

// natsAccountsWatcher watches a directory for create/remove/rename events
// and invokes onChange (debounced) so the queue can reconcile. It also
// invokes onChange on a timer, because the watch alone is not a reliable
// signal — see NATSPublishConfig.AccountsRescanInterval.
type natsAccountsWatcher struct {
	dir      string
	onChange func()
	rescan   time.Duration
	w        *fsnotify.Watcher

	stopCh chan struct{}
	once   sync.Once
}

// debounceWindow collapses bursts of FS events (e.g. provisioning writes
// user.creds then meta.yaml) into a single reconcile.
const debounceWindow = 250 * time.Millisecond

// defaultRescanInterval is the fallback sweep when nothing else says a
// directory changed. Reconciling is a directory listing plus one small YAML
// parse per account, then a no-op for every account already connected, so
// this is cheap enough to run often and slow enough not to matter.
const defaultRescanInterval = 60 * time.Second

func newNATSAccountsWatcher(dir string, onChange func(), rescan time.Duration) (*natsAccountsWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := w.Add(dir); err != nil {
		_ = w.Close()
		return nil, err
	}

	// Also watch existing subdirectories so we catch file changes inside
	// an account dir (e.g. credentials rotation, meta.yaml edits).
	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() {
				_ = w.Add(filepath.Join(dir, e.Name()))
			}
		}
	}

	if rescan == 0 {
		rescan = defaultRescanInterval
	}

	return &natsAccountsWatcher{
		dir:      dir,
		onChange: onChange,
		rescan:   rescan,
		w:        w,
		stopCh:   make(chan struct{}),
	}, nil
}

func (w *natsAccountsWatcher) start() {
	go w.run()
}

func (w *natsAccountsWatcher) run() {
	defer w.w.Close()

	// Single reusable timer for debouncing FS events. Initialise stopped
	// so timer.C blocks until the first event arms it.
	timer := time.NewTimer(debounceWindow)
	if !timer.Stop() {
		<-timer.C
	}
	armed := false

	// A negative interval opts out and leaves discovery on fsnotify alone.
	// nil channels block forever in select, so this needs no second branch.
	var rescanC <-chan time.Time
	if w.rescan > 0 {
		ticker := time.NewTicker(w.rescan)
		defer ticker.Stop()
		rescanC = ticker.C
	}

	arm := func() {
		if armed {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		timer.Reset(debounceWindow)
		armed = true
	}

	for {
		select {
		case <-w.stopCh:
			if armed {
				timer.Stop()
			}
			return
		case ev, ok := <-w.w.Events:
			if !ok {
				return
			}
			// Newly created subdirectory: add it to the watch list so we
			// also see meta.yaml/credentials writes inside.
			if ev.Op&fsnotify.Create != 0 {
				if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
					_ = w.w.Add(ev.Name)
				}
			}
			// On Remove/Rename fsnotify cleans up its own watch — nothing
			// to do here. The next reconcile will catch the diff.
			arm()
		case <-w.w.Errors:
			// Errors are non-fatal; the next event will retry.
		case <-timer.C:
			armed = false
			w.onChange()
		case <-rescanC:
			// Deliberately not debounced. The debounce timer only arms on a
			// filesystem event, and the whole point of this case is the run
			// in which no such event ever arrives.
			w.onChange()
		}
	}
}

func (w *natsAccountsWatcher) stop() {
	w.once.Do(func() { close(w.stopCh) })
}
