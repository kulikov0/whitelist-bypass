package pion

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
	"whitelist-bypass/relay/common"
)

type tmPCState struct {
	pc          *webrtc.PeerConnection
	remoteSet   bool
	pending     []webrtc.ICECandidateInit
}

type TelemostClient struct {
	WSHelper
	pcs         map[string]*tmPCState
	pcMu        sync.Mutex
	sampleTrack *webrtc.TrackLocalStaticSample
	tunnel      *VP8DataTunnel
	logFn       func(string, ...any)
	LocalIP     string
	ipReady     chan struct{}
	ipOnce      sync.Once
	OnConnected func(DataTunnel)
}

func NewTelemostClient(logFn func(string, ...any)) *TelemostClient {
	if logFn == nil {
		logFn = log.Printf
	}
	return &TelemostClient{
		logFn:   logFn,
		pcs:     make(map[string]*tmPCState),
		ipReady: make(chan struct{}),
	}
}

func (c *TelemostClient) HandleSignaling(w http.ResponseWriter, r *http.Request) {
	ws, err := WsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		c.logFn("telemost: ws upgrade error: %v", err)
		return
	}
	c.SetConn(ws)
	c.logFn("telemost: signaling connected")
	c.ReadMessages(c.handleMessage, c.cleanup)
}

func (c *TelemostClient) handleMessage(raw []byte) {
	var msg SignalingMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return
	}

	role := msg.Role
	if role == "" {
		role = "pub"
	}

	switch msg.Type {
	case "local-ip":
		var ip string
		json.Unmarshal(msg.Data, &ip)
		c.LocalIP = ip
		c.logFn("telemost: local IP set to %s", common.MaskAddr(ip))
		c.ipOnce.Do(func() { close(c.ipReady) })
	case "ice-servers":
		go c.handleICEServers(msg.Data, role)
	case "create-offer":
		go c.waitAndDo(role, func() { c.handleCreateOffer(msg.ID, role) })
	case "create-answer":
		go c.waitAndDo(role, func() { c.handleCreateAnswer(msg.ID, role) })
	case "set-local-description":
	case "set-remote-description":
		go c.waitAndDo(role, func() { c.handleSetRemoteDescription(msg.Data, msg.ID, role) })
	case "add-ice-candidate":
		c.handleICECandidate(msg.Data, role)
	case "add-track", "create-data-channel":
	case "close":
		c.cleanup()
	}
}

func (c *TelemostClient) waitAndDo(role string, fn func()) {
	for i := 0; i < 50; i++ {
		c.pcMu.Lock()
		ps := c.pcs[role]
		c.pcMu.Unlock()
		if ps != nil && ps.pc != nil {
			fn()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	c.logFn("telemost [%s]: timeout waiting for PC", role)
}

func (c *TelemostClient) handleICEServers(data json.RawMessage, role string) {
	c.pcMu.Lock()
	if ps, ok := c.pcs[role]; ok && ps.pc != nil {
		c.pcMu.Unlock()
		return
	}
	c.pcMu.Unlock()

	if c.LocalIP == "" {
		select {
		case <-c.ipReady:
		case <-time.After(3 * time.Second):
			c.logFn("telemost: no local IP received")
		}
	}

	iceLogFn = c.logFn
	iceServers, err := ParseICEServers(data)
	if err != nil {
		return
	}

	config := webrtc.Configuration{
		ICEServers:         iceServers,
		ICETransportPolicy: webrtc.ICETransportPolicyRelay,
	}

	pc, err := NewPionAPI(c.LocalIP).NewPeerConnection(config)
	if err != nil {
		c.logFn("telemost [%s]: failed to create PC: %v", role, err)
		return
	}

	ps := &tmPCState{pc: pc}
	c.pcMu.Lock()
	if existing, ok := c.pcs[role]; ok && existing.pc != nil {
		c.pcMu.Unlock()
		pc.Close()
		return
	}
	c.pcs[role] = ps
	c.pcMu.Unlock()

	if role == "pub" {
		c.sampleTrack = AddTunnelTracks(pc, c.logFn, "telemost [pub]")
	}

	pc.OnICECandidate(func(cand *webrtc.ICECandidate) {
		if cand == nil {
			c.logFn("telemost [%s]: ICE gathering complete", role)
			return
		}
		c.logFn("telemost [%s]: ICE candidate: type=%s protocol=%s address=%s", role, cand.Typ.String(), cand.Protocol.String(), common.MaskAddr(cand.Address))
		c.SendToHookWithRole("ice-candidate", cand.ToJSON(), role)
	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		c.logFn("telemost [%s]: connection state: %s", role, state.String())
		c.SendToHook("connection-state", state.String())
		if state == webrtc.PeerConnectionStateConnected && role == "pub" && c.tunnel == nil {
			c.logFn("telemost: === CONNECTED - starting VP8 tunnel ===")
			c.logFn("telemost: sampleTrack id=%s kind=%s", c.sampleTrack.ID(), c.sampleTrack.Kind().String())
			c.logFn("telemost: pub senders=%d receivers=%d", len(pc.GetSenders()), len(pc.GetReceivers()))
			c.tunnel = NewVP8DataTunnel(c.sampleTrack, c.logFn)
			c.tunnel.Start(25)
			if c.OnConnected != nil {
				c.OnConnected(c.tunnel)
			}
		}
	})

	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		c.logFn("telemost [%s]: remote track: %s", role, track.Codec().MimeType)
		c.SendToHook("remote-track", map[string]string{"kind": track.Kind().String()})
		go c.readTrack(track)
	})

	c.logFn("telemost [%s]: PC created (%d ICE servers)", role, len(iceServers))
}

