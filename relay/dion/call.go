package dion

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/pion/rtp/codecs"
	"github.com/kulikov0/headless-client/webrtc"

	"whitelist-bypass/relay/common"
	"whitelist-bypass/relay/tunnel"
)

const (
	sendVideoMidIndex       = 12
	sendScreenShareMidIndex = 13
	recvScreenShareMidStr   = "14"
	defaultRecvVideoMid     = "1"
	recvVideoMidCount       = 9

	creatorVP8FPS   = 24
	creatorVP8Batch = 10
	joinerVP8FPS    = 24
	joinerVP8Batch  = 5
)

type Role string

const (
	RoleCreator Role = "creator"
	RoleJoiner  Role = "joiner"
)

// CallConfig configures a Call lifecycle. Auth must already have a valid
// access token (caller did LoadCookiesFromFile + EnsureValidToken). Event
// must be a usable EventInfo (CreateRoom or GetEventBySlug result).
type CallConfig struct {
	Auth        *Session
	Event       *EventInfo
	Obfuscator  *tunnel.TunnelObfuscator
	DisplayName string
	LogFn       func(string, ...any)
	RecvMid     string
	Role        Role

	// SettingEngine, NetDialContext, and ResolveICEHost are forwarded to Pion
	// and the WebSocket dialer. All three are no-ops if nil (desktop default).
	// They exist so the Android relay can plug in AndroidNet plus stdin-based
	// DNS resolution before the VPN starts intercepting traffic.
	SettingEngine  *webrtc.SettingEngine
	NetDialContext func(ctx context.Context, network, addr string) (net.Conn, error)
	ResolveICEHost func(host string) (string, error)
}

// PeerEntry tracks one remote peer's signaling state.
type PeerEntry struct {
	SessionID string
	UserID    string
	Name      string
	CamState  bool
	JoinedAt  time.Time
}

// Call drives one full DION room session: signaling, Pion peer, VP8 send
// track on mid=12, OnTrack reader on the bound recv mid, plus discovery and
// subscription to other peers via conf_speakers_state and get_video_from_user.
//
// Lifecycle: NewCall(cfg) -> Start() -> wait OnConnected(tunnel.DataTunnel) ->
// use tunnel via RelayBridge -> wait Done() (read-loop or ICE death) -> Close().
type Call struct {
	cfg         CallConfig
	signaling   *SignalingClient
	peer        *PionPeer
	sendTrack   *webrtc.TrackLocalStaticSample
	vp8tun      *tunnel.VP8DataTunnel
	mySessionID string

	peersMu    sync.Mutex
	peersByID  map[string]*PeerEntry
	subscribed map[string]bool
	peerToMid  map[string]string
	freeMids   []string
	pendingSubs []string

	onConnectedFired atomic.Bool

	OnConnected   func(tunnel.DataTunnel)
	OnPeerRestart func()
	OnRemoteSDP   func(sdp string)

	done     chan struct{}
	closeOnce sync.Once
}

func NewCall(cfg CallConfig) *Call {
	if cfg.LogFn == nil {
		cfg.LogFn = log.Printf
	}
	if cfg.Role == "" {
		cfg.Role = RoleCreator
	}
	if cfg.RecvMid == "" {
		cfg.RecvMid = defaultRecvVideoMid
	}
	var freeMids []string
	if cfg.Role == RoleJoiner {
		freeMids = []string{recvScreenShareMidStr}
	} else {
		freeMids = make([]string, 0, recvVideoMidCount)
		for midIndex := 1; midIndex < recvVideoMidCount; midIndex++ {
			freeMids = append(freeMids, fmt.Sprintf("%d", midIndex))
		}
		freeMids = append(freeMids, "0")
	}
	return &Call{
		cfg:        cfg,
		peersByID:  make(map[string]*PeerEntry),
		subscribed: make(map[string]bool),
		peerToMid:  make(map[string]string),
		freeMids:   freeMids,
		done:       make(chan struct{}),
	}
}

