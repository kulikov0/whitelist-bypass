package tunnel

import (
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"strings"
	"sync"

	"golang.org/x/crypto/chacha20poly1305"
)

var vp8Keepalive = []byte{
	0x30, 0x01, 0x00, 0x9d, 0x01, 0x2a, 0x10, 0x00,
	0x10, 0x00, 0x00, 0x47, 0x08, 0x85, 0x85, 0x88,
	0x99, 0x84, 0x88, 0xfc,
}

var vp8Interframe = []byte{
	0xb1, 0x01, 0x00, 0x08, 0x11, 0x18, 0x00, 0x18,
	0x00, 0x18, 0x58, 0x2f, 0xf4, 0x00, 0x08, 0x00,
	0x00,
}

const (
	vp8KeepaliveLen  = 20
	vp8InterframeLen = 17
	epochFieldLen    = 4
	keepaliveHdrLen  = vp8KeepaliveLen + epochFieldLen
	interframeHdrLen = vp8InterframeLen + epochFieldLen
)

var ErrEmptySecret = errors.New("tunnel: obfuscator requires a non-empty secret")

type DecodeResult struct {
	HasFrame    bool
	Keepalive   bool
	SelfEcho    bool
	PeerRestart bool
	Payload     []byte
	PeerEpoch   uint32
}

type TunnelObfuscator struct {
	aead       cipher.AEAD
	localEpoch uint32

	mu        sync.Mutex
	peerEpoch uint32
	hasPeer   bool
}

func DeriveSecretFromJoinLink(joinLink string) []byte {
	token := extractJoinToken(joinLink)
	if token == "" {
		return nil
	}
	return []byte(token)
}

func extractJoinToken(joinLink string) string {
	s := strings.TrimSpace(joinLink)
	s = strings.TrimRight(s, "/")
	if i := strings.IndexByte(s, '?'); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, '#'); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		s = s[i+1:]
	}
	return s
}

func NewTunnelObfuscator(secret []byte) (*TunnelObfuscator, error) {
	if len(secret) == 0 {
		return nil, ErrEmptySecret
	}
	keyHash := sha256.Sum256(secret)
	aead, err := chacha20poly1305.NewX(keyHash[:])
	if err != nil {
		return nil, err
	}
	var epochBytes [4]byte
	if _, err := rand.Read(epochBytes[:]); err != nil {
		return nil, err
	}
	epoch := binary.BigEndian.Uint32(epochBytes[:])
	if epoch == 0 {
		epoch = 1
	}
	return &TunnelObfuscator{aead: aead, localEpoch: epoch}, nil
}

func (o *TunnelObfuscator) LocalEpoch() uint32 { return o.localEpoch }

func (o *TunnelObfuscator) keepaliveHeader() []byte {
	hdr := make([]byte, keepaliveHdrLen)
	copy(hdr, vp8Keepalive)
	binary.BigEndian.PutUint32(hdr[vp8KeepaliveLen:], o.localEpoch)
	return hdr
}

func (o *TunnelObfuscator) dataHeader() []byte {
	hdr := make([]byte, interframeHdrLen)
	copy(hdr, vp8Interframe)
	binary.BigEndian.PutUint32(hdr[vp8InterframeLen:], o.localEpoch)
	return hdr
}

func (o *TunnelObfuscator) EncodeKeepalive(padLen int) []byte {
	hdr := o.keepaliveHeader()
	if padLen <= 0 {
		return hdr
	}
	out := make([]byte, keepaliveHdrLen+padLen)
	copy(out, hdr)
	if _, err := rand.Read(out[keepaliveHdrLen:]); err != nil {
		return hdr
	}
	return out
}

func (o *TunnelObfuscator) EncodeData(payload []byte) []byte {
	hdr := o.dataHeader()
	nonce := make([]byte, o.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil
	}
	out := make([]byte, 0, len(hdr)+len(nonce)+len(payload)+o.aead.Overhead())
	out = append(out, hdr...)
	out = append(out, nonce...)
	out = o.aead.Seal(out, nonce, payload, nil)
	return out
}

func (o *TunnelObfuscator) EncryptPayload(plaintext []byte) []byte {
	if o == nil {
		return plaintext
	}
	nonce := make([]byte, o.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil
	}
	out := make([]byte, 0, len(nonce)+len(plaintext)+o.aead.Overhead())
	out = append(out, nonce...)
	return o.aead.Seal(out, nonce, plaintext, nil)
}

func (o *TunnelObfuscator) DecryptPayload(data []byte) ([]byte, bool) {
	if o == nil {
		return data, true
	}
	nonceSize := o.aead.NonceSize()
	if len(data) < nonceSize+o.aead.Overhead() {
		return nil, false
	}
	nonce := data[:nonceSize]
	ciphertext := data[nonceSize:]
	plaintext, err := o.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, false
	}
	return plaintext, true
}

func (o *TunnelObfuscator) Decode(frame []byte) DecodeResult {
	if len(frame) < 1 {
		return DecodeResult{}
	}
	var hdrLen, epochOff int
	isKeepaliveFrame := false
	switch frame[0] {
	case vp8Keepalive[0]:
		hdrLen = keepaliveHdrLen
		epochOff = vp8KeepaliveLen
		isKeepaliveFrame = true
	case vp8Interframe[0]:
		hdrLen = interframeHdrLen
		epochOff = vp8InterframeLen
	default:
		return DecodeResult{}
	}
	if len(frame) < hdrLen {
		return DecodeResult{}
	}
	peerEpoch := binary.BigEndian.Uint32(frame[epochOff : epochOff+epochFieldLen])
	if peerEpoch == o.localEpoch {
		return DecodeResult{HasFrame: true, SelfEcho: true, PeerEpoch: peerEpoch}
	}

	res := DecodeResult{HasFrame: true, PeerEpoch: peerEpoch}
	o.mu.Lock()
	if !o.hasPeer {
		o.peerEpoch = peerEpoch
		o.hasPeer = true
	} else if o.peerEpoch != peerEpoch {
		o.peerEpoch = peerEpoch
		res.PeerRestart = true
	}
	o.mu.Unlock()

	if isKeepaliveFrame || len(frame) == hdrLen {
		res.Keepalive = true
		return res
	}

	body := frame[hdrLen:]
	nonceSize := o.aead.NonceSize()
	if len(body) < nonceSize+o.aead.Overhead() {
		return DecodeResult{}
	}
	nonce := body[:nonceSize]
	ciphertext := body[nonceSize:]
	plaintext, err := o.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return DecodeResult{}
	}
	res.Payload = plaintext
	return res
}
