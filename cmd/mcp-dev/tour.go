package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpcheck"
	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpclient"
	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpreceiver"
	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpserver"
	"github.com/urfave/cli/v3"
)

func tourCommand() *cli.Command {
	return &cli.Command{
		Name:  "tour",
		Usage: "Walk through subscribe, filtered delivery, refresh and unsubscribe in one terminal",
		Flags: append([]cli.Flag{
			&cli.StringFlag{Name: "tenant", Value: "demo", Usage: "Outpost tenant"},
			&cli.StringFlag{Name: "principal", Value: "alice", Usage: "bearer token of the subscriber"},
			&cli.StringFlag{Name: "listen", Value: ":4501", Usage: "receiver listen address (all interfaces so Docker can reach it)"},
			&cli.StringFlag{Name: "public-url", Value: "http://host.docker.internal:4501", Usage: "base URL Outpost reaches the receiver at"},
			&cli.StringFlag{Name: "event", Usage: "event to subscribe to (default: the first webhook event with a scalar argument)"},
			&cli.StringFlag{Name: "arguments", Usage: "subscription arguments, a JSON object"},
			&cli.StringFlag{Name: "data", Usage: "payload that matches the arguments, a JSON object"},
			&cli.StringFlag{Name: "other-data", Usage: "payload that doesn't match, a JSON object"},
			&cli.DurationFlag{Name: "wait", Value: 10 * time.Second, Usage: "wait per expected delivery"},
			&cli.DurationFlag{Name: "quiet", Value: 3 * time.Second, Usage: "wait before concluding nothing arrives"},
			&cli.BoolFlag{Name: "pause", Usage: "wait for Enter between steps"},
		}, outpostFlags()...),
		Action: tour,
	}
}

type tourConfig struct {
	conn                  conn
	tenant, principal     string
	listen, publicURL     string
	event                 string
	args, data, otherData map[string]any
	wait, quiet           time.Duration
	pause                 bool
	out                   io.Writer
	in                    io.Reader
}

func tour(ctx context.Context, cmd *cli.Command) error {
	c, err := outpostConn(cmd)
	if err != nil {
		return err
	}
	var objs [3]map[string]any
	for i, f := range []string{"arguments", "data", "other-data"} {
		if objs[i], err = parseObject(f, cmd.String(f)); err != nil {
			return err
		}
	}
	ok, err := runTour(ctx, tourConfig{
		conn: c, tenant: cmd.String("tenant"), principal: cmd.String("principal"),
		listen: cmd.String("listen"), publicURL: cmd.String("public-url"), event: cmd.String("event"),
		args: objs[0], data: objs[1], otherData: objs[2],
		wait: cmd.Duration("wait"), quiet: cmd.Duration("quiet"), pause: cmd.Bool("pause"),
		out: os.Stdout, in: os.Stdin,
	})
	if err != nil {
		return err
	}
	if !ok {
		return exitError{1}
	}
	return nil
}

type tourRun struct {
	cfg    tourConfig
	mu     sync.Mutex
	rcv    *mcpreceiver.Receiver
	secret string
	path   string
	url    string

	client  *mcpclient.Client
	publish publishFunc
	event   mcpclient.Event
	fc      mcpcheck.FilterCaseResult
	args    map[string]any
	subID   string
	seen    map[mcpreceiver.Violation]bool
	step    int
	okSteps int
	input   *bufio.Reader
}

func (t *tourRun) say(format string, args ...any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	fmt.Fprintf(t.cfg.out, format+"\n", args...)
}

func (t *tourRun) sayReceiver(text string) {
	lines := strings.Split(text, "\n")
	t.say("     receiver  %s", lines[0])
	for _, l := range lines[1:] {
		t.say("               %s", l)
	}
}