func (c *Call) Done() <-chan struct{} { return c.done }

func (c *Call) SessionID() string { return c.mySessionID }

func (c *Call) Close() {
	c.closeOnce.Do(func() {
		if c.vp8tun != nil {
			c.vp8tun.Stop()
		}
		if c.signaling != nil {
			c.signaling.Close()
		}
		if c.peer != nil {
			c.peer.Close()
		}
	})
}

// Start runs the full lifecycle to the point where the VP8 tunnel is up and
// OnConnected has fired. It returns nil on success; from that point the call
// continues in the background until ICE death or signaling read-loop end, at
// which point Done() is closed.
func (c *Call) Start() error {
	sessionID := uuid.New().String()
	c.mySessionID = sessionID
	c.cfg.LogFn("[call] my session_id=%s", sessionID)

	wss, err := c.cfg.Auth.ConnectWSS(sessionID)
	if err != nil {
		return fmt.Errorf("ConnectWSS: %w", err)
	}

	signaling, err := DialSignaling(wss.URL, SignalingDialOptions{
		UserAgent:      c.cfg.Auth.Device.UserAgent,
		LogFn:          c.cfg.LogFn,
		NetDialContext: c.cfg.NetDialContext,
	})
	if err != nil {
		return fmt.Errorf("DialSignaling: %w", err)
	}
	c.signaling = signaling
	if err := signaling.WaitConnected(15 * time.Second); err != nil {
		return fmt.Errorf("WaitConnected: %w", err)
	}

	youJoinedChan := make(chan YouJoinedParams, 1)
	sdpAnswerChan := make(chan SDPAnswerParams, 4)
	var onceYouJoined sync.Once

	signaling.OnYouJoined = func(params YouJoinedParams) {
		onceYouJoined.Do(func() { youJoinedChan <- params })
	}
	signaling.OnSDPAnswer = func(answerSDP string, transceivers []TransceiverDesc) {
		select {
		case sdpAnswerChan <- SDPAnswerParams{Answer: answerSDP, Transceivers: transceivers}:
		default:
		}
	}
	signaling.OnSpeakerJoined = c.handleSpeakerJoined
	signaling.OnSpeakerDisconnected = c.handleSpeakerDisconnected
	signaling.OnSpeakerCamStateChanged = c.handleSpeakerCamStateChanged
	signaling.OnConfSpeakersState = c.handleConfSpeakersState
	signaling.OnGetVideoFromUserResponse = c.handleGetVideoFromUserResponse
	signaling.OnGetScreenSharingFromUserResponse = c.handleGetScreenSharingFromUserResponse

	readLoopDone := make(chan error, 1)
	go func() { readLoopDone <- signaling.ReadLoop() }()

	if err := signaling.Subscribe(c.cfg.Event.ID, sessionID); err != nil {
		return fmt.Errorf("Subscribe: %w", err)
	}

	var youJoined YouJoinedParams
	select {
	case youJoined = <-youJoinedChan:
	case err := <-readLoopDone:
		return fmt.Errorf("read loop ended before you_joined: %v", err)
	case <-time.After(15 * time.Second):
		return fmt.Errorf("timeout waiting for you_joined")
	}
	c.cfg.LogFn("[call] you_joined ice_servers=%d", len(youJoined.IceServers))

	pionAPI := NewPionAPI(c.cfg.SettingEngine)
	iceServers := ResolveICEServerHosts(youJoined.IceServers, c.cfg.ResolveICEHost, c.cfg.LogFn)
	peer, err := BuildPionPeer(pionAPI, iceServers)
	if err != nil {
		return fmt.Errorf("BuildPionPeer: %w", err)
	}
	c.peer = peer

	sendMidIndex := sendVideoMidIndex
	trackLabel := "dion-tunnel-" + sessionID
	if c.cfg.Role == RoleCreator {
		sendMidIndex = sendScreenShareMidIndex
		trackLabel = "dion-tunnel-screen-" + sessionID
	}
	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000},
		"video", trackLabel,
	)
	if err != nil {
		return fmt.Errorf("NewTrackLocalStaticSample: %w", err)
	}
	c.sendTrack = track
	if len(peer.Transceivers) <= sendMidIndex {
		return fmt.Errorf("transceiver layout short, have %d", len(peer.Transceivers))
	}
	sender := peer.Transceivers[sendMidIndex].Sender()
	if sender == nil {
		return fmt.Errorf("mid=%d sender nil", sendMidIndex)
	}
	if err := sender.ReplaceTrack(track); err != nil {
		return fmt.Errorf("ReplaceTrack: %w", err)
	}
	go tunnel.DrainSenderRTCP(sender)
	c.cfg.LogFn("[call] role=%s attached send track to mid=%d", c.cfg.Role, sendMidIndex)

	peer.PC.OnTrack(func(remoteTrack *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		c.cfg.LogFn("[call] OnTrack id=%q kind=%s codec=%s ssrc=%d",
			remoteTrack.ID(), remoteTrack.Kind().String(), remoteTrack.Codec().MimeType, remoteTrack.SSRC())
		if remoteTrack.Codec().MimeType != webrtc.MimeTypeVP8 {
			go drainTrack(remoteTrack)
			return
		}
		go c.readVP8Track(remoteTrack)
	})

	var pendingMu sync.Mutex
	pendingCandidates := make([]webrtc.ICECandidateInit, 0, 32)
	remoteSet := false
	sendCandidate := func(cand webrtc.ICECandidateInit) {
		entry := ICECandidateJSON{Candidate: cand.Candidate}
		if cand.SDPMid != nil {
			m := *cand.SDPMid
			entry.SDPMid = &m
		}
		if cand.SDPMLineIndex != nil {
			i := *cand.SDPMLineIndex
			entry.SDPMLineIndex = &i
		}
		if cand.UsernameFragment != nil {
			entry.UsernameFragment = *cand.UsernameFragment
		}
		if err := signaling.SendICECandidates([]ICECandidateJSON{entry}); err != nil {
			c.cfg.LogFn("[ice] SendICECandidates: %v", err)
		}
	}
	flushPending := func() {
		pendingMu.Lock()
		toFlush := pendingCandidates
		pendingCandidates = nil
		pendingMu.Unlock()
		for _, cand := range toFlush {
			sendCandidate(cand)
		}
	}
	peer.PC.OnICECandidate(func(cand *webrtc.ICECandidate) {
		if cand == nil {
			return
		}
		init := cand.ToJSON()
		pendingMu.Lock()
		alreadyRemote := remoteSet
		if !alreadyRemote {
			pendingCandidates = append(pendingCandidates, init)
		}
		pendingMu.Unlock()
		if alreadyRemote {
			sendCandidate(init)
		}
	})

	iceConnected := make(chan struct{}, 1)
	iceDead := make(chan webrtc.ICEConnectionState, 1)
	peer.PC.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		c.cfg.LogFn("[ice] state=%s", state.String())
		switch state {
		case webrtc.ICEConnectionStateConnected, webrtc.ICEConnectionStateCompleted:
			select {
			case iceConnected <- struct{}{}:
			default:
			}
		case webrtc.ICEConnectionStateFailed, webrtc.ICEConnectionStateClosed:
			select {
			case iceDead <- state:
			default:
			}
		}
	})

	envelope, _, err := peer.CreateAndSetOffer()
	if err != nil {
		return fmt.Errorf("CreateAndSetOffer: %w", err)
	}
	offerParams := SDPOfferParams{
		MicState:              false,
		CamState:              false,
		NoiseSuppressionState: true,
		ScreenSharingQuality:  "default",
		Datachannels:          peer.DatachannelDescs,
		Transceivers:          peer.TransceiverDescs,
		Offer:                 envelope,
	}
	if err := signaling.SendSDPOffer(offerParams); err != nil {
		return fmt.Errorf("SendSDPOffer: %w", err)
	}

	var answer SDPAnswerParams
	select {
	case answer = <-sdpAnswerChan:
	case err := <-readLoopDone:
		return fmt.Errorf("read loop ended before sdp_answer: %v", err)
	case <-time.After(20 * time.Second):
		return fmt.Errorf("timeout waiting for sdp_answer")
	}
	if c.OnRemoteSDP != nil {
		c.OnRemoteSDP(answer.Answer)
	}
	if err := peer.PC.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answer.Answer,
	}); err != nil {
		return fmt.Errorf("SetRemoteDescription: %w", err)
	}
	pendingMu.Lock()
	remoteSet = true
	pendingMu.Unlock()
	flushPending()

	select {
	case <-iceConnected:
	case state := <-iceDead:
		return fmt.Errorf("ICE died before connected: %s", state.String())
	case err := <-readLoopDone:
		return fmt.Errorf("read loop ended before ICE connected: %v", err)
	case <-time.After(30 * time.Second):
		return fmt.Errorf("timeout waiting for ICE connected; state=%s", peer.PC.ICEConnectionState().String())
	}
	c.cfg.LogFn("[ice] connected")

	fps, batch := joinerVP8FPS, joinerVP8Batch
	if c.cfg.Role == RoleCreator {
		fps, batch = creatorVP8FPS, creatorVP8Batch
	}
	c.vp8tun = tunnel.NewVP8DataTunnel(c.sendTrack, c.cfg.Obfuscator, c.cfg.LogFn)
	c.vp8tun.Start(fps, batch)
	c.fireOnConnected(c.vp8tun)

	if c.cfg.Role == RoleCreator {
		if err := signaling.SendScreenSharingSwitchOn(); err != nil {
			c.cfg.LogFn("[call] SendScreenSharingSwitchOn: %v", err)
		} else {
			c.cfg.LogFn("[call] sent screensharing_switch_on")
		}
		if err := signaling.SendScreensharingQualityChange("good"); err != nil {
			c.cfg.LogFn("[call] SendScreensharingQualityChange: %v", err)
		} else {
			c.cfg.LogFn("[call] sent screensharing_quality_change=good")
		}
	} else {
		if err := signaling.SendCamStateChange(true); err != nil {
			c.cfg.LogFn("[call] SendCamStateChange: %v", err)
		} else {
			c.cfg.LogFn("[call] sent cam_state_change=true")
		}
	}

	go c.discoverPeersAndSubscribe()
	go c.runStatReporter()

	go func() {
		defer close(c.done)
		select {
		case state := <-iceDead:
			c.cfg.LogFn("[call] ICE went to %s", state.String())
		case err := <-readLoopDone:
			c.cfg.LogFn("[call] read loop ended: %v", err)
		}
	}()

	return nil
}

