package lib

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// AppMonitor records and reports runtime statistics of an thestral app.
type AppMonitor struct {
	tunnelMonitors sync.Map // ReqID (string) -> *TunnelMonitor
}

// AppMonitorReport is the statistics report generated by AppMonitor.
type AppMonitorReport struct {
	Tunnels []*TunnelMonitorReport
}

// Start the AppMonitor.
func (m *AppMonitor) Start(path string, updateInterval time.Duration) {
	go func() {
		tickCh := time.Tick(updateInterval)
		for {
			_ = <-tickCh
			m.updateEpoch()
		}
	}()

	if len(path) == 0 {
		path = "/"
	} else {
		if path[0] != '/' {
			path = "/" + path
		}
		if path[len(path)-1] != '/' {
			path = path + "/"
		}
	}
	m.registerRPCHandlers(path)
}

func (m *AppMonitor) registerRPCHandlers(path string) {
	// full report
	http.HandleFunc("/debug/monitor"+path,
		func(w http.ResponseWriter, r *http.Request) {
			if reportJSONBytes, err :=
				json.MarshalIndent(m.Report(), "", "  "); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(fmt.Sprintf(
					"Failed to generate monitor report: %s", err.Error())))
			} else {
				w.Header().Set("Content-Type", "text/json; charset=utf-8")
				_, _ = w.Write(reportJSONBytes)
			}
		})
	// single tunnel
	// HTTP DELETE: kill the tunnel
	// Other methods: report the tunnel report
	tunnelMonitorBaseURI := "/debug/monitor" + path + "tunnel/"
	tunnelMonitorBaseURILen := len(tunnelMonitorBaseURI)
	http.HandleFunc(tunnelMonitorBaseURI,
		func(w http.ResponseWriter, r *http.Request) {
			if len(r.URL.Path) <= tunnelMonitorBaseURILen {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			reqID := r.URL.Path[tunnelMonitorBaseURILen:]
			if tunnel := m.getTunnelMonitor(reqID); tunnel == nil {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write(
					[]byte(fmt.Sprintf("Tunnel %s not found", reqID)))
			} else if r.Method == http.MethodDelete {
				tunnel.ForceKillTunnel()
			} else if reportJSONBytes, err :=
				json.MarshalIndent(tunnel.Report(), "", "  "); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(fmt.Sprintf(
					"Failed to generate monitor report: %s", err.Error())))
			} else {
				w.Header().Set("Content-Type", "text/json; charset=utf-8")
				_, _ = w.Write(reportJSONBytes)
			}
		})
}

// OpenTunnelMonitor creates a tunnel monitor. The TunnelMonitor must be Closed
// when the tunnel ends.
func (m *AppMonitor) OpenTunnelMonitor(
	req ProxyRequest, rule string, downstream string,
	upstream string, serverIDs []*PeerIdentifier, boundAddr string,
	cancelFunc context.CancelFunc) *TunnelMonitor {
	tm := newTunnelMonitor(
		m, req, rule, downstream, upstream, serverIDs, boundAddr, cancelFunc)
	m.tunnelMonitors.Store(req.ID(), tm)
	return tm
}

func (m *AppMonitor) updateEpoch() {
	m.tunnelMonitors.Range(func(key interface{}, value interface{}) bool {
		value.(*TunnelMonitor).updateEpoch()
		return true
	})
}

// Report generates a AppMonitorReport.
func (m *AppMonitor) Report() (report AppMonitorReport) {
	m.tunnelMonitors.Range(func(key interface{}, value interface{}) bool {
		tunnelReport := value.(*TunnelMonitor).Report()
		report.Tunnels = append(report.Tunnels, &tunnelReport)
		return true
	})
	sort.Slice(report.Tunnels, func(i, j int) bool {
		return report.Tunnels[i].EstablishedSince.After(
			report.Tunnels[j].EstablishedSince)
	})
	return
}

func (m *AppMonitor) getTunnelMonitor(requestID string) *TunnelMonitor {
	if value, ok := m.tunnelMonitors.Load(requestID); ok {
		return value.(*TunnelMonitor)
	}
	return nil
}

// TunnelMonitor records statistics of a proxy tunnel.
type TunnelMonitor struct {
	appMonitor       *AppMonitor
	request          ProxyRequest
	rule             string
	downstream       string
	upstream         string
	serverIDs        []*PeerIdentifier
	boundAddr        string
	establishedSince time.Time
	transferMeter    transferMeter
	cancelFunc       context.CancelFunc
}

// TunnelMonitorReport is the report generated by TunnelMonitor.
type TunnelMonitorReport struct {
	// basic
	RequestID        string
	Rule             string
	EstablishedSince time.Time
	ElapsedTimeSecs  float64
	// downstream info
	Downstream string
	ClientIDs  []*PeerIdentifier
	ClientAddr string
	TargetAddr string
	// upstream info
	Upstream  string
	ServerIDs []*PeerIdentifier
	BoundAddr string
	// statistics
	UploadSpeed     float32
	DownloadSpeed   float32
	BytesUploaded   uint64
	BytesDownloaded uint64
}