// runTour reports whether every step held. A setup problem is an error.
func runTour(ctx context.Context, cfg tourConfig) (bool, error) {
	t := &tourRun{cfg: cfg, seen: map[mcpreceiver.Violation]bool{}, secret: mcpclient.NewSecret()}
	t.input = bufio.NewReader(cfg.in)

	srvLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return false, err
	}
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: mcpserver.New(mcpserver.Config{
		OutpostURL: cfg.conn.URL, APIKey: cfg.conn.APIKey, DefaultTenant: cfg.tenant, Header: cfg.conn.Header,
	})}
	go func() { _ = srv.Serve(srvLn) }()
	defer srv.Close()
	serverURL := "http://" + srvLn.Addr().String() + "/"

	t.rcv, err = mcpreceiver.Listen(mcpreceiver.Options{
		Addr: cfg.listen, PublicURL: cfg.publicURL, Profile: mcpreceiver.ProfileSpec,
		OnRequest: func(q mcpreceiver.Request) { t.sayReceiver(describe(q, []string{t.secret})) },
	})
	if err != nil {
		return false, fmt.Errorf("can't listen on %s: %w (is mcp-dev receive running?)", cfg.listen, err)
	}
	defer t.rcv.Close()
	t.path, t.url = t.rcv.Register(mcpreceiver.PathConfig{Secrets: []string{t.secret}, RejectUnverified: true})

	t.client = mcpclient.New(serverURL, cfg.principal, mcpclient.Stateless(), mcpclient.WithClientInfoName("outpost-mcp-dev"),
		mcpclient.WithHTTPClient(&http.Client{Timeout: 60 * time.Second}))
	t.publish = cfg.conn.publisher(cfg.tenant)

	t.say("MCP Events tour (experimental)")
	t.say("  Outpost   %s, tenant %s", cfg.conn.URL, cfg.tenant)
	t.say("  server    %s (in-process), principal %s", strings.TrimSuffix(serverURL, "/"), cfg.principal)
	t.say("  receiver  %s (listening on %s)", t.url, cfg.listen)
	t.say("  secret    %s", t.secret)

	if !t.list(ctx) {
		return false, errors.New("setup failed")
	}
	t.subscribe(ctx)
	subscribed := t.subID != ""
	if subscribed {
		t.publishPair(ctx)
		t.refresh(ctx)
		t.unsubscribe(ctx)
		t.publishAfter(ctx)
	} else {
		for i := 3; i <= 6; i++ {
			t.begin(fmt.Sprintf("step %d", i))
			t.say("   skipped: subscribe failed")
		}
	}
	t.say("")
	if t.okSteps == 6 {
		t.say("Tour complete: 6/6 ok. Next: drive it yourself (cmd/mcp-dev/README.md) or `make mcp/dev ARGS=\"check --publish-tenant %s\"`.", cfg.tenant)
		return true, nil
	}
	t.say("Tour finished: %d/6 ok.", t.okSteps)
	return false, nil
}

func (t *tourRun) begin(title string) {
	if t.cfg.pause && t.step > 0 {
		t.say("   (press Enter)")
		_, _ = t.input.ReadString('\n')
	}
	t.step++
	t.say("\n%d. %s", t.step, title)
}

func (t *tourRun) violations() []string {
	var out []string
	for _, v := range t.rcv.Violations() {
		if !t.seen[v] {
			t.seen[v] = true
			out = append(out, fmt.Sprintf("VIOLATION %s: %s", v.Check, v.Detail))
		}
	}
	return out
}

// finish closes a step: ok when problem is empty and no new violation.
func (t *tourRun) finish(good, problem string) bool {
	bad := t.violations()
	if problem != "" {
		bad = append([]string{"FAIL: " + problem}, bad...)
	}
	if len(bad) > 0 {
		for _, b := range bad {
			t.say("   %s", b)
		}
		return false
	}
	t.okSteps++
	t.say("   ok%s", good)
	return true
}