func (c *Call) fireOnConnected(tun tunnel.DataTunnel) {
	if !c.onConnectedFired.CompareAndSwap(false, true) {
		return
	}
	if c.OnConnected != nil {
		c.OnConnected(tun)
	}
}

func (c *Call) handleSpeakerJoined(params SpeakerJoinedParams) {
	if params.SessionID == c.mySessionID {
		return
	}
	c.peersMu.Lock()
	_, wasKnown := c.peersByID[params.SessionID]
	c.peersByID[params.SessionID] = &PeerEntry{
		SessionID: params.SessionID,
		UserID:    params.UserID,
		Name:      params.Name,
		CamState:  params.CamState,
		JoinedAt:  time.Now(),
	}
	var toKick []string
	if !wasKnown {
		for id := range c.peersByID {
			if id != params.SessionID {
				toKick = append(toKick, id)
			}
		}
	}
	c.peersMu.Unlock()
	c.cfg.LogFn("[call] speaker_joined session_id=%s name=%q cam=%v", params.SessionID, params.Name, params.CamState)

	for _, staleID := range toKick {
		if err := c.signaling.SendKickOne(staleID); err != nil {
			c.cfg.LogFn("[call] SendKickOne(%s): %v", staleID, err)
			continue
		}
		c.cfg.LogFn("[call] kicked stale peer session_id=%s for newcomer=%s", staleID, params.SessionID)
		c.peersMu.Lock()
		delete(c.peersByID, staleID)
		delete(c.subscribed, staleID)
		c.releaseMidLocked(staleID)
		c.peersMu.Unlock()
	}
	if len(toKick) > 0 && c.OnPeerRestart != nil {
		c.cfg.LogFn("[call] firing OnPeerRestart from kick path (kicked=%d newcomer=%s)", len(toKick), params.SessionID)
		c.OnPeerRestart()
	}

	if c.cfg.Role == RoleJoiner || params.CamState {
		c.subscribeIfNeeded(params.SessionID)
	}
}

