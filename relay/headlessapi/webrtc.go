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

func resolveProfile(options Options) headless.Profile {
	if options.Profile == (headless.Profile{}) {
		return headless.ChromeWindows
	}

	return options.Profile
}

func WebRTCSettingEngine(options Options) (webrtc.SettingEngine, error) {
	profile := resolveProfile(options)

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

func buildMediaEngine(options Options) (*webrtc.MediaEngine, error) {
	mediaEngine := &webrtc.MediaEngine{}
	if err := mediaEngine.RegisterDefaultCodecs(); err != nil {
		return nil, fmt.Errorf("register default codecs: %w", err)
	}
	if err := resolveProfile(options).RegisterHeaderExtensions(mediaEngine); err != nil {
		return nil, err
	}

	return mediaEngine, nil
}

func WebRTCAPI(options Options, apiOptions ...func(*webrtc.API)) (*webrtc.API, error) {
	settingEngine, err := WebRTCSettingEngine(options)
	if err != nil {
		return nil, err
	}
	mediaEngine, err := buildMediaEngine(options)
	if err != nil {
		return nil, err
	}

	allAPIOptions := make([]func(*webrtc.API), 0, len(apiOptions)+2)
	allAPIOptions = append(allAPIOptions, webrtc.WithMediaEngine(mediaEngine))
	allAPIOptions = append(allAPIOptions, apiOptions...)
	allAPIOptions = append(allAPIOptions, webrtc.WithSettingEngine(settingEngine))

	return webrtc.NewAPI(allAPIOptions...), nil
}
