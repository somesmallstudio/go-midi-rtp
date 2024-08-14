package rtp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"time"

	"github.com/laenzlinger/go-midi-rtp/midi"
	"github.com/laenzlinger/go-midi-rtp/timestamp"
)

// Generic RTP constants
const (
	version2Bit  = 0x80
	extensionBit = 0x10
	paddingBit   = 0x20
	markerBit    = 0x80
	ccMask       = 0x0f
	ptMask       = 0x7f
	countMask    = 0x1f
)

// RTP-MIDI constants
const (
	minimumBufferLength = 12
)

const (
	padding   = 0x00
	extension = 0x00
	ccBits    = 0x00
	firstByte = version2Bit | padding | extension | ccBits
)

const (
	marker      = markerBit
	payloadType = 0x61
	secondByte  = payloadType
)

// MIDI List constants
const (
	deltaTimeMask    = 0x7f
	deltaTimeHasNext = 0x80
)

// MIDIMessage represents a MIDI package exchanged over RTP.
//
// The implementation is tested only with Apple MIDI Network Driver.
//
// see https://en.wikipedia.org/wiki/RTP-MIDI
// see https://developer.apple.com/library/archive/documentation/Audio/Conceptual/MIDINetworkDriverProtocol/MIDI/MIDI.html
// see https://tools.ietf.org/html/rfc6295
/*
    0                   1                   2                   3
    0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
   | V |P|X|  CC   |M|     PT      |        Sequence number        |
   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
   |                           Timestamp                           |
   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
   |                             SSRC                              |
   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+


   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
   |                     MIDI command section ...                  |
   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
   |                       Journal section ...                     |
   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+

*/
type RTPMIDIHeader struct {
	// version (V): 2 bits
	// 		This field identifies the version of RTP.  The version defined by
	// 		this specification is two (2).  (The value 1 is used by the first
	// 		draft version of RTP and the value 0 is used by the protocol
	// 		initially implemented in the "vat" audio tool.)
	Version uint8
	// padding (P): 1 bit
	// 		If the padding bit is set, the packet contains one or more
	// 		additional padding octets at the end which are not part of the
	// 		payload.  The last octet of the padding contains a count of how
	// 		many padding octets should be ignored, including itself.  Padding
	// 		may be needed by some encryption algorithms with fixed block sizes
	// 		or for carrying several RTP packets in a lower-layer protocol data
	// 		unit.
	Padding bool
	// extension (X): 1 bit
	// 		If the extension bit is set, the fixed header MUST be followed by
	// 		exactly one header extension, with a format defined in Section
	// 		5.3.1.
	Extension bool
	// CSRC count (CC): 4 bits
	// 		The CSRC count contains the number of CSRC identifiers that follow
	// 		the fixed header.
	CSRCCount uint8
	// marker (M): 1 bit
	// 		The interpretation of the marker is defined by a profile.  It is
	// 		intended to allow significant events such as frame boundaries to
	// 		be marked in the packet stream.  A profile MAY define additional
	// 		marker bits or specify that there is no marker bit by changing the
	// 		number of bits in the payload type field (see Section 5.3).
	// For RTP MIDI:
	// 		The behavior of the 1-bit M field depends on the media type of the
	// 		stream.  For native streams, the M bit MUST be set to 1 if the MIDI
	// 		command section has a non-zero LEN field and MUST be set to 0
	// 		otherwise.  For mpeg4-generic streams, the M bit MUST be set to 1 for
	// 		all packets (to conform to [RFC3640]).
	Marker byte
	// payload type (PT): 7 bits
	//      This field identifies the format of the RTP payload and determines
	//      its interpretation by the application.  A profile MAY specify a
	//      default static mapping of payload type codes to payload formats.
	//      Additional payload type codes MAY be defined dynamically through
	//      non-RTP means (see Section 3).  A set of default mappings for
	//      audio and video is specified in the companion RFC 3551 [1].  An
	//      RTP source MAY change the payload type during a session, but this
	//      field SHOULD NOT be used for multiplexing separate media streams
	//      (see Section 5.2).
	//      A receiver MUST ignore packets with payload types that it does not
	//      understand.
	// For RTP MIDI: 0x61
	PayloadType uint8
}

func (h *RTPMIDIHeader) Valid() error {
	if h.PayloadType != payloadType {
		return fmt.Errorf("payload type mismatch: expected %X, got %X", payloadType, h.PayloadType)
	}
	return nil
}

func (h *RTPMIDIHeader) HasMIDIData() bool {
	return h.Marker > 0
}

type MIDIMessage struct {
	SequenceNumber uint16
	SSRC           uint32
	Commands       MIDICommands
}