func (c *TelemostClient) handleCreateOffer(id int, role string) {
	c.pcMu.Lock()
	ps := c.pcs[role]
	c.pcMu.Unlock()
	if ps == nil || ps.pc == nil {
		return
	}
	offer, err := ps.pc.CreateOffer(nil)
	if err != nil {
		return
	}
	ps.pc.SetLocalDescription(offer)
	c.logFn("telemost [%s]: created offer, senders=%d signalingState=%s", role, len(ps.pc.GetSenders()), ps.pc.SignalingState().String())
	c.SendResponse(id, SDPMessage{Type: offer.Type.String(), SDP: offer.SDP})
}

func (c *TelemostClient) handleCreateAnswer(id int, role string) {
	c.pcMu.Lock()
	ps := c.pcs[role]
	c.pcMu.Unlock()
	if ps == nil || ps.pc == nil {
		return
	}
	answer, err := ps.pc.CreateAnswer(nil)
	if err != nil {
		return
	}
	ps.pc.SetLocalDescription(answer)
	c.logFn("telemost [%s]: created answer, senders=%d signalingState=%s", role, len(ps.pc.GetSenders()), ps.pc.SignalingState().String())
	c.SendResponse(id, SDPMessage{Type: answer.Type.String(), SDP: answer.SDP})
}

func (c *TelemostClient) handleSetRemoteDescription(data json.RawMessage, id int, role string) {
	var sdpMsg SDPMessage
	if err := json.Unmarshal(data, &sdpMsg); err != nil {
		return
	}
	c.pcMu.Lock()
	ps := c.pcs[role]
	c.pcMu.Unlock()
	if ps == nil || ps.pc == nil {
		return
	}
	sdpType := ParseSDPType(sdpMsg.Type)
	if err := ps.pc.SetRemoteDescription(webrtc.SessionDescription{Type: sdpType, SDP: sdpMsg.SDP}); err != nil {
		c.logFn("telemost [%s]: setRemoteDescription error: %v", role, err)
		return
	}
	c.logFn("telemost [%s]: set remote description: %s, signalingState=%s, senders=%d", role, sdpMsg.Type, ps.pc.SignalingState().String(), len(ps.pc.GetSenders()))
	ps.remoteSet = true
	for _, cand := range ps.pending {
		ps.pc.AddICECandidate(cand)
	}
	ps.pending = nil
	if id > 0 {
		c.SendResponse(id, "ok")
	}
}

func (c *TelemostClient) handleICECandidate(data json.RawMessage, role string) {
	var cand ICECandidateMessage
	if err := json.Unmarshal(data, &cand); err != nil {
		return
	}
	c.pcMu.Lock()
	ps := c.pcs[role]
	c.pcMu.Unlock()
	if ps == nil || ps.pc == nil {
		return
	}
	init := webrtc.ICECandidateInit{
		Candidate: cand.Candidate, SDPMid: &cand.SDPMid, SDPMLineIndex: &cand.SDPMLineIndex,
	}
	if !ps.remoteSet {
		ps.pending = append(ps.pending, init)
		return
	}
	ps.pc.AddICECandidate(init)
}

func (c *TelemostClient) readTrack(track *webrtc.TrackRemote) {
	ReadTrack(track, c.tunnel, c.logFn, "telemost")
}

func (c *TelemostClient) cleanup() {
	if c.tunnel != nil {
		c.tunnel.Stop()
		c.tunnel = nil
	}
	c.pcMu.Lock()
	for role, ps := range c.pcs {
		if ps.pc != nil {
			ps.pc.Close()
		}
		delete(c.pcs, role)
	}
	c.pcMu.Unlock()
	c.logFn("telemost: cleaned up")
}
