// Command mqcheck validates Outpost's internal queue consumer against the
// queue requirements (REQUIREMENTS.md) on a message broker, and measures its
// capacity envelope. See README.md.
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/hookdeck/outpost/internal/mqcheck/cases"
	"github.com/hookdeck/outpost/internal/mqcheck/profile"
	"github.com/hookdeck/outpost/internal/mqcheck/provider"
	_ "github.com/hookdeck/outpost/internal/mqcheck/providers"
	"github.com/hookdeck/outpost/internal/mqcheck/report"
	"github.com/hookdeck/outpost/internal/mqcheck/runner"
	"github.com/hookdeck/outpost/internal/mqcheck/sut"
)

//go:embed profiles/*.yaml
var profilesFS embed.FS

const usage = `mqcheck: validate Outpost's queue consumer on a message broker.

Usage:
  mqcheck validate --provider <name> [--profile quick|official|<file.yaml>] [--cases C4.1,C12.1] [--out DIR] [--prefix P] [--no-capacity|--capacity-only]
  mqcheck providers                     list providers and their settings
  mqcheck cases                         list cases
  mqcheck doctor --provider <name>      check the broker is reachable and queues can be created
  mqcheck sweep --provider <name> [--prefix P] [--older-than 2h]
                                        delete leftover resources of crashed runs

Exit codes: 0 PASS (everything judged), 1 FAIL, 2 harness or infrastructure
error, 3 no FAIL but not fully judged (not observable, not in build, capacity
measured only).
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "worker":
		os.Exit(sut.Main())
	case "validate":
		os.Exit(validate(os.Args[2:]))
	case "providers":
		listProviders()
	case "cases":
		listCases()
	case "doctor":
		os.Exit(doctor(os.Args[2:]))
	case "sweep":
		os.Exit(sweep(os.Args[2:]))
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
}

var validPrefix = regexp.MustCompile(`^[a-z][a-z0-9]{0,19}$`)

func openProvider(ctx context.Context, name, prefix string) (provider.Registration, provider.Provider, error) {
	if !validPrefix.MatchString(prefix) {
		return provider.Registration{}, nil, fmt.Errorf("--prefix %q: want a lowercase letter, then up to 19 lowercase letters or digits (no '-': it separates the run id)", prefix)
	}
	reg, ok := provider.Lookup(name)
	if !ok {
		var names []string
		for _, r := range provider.All() {
			names = append(names, r.Name)
		}
		return reg, nil, fmt.Errorf("unknown provider %q (have: %s)", name, strings.Join(names, ", "))
	}
	p, err := reg.Open(ctx, provider.NewConfig(reg, prefix))
	if err != nil {
		return reg, nil, fmt.Errorf("open %s: %w", name, err)
	}
	return reg, p, nil
}

func validate(args []string) int {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	prov := fs.String("provider", "", "provider name (see `mqcheck providers`)")
	prof := fs.String("profile", "quick", "profile: quick, official, or a YAML file")
	only := fs.String("cases", "", "comma-separated case ids (default: the profile's)")
	out := fs.String("out", "out", "output directory")
	prefix := fs.String("prefix", "mqcheck", "resource name prefix")
	noCap := fs.Bool("no-capacity", false, "skip the capacity envelope")
	capOnly := fs.Bool("capacity-only", false, "run only the capacity envelope")
	parallel := fs.Int("parallel", 0, "cases at once (default: the profile's)")
	fs.Parse(args)
	if *prov == "" {
		fmt.Fprintln(os.Stderr, "--provider is required")
		return 2
	}
	p, err := profile.Load(*prof, mustSub(profilesFS, "profiles"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if *parallel > 0 {
		p.Parallel = *parallel
	}
	if *noCap {
		p.Capacity.Enabled = false
	}
	ctx, cancel := signalContext()
	defer cancel()
	reg, prv, err := openProvider(ctx, *prov, *prefix)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer prv.Close()
	if err := check(ctx, prv, *prefix); err != nil {
		fmt.Fprintf(os.Stderr, "doctor: %v\n", err)
		return 2
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	r := runner.New(reg, prv, p, *prefix, *out, exe, os.Stderr)
	started := time.Now()
	var ids []string
	if *only != "" {
		ids = strings.Split(*only, ",")
	}
	selected := r.Select(cases.All(), ids)
	if *capOnly {
		selected = nil
	}
	r.Progress.Write([]byte(fmt.Sprintf("mqcheck %s, profile %s: %d cases, output %s\n", reg.Name, p.Name, len(selected), r.OutDir)))
	results := r.RunCases(ctx, selected)
	var env *runner.Envelope
	if ctx.Err() == nil && (len(ids) == 0 || *capOnly) {
		env = r.Capacity(ctx)
	}
	host, _ := os.Hostname()
	subset := ""
	switch {
	case len(ids) > 0 || *capOnly:
		subset = fmt.Sprintf("subset: %d of %d cases", len(selected), len(r.Select(cases.All(), nil)))
		if env == nil {
			subset += ", capacity not run"
		}
	case *noCap:
		subset = "subset: capacity not run"
	}
	run := report.Run{
		Subset: subset,
		RunID:  r.RunID, Provider: reg.Name, Broker: r.Caps.Broker, Profile: p, Build: build(), Host: host,
		Started: started, Finished: time.Now(), Prefix: *prefix,
		VisibilityTimeout: r.VisibilityTimeout().String(), MaxAttempts: r.MaxAttempts(),
		ByteLimitSetting: p.Worker.BytesEnv, ByteLimitInBuild: r.HasBytes, Caps: r.Caps,
	}
	v, err := report.Write(r.OutDir, run, results, env)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Println(v.Line)
	for _, f := range v.Failing {
		fmt.Println("  " + f)
	}
	fmt.Println("Report:", filepath.Join(r.OutDir, "report.md"))
	if ctx.Err() != nil {
		return 2
	}
	return v.ExitCode
}

// check provisions and tears down one queue: credentials, permissions and
// connectivity in one go.
func check(ctx context.Context, p provider.Provider, prefix string) error {
	name := provider.ResourceName(prefix, provider.RunName(time.Now()), "doctor")
	t, err := p.Provision(ctx, provider.QueueSpec{Name: name, VisibilityTimeout: 30 * time.Second, MaxAttempts: max(5, p.Caps().MinMaxAttempts)})
	if err != nil {
		return fmt.Errorf("provision a test queue: %w", err)
	}
	if _, err := p.Sample(ctx, t); err != nil {
		p.Teardown(ctx, t)
		return fmt.Errorf("read queue counters: %w", err)
	}
	return p.Teardown(ctx, t)
}

func doctor(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	prov := fs.String("provider", "", "provider name")
	prefix := fs.String("prefix", "mqcheck", "resource name prefix")
	fs.Parse(args)
	ctx, cancel := signalContext()
	defer cancel()
	_, p, err := openProvider(ctx, *prov, *prefix)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer p.Close()
	if err := check(ctx, p, *prefix); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	c := p.Caps()
	fmt.Printf("ok: %s (in-flight count: %s)\n", c.Broker, c.InFlight)
	if !runner.HasSetting(profile.Defaults().Worker.BytesEnv) {
		fmt.Printf("note: this build has no %s setting; byte limit cases report NOT-IN-BUILD\n", profile.Defaults().Worker.BytesEnv)
	}
	return 0
}

func sweep(args []string) int {
	fs := flag.NewFlagSet("sweep", flag.ExitOnError)
	prov := fs.String("provider", "", "provider name")
	prefix := fs.String("prefix", "mqcheck", "resource name prefix")
	older := fs.Duration("older-than", 2*time.Hour, "only resources created longer ago than this")
	fs.Parse(args)
	ctx, cancel := signalContext()
	defer cancel()
	_, p, err := openProvider(ctx, *prov, *prefix)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer p.Close()
	n, err := p.Sweep(ctx, *prefix, *older)
	fmt.Printf("deleted %d resources\n", n)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	return 0
}

func listProviders() {
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	for _, r := range provider.All() {
		fmt.Fprintf(tw, "%s\t%s\n", r.Name, r.Summary)
		for _, s := range r.Settings {
			def := s.Default
			if def == "" {
				def = "-"
			}
			fmt.Fprintf(tw, "  %s\t(default %s) %s\n", s.Env, def, s.Description)
		}
	}
	tw.Flush()
}

func listCases() {
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	for _, c := range cases.All() {
		var reqs []string
		for _, q := range c.Req {
			reqs = append(reqs, fmt.Sprintf("R%d", q))
		}
		extra := ""
		if c.Bytes {
			extra = " [byte limit]"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s%s\n", c.ID, strings.Join(reqs, ","), c.Title, extra)
	}
	tw.Flush()
}

func build() report.Build {
	b := report.Build{Commit: "unknown", GoVersion: runtime.Version()}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				b.Commit = s.Value
			case "vcs.modified":
				b.Dirty = s.Value == "true"
			}
		}
	}
	if b.Commit == "unknown" {
		if out, err := exec.Command("git", "rev-parse", "HEAD").Output(); err == nil {
			b.Commit = strings.TrimSpace(string(out))
		}
		if out, err := exec.Command("git", "status", "--porcelain", "--untracked-files=no").Output(); err == nil {
			b.Dirty = len(strings.TrimSpace(string(out))) > 0
		}
	}
	if len(b.Commit) > 12 {
		b.Commit = b.Commit[:12]
	}
	return b
}

func mustSub(f fs.FS, dir string) fs.FS {
	s, err := fs.Sub(f, dir)
	if err != nil {
		panic(err)
	}
	return s
}
