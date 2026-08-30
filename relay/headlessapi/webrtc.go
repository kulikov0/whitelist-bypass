package headlessapi

import (
	"fmt"

	headless "github.com/kulikov0/headless-client"
	"github.com/kulikov0/headless-client/webrtc"
)

type Options struct {
	Profile            headless.Profile
	AnswerAsDTLSServer bool
	Configure          func(*webrtc.SettingEngine)
}

func WebRTCSettingEngine(options Options) (webrtc.SettingEngine, error) {
	profile := options.Profile
	if profile == (headless.Profile{}) {
		profile = headless.ChromeWindows
	}

	settingEngine, err := profile.SettingEngine()
	if err != nil {
		return webrtc.SettingEngine{}, fmt.Errorf("build setting engine: %w", err)
	}
	if options.Configure != nil {
		options.Configure(&settingEngine)
	}
	if options.AnswerAsDTLSServer {
		if err := settingEngine.SetAnsweringDTLSRole(webrtc.DTLSRoleServer); err != nil {
			return webrtc.SettingEngine{}, fmt.Errorf("set answering dtls role: %w", err)
		}
	}

	return settingEngine, nil
}

func WebRTCAPI(options Options, apiOptions ...func(*webrtc.API)) (*webrtc.API, error) {
	settingEngine, err := WebRTCSettingEngine(options)
	if err != nil {
		return nil, err
	}

	return webrtc.NewAPI(append(apiOptions, webrtc.WithSettingEngine(settingEngine))...), nil
}