// MIDICommands the list of MIDICommand sent inside a MIDIMessage
type MIDICommands struct {
	Timestamp time.Time
	Commands  []MIDICommand
}

// MIDIPayload contains the MIDI payload to be sent.
type MIDIPayload []byte

// MIDICommand represents a single command containing a DeltaTime and the Payload
type MIDICommand struct {
	DeltaTime time.Duration
	Payload   MIDIPayload
}

type MIDIListHeader struct {
	// B
	bigHeader bool
	// J
	hasJournal bool
	// Z
	preceedingDeltaTime bool
	// P
	P bool
	// LEN
	Len uint16
}

// Decode a byte buffer into a MIDIMessage
func Decode(buffer []byte) (msg MIDIMessage, err error) {
	msg = MIDIMessage{}
	if len(buffer) < minimumBufferLength {
		err = fmt.Errorf("buffer is too small: %d bytes", len(buffer))
		return msg, err
	}

	// FIXME implement decoder
	// fmt.Println("RTP packet dump ****")
	// fmt.Print(hex.Dump(buffer))
	// fmt.Println("****")

	offset := 0
	header := RTPMIDIHeader{}
	header.Version = (buffer[offset] & version2Bit) >> 6
	header.Padding = (buffer[offset] & paddingBit) > 0
	header.Extension = (buffer[offset] & extensionBit) > 0
	header.CSRCCount = buffer[offset] & ccMask

	offset = 1
	header.PayloadType = buffer[offset] & ptMask
	header.Marker = (buffer[offset] & markerBit) >> 7

	offset = 2
	msg.SequenceNumber = binary.BigEndian.Uint16(buffer[offset : offset+2]) // 2 bytes

	offset = 8
	msg.SSRC = binary.BigEndian.Uint32(buffer[offset : offset+4]) // 4 bytes

	// fmt.Printf("RTP Header RAW \n")
	// dumpPacket(buffer, 0, 12)
	// fmt.Printf("RTP Header %#v\n\n", header)

	err = header.Valid()
	if err != nil {
		return msg, err
	}

	// if !header.HasMIDIData() {
	// 	return msg, nil
	// }

	//MIDI List starts at index 12 / byte 13
	offset = 12

	midiListHeader := MIDIListHeader{
		bigHeader:           buffer[offset]&bigHeaderBit > 0,
		hasJournal:          buffer[offset]&journalBit > 0,
		preceedingDeltaTime: buffer[offset]&zeroDeltaBit > 0,
	}

	listStart := offset + 1
	if midiListHeader.bigHeader {
		midiListHeader.Len = binary.BigEndian.Uint16(buffer[offset:offset+2]) & 0x0fff
		listStart = offset + 2
	} else {
		midiListHeader.Len = uint16(buffer[offset] & lenMask)
	}

	commands, err := parseMIDIList(buffer, listStart, &midiListHeader)
	if err != nil {
		fmt.Printf("[INFO] Error parsing midi list, returning parsed commands so far: %s\n", err)
	}
	msg.Commands = MIDICommands{
		Timestamp: time.Now(),
		Commands:  commands,
	}
	return msg, nil
}

func dumpPacket(buffer []byte, startByte uint, length uint) {
	lines := 0
	for _, b := range buffer[startByte : startByte+length] {
		fmt.Printf("%08b", b)
		lines++
		if lines%4 == 0 {
			fmt.Println()
		} else {
			fmt.Print(" ")
		}
	}
	fmt.Println()
}

