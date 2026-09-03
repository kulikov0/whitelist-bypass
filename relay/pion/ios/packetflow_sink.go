package ios

type PacketFlowSink interface {
	WritePacket(packet []byte, afFamily int)
}
