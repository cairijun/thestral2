package lib

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestMonitor(t *testing.T) {
	require := require.New(t)
	const transferInterval = 15 * time.Millisecond
	const baseUploadPerBlock = 123
	const baseDownloadPerBlock = 321
	const numberTunnels = 30
	const numberReaders = 10
	const readRepeat = 10
	const readInterval = 400 * time.Millisecond

	oldMonitorUpdateInterval := monitorUpdateInterval
	monitorUpdateInterval = 200 * time.Millisecond
	defer func() { monitorUpdateInterval = oldMonitorUpdateInterval }()

	var tunnelWg sync.WaitGroup
	var tunnelStartWg sync.WaitGroup
	var monitor AppMonitor
	monitor.Start("test_monitor")
	tickers := make([]*time.Ticker, numberTunnels)
	cancelFuncs := make([]func(), numberTunnels)
	tunnelStartWg.Add(numberTunnels)
	for i := 0; i < numberTunnels; i++ {
		tickers[i] = time.NewTicker(transferInterval)
		stopCh := make(chan interface{})
		cancelFuncs[i] = func() { close(stopCh) }
		tunnelWg.Add(1)
		go func(i int) {
			defer tunnelWg.Done()
			name := func(pfx string) string { return pfx + strconv.Itoa(i) }
			latency := time.Millisecond * time.Duration(i)
			tunnelMonitor := monitor.OpenTunnelMonitor(
				testProxyRequest(i), name("Rule"), name("Downstream"),
				name("Upstream"), nil, name("BoundAddr"), latency, cancelFuncs[i])
			defer tunnelMonitor.Close()
			tunnelStartWg.Done()
			for {
				select {
				case <-stopCh:
					return
				case <-tickers[i].C:
					tunnelMonitor.IncBytesUploaded(
						uint32(baseUploadPerBlock * (i + 1)))
					tunnelMonitor.IncBytesDownloaded(
						uint32(baseDownloadPerBlock * (i + 1)))
				}
			}
		}(i)
	}
	time.Sleep(500 * time.Millisecond)
	tunnelStartWg.Wait()

	var readerWg sync.WaitGroup
	for i := 0; i < numberReaders; i++ {
		readerWg.Add(1)
		go func() {
			defer readerWg.Done()
			for j := 0; j < readRepeat; j++ {
				report := monitor.Report()
				require.Len(report.Tunnels, numberTunnels)
				require.Len(report.Upstreams, numberTunnels)
				sort.Slice(report.Tunnels, func(i, j int) bool {
					return report.Tunnels[i].RequestID < report.Tunnels[j].RequestID
				})
				sort.Slice(report.Upstreams, func(i, j int) bool {
					return report.Upstreams[i].Name < report.Upstreams[j].Name
				})
				lastUploaded := make([]uint64, numberTunnels)
				lastDownloaded := make([]uint64, numberTunnels)
				var totalUploadSpeed float32
				var totalDownloadSpeed float32
				for k := 0; k < numberTunnels; k++ {
					r := report.Tunnels[k]
					ur := report.Upstreams[k]
					idx, err := strconv.Atoi(r.RequestID)
					require.NoError(err)
					name := func(pfx string) string {
						return pfx + strconv.Itoa(idx)
					}
					require.Equal(name("Rule"), r.Rule)
					require.Equal(name("Downstream"), r.Downstream)
					require.Empty(r.ClientIDs)
					require.Equal(name("ClientAddr"), r.ClientAddr)
					require.Equal(name("target.addr:"), r.TargetAddr)
					require.Equal(name("Upstream"), r.Upstream)
					require.Equal(name("Upstream"), ur.Name)
					require.Empty(r.ServerIDs)
					require.Equal(name("BoundAddr"), r.BoundAddr)
					require.Equal(float32(idx), r.ConnLatencyMs)
					require.Equal(float32(idx), ur.AvgConnLatencyMs)
					expUploadSpeed := float32(idx+1) * baseUploadPerBlock *
						float32(time.Second) / float32(transferInterval)
					expDownloadSpeed := float32(idx+1) * baseDownloadPerBlock *
						float32(time.Second) / float32(transferInterval)
					require.InEpsilon(expUploadSpeed, r.UploadSpeed, 0.1)
					require.InEpsilon(expUploadSpeed, ur.UploadSpeed, 0.1)
					require.InEpsilon(expDownloadSpeed, r.DownloadSpeed, 0.1)
					require.InEpsilon(expDownloadSpeed, ur.DownloadSpeed, 0.1)
					totalUploadSpeed += r.UploadSpeed
					totalDownloadSpeed += r.DownloadSpeed
					if lastUploaded[idx] != 0 {
						require.InEpsilon(expUploadSpeed*
							float32(readInterval)/float32(time.Second),
							r.BytesUploaded-lastUploaded[idx], 0.1)
					}
					if lastDownloaded[idx] != 0 {
						require.InEpsilon(expDownloadSpeed*
							float32(readInterval)/float32(time.Second),
							r.BytesDownloaded-lastDownloaded[idx], 0.1)
					}
					lastUploaded[idx] = r.BytesUploaded
					lastDownloaded[idx] = r.BytesDownloaded
				}
				require.InEpsilon(totalUploadSpeed, report.UploadSpeed, 0.1)
				require.InEpsilon(totalDownloadSpeed, report.DownloadSpeed, 0.1)
				time.Sleep(readInterval)
			}
		}()
	}
	readerWg.Wait()
	for i := 0; i < numberTunnels; i++ {
		tickers[i].Stop()
		cancelFuncs[i]()
	}
	tunnelWg.Wait()
}

