package pion

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
)

type VKClient struct {
	WSHelper
	pc          *webrtc.PeerConnection
	sampleTrack *webrtc.TrackLocalStaticSample
	tunnel      *VP8DataTunnel
	logFn       func(string, ...any)
	remoteSet   bool
	pending     []webrtc.ICECandidateInit
	OnConnected func(*VP8DataTunnel)
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
	case "close":
		c.cleanup()
	}
}

func (c *VKClient) handleICEServers(data json.RawMessage) {
	if c.pc != nil {
		return
	}
	iceServers, err := ParseICEServers(data)
	if err != nil {
		return
	}

	config := webrtc.Configuration{
		ICEServers:         iceServers,
		ICETransportPolicy: webrtc.ICETransportPolicyRelay,
	}

	se := webrtc.SettingEngine{}
	se.SetNet(&AndroidNet{})
	se.SetInterfaceFilter(func(iface string) bool { return false })
	api := webrtc.NewAPI(webrtc.WithSettingEngine(se))
	pc, err := api.NewPeerConnection(config)
	if err != nil {
		c.logFn("vk: failed to create PC: %v", err)
		return
	}
	c.pc = pc

	sampleTrack, _ := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8},
		"video", "tunnel-video",
	)
	c.sampleTrack = sampleTrack

	audioTrack, _ := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio", "tunnel-audio",
	)

	audioSender, audioErr := pc.AddTrack(audioTrack)
	videoSender, videoErr := pc.AddTrack(sampleTrack)
	c.logFn("vk: AddTrack audio: sender=%v err=%v", audioSender != nil, audioErr)
	c.logFn("vk: AddTrack video: sender=%v err=%v", videoSender != nil, videoErr)
	c.logFn("vk: senders count: %d", len(pc.GetSenders()))

	pc.OnICECandidate(func(cand *webrtc.ICECandidate) {
		if cand == nil {
			c.logFn("vk: ICE gathering complete (nil candidate)")
			return
		}
		c.logFn("vk: ICE candidate: type=%s protocol=%s address=%s", cand.Typ.String(), cand.Protocol.String(), maskAddr(cand.Address))
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

	c.logFn("vk: PC created (%d ICE servers)", len(iceServers))
}

func (c *VKClient) handleCreateOffer(id int) {
	if c.pc == nil {
		return
	}
	offer, err := c.pc.CreateOffer(nil)
	if err != nil {
		return
	}
	c.pc.SetLocalDescription(offer)
	c.logFn("vk: created offer, senders=%d signalingState=%s", len(c.pc.GetSenders()), c.pc.SignalingState().String())
	c.SendResponse(id, SDPMessage{Type: offer.Type.String(), SDP: offer.SDP})
}

func (c *VKClient) handleCreateAnswer(id int) {
	if c.pc == nil {
		return
	}
	answer, err := c.pc.CreateAnswer(nil)
	if err != nil {
		c.logFn("vk: createAnswer error: %v", err)
		return
	}
	c.pc.SetLocalDescription(answer)
	c.logFn("vk: created answer, senders=%d signalingState=%s", len(c.pc.GetSenders()), c.pc.SignalingState().String())
	c.SendResponse(id, SDPMessage{Type: answer.Type.String(), SDP: answer.SDP})
}

func (c *VKClient) handleSetRemoteDescription(data json.RawMessage, id int) {
	var sdpMsg SDPMessage
	if err := json.Unmarshal(data, &sdpMsg); err != nil || c.pc == nil {
		return
	}
	var sdpType webrtc.SDPType
	if sdpMsg.Type == "offer" {
		sdpType = webrtc.SDPTypeOffer
	} else {
		sdpType = webrtc.SDPTypeAnswer
	}
	if err := c.pc.SetRemoteDescription(webrtc.SessionDescription{Type: sdpType, SDP: sdpMsg.SDP}); err != nil {
		c.logFn("vk: setRemoteDescription error: %v", err)
		return
	}
	c.logFn("vk: set remote description: %s, signalingState=%s, senders=%d", sdpMsg.Type, c.pc.SignalingState().String(), len(c.pc.GetSenders()))
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
	if track.Codec().MimeType != webrtc.MimeTypeVP8 {
		buf := make([]byte, 4096)
		for {
			if _, _, err := track.Read(buf); err != nil {
				return
			}
		}
	}

	var vp8Pkt codecs.VP8Packet
	var frameBuf []byte
	dataCount := 0
	recvCount := 0
	buf := make([]byte, 65536)
	for {
		n, _, err := track.Read(buf)
		if err != nil {
			return
		}
		pkt := &rtp.Packet{}
		if pkt.Unmarshal(buf[:n]) != nil {
			continue
		}
		vp8Payload, err := vp8Pkt.Unmarshal(pkt.Payload)
		if err != nil {
			continue
		}
		if vp8Pkt.S == 1 {
			frameBuf = frameBuf[:0]
		}
		frameBuf = append(frameBuf, vp8Payload...)
		if pkt.Marker {
			recvCount++
			if recvCount <= 3 || recvCount%25 == 0 {
				if len(frameBuf) > 0 {
					c.logFn("vk: recv frame #%d %d bytes, first=0x%02x", recvCount, len(frameBuf), frameBuf[0])
				}
			}
			data := ExtractDataFromPayload(frameBuf)
			if data != nil {
				dataCount++
				if dataCount <= 5 || dataCount%100 == 0 {
					c.logFn("vk: TUNNEL DATA #%d: %d bytes", dataCount, len(data))
				}
				if c.tunnel != nil && c.tunnel.onData != nil {
					c.tunnel.onData(data)
				}
			}
		}
	}
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
