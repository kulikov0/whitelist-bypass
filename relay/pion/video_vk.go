package pion

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/pion/webrtc/v4"
	"whitelist-bypass/relay/common"
)

type VKClient struct {
	WSHelper
	pc              *webrtc.PeerConnection
	sampleTrack     *webrtc.TrackLocalStaticSample
	tunnel          *VP8DataTunnel
	logFn           func(string, ...any)
	remoteSet       bool
	pending         []webrtc.ICECandidateInit
	OnConnected     func(DataTunnel)
	dcProducerNotif *webrtc.DataChannel
	dcProducerCmd   *webrtc.DataChannel
}

func NewVKClient(logFn func(string, ...any)) *VKClient {
	if logFn == nil {
		logFn = log.Printf
	}
	return &VKClient{logFn: logFn}
}

func (c *VKClient) HandleSignaling(w http.ResponseWriter, r *http.Request) {
	ws, err := WsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		c.logFn("vk: ws upgrade error: %v", err)
		return
	}
	c.SetConn(ws)
	c.logFn("vk: signaling connected")
	c.ReadMessages(c.handleMessage, c.cleanup)
}

func (c *VKClient) handleMessage(raw []byte) {
	var msg SignalingMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return
	}
	c.logFn("vk: >> msg type=%s id=%d", msg.Type, msg.ID)

	switch msg.Type {
	case "ice-servers":
		c.handleICEServers(msg.Data)
	case "create-offer":
		c.handleCreateOffer(msg.ID)
	case "create-answer":
		c.handleCreateAnswer(msg.ID)
	case "set-local-description":
	case "set-remote-description":
		c.handleSetRemoteDescription(msg.Data, msg.ID)
	case "add-ice-candidate", "remote-ice-candidate":
		c.handleICECandidate(msg.Data)
	case "add-track", "create-data-channel":
	case "reset":
		c.handleReset(msg.ID)
	case "close":
		c.cleanup()
	}
	c.logFn("vk: << msg type=%s id=%d done", msg.Type, msg.ID)
}

func (c *VKClient) createPC(config webrtc.Configuration) error {
	se := webrtc.SettingEngine{}
	se.SetNet(&AndroidNet{})
	se.SetInterfaceFilter(func(iface string) bool { return false })
	api := webrtc.NewAPI(webrtc.WithSettingEngine(se))
	pc, err := api.NewPeerConnection(config)
	if err != nil {
		c.logFn("vk: failed to create PC: %v", err)
		return err
	}
	c.pc = pc

	sampleTrack := AddTunnelTracks(pc, c.logFn, "vk")
	c.sampleTrack = sampleTrack

	// Create DataChannels required by VK SFU.
	// The SFU sends producer-updated (SDP offer) via producerNotification DC.
	ordered := true
	dcNotif, err := pc.CreateDataChannel("producerNotification", &webrtc.DataChannelInit{Ordered: &ordered})
	if err != nil {
		c.logFn("vk: failed to create producerNotification DC: %v", err)
	} else {
		c.dcProducerNotif = dcNotif
		dcNotif.OnOpen(func() {
			c.logFn("vk: producerNotification DC opened")
		})
		dcNotif.OnMessage(func(msg webrtc.DataChannelMessage) {
			c.logFn("vk: producerNotification msg len=%d isString=%v", len(msg.Data), msg.IsString)
			// Forward to Node bridge as sfu-dc-message
			c.SendToHook("sfu-dc-message", map[string]interface{}{
				"channel": "producerNotification",
				"data":    string(msg.Data),
			})
		})
	}
	dcCmd, err := pc.CreateDataChannel("producerCommand", &webrtc.DataChannelInit{Ordered: &ordered})
	if err != nil {
		c.logFn("vk: failed to create producerCommand DC: %v", err)
	} else {
		c.dcProducerCmd = dcCmd
		dcCmd.OnOpen(func() {
			c.logFn("vk: producerCommand DC opened")
		})
		dcCmd.OnMessage(func(msg webrtc.DataChannelMessage) {
			c.logFn("vk: producerCommand msg len=%d", len(msg.Data))
			c.SendToHook("sfu-dc-message", map[string]interface{}{
				"channel": "producerCommand",
				"data":    string(msg.Data),
			})
		})
	}
	// SFU also expects screen share DCs
	pc.CreateDataChannel("producerScreenShare", &webrtc.DataChannelInit{Ordered: &ordered})
	pc.CreateDataChannel("consumerScreenShare", &webrtc.DataChannelInit{Ordered: &ordered})

	pc.OnICECandidate(func(cand *webrtc.ICECandidate) {
		if cand == nil {
			c.logFn("vk: ICE gathering complete (nil candidate)")
			return
		}
		c.logFn("vk: ICE candidate: type=%s protocol=%s address=%s", cand.Typ.String(), cand.Protocol.String(), common.MaskAddr(cand.Address))
		c.SendToHook("ice-candidate", cand.ToJSON())
	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		c.logFn("vk: connection state: %s", state.String())
		c.SendToHook("connection-state", state.String())
		if state == webrtc.PeerConnectionStateConnected && c.tunnel == nil {
			c.logFn("vk: === CONNECTED - starting VP8 tunnel ===")
			c.logFn("vk: sampleTrack id=%s kind=%s", sampleTrack.ID(), sampleTrack.Kind().String())
			c.logFn("vk: PC senders=%d receivers=%d signalingState=%s", len(pc.GetSenders()), len(pc.GetReceivers()), pc.SignalingState().String())
			c.tunnel = NewVP8DataTunnel(sampleTrack, c.logFn)
			c.tunnel.Start(25)
			if c.OnConnected != nil {
				c.OnConnected(c.tunnel)
			}
		}
	})

	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		c.logFn("vk: remote track: %s", track.Codec().MimeType)
		c.SendToHook("remote-track", map[string]string{"kind": track.Kind().String()})
		go c.readTrack(track)
	})

	c.logFn("vk: PC created (%d ICE servers)", len(config.ICEServers))
	return nil
}

