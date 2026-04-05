package tunnel

const (
	MsgConnect    byte = 0x01
	MsgConnectOK  byte = 0x02
	MsgConnectErr byte = 0x03
	MsgData       byte = 0x04
	MsgClose      byte = 0x05
	MsgUDP        byte = 0x06
	MsgUDPReply   byte = 0x07

	UDPBufSize      = 4096
	RTPBufSize      = 65536
	VP8RelayBufSize = 900
)