func parseMIDIList(buffer []byte, offset int, header *MIDIListHeader) ([]MIDICommand, error) {
	commands := make([]MIDICommand, 0)
	// fmt.Printf("MIDI List Header %#v\n", header)
	// fmt.Printf("Remaining buffer size %d\n", uint(len(buffer)-12))
	// dumpPacket(buffer, 12, uint(len(buffer)-12))
	// fmt.Printf("--- midi list buffer with length %2d\n", header.Len)
	// dumpPacket(buffer, uint(offset), uint(header.Len))
	// fmt.Println("---")

	// Keep track of the last status byte to infer for succeeding ones
	var lastStatusByte byte

	end := offset + int(header.Len)
	// Based on a NodeJS implementation
	for offset < end {
		command := MIDICommand{}
		dataLength := 0
		deltaTime := uint32(0)

		// Decode the delta time
		if len(commands) > 0 || header.preceedingDeltaTime {
			for k := 0; k < 4; k++ {
				currentOctet := buffer[offset]
				deltaTime <<= 7
				deltaTime |= uint32(currentOctet) & deltaTimeMask
				offset += 1
				if currentOctet&deltaTimeHasNext == 0 {
					break
				}
			}
		}
		command.DeltaTime = time.Millisecond * time.Duration(deltaTime)

		statusByte := buffer[offset]
		hasOwnStatusByte := (statusByte & 0x80) == 0x80
		if hasOwnStatusByte {
			lastStatusByte = statusByte
			offset += 1
		} else {
			statusByte = lastStatusByte
		}

		//  Parse SysEx (experimental, needs testing)
		if statusByte == 0xf0 {
			dataLength = 0
			for len(buffer) > offset+dataLength &&
				!(buffer[offset+dataLength]&0x80 > 0x00) {
				// TODO: possibly append byte to sysex buffer?
				dataLength += 1
			}
			// TODO: SysEx end?
			if buffer[offset+dataLength] != 0xf7 {
				dataLength -= 1
			}
			dataLength += 1
		} else {
			dataLength = midi.GetDataLength(statusByte)
		}

		command.Payload = []byte{statusByte}

		if len(buffer) < offset+dataLength {
			// isValid = false
			return commands, fmt.Errorf("Not enough buffer data to read additional %03d command bytes", dataLength)
		}
		if dataLength > 0 {
			command.Payload = append(command.Payload, buffer[offset:offset+dataLength]...)
			offset += dataLength
		}
		if !(command.Payload[0] == 0xf0 && command.Payload[len(command.Payload)-1] != 0xf7) {
			// fmt.Printf("Successfully parsed MIDI command %#v\n", command)
			commands = append(commands, command)
		} else {
			continue
		}
	}
	// fmt.Printf("Found %3d commands\n", len(commands))
	// for _, cmd := range commands {
	// 	fmt.Println(hex.Dump(cmd.Payload))
	// }
	return commands, nil
}

// Encode the MIDIMessage into a byte buffer.
func Encode(m MIDIMessage, start time.Time) []byte {

	b := new(bytes.Buffer)

	b.WriteByte(firstByte)
	b.WriteByte(secondByte)
	binary.Write(b, binary.BigEndian, m.SequenceNumber)
	ts := timestamp.Of(m.Commands.Timestamp, start).Uint32()
	binary.Write(b, binary.BigEndian, uint32(ts))
	binary.Write(b, binary.BigEndian, m.SSRC)

	m.Commands.encode(b, start)

	return b.Bytes()
}

func (m MIDIMessage) String() string {
	return fmt.Sprintf("RM SSRC=0x%x sn=%d", m.SSRC, m.SequenceNumber)
}

/*

0                   1                   2                   3
0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|B|J|Z|P|LEN... |  MIDI list ...                                |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+

                  Figure 2 -- MIDI Command Section


+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|  Delta Time 0     (1-4 octets long, or 0 octets if Z = 0)     |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|  MIDI Command 0   (1 or more octets long)                     |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|  Delta Time 1     (1-4 octets long)                           |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|  MIDI Command 1   (1 or more octets long)                     |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                              ...                              |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|  Delta Time N     (1-4 octets long)                           |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|  MIDI Command N   (0 or more octets long)                     |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+

                Figure 3 -- MIDI List Structure
*/

const (
	emtpyHeader  = byte(0x00)
	bigHeaderBit = 0x80 // Big Header: 2 octets
	journalBit   = 0x40 // Journal persent
	zeroDeltaBit = 0x20 // DeltaTime present for first MIDI command
	phantomBit   = 0x10 // Status byte was not present in original MIDI command
	lenMask      = 0x0f // Mask for the length information
)

func (mcs MIDICommands) encode(w io.Writer, start time.Time) {
	if len(mcs.Commands) == 0 {
		w.Write([]byte{emtpyHeader})
		return
	}
	header := emtpyHeader
	b := new(bytes.Buffer)

	for i, mc := range mcs.Commands {
		if i == 0 && mc.DeltaTime > 0 {
			header = header | zeroDeltaBit
			timestamp.EncodeDeltaTime(mcs.Timestamp, start, mc.DeltaTime, b)
		}
		if i > 0 {
			timestamp.EncodeDeltaTime(mcs.Timestamp, start, mc.DeltaTime, b)
		}
		mc.Payload.encode(b)
	}

	if b.Len() > 4095 {
		// FIXME handle messages with size > 4095 octets (error and crop)
	} else if b.Len() > 15 {
		header = header | bigHeaderBit | (byte(b.Len()>>8) & lenMask)
		count := byte(b.Len())
		w.Write([]byte{header, count})
	} else {
		header = header | (byte(b.Len()) & lenMask)
		w.Write([]byte{header})
	}

	w.Write(b.Bytes())
}

func (p MIDIPayload) encode(w io.Writer) {
	// FIXME maybe this encoding is not correct
	if len(p) == 0 {
		return
	}
	w.Write(p)
}