func (c *VKClient) handleICEServers(data json.RawMessage) {
	if c.pc != nil {
		return
	}
	iceLogFn = c.logFn
	iceServers, err := ParseICEServers(data)
	if err != nil {
		c.logFn("vk: failed to parse ICE servers: %v", err)
		return
	}
	if err := c.createPC(webrtc.Configuration{
		ICEServers:         iceServers,
		ICETransportPolicy: webrtc.ICETransportPolicyRelay,
	}); err != nil {
		c.logFn("vk: handleICEServers createPC failed: %v", err)
	}
}

func (c *VKClient) handleCreateOffer(id int) {
	if c.pc == nil {
		return
	}
	offer, err := c.pc.CreateOffer(nil)
	if err != nil {
		c.logFn("vk: createOffer error: %v", err)
		return
	}
	c.pc.SetLocalDescription(offer)
	c.logFn("vk: created offer, senders=%d signalingState=%s", len(c.pc.GetSenders()), c.pc.SignalingState().String())
	// Log offer media lines
	for _, line := range splitLines(offer.SDP) {
		if len(line) > 2 && line[:2] == "m=" {
			c.logFn("vk: offer>> %s", line)
		}
	}
	c.SendResponse(id, SDPMessage{Type: offer.Type.String(), SDP: offer.SDP})
}

func (c *VKClient) handleCreateAnswer(id int) {
	if c.pc == nil {
		c.logFn("vk: createAnswer error: no PC")
		return
	}
	c.logFn("vk: createAnswer: signalingState=%s", c.pc.SignalingState().String())
	answer, err := c.pc.CreateAnswer(nil)
	if err != nil {
		c.logFn("vk: createAnswer error: %v", err)
		return
	}
	c.logFn("vk: createAnswer: setting local description...")
	c.pc.SetLocalDescription(answer)

	// Wait for ICE gathering so the answer SDP includes candidates.
	// In SFU mode the SDK reads localDescription after setLocalDescription
	// and sends it via acceptProducer - it must have candidates already.
	gatherDone := webrtc.GatheringCompletePromise(c.pc)
	go func() {
		<-gatherDone
		localDesc := c.pc.LocalDescription()
		c.logFn("vk: created answer with ICE, senders=%d signalingState=%s", len(c.pc.GetSenders()), c.pc.SignalingState().String())
		c.SendResponse(id, SDPMessage{Type: localDesc.Type.String(), SDP: localDesc.SDP})
	}()
}