func (c *Call) handleSpeakerDisconnected(params SpeakerDisconnectedParams) {
	c.peersMu.Lock()
	delete(c.peersByID, params.SessionID)
	delete(c.subscribed, params.SessionID)
	c.releaseMidLocked(params.SessionID)
	var freshestUnsubscribed string
	var freshestAt time.Time
	for sid, entry := range c.peersByID {
		if c.subscribed[sid] {
			continue
		}
		if entry.JoinedAt.After(freshestAt) {
			freshestAt = entry.JoinedAt
			freshestUnsubscribed = sid
		}
	}
	hasFreeMid := len(c.freeMids) > 0
	c.peersMu.Unlock()
	c.cfg.LogFn("[call] speaker_disconnected session_id=%s", params.SessionID)
	if freshestUnsubscribed != "" && hasFreeMid {
		c.cfg.LogFn("[call] claiming freed mid for unsubscribed peer %s", freshestUnsubscribed)
		c.subscribeIfNeeded(freshestUnsubscribed)
	}
}

func (c *Call) handleSpeakerCamStateChanged(params SpeakerCamStateChangedParams) {
	if params.SessionID == c.mySessionID {
		return
	}
	c.peersMu.Lock()
	if entry, ok := c.peersByID[params.SessionID]; ok {
		entry.CamState = params.CamState
	} else {
		c.peersByID[params.SessionID] = &PeerEntry{SessionID: params.SessionID, CamState: params.CamState, JoinedAt: time.Now()}
	}
	c.peersMu.Unlock()
	if common.Debug {
		c.cfg.LogFn("[call] speaker_cam_state_changed session_id=%s cam=%v", params.SessionID, params.CamState)
	}
	if params.CamState {
		c.subscribeIfNeeded(params.SessionID)
	}
}