func TestAppMonitorAvgLatErrCnt(t *testing.T) {
	var monitor AppMonitor
	monitor.Start("test_monitor_TestAppMonitorAvgLatErrCnt")
	const errCnt = 10
	for i := 0; i < errCnt; i++ {
		monitor.AddError("")
	}
	for i := 0; i < 100; i++ {
		name := func(pfx string) string { return pfx + strconv.Itoa(i) }
		latency := time.Millisecond * time.Duration(i)
		tunnelMonitor := monitor.OpenTunnelMonitor(
			testProxyRequest(i), name("Rule"), name("Downstream"),
			name("Upstream"), nil, name("BoundAddr"), latency, func() {})
		defer tunnelMonitor.Close()
	}
	report := monitor.Report()
	assert.Equal(t, uint32(errCnt), report.ErrorCount)
	assert.InEpsilon(t, 98.75, report.AvgConnLatencyMs, 1e-3)
}

func TestUpstreamMonitor(t *testing.T) {
	var monitor AppMonitor
	monitor.Start("test_monitor_TestUpstreamMonitor")
	wg := sync.WaitGroup{}
	upName := func(i int) string { return "upstream_" + strconv.Itoa(i) }
	for i := 1; i <= 5; i++ {
		for j := 1; j <= i; j++ {
			wg.Add(1)
			go func(upstream string) {
				defer wg.Done()
				monitor.AddError(upstream)
			}(upName(j))
		}
	}
	wg.Wait()
	for i := 0; i < 5*10; i++ {
		upstream := "upstream_" + strconv.Itoa(i%5+1)
		name := func(pfx string) string { return pfx + strconv.Itoa(i) }
		latency := time.Millisecond * time.Duration(i)
		tunnelMonitor := monitor.OpenTunnelMonitor(
			testProxyRequest(i), name("Rule"), name("Downstream"),
			upstream, nil, name("BoundAddr"), latency, func() {})
		defer tunnelMonitor.Close()
	}
	expectedErrCnts := map[string]uint32{
		"upstream_1": 5,
		"upstream_2": 4,
		"upstream_3": 3,
		"upstream_4": 2,
		"upstream_5": 1,
	}
	expectedAvgLats := map[string]float32{
		"upstream_1": 43.75,
		"upstream_2": 44.75,
		"upstream_3": 45.75,
		"upstream_4": 46.75,
		"upstream_5": 47.75,
	}
	reports := monitor.Report().Upstreams
	for _, report := range reports {
		require.Equal(t, expectedErrCnts[report.Name], report.ErrorCount)
		require.InEpsilon(
			t, expectedAvgLats[report.Name], report.AvgConnLatencyMs, 1e-3)
	}
}

type testProxyRequest int

func (r testProxyRequest) GetPeerIdentifiers() ([]*PeerIdentifier, error) {
	return nil, nil
}

func (r testProxyRequest) PeerAddr() string {
	return fmt.Sprintf("ClientAddr%d", int(r))
}

func (r testProxyRequest) TargetAddr() Address {
	return &DomainNameAddr{DomainName: "target.addr", Port: uint16(r)}
}

func (r testProxyRequest) Success(addr Address) io.ReadWriteCloser {
	panic("not implemented")
}

func (r testProxyRequest) Fail(err *ProxyError) {
	panic("not implemented")
}

func (r testProxyRequest) ID() string {
	return strconv.Itoa(int(r))
}

func (r testProxyRequest) Logger() *zap.SugaredLogger {
	panic("not implemented")
}