func (c *VKClient) handleSetRemoteDescription(data json.RawMessage, id int) {
	var sdpMsg SDPMessage
	if err := json.Unmarshal(data, &sdpMsg); err != nil || c.pc == nil {
		c.logFn("vk: setRemoteDescription: no pc or bad data")
		if id > 0 {
			c.SendResponse(id, "error: no pc or bad data")
		}
		return
	}
	sdpType := ParseSDPType(sdpMsg.Type)
	connState := c.pc.ConnectionState().String()
	iceState := c.pc.ICEConnectionState().String()
	c.logFn("vk: setRemoteDescription: type=%s signalingState=%s connectionState=%s iceState=%s senders=%d receivers=%d",
		sdpMsg.Type, c.pc.SignalingState().String(), connState, iceState,
		len(c.pc.GetSenders()), len(c.pc.GetReceivers()))

	// Log SDP media lines for debugging codec issues
	lines := 0
	for _, line := range splitLines(sdpMsg.SDP) {
		if len(line) > 2 && (line[:2] == "m=" || line[:2] == "a=") {
			if line[:2] == "m=" || (len(line) > 9 && line[:9] == "a=rtpmap:") || (len(line) > 12 && line[:12] == "a=candidate:") {
				c.logFn("vk: SDP>> %s", line)
				lines++
				if lines > 20 {
					break
				}
			}
		}
	}

	if err := c.pc.SetRemoteDescription(webrtc.SessionDescription{Type: sdpType, SDP: sdpMsg.SDP}); err != nil {
		c.logFn("vk: setRemoteDescription error (%s, state=%s): %v", sdpMsg.Type, c.pc.SignalingState().String(), err)
		if id > 0 {
			c.SendResponse(id, "ok")
		}
		return
	}
	c.logFn("vk: set remote description OK: %s, signalingState=%s, senders=%d", sdpMsg.Type, c.pc.SignalingState().String(), len(c.pc.GetSenders()))
	for i, s := range c.pc.GetSenders() {
		if s.Track() != nil {
			c.logFn("vk: sender[%d]: kind=%s id=%s", i, s.Track().Kind().String(), s.Track().ID())
		} else {
			c.logFn("vk: sender[%d]: track=nil", i)
		}
	}
	c.remoteSet = true
	for _, cand := range c.pending {
		c.pc.AddICECandidate(cand)
	}
	c.pending = nil
	if id > 0 {
		c.SendResponse(id, "ok")
	}
}

func (c *VKClient) handleICECandidate(data json.RawMessage) {
	var cand ICECandidateMessage
	if err := json.Unmarshal(data, &cand); err != nil || c.pc == nil {
		return
	}
	init := webrtc.ICECandidateInit{
		Candidate: cand.Candidate, SDPMid: &cand.SDPMid, SDPMLineIndex: &cand.SDPMLineIndex,
	}
	if !c.remoteSet {
		c.pending = append(c.pending, init)
		return
	}
	c.pc.AddICECandidate(init)
}

func (c *VKClient) readTrack(track *webrtc.TrackRemote) {
	ReadTrack(track, c.tunnel, c.logFn, "vk")
}

func (c *VKClient) handleReset(id int) {
	c.logFn("vk: reset - closing PC for reconnection")
	if c.tunnel != nil {
		c.tunnel.Stop()
		c.tunnel = nil
	}
	if c.pc != nil {
		// Remove callbacks before Close() to prevent stale state events
		c.pc.OnConnectionStateChange(nil)
		c.pc.OnICECandidate(nil)
		c.pc.OnTrack(nil)
		oldPC := c.pc
		c.pc = nil
		// Close asynchronously - pc.Close() blocks on DTLS shutdown
		go oldPC.Close()
	}
	c.remoteSet = false
	c.pending = nil
	c.dcProducerNotif = nil
	c.dcProducerCmd = nil
	c.sampleTrack = nil
	if id > 0 {
		c.SendResponse(id, "ok")
	}
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lines = append(lines, line)
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func (c *VKClient) cleanup() {
	if c.tunnel != nil {
		c.tunnel.Stop()
		c.tunnel = nil
	}
	if c.pc != nil {
		c.pc.Close()
		c.pc = nil
	}
	c.logFn("vk: cleaned up")
}
