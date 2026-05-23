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
func loadAccountsFromDir(dir string) ([]NATSAccountConfig, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read accounts_dir %q: %w", dir, err)
	}
	var accounts []NATSAccountConfig
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

func loadAccountMeta(metaPath, accountDir, dirName string) (NATSAccountConfig, error) {
	body, err := os.ReadFile(metaPath)
	if err != nil {
		return NATSAccountConfig{}, err
	}
	var meta accountMetaFile
	if err := yaml.Unmarshal(body, &meta); err != nil {
		return NATSAccountConfig{}, fmt.Errorf("parse meta.yaml: %w", err)
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

	return NATSAccountConfig{
		Name:            name,
		CredentialsFile: creds,
		Stream:          meta.Stream,
		Consumer:        meta.Consumer,
		TenantID:        meta.TenantID,
	}, nil
}

// natsAccountsWatcher watches a directory for create/remove/rename events
// and invokes onChange (debounced) so the queue can reconcile.
type natsAccountsWatcher struct {
	dir      string
	onChange func()
	w        *fsnotify.Watcher

	stopCh chan struct{}
	once   sync.Once
}

// debounceWindow collapses bursts of FS events (e.g. provisioning writes
// user.creds then meta.yaml) into a single reconcile.
const debounceWindow = 250 * time.Millisecond

func newNATSAccountsWatcher(dir string, onChange func()) (*natsAccountsWatcher, error) {
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

	return &natsAccountsWatcher{
		dir:      dir,
		onChange: onChange,
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
		}
	}
}

func (w *natsAccountsWatcher) stop() {
	w.once.Do(func() { close(w.stopCh) })
}