func (c *Call) handleConfSpeakersState(response ConfSpeakersStateResponse) {
	for _, entry := range response.Speakers {
		if entry.SessionID == c.mySessionID || entry.SessionID == "" {
			continue
		}
		c.peersMu.Lock()
		c.peersByID[entry.SessionID] = &PeerEntry{
			SessionID: entry.SessionID,
			UserID:    entry.UserID,
			Name:      entry.Name,
			CamState:  entry.CamState,
			JoinedAt:  time.Now(),
		}
		c.peersMu.Unlock()
		if c.cfg.Role == RoleJoiner || entry.CamState {
			c.subscribeIfNeeded(entry.SessionID)
		}
	}
}

func (c *Call) discoverPeersAndSubscribe() {
	time.Sleep(500 * time.Millisecond)
	if err := c.signaling.SendConfSpeakersState(DefaultConfSpeakersStateRequest()); err != nil {
		c.cfg.LogFn("[call] SendConfSpeakersState: %v", err)
	}
}

func (c *Call) subscribeIfNeeded(peerSessionID string) {
	c.peersMu.Lock()
	if c.subscribed[peerSessionID] {
		c.peersMu.Unlock()
		return
	}
	entry := c.peersByID[peerSessionID]
	if entry == nil {
		c.peersMu.Unlock()
		return
	}
	if len(c.freeMids) == 0 {
		c.peersMu.Unlock()
		c.cfg.LogFn("[call] no free recv mid for peer %s, ignoring", peerSessionID)
		return
	}
	mid := c.freeMids[0]
	c.freeMids = c.freeMids[1:]
	c.peerToMid[peerSessionID] = mid
	c.subscribed[peerSessionID] = true
	c.pendingSubs = append(c.pendingSubs, peerSessionID)
	c.peersMu.Unlock()
	var sendErr error
	if c.cfg.Role == RoleJoiner {
		sendErr = c.signaling.SendGetScreenSharingFromUser(GetScreenSharingFromUserRequest{
			SessionID:     entry.SessionID,
			TransceiverID: mid,
			UserID:        entry.UserID,
		})
	} else {
		sendErr = c.signaling.SendGetVideoFromUser(GetVideoFromUserRequest{
			SessionID:     entry.SessionID,
			TransceiverID: mid,
			UserID:        entry.UserID,
			Username:      entry.Name,
		})
	}
	if sendErr != nil {
		c.cfg.LogFn("[call] subscribe to peer %s failed: %v", peerSessionID, sendErr)
		c.peersMu.Lock()
		delete(c.subscribed, peerSessionID)
		delete(c.peerToMid, peerSessionID)
		c.freeMids = append(c.freeMids, mid)
		if len(c.pendingSubs) > 0 && c.pendingSubs[len(c.pendingSubs)-1] == peerSessionID {
			c.pendingSubs = c.pendingSubs[:len(c.pendingSubs)-1]
		}
		c.peersMu.Unlock()
		return
	}
	c.cfg.LogFn("[call] subscribed to %s on mid=%s", peerSessionID, mid)
	if c.OnPeerRestart != nil {
		c.cfg.LogFn("[call] firing OnPeerRestart from subscribe path (peer=%s)", peerSessionID)
		c.OnPeerRestart()
	}
}

