package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
)

type P2PHandler struct {
	bridge            *Bridge
	remotePeerId      *int64
	pendingOffer      *webrtc.SessionDescription
	pendingCandidates []webrtc.ICECandidateInit
	connected         bool
}

func NewP2PHandler(bridge *Bridge) *P2PHandler {
	return &P2PHandler{bridge: bridge}
}

func (p *P2PHandler) setupCallbacks() {
	p.bridge.relay.OnICECandidate(func(cand *webrtc.ICECandidate) {
		if cand == nil {
			return
		}
		log.Printf("[p2p] ICE candidate: type=%s proto=%s", cand.Typ.String(), cand.Protocol.String())
		candJSON := cand.ToJSON()
		raw, _ := json.Marshal(candJSON)
		p.OnPionICECandidate(raw)
	})

	p.bridge.relay.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		p.OnConnectionState(state.String())
	})
}

// Init creates the initial Pion PC and offer.
func (p *P2PHandler) Init() {
	relay := p.bridge.relay
	if err := relay.Init(p.bridge.iceServers); err != nil {
		log.Fatalf("[p2p] Init relay failed: %v", err)
	}
	p.setupCallbacks()

	offer, err := relay.CreateOffer()
	if err != nil {
		log.Fatalf("[p2p] Create offer failed: %v", err)
	}
	p.pendingOffer = &offer
	log.Printf("[p2p] Offer ready, SDP length: %d", len(offer.SDP))
}

// Reset tears down the current Pion PC and creates a fresh one with a new offer.
func (p *P2PHandler) Reset() {
	log.Println("[p2p] Resetting Pion PC...")
	p.connected = false

	p.bridge.relay.Close()
	p.bridge.relay = p.bridge.newRelay()

	relay := p.bridge.relay
	if err := relay.Init(p.bridge.iceServers); err != nil {
		log.Printf("[p2p] Reset init failed: %v", err)
		return
	}
	p.setupCallbacks()

	offer, err := relay.CreateOffer()
	if err != nil {
		log.Printf("[p2p] Reset create-offer failed: %v", err)
		return
	}
	p.pendingOffer = &offer
	p.pendingCandidates = nil
	log.Printf("[p2p] New offer ready after reset, SDP length: %d", len(offer.SDP))
}

// OnRegisteredPeer handles the registered-peer notification.
func (p *P2PHandler) OnRegisteredPeer(participantId int64) {
	oldPeer := p.remotePeerId
	p.remotePeerId = &participantId

	log.Printf("[p2p] Peer registered: %d (connected=%v)", participantId, p.connected)

	if oldPeer != nil && (p.pendingOffer == nil) {
		if *oldPeer != participantId {
			log.Printf("[p2p] New peer %d replacing old peer %d, resetting", participantId, *oldPeer)
		} else {
			log.Printf("[p2p] Same peer %d re-registered, no pending offer, resetting", participantId)
		}
		p.Reset()
	}

	p.sendOfferToPeer(participantId)
}

// OnTransmittedData handles SDP and ICE candidates from the remote peer.
func (p *P2PHandler) OnTransmittedData(data map[string]interface{}) {
	if cand, ok := data["candidate"]; ok {
		log.Println("[p2p] Remote ICE candidate")
		candJSON, _ := json.Marshal(cand)
		var candInit webrtc.ICECandidateInit
		json.Unmarshal(candJSON, &candInit)
		p.bridge.relay.AddICECandidate(candInit)
	}
	if sdp, ok := data["sdp"].(map[string]interface{}); ok {
		sdpType, _ := sdp["type"].(string)
		sdpStr, _ := sdp["sdp"].(string)
		log.Printf("[p2p] Remote SDP: %s", sdpType)
		if sdpType == "answer" {
			p.bridge.relay.SetRemoteDescription(webrtc.SDPTypeAnswer, sdpStr)
		} else if sdpType == "offer" {
			p.bridge.relay.SetRemoteDescription(webrtc.SDPTypeOffer, sdpStr)
			answer, err := p.bridge.relay.CreateAnswer()
			if err == nil && p.remotePeerId != nil {
				p.bridge.vkSend("transmit-data", map[string]interface{}{
					"participantId": *p.remotePeerId,
					"data": map[string]interface{}{"sdp": map[string]interface{}{
						"type": answer.Type.String(), "sdp": answer.SDP,
					}},
				})
			}
		}
	}
}

// OnPionICECandidate handles an ICE candidate from the local Pion PC.
func (p *P2PHandler) OnPionICECandidate(data json.RawMessage) {
	if p.remotePeerId != nil {
		var cand interface{}
		json.Unmarshal(data, &cand)
		p.bridge.vkSend("transmit-data", map[string]interface{}{
			"participantId": *p.remotePeerId,
			"data":          map[string]interface{}{"candidate": cand},
		})
	} else {
		var candInit webrtc.ICECandidateInit
		json.Unmarshal(data, &candInit)
		p.pendingCandidates = append(p.pendingCandidates, candInit)
	}
}

// OnConnectionState updates P2P connection state.
func (p *P2PHandler) OnConnectionState(state string) {
	switch state {
	case "connected":
		p.connected = true
		log.Println("\n  TUNNEL CONNECTED\n")
	case "disconnected":
		p.connected = false
		log.Println("[p2p] Connection disconnected, waiting for peer to rejoin")
	case "failed":
		p.connected = false
		log.Println("[p2p] Connection failed, removing stale peer")
		if p.remotePeerId != nil {
			p.bridge.vkSend("remove-participant", map[string]interface{}{
				"participantId": *p.remotePeerId,
				"ban":           false,
			})
		}
	case "closed":
		p.connected = false
		log.Println("[p2p] Connection closed")
	}
}

func (p *P2PHandler) sendOfferToPeer(participantId int64) {
	offer := p.pendingOffer
	candidates := p.pendingCandidates
	p.pendingOffer = nil
	p.pendingCandidates = nil

	if offer != nil {
		log.Printf("[p2p] Sending offer to peer %d", participantId)
		sdpStr, _ := json.Marshal(offer.SDP)
		p.bridge.mu.Lock()
		p.bridge.vkSeq++
		seq := p.bridge.vkSeq
		raw := fmt.Sprintf(`{"command":"transmit-data","sequence":%d,"participantId":%d,"data":{"sdp":{"type":%q,"sdp":%s}}}`,
			seq, participantId, offer.Type.String(), sdpStr)
		if p.bridge.vkWs != nil {
			p.bridge.vkWs.WriteMessage(websocket.TextMessage, []byte(raw))
		}
		p.bridge.mu.Unlock()
		log.Printf("[vk-ws] -> transmit-data (offer)")
	}

	for _, cand := range candidates {
		candJSON, _ := json.Marshal(cand)
		var c interface{}
		json.Unmarshal(candJSON, &c)
		p.bridge.vkSend("transmit-data", map[string]interface{}{
			"participantId": participantId,
			"data":          map[string]interface{}{"candidate": c},
		})
	}
	if len(candidates) > 0 {
		log.Printf("[p2p] Flushed %d ICE candidates", len(candidates))
	}
}
