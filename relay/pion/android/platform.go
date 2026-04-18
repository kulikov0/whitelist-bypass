package android

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/pion/webrtc/v4"
	"whitelist-bypass/relay/common"
)

var stdinReader = bufio.NewReader(os.Stdin)
var stdinMu sync.Mutex

func ReadStdinLine() (string, error) {
	stdinMu.Lock()
	defer stdinMu.Unlock()
	line, err := stdinReader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func RequestResolve(hostname string) (string, error) {
	stdinMu.Lock()
	fmt.Printf("RESOLVE:%s\n", hostname)
	stdinMu.Unlock()
	line, err := ReadStdinLine()
	if err != nil {
		return "", fmt.Errorf("read resolve response: %w", err)
	}
	if line == "" {
		return "", fmt.Errorf("empty resolve for %s", hostname)
	}
	return line, nil
}

type StatusEmitter struct{}

func (StatusEmitter) EmitStatus(status string)  { common.EmitStatus(status) }
func (StatusEmitter) EmitStatusError(msg string) { common.EmitStatusError(msg) }

type PCConfigurer struct{}

func (PCConfigurer) ConfigureSettingEngine(settingEngine *webrtc.SettingEngine) {
	settingEngine.SetNet(&common.AndroidNet{})
}
