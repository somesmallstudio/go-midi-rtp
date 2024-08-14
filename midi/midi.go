package midi

// Based on the NodeJS midi-common package with selected features and functionality

func GetDataLength(command byte) int {
	info := GetCommandInfo(command)
	if info != nil {
		return info.dataLength
	}
	return 0
}

func GetCommandInfo(command byte) *commandInfo {
	if info, ok := commandsInfos[command]; ok {
		return &info
	}
	if info, ok := commandsInfos[command&0xf0]; ok {
		return &info
	}
	return nil
}

type commandInfo struct {
	dataLength int
	name       string
}

var (
	commandsInfos = map[byte]commandInfo{
		// # Channel Messages

		// ## Note Off
		0x80: {dataLength: 2, name: "noteOff"},

		// ## Note On
		0x90: {dataLength: 2, name: "noteOn"},

		// ## PolyphonicAftertouch
		0xa0: {dataLength: 2, name: "polyphonicAftertouch"},

		// ## Control Change
		0xb0: {dataLength: 2, name: "controlChange"},

		// ## Program/ Mode Change
		0xc0: {dataLength: 1, name: "programChange"},

		// ## Channel Aftertouch
		0xd0: {dataLength: 1, name: "channelAftertouch"},

		// ## PitchWheel
		0xe0: {dataLength: 2, name: "pitchBend"},

		// # System Common Messages

		0xf0: {dataLength: -1, name: "systemExclusive"}, //SysEx Start, length is determined by SysEx end byte

		0xf1: {dataLength: 1, name: "quarterFrame"}, // Quarter frame
		0xf2: {dataLength: 2, name: "songPosition"}, // Song Position Pointer
		0xf3: {dataLength: 1, name: "songSelect"},   // Song select

		/*
		   0xf4: Undefined
		   0xf5: Undefined
		*/
		0xf6: {dataLength: 0, name: "tuneRequest"}, // Tune request (no data)
		/*
		   0xf7: End of SysEx
		*/

		// # System Realtime Messages
		0xf8: {dataLength: 0, name: "clock"},    // Timing clock
		0xfa: {dataLength: 0, name: "start"},    // Start
		0xfb: {dataLength: 0, name: "continue"}, // Continue
		0xfc: {dataLength: 0, name: "stop"},     // Stop
		/*
		   0xfd: Undefined
		*/
		0xfe: {dataLength: 0, name: "activeSensing"}, // Active Sensing
		0xff: {dataLength: 0, name: "reset"},         // System Reset
	}
)
