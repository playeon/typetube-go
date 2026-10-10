package tap

import (
	"encoding/binary"
	"fmt"
)

const (
	HeaderSize     = 12
	MaxPayloadSize = 1024 * 1024 // 1 MB
)

type Origin uint32

const (
	OriginGenericTap Origin = 0x54415001 // "TAP\x01"
	OriginAudiumCore Origin = 0x41554401 // "AUD\x01"
	OriginTypetubeCLI Origin = 0x54544301 // "TTC\x01"
	OriginTypetubeTS  Origin = 0x54545301 // "TTS\x01"
	OriginTypetubeGo  Origin = 0x54415001 // Standard TAP
)

type Intent uint8

const (
	IntentPing       Intent = 0x00
	IntentNegotiate  Intent = 0x01
	IntentAuth       Intent = 0x02
	IntentGoAway     Intent = 0x07
	IntentRPCCall    Intent = 0x10
	IntentDisconnect Intent = 0x7E
	IntentError      Intent = 0x7F
)

type Flags uint8

const (
	FlagRequest          Flags = 0x00
	FlagResponse         Flags = 0x01
	FlagError            Flags = 0x02
	FlagCompressed       Flags = 0x04
	FlagEncrypted        Flags = 0x08
	FlagAcceptCompressed Flags = 0x10
)

type Header struct {
	Origin  Origin
	Intent  Intent
	Flags   Flags
	TraceID uint16
	Length  uint32
}

type Frame struct {
	Header
	Payload []byte
}

func EncodeFrame(intent Intent, traceID uint16, flags Flags, payload []byte, origin Origin) ([]byte, error) {
	if len(payload) > MaxPayloadSize {
		return nil, fmt.Errorf("TAP frame payload exceeds maximum size (%d > %d)", len(payload), MaxPayloadSize)
	}
	buf := make([]byte, HeaderSize+len(payload))
	binary.BigEndian.PutUint32(buf[0:4], uint32(origin))
	buf[4] = byte(intent)
	buf[5] = byte(flags)
	binary.BigEndian.PutUint16(buf[6:8], traceID)
	binary.BigEndian.PutUint32(buf[8:12], uint32(len(payload)))
	copy(buf[12:], payload)
	return buf, nil
}

func ParseHeader(buf []byte) (*Header, error) {
	if len(buf) < HeaderSize {
		return nil, nil
	}
	origin := Origin(binary.BigEndian.Uint32(buf[0:4]))
	intent := Intent(buf[4])
	flags := Flags(buf[5])
	traceID := binary.BigEndian.Uint16(buf[6:8])
	length := binary.BigEndian.Uint32(buf[8:12])

	if length > MaxPayloadSize {
		return nil, fmt.Errorf("TAP payload size %d exceeds safety cap of %d bytes", length, MaxPayloadSize)
	}
	if (intent == IntentPing || intent == IntentNegotiate || intent == IntentAuth) && length > 2048 {
		return nil, fmt.Errorf("TAP handshake frame %d exceeds pre-allocation limit (%d > 2048 bytes)", intent, length)
	}

	return &Header{
		Origin:  origin,
		Intent:  intent,
		Flags:   flags,
		TraceID: traceID,
		Length:  length,
	}, nil
}

func GetFrameHeaderAAD(origin Origin, intent Intent, flags Flags, traceID uint16, length uint32) []byte {
	aad := make([]byte, HeaderSize)
	binary.BigEndian.PutUint32(aad[0:4], uint32(origin))
	aad[4] = byte(intent)
	aad[5] = byte(flags)
	binary.BigEndian.PutUint16(aad[6:8], traceID)
	binary.BigEndian.PutUint32(aad[8:12], length)
	return aad
}
