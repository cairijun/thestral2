package tools

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"strconv"
	"text/tabwriter"
	"time"

	"golang.org/x/crypto/ssh/terminal"

	"github.com/richardtsai/thestral2/lib"
)

func init() {
	allTools = append(allTools, &monitorTool{})
}

type monitorTool struct {
	consoleTool
	addr             string
	client           http.Client
	lastListedReqIDs []string
}

func (monitorTool) Name() string {
	return "monitor"
}

func (monitorTool) Description() string {
	return "Inspect the runtime statistics of a thestral2 service"
}

func (t *monitorTool) Run(args []string) {
	fs := flag.NewFlagSet("monitor", flag.ExitOnError)
	fs.StringVar(&t.addr, "addr", "http://localhost:6060/debug/monitor",
		"base address to the service monitor.")
	cert := fs.String("cert", "", "optional TLS client certificate.")
	key := fs.String("key", "", "private key file for the client certificate.")
	_ = fs.Parse(args)
	if t.addr == "" {
		panic("-addr must be specified")
	}
	if (*cert == "") != (*key == "") {
		panic("-cert must be used with -key")
	}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	if *cert != "" {
		if c, err := tls.LoadX509KeyPair(*cert, *key); err != nil {
			panic("Failed to load certificate: " + err.Error())
		} else {
			transport.TLSClientConfig = &tls.Config{
				Certificates: []tls.Certificate{c},
			}
		}
	}
	t.client.Transport = transport

	if err := t.setupConsole("monitor> "); err != nil {
		panic(err)
	}
	t.addCmd("ls", "ls", t.ls)
	t.addCmd("show", "show INDEX_IN_LAST_LS", t.show)
	t.addCmd("showreq", "showreq REQUEST_ID", t.showreq)
	t.addCmd("kill", "kill INDEX_IN_LAST_LS", t.kill)
	t.addCmd("killreq", "killreq REQUEST_ID", t.killreq)
	defer t.teardownConsole()
	t.runLoop()
}

func (t *monitorTool) ls(term *terminal.Terminal, args []string) bool {
	if len(args) != 0 {
		fmt.Fprintln(term, "'ls' doesn't take any argument")
		return true
	}
	var report lib.AppMonitorReport
	if err := t.request(http.MethodGet, "/", &report); err != nil {
		fmt.Fprintln(term, err.Error())
		return true
	}

	w := tabwriter.NewWriter(term, 2, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(w, "Tunnels")
	fmt.Fprintln(w,
		"#\tReqID\tClient\tTarget\tUpstream\tUpload\tDownload\tElapsed\t")
	t.lastListedReqIDs = make([]string, len(report.Tunnels))
	upstreamTunnelCount := make(map[string]int)
	for i, r := range report.Tunnels {
		t.lastListedReqIDs[i] = r.RequestID
		upstreamTunnelCount[r.Upstream]++
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s/s\t%s/s\t%s\t\n",
			i, r.RequestID, r.ClientAddr, r.TargetAddr, r.Upstream,
			lib.BytesHumanized(uint64(r.UploadSpeed)),
			lib.BytesHumanized(uint64(r.DownloadSpeed)),
			t.formatSeconds(r.ElapsedTimeSecs))
	}
	fmt.Fprintln(w, "Upstreams")
	fmt.Fprintln(w,
		"Name\tTunnels\t\tUpload\t\tDownload\tLatencyMs\tErrors\t")
	for _, r := range report.Upstreams {
		fmt.Fprintf(w, "%s\t%d\t%s/s\t(%s)\t%s/s\t(%s)\t%.2f ms\t%d\t\n",
			r.Name, upstreamTunnelCount[r.Name],
			lib.BytesHumanized(uint64(r.UploadSpeed)),
			lib.BytesHumanized(r.BytesUploaded),
			lib.BytesHumanized(uint64(r.DownloadSpeed)),
			lib.BytesHumanized(r.BytesDownloaded),
			r.AvgConnLatencyMs, r.ErrorCount,
		)
	}
	_ = w.Flush()

	w = tabwriter.NewWriter(term, 2, 0, 2, ' ', 0)
	fmt.Fprintf(w, "\nServer:\tThestral2 %s\t%s\t\n",
		report.ThestralVersion, report.Runtime)
	fmt.Fprintf(w, "AvgConnLatencyMs:\t%.2f ms\n", report.AvgConnLatencyMs)
	fmt.Fprintf(w, "ErrorCount:\t%d\n", report.ErrorCount)
	fmt.Fprintf(w, "Upload:\t%s/s\t(%s)\t\n",
		lib.BytesHumanized(uint64(report.UploadSpeed)),
		lib.BytesHumanized(report.BytesUploaded))
	fmt.Fprintf(w, "Download:\t%s/s\t(%s)\t\n",
		lib.BytesHumanized(uint64(report.DownloadSpeed)),
		lib.BytesHumanized(report.BytesDownloaded))
	_ = w.Flush()
	return true
}

func (t *monitorTool) show(term *terminal.Terminal, args []string) bool {
	if len(args) != 1 {
		fmt.Fprintln(term, "'show' takes exactly one argument")
		return true
	}
	idx, err := strconv.Atoi(args[0])
	if err != nil {
		fmt.Fprintln(term, err.Error())
		return true
	}
	if idx < 0 || idx >= len(t.lastListedReqIDs) {
		fmt.Fprintf(term, "Unknown index: %d\n", idx)
		return true
	}
	return t.showreq(term, []string{t.lastListedReqIDs[idx]})
}

func (t *monitorTool) kill(term *terminal.Terminal, args []string) bool {
	if len(args) != 1 {
		fmt.Fprintln(term, "'kill' takes exactly one argument")
		return true
	}
	idx, err := strconv.Atoi(args[0])
	if err != nil {
		fmt.Fprintln(term, err.Error())
		return true
	}
	if idx < 0 || idx >= len(t.lastListedReqIDs) {
		fmt.Fprintf(term, "Unknown index: %d\n", idx)
		return true
	}
	return t.killreq(term, []string{t.lastListedReqIDs[idx]})
}

func (t *monitorTool) showreq(term *terminal.Terminal, args []string) bool {
	if len(args) != 1 {
		fmt.Fprintln(term, "'showreq' takes exactly one argument")
		return true
	}
	var report lib.TunnelMonitorReport
	if err := t.request(
		http.MethodGet, "/tunnel/"+args[0], &report); err != nil {
		fmt.Fprintln(term, err.Error())
		return true
	}
	fmt.Fprintf(term, "%v\n", report)
	return true
}

func (t *monitorTool) killreq(term *terminal.Terminal, args []string) bool {
	if len(args) != 1 {
		fmt.Fprintln(term, "'killreq' takes exactly one argument")
		return true
	}
	if err := t.request(
		http.MethodDelete, "/tunnel/"+args[0], nil); err != nil {
		fmt.Fprintln(term, err.Error())
		return true
	}
	fmt.Fprintln(term, "Done")
	return true
}

func (t *monitorTool) request(
	method, uri string, optPtrResp interface{}) error {
	req, err := http.NewRequest(method, t.addr+uri, nil)
	if err != nil {
		return err
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() // nolint: errcheck
	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("request status %s: %s", resp.Status, string(body))
	}
	if optPtrResp != nil {
		decoder := json.NewDecoder(resp.Body)
		return decoder.Decode(optPtrResp)
	}
	return nil
}

func (monitorTool) formatSeconds(seconds float64) string {
	secsInt := int64(seconds) * int64(time.Second)
	return time.Duration(secsInt).String()
}