func asJSON(v any) any {
	b, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

func (t *tourRun) call(method string, params any) {
	v := asJSON(params)
	if m, ok := v.(map[string]any); ok {
		if d, ok := m["delivery"].(map[string]any); ok {
			if s, ok := d["secret"].(string); ok && len(s) > 12 {
				d["secret"] = s[:12] + "…"
			}
		}
	}
	t.say("   > %s %s", method, compact(v, 100))
}

func (t *tourRun) reply(resp *mcpclient.Response) {
	switch {
	case resp.Error != nil:
		t.say("   < error %d %s %s", resp.Error.Code, resp.Error.Message, resp.Error.Data)
	case resp.Status != http.StatusOK:
		t.say("   < http %d %s", resp.Status, clip(string(resp.Raw), 200))
	default:
		var res struct {
			Events []json.RawMessage `json:"events"`
		}
		if json.Unmarshal(resp.Result, &res) == nil && res.Events != nil {
			t.say("   < {\"resultType\":\"complete\",\"events\":[…]}  (%d events)", len(res.Events))
			return
		}
		t.say("   < %s", compactRaw(resp.Result, 80))
	}
}

func okResponse(resp *mcpclient.Response) bool {
	return resp.Error == nil && resp.Status == http.StatusOK
}

func (t *tourRun) list(ctx context.Context) bool {
	t.begin("List event types")
	t.call("events/list", map[string]any{})
	resp, err := t.client.EventsListPage(ctx, "")
	if err != nil {
		t.say("   FAIL: %v", err)
		t.say("   hint: is Outpost running with MCP enabled? make up/mcp")
		return false
	}
	t.reply(resp)
	var res mcpclient.ListResult
	if !okResponse(resp) || json.Unmarshal(resp.Result, &res) != nil {
		t.say("   hint: is Outpost running with MCP enabled? make up/mcp")
		return false
	}
	for _, e := range res.Events {
		t.say("     %-18s %s", e.Name, describeArgs(e))
	}
	ev, err := mcpcheck.ChooseEvent(res.Events, t.cfg.event)
	if err != nil {
		t.say("   FAIL: %v", err)
		return false
	}
	t.event = ev
	fc, err := mcpcheck.FilterCase(t.event, t.cfg.args, t.cfg.data, t.cfg.otherData)
	if err != nil && fc.Other == nil {
		t.say("   FAIL: %v", err)
		return false
	}
	if fc.OtherData == nil {
		t.say("   FAIL: no non-matching payload: pass --other-data")
		return false
	}
	t.fc, t.args = fc, fc.Match
	return t.finish("", "")
}

func describeArgs(e mcpclient.Event) string {
	props, _ := e.InputSchema["properties"].(map[string]any)
	var parts []string
	for name, p := range props {
		s := name
		if m, ok := p.(map[string]any); ok {
			if enum, ok := m["enum"].([]any); ok {
				var vals []string
				for _, v := range enum {
					vals = append(vals, fmt.Sprint(v))
				}
				s += " (" + strings.Join(vals, "|") + ")"
			}
		}
		parts = append(parts, s)
	}
	if len(parts) == 0 {
		return "no arguments"
	}
	sort.Strings(parts)
	return "arguments: " + strings.Join(parts, ", ")
}

func (t *tourRun) params() mcpclient.SubscribeParams {
	return mcpclient.SubscribeParams{Name: t.event.Name, Arguments: t.args, URL: t.url, Secret: t.secret, TTLMs: int64(600000)}
}

func (t *tourRun) subscribe(ctx context.Context) {
	t.begin(fmt.Sprintf("Subscribe to %s with arguments %s", t.event.Name, compact(t.args, 80)))
	p := t.params()
	t.call("events/subscribe", p)
	resp, err := t.client.Subscribe(ctx, p)
	if err != nil {
		t.finish("", err.Error())
		return
	}
	t.reply(resp)
	if !okResponse(resp) {
		if resp.Error != nil && resp.Error.Code == mcpclient.CodeCallbackEndpointError {
			t.say("   hint: Outpost can't reach %s: check the allowlist (make up/mcp sets it) and that nothing blocks port 4501", t.cfg.publicURL)
		}
		t.finish("", "subscribe failed")
		return
	}
	var res mcpclient.SubscribeResult
	_ = json.Unmarshal(resp.Result, &res)
	if res.ID == "" {
		t.finish("", "no subscription id in the result")
		return
	}
	t.subID = res.ID
	if len(t.rcv.Challenges(t.path)) > 0 {
		t.finish(": challenge answered before subscribe returned", "")
	} else {
		t.finish(": no challenge arrived (the server verified another way)", "")
	}
}

func (t *tourRun) publishEvent(ctx context.Context, data map[string]any) (string, error) {
	id, err := t.publish(ctx, t.event.Name, data)
	if err != nil {
		return "", err
	}
	t.say("   > POST /publish %s %s  -> %s", t.event.Name, compact(data, 40), id)
	return id, nil
}

func (t *tourRun) deliveries(eventID string) []mcpreceiver.Request {
	return t.rcv.Matching(t.path, func(q mcpreceiver.Request) bool {
		return q.IsEvent() && (eventID == "" || q.Envelope().EventID == eventID)
	})
}

func (t *tourRun) waitDelivery(eventID string, d time.Duration) []mcpreceiver.Request {
	deadline := time.Now().Add(d)
	for {
		if got := t.deliveries(eventID); len(got) > 0 || time.Now().After(deadline) {
			return got
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (t *tourRun) field() string {
	return fmt.Sprintf("%s=%v", t.fc.Field, t.fc.Match[t.fc.Field])
}

func (t *tourRun) publishPair(ctx context.Context) {
	other := fmt.Sprintf("%s=%v", t.fc.Field, t.fc.Other[t.fc.Field])
	t.begin(fmt.Sprintf("Publish a matching event (%s) and a non-matching one (%s)", t.field(), other))
	hit, err := t.publishEvent(ctx, t.fc.Data)
	if err != nil {
		t.finish("", err.Error())
		return
	}
	miss, err := t.publishEvent(ctx, t.fc.OtherData)
	if err != nil {
		t.finish("", err.Error())
		return
	}
	got := t.waitDelivery(hit, t.cfg.wait)
	switch {
	case len(got) == 0:
		t.finish("", fmt.Sprintf("the matching event (%s) was not delivered within %s", t.field(), t.cfg.wait))
		return
	case !got[0].VerifiedWith(t.secret)[0]:
		t.finish("", "the delivery does not verify with the subscription secret")
		return
	}
	time.Sleep(t.cfg.quiet)
	if extra := t.deliveries(miss); len(extra) > 0 {
		t.finish("", fmt.Sprintf("the non-matching event (%s) was delivered (eventId %s)", other, miss))
		return
	}
	t.finish(fmt.Sprintf(": %s delivered with a valid signature; %s not delivered (waited %s)", t.field(), other, t.cfg.quiet), "")
}

func (t *tourRun) refresh(ctx context.Context) {
	t.begin("Refresh: subscribe again with the same name, arguments and URL")
	p := t.params()
	t.call("events/subscribe", p)
	resp, err := t.client.Subscribe(ctx, p)
	if err != nil {
		t.finish("", err.Error())
		return
	}
	t.reply(resp)
	var res mcpclient.SubscribeResult
	if !okResponse(resp) || json.Unmarshal(resp.Result, &res) != nil {
		t.finish("", "refresh failed")
		return
	}
	if res.ID != t.subID {
		t.finish("", fmt.Sprintf("subscription id changed: %s then %s", t.subID, res.ID))
		return
	}
	t.finish(": same id, refreshBefore "+deref(res.RefreshBefore), "")
}

func deref(s *string) string {
	if s == nil {
		return "absent"
	}
	return *s
}

func (t *tourRun) unsubscribe(ctx context.Context) {
	t.begin("Unsubscribe")
	p := mcpclient.UnsubscribeParams{Name: t.event.Name, Arguments: t.args, URL: t.url}
	t.call("events/unsubscribe", p)
	resp, err := t.client.Unsubscribe(ctx, p)
	if err != nil {
		t.finish("", err.Error())
		return
	}
	t.reply(resp)
	if !okResponse(resp) {
		t.finish("", "unsubscribe failed")
		return
	}
	t.finish("", "")
}

func (t *tourRun) publishAfter(ctx context.Context) {
	t.begin(fmt.Sprintf("Publish another matching event (%s)", t.field()))
	id, err := t.publishEvent(ctx, t.fc.Data)
	if err != nil {
		t.finish("", err.Error())
		return
	}
	if got := t.waitDelivery(id, t.cfg.quiet); len(got) > 0 {
		t.finish("", fmt.Sprintf("the event was delivered after unsubscribe (eventId %s)", id))
		return
	}
	t.finish(fmt.Sprintf(": nothing arrived (waited %s)", t.cfg.quiet), "")
}
