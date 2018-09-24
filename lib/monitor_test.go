package lib

import (
	"fmt"
	"io"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestMonitor(t *testing.T) {
	require := require.New(t)
	const transferInterval = 15 * time.Millisecond
	const baseUploadPerBlock = 123
	const baseDownloadPerBlock = 321
	const numberTunnels = 100
	const numberReaders = 10
	const readRepeat = 10
	const readInterval = 300 * time.Millisecond

	monitorUpdateInterval = 200 * time.Millisecond

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
			tunnelMonitor := monitor.OpenTunnelMonitor(
				testProxyRequest(i), name("Rule"), name("Downstream"),
				name("Upstream"), nil, name("BoundAddr"), cancelFuncs[i])
			defer tunnelMonitor.Close()
			tunnelStartWg.Done()
			for {
				select {
				case <-stopCh:
					return
				case _ = <-tickers[i].C:
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
		go func(i int) {
			defer readerWg.Done()
			for j := 0; j < readRepeat; j++ {
				report := monitor.Report()
				require.Len(report.Tunnels, numberTunnels)
				lastUploaded := make([]uint64, numberTunnels)
				lastDownloaded := make([]uint64, numberTunnels)
				var totalUploadSpeed float32
				var totalDownloadSpeed float32
				for k := 0; k < numberTunnels; k++ {
					r := report.Tunnels[k]
					idx, err := strconv.Atoi(r.RequestID)
					require.NoError(err)
					name := func(pfx string) string {
						return pfx + strconv.Itoa(idx)
					}
					require.Equal(name(""), r.RequestID)
					require.Equal(name("Rule"), r.Rule)
					require.Equal(name("Downstream"), r.Downstream)
					require.Empty(r.ClientIDs)
					require.Equal(name("ClientAddr"), r.ClientAddr)
					require.Equal(name("target.addr:"), r.TargetAddr)
					require.Equal(name("Upstream"), r.Upstream)
					require.Empty(r.ServerIDs)
					require.Equal(name("BoundAddr"), r.BoundAddr)
					expUploadSpeed := float32(idx+1) * baseUploadPerBlock *
						float32(time.Second) / float32(transferInterval)
					expDownloadSpeed := float32(idx+1) * baseDownloadPerBlock *
						float32(time.Second) / float32(transferInterval)
					require.InEpsilon(expUploadSpeed, r.UploadSpeed, 0.1)
					require.InEpsilon(expDownloadSpeed, r.DownloadSpeed, 0.1)
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
		}(i)
	}
	readerWg.Wait()
	for i := 0; i < numberTunnels; i++ {
		tickers[i].Stop()
		cancelFuncs[i]()
	}
	tunnelWg.Wait()
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