func newTunnelMonitor(
	appMonitor *AppMonitor, req ProxyRequest, rule string, downstream string,
	upstream string, serverIDs []*PeerIdentifier, boundAddr string,
	cancelFunc context.CancelFunc) *TunnelMonitor {
	return &TunnelMonitor{
		appMonitor:       appMonitor,
		request:          req,
		rule:             rule,
		downstream:       downstream,
		upstream:         upstream,
		serverIDs:        serverIDs,
		boundAddr:        boundAddr,
		establishedSince: time.Now(),
		cancelFunc:       cancelFunc,
	}
}

func (m *TunnelMonitor) updateEpoch() {
	m.transferMeter.pushHistory()
}

// IncBytesUploaded records the number of bytes in a trunk uploaded.
func (m *TunnelMonitor) IncBytesUploaded(n uint32) {
	m.transferMeter.incUploaded(n)
}

// IncBytesDownloaded records the number of bytes in a trunk downloaded.
func (m *TunnelMonitor) IncBytesDownloaded(n uint32) {
	m.transferMeter.incDownloaded(n)
}

// ForceKillTunnel forcely kill the tunnel.
func (m *TunnelMonitor) ForceKillTunnel() {
	m.cancelFunc()
}

// Close the tunnel monitor. This must be called at the end of the tunnel.
func (m *TunnelMonitor) Close() {
	m.appMonitor.tunnelMonitors.Delete(m.request.ID())
}

// Report the statistics of the tunnel.
func (m *TunnelMonitor) Report() (report TunnelMonitorReport) {
	report.RequestID = m.request.ID()
	report.Rule = m.rule
	report.EstablishedSince = m.establishedSince
	report.ElapsedTimeSecs = time.Now().Sub(m.establishedSince).Seconds()
	report.Downstream = m.downstream
	report.ClientIDs, _ = m.request.GetPeerIdentifiers()
	report.ClientAddr = m.request.PeerAddr()
	report.TargetAddr = m.request.TargetAddr().String()
	report.Upstream = m.upstream
	report.ServerIDs = m.serverIDs
	report.BoundAddr = m.boundAddr
	report.UploadSpeed, report.DownloadSpeed = m.transferMeter.speed()
	report.BytesUploaded, report.BytesDownloaded =
		m.transferMeter.bytesTransferred()
	return
}

// transferMeter measures the speed of a bidirection transfer.
type transferMeter struct {
	bytesUploaded          uint64
	bytesDownloaded        uint64
	bytesUploadedHistory   uint64 // high, low = bytes[t - 2], bytes[t - 1]
	bytesDownloadedHistory uint64 // high, low = bytes[t - 2], bytes[t - 1]
	// gap between the lastest two consecutive lastPushTimes
	lastPushGapNs int64
	// last time we pushed bytesXxx to bytesXxxHistory
	lastPushTime time.Time
}

func (m *transferMeter) incUploaded(n uint32) {
	atomic.AddUint64(&m.bytesUploaded, uint64(n))
}

func (m *transferMeter) incDownloaded(n uint32) {
	atomic.AddUint64(&m.bytesDownloaded, uint64(n))
}

// pushHistory records the current transfered statistics.
// It cannot be called concurrently.
func (m *transferMeter) pushHistory() {
	bytesUploaded := uint32(atomic.LoadUint64(&m.bytesUploaded))
	bytesDownloaded := uint32(atomic.LoadUint64(&m.bytesDownloaded))
	now := time.Now()
	// We should be the ONLY WRITER to the history fields,
	// so we don't need atomic loads for them here.
	upHistory := (m.bytesUploadedHistory << 32) | uint64(bytesUploaded)
	downHistory := (m.bytesDownloadedHistory << 32) | uint64(bytesDownloaded)
	atomic.StoreUint64(&m.bytesUploadedHistory, upHistory)
	atomic.StoreUint64(&m.bytesDownloadedHistory, downHistory)
	if !m.lastPushTime.IsZero() {
		atomic.StoreInt64(
			&m.lastPushGapNs, now.Sub(m.lastPushTime).Nanoseconds())
	}
	// Others SHOULD NOT ACCESS lastPushTime in any case,
	// so we don't use atomic store for it.
	m.lastPushTime = now
}

func (m *transferMeter) bytesTransferred() (up uint64, down uint64) {
	up = atomic.LoadUint64(&m.bytesUploaded)
	down = atomic.LoadUint64(&m.bytesDownloaded)
	return
}

// speed calculates the number of bytes transfered per second.
func (m *transferMeter) speed() (uploadSpeed float32, downloadSpeed float32) {
	lastPushGapNs := atomic.LoadInt64(&m.lastPushGapNs)
	bytesUploadedHistory := atomic.LoadUint64(&m.bytesUploadedHistory)
	bytesDownloadedHistory := atomic.LoadUint64(&m.bytesDownloadedHistory)
	if lastPushGapNs == 0 {
		return 0, 0
	}
	gapSecs := float32(lastPushGapNs/1e9) + float32(lastPushGapNs%1e9)/1e9
	upBytes := uint32(bytesUploadedHistory) - uint32(bytesUploadedHistory>>32)
	downBytes := uint32(bytesDownloadedHistory) - uint32(bytesDownloadedHistory>>32)
	uploadSpeed = float32(upBytes) / gapSecs
	downloadSpeed = float32(downBytes) / gapSecs
	return
}