func (c *Call) handleGetVideoFromUserResponse(resp GetVideoFromUserResponse, errCode int, errMsg string) {
	c.handleSubscribeResponse("get_video_from_user", resp.SessionID, resp.TransceiverID, errCode, errMsg)
}

func (c *Call) handleGetScreenSharingFromUserResponse(resp GetScreenSharingFromUserResponse, errCode int, errMsg string) {
	c.handleSubscribeResponse("get_screensharing_from_user", resp.SessionID, resp.TransceiverID, errCode, errMsg)
}

func (c *Call) handleSubscribeResponse(rpc, sessionID, transceiverID string, errCode int, errMsg string) {
	c.peersMu.Lock()
	if sessionID == "" && len(c.pendingSubs) > 0 {
		sessionID = c.pendingSubs[0]
		c.pendingSubs = c.pendingSubs[1:]
	} else if len(c.pendingSubs) > 0 {
		for i, pending := range c.pendingSubs {
			if pending == sessionID {
				c.pendingSubs = append(c.pendingSubs[:i], c.pendingSubs[i+1:]...)
				break
			}
		}
	}
	if errCode != 0 {
		mid := c.peerToMid[sessionID]
		delete(c.subscribed, sessionID)
		delete(c.peerToMid, sessionID)
		if mid != "" {
			c.freeMids = append(c.freeMids, mid)
		}
		c.peersMu.Unlock()
		c.cfg.LogFn("[call] %s FAILED session=%s mid=%s code=%d msg=%q", rpc, sessionID, mid, errCode, errMsg)
		return
	}
	c.peersMu.Unlock()
	c.cfg.LogFn("[call] %s OK session=%s mid=%s", rpc, sessionID, transceiverID)
}

