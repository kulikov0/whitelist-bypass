package tunnel

import (
	"fmt"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

// TestIdleEmissionRate measures what an idle tunnel actually costs: how many
// frames per second it puts on the wire with nothing to send, and how much CPU
// the writer burns doing it. The frame rate is the number that matters on a
// phone - every frame is a packet that keeps the radio out of sleep.
//
// Run against this tree and against the original one and compare:
//
//	go test ./tunnel/ -run TestIdleEmissionRate -v
func TestIdleEmissionRate(t *testing.T) {
	const (
		warmup  = 5 * time.Second
		measure = 30 * time.Second
	)

	obf, err := NewTunnelObfuscator([]byte("idle-rate-test"))
	if err != nil {
		t.Fatalf("obfuscator: %v", err)
	}
	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000},
		"idle-rate", "idle-rate-stream",
	)
	if err != nil {
		t.Fatalf("track: %v", err)
	}

	tun := NewVP8DataTunnel(track, obf, func(string, ...any) {})
	if os.Getenv("KEEPALIVE_MS") != "" {
		var lo, hi int
		fmt.Sscanf(os.Getenv("KEEPALIVE_MS"), "%d,%d", &lo, &hi)
		tun.SetKeepaliveShape(time.Duration(lo)*time.Millisecond, time.Duration(hi)*time.Millisecond, -1)
	}
	tun.Start(defaultVP8FPS, defaultVP8Batch)
	defer tun.Stop()

	time.Sleep(warmup)

	framesBefore := tun.sentFrames.Load()
	keepBefore := tun.keepaliveFrames.Load()
	cpuBefore := selfCPU()

	time.Sleep(measure)

	frames := tun.sentFrames.Load() - framesBefore
	keepalives := tun.keepaliveFrames.Load() - keepBefore
	cpu := selfCPU() - cpuBefore

	secs := measure.Seconds()
	fmt.Printf("\n  idle window:      %s\n", measure)
	fmt.Printf("  frames emitted:   %d  (%.1f/s)\n", frames, float64(frames)/secs)
	fmt.Printf("  of them keepalive:%d\n", keepalives)
	fmt.Printf("  writer cpu time:  %s  (%.3f%% of a core)\n\n",
		cpu.Round(time.Millisecond), float64(cpu)/float64(measure)*100)
}

func selfCPU() time.Duration {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		panic(err)
	}
	u := time.Duration(ru.Utime.Sec)*time.Second + time.Duration(ru.Utime.Usec)*time.Microsecond
	s := time.Duration(ru.Stime.Sec)*time.Second + time.Duration(ru.Stime.Usec)*time.Microsecond
	return u + s
}
