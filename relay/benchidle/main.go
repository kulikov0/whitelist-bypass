// Idle-cost benchmark for the VP8 writer: how much CPU the tunnel burns when
// no data flows at all. Run it on the patched and on the original tree and
// compare the numbers.
package main

import (
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/pion/webrtc/v4"

	"whitelist-bypass/relay/tunnel"
)

const (
	tracks  = 1
	measure = 30 * time.Second
)

// withKCP=1 wraps the tunnel in MultiTrackKCPTunnel (the phone's Reliable KCP).

func cpuTime() time.Duration {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		panic(err)
	}
	u := time.Duration(ru.Utime.Sec)*time.Second + time.Duration(ru.Utime.Usec)*time.Microsecond
	s := time.Duration(ru.Stime.Sec)*time.Second + time.Duration(ru.Stime.Usec)*time.Microsecond
	return u + s
}

func main() {
	obf, err := tunnel.NewTunnelObfuscator([]byte("benchmark-secret-key"))
	if err != nil {
		fmt.Println("obfuscator:", err)
		os.Exit(1)
	}
	quiet := func(string, ...any) {}

	tuns := make([]*tunnel.VP8DataTunnel, 0, tracks)
	for i := 0; i < tracks; i++ {
		track, err := webrtc.NewTrackLocalStaticSample(
			webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000},
			fmt.Sprintf("bench-%d", i), "bench-stream",
		)
		if err != nil {
			fmt.Println("track:", err)
			os.Exit(1)
		}
		t := tunnel.NewVP8DataTunnel(track, obf, quiet)
		t.Start(24, 30)
		tuns = append(tuns, t)
	}

	mt := tunnel.NewMultiTrackTunnel(tuns)
	label := "vp8 only"
	if os.Getenv("withKCP") == "1" {
		tunnel.NewMultiTrackKCPTunnel(mt, quiet)
		label = "vp8 + reliable KCP"
	}

	// Let the writer settle, then measure pure idle.
	time.Sleep(6 * time.Second)

	start := cpuTime()
	wall := time.Now()
	time.Sleep(measure)
	used := cpuTime() - start
	elapsed := time.Since(wall)

	for _, t := range tuns {
		t.Stop()
	}

	fmt.Printf("mode:            %s\n", label)
	fmt.Printf("tracks:          %d\n", tracks)
	fmt.Printf("window:          %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("cpu time:        %s\n", used.Round(time.Millisecond))
	fmt.Printf("core load:       %.3f%%\n", float64(used)/float64(elapsed)*100)
}