func (c *Call) releaseMidLocked(peerSessionID string) {
	if mid, ok := c.peerToMid[peerSessionID]; ok {
		delete(c.peerToMid, peerSessionID)
		c.freeMids = append(c.freeMids, mid)
	}
}

func (c *Call) readVP8Track(track *webrtc.TrackRemote) {
	var vp8Pkt codecs.VP8Packet
	var frameBuf []byte
	var lastSeq uint16
	var haveLastSeq bool
	frameValid := false
	for {
		pkt, _, err := track.ReadRTP()
		if err != nil {
			return
		}
		if pkt == nil {
			continue
		}
		if haveLastSeq && pkt.SequenceNumber != lastSeq+1 {
			frameValid = false
			frameBuf = frameBuf[:0]
		}
		lastSeq = pkt.SequenceNumber
		haveLastSeq = true
		vp8Payload, err := vp8Pkt.Unmarshal(pkt.Payload)
		if err != nil {
			frameValid = false
			frameBuf = frameBuf[:0]
			continue
		}
		if vp8Pkt.S == 1 {
			frameBuf = frameBuf[:0]
			frameValid = true
		}
		if !frameValid {
			continue
		}
		frameBuf = append(frameBuf, vp8Payload...)
		if !pkt.Marker {
			continue
		}
		if c.vp8tun != nil {
			c.vp8tun.HandleFrame(frameBuf)
		}
		frameBuf = frameBuf[:0]
		frameValid = false
	}
}

func (c *Call) runStatReporter() {
	select {
	case <-c.done:
		return
	case <-time.After(1500 * time.Millisecond):
	}
	if err := c.signaling.SendPCIceStat(); err != nil {
		c.cfg.LogFn("[call] SendPCIceStat: %v", err)
	} else {
		c.cfg.LogFn("[call] sent pc_ice_stat")
	}
	c.sendStatReport()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			c.sendStatReport()
		}
	}
}

func (c *Call) sendStatReport() {
	report := ClientStatReport{
		ReportTimeUnixMS: time.Now().UnixMilli(),
		Connection:       ClientStatConnection{},
	}
	report.Audio.In = ClientStatAudioIn{Codec: "opus", IsEnabled: true, Mid: 9}
	report.Video.In = c.buildVideoInStats()
	outStat := ClientStatVideoOut{
		Mid:             sendVideoMidIndex,
		Codec:           "VP8",
		IsEnabled:       true,
		Resolution:      ClientStatResolution{Width: 1280, Height: 720},
		Framerate:       c.vp8tun.FPS(),
		ScalabilityMode: "L1T1",
	}
	report.Video.Out = outStat
	report.Video.OutV2 = []ClientStatVideoOut{outStat}
	if err := c.signaling.SendClientStatZip(report); err != nil {
		c.cfg.LogFn("[call] SendClientStatZip: %v", err)
	}
}

func (c *Call) buildVideoInStats() []ClientStatVideoIn {
	c.peersMu.Lock()
	defer c.peersMu.Unlock()
	out := make([]ClientStatVideoIn, 0, len(c.peerToMid))
	for sessionID, midStr := range c.peerToMid {
		midInt := 0
		fmt.Sscanf(midStr, "%d", &midInt)
		out = append(out, ClientStatVideoIn{
			Codec:      "VP8",
			IsEnabled:  true,
			Mid:        midInt,
			Resolution: ClientStatResolution{Width: 1280, Height: 720},
			Framerate:  c.vp8tun.FPS(),
			SessionID:  sessionID,
		})
	}
	return out
}

func drainTrack(track *webrtc.TrackRemote) {
	buf := make([]byte, 1500)
	for {
		if _, _, err := track.Read(buf); err != nil {
			return
		}
	}
}
