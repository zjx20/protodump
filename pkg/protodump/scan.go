package protodump

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"
)

const scan = ".proto"
const magicByte = 0xa

// Debug flag for verbose output
var DebugScan = false

func debugPrintf(format string, args ...interface{}) {
	if DebugScan {
		fmt.Printf("[DEBUG] "+format, args...)
	}
}

func consumeBytes(data []byte, position int) (int, error) {
	start := position
	consumedFieldOne := false
	for {
		number, _, length := protowire.ConsumeField(data[position:])
		if length < 0 {
			err := protowire.ParseError(length)
			// Treat "invalid field number" as end of data, not an error
			if strings.Contains(err.Error(), "invalid field number") {
				return position - start, nil
			}
			// Return other parse errors as actual errors
			return position - start, fmt.Errorf("couldn't consume proto bytes: %w", err)
		}

		// Prevent infinite loop - if we can't consume any bytes, we're done
		if length == 0 {
			return position - start, nil
		}

		// Only consume Field 1 once (to handle the case where protobuf definitions are adjacent
		// in program memory)
		if number == 1 {
			if consumedFieldOne {
				return position - start, nil
			}
			consumedFieldOne = true
		}

		position += length

		// Additional safety check - don't read beyond data bounds
		if position-start >= len(data[start:]) {
			return position - start, nil
		}
	}
}

func ScanFile(path string) ([][]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("couldn't open file: %w", err)
	}
	return Scan(data), nil
}

// findValidStartWithLength searches backwards from index to find a valid Field 1 tag (0xa)
// that correctly encodes the filename ending with ".proto"
// Returns:
// - start: the position of the 0xa tag (Field 1), or -1 if not found
// - prefixLen: if there's a varint length prefix before start, this is the decoded length; 0 otherwise
// - prefixBytes: the number of bytes used by the length prefix varint
func findValidStartWithLength(data []byte, protoIndex int) (start int, prefixLen int, prefixBytes int) {
	// The filename ends at protoIndex + len(".proto")
	filenameEnd := protoIndex + len(scan)

	// Search backwards for potential start positions
	searchStart := protoIndex
	for searchStart > 0 {
		// Find the previous 0xa byte
		pos := bytes.LastIndexByte(data[:searchStart], magicByte)
		if pos == -1 {
			return -1, 0, 0
		}

		debugPrintf("    Checking candidate 0xa at offset %d\n", pos)

		// After the 0xa tag, the next byte(s) should be the string length (varint)
		// For simple cases, the length is a single byte
		if pos+1 >= len(data) {
			searchStart = pos
			continue
		}

		// Read the length varint after the 0xa
		lengthBytes := data[pos+1:]
		filenameLen, varintLen := protowire.ConsumeVarint(lengthBytes)
		if varintLen < 0 {
			debugPrintf("    Failed to parse varint at offset %d\n", pos+1)
			searchStart = pos
			continue
		}

		debugPrintf("    Varint: length=%d, varintLen=%d\n", filenameLen, varintLen)

		// Check if the computed filename end matches the actual ".proto" position
		// Position after 0xa + varint length + filename length should point to after ".proto"
		computedEnd := pos + 1 + varintLen + int(filenameLen)

		debugPrintf("    Computed filename end: %d, actual end: %d\n", computedEnd, filenameEnd)

		if computedEnd == filenameEnd {
			// Additional validation: check that the filename contains only printable chars
			filenameStart := pos + 1 + varintLen
			if filenameStart < len(data) && filenameStart < filenameEnd {
				filename := data[filenameStart:filenameEnd]
				valid := true
				for _, b := range filename {
					if b < 0x20 || b > 0x7e {
						valid = false
						break
					}
				}
				if valid {
					debugPrintf("    Found valid start at offset %d, filename: %q\n", pos, string(filename))

					// Check if there's a length prefix before this position
					// Try to find a varint that could be a length prefix
					// The length prefix should be large enough to contain at least the filename + some proto data
					minValidLength := len(filename) + 50 // At minimum, filename + package + some structure

					if pos >= 1 {
						// Try different varint lengths (1-4 bytes), starting from longest
						for tryLen := 4; tryLen >= 1 && pos >= tryLen; tryLen-- {
							prefixStart := pos - tryLen
							candidateLen, n := protowire.ConsumeVarint(data[prefixStart:])
							if n == tryLen && int(candidateLen) >= minValidLength {
								// Verify this is a reasonable length prefix
								// The length should point to data that's within our bounds
								expectedEnd := pos + int(candidateLen)
								if expectedEnd <= len(data) {
									debugPrintf("    Found valid length prefix at %d: %d bytes (ends at %d)\n",
										prefixStart, candidateLen, expectedEnd)
									return pos, int(candidateLen), n
								}
							}
						}
					}

					return pos, 0, 0
				}
			}
		}

		// This 0xa is not the right one, continue searching backwards
		searchStart = pos
	}

	return -1, 0, 0
}

func Scan(data []byte) [][]byte {
	results := make([][]byte, 0)
	totalOffset := 0 // Track absolute offset for debugging

	for {
		index := bytes.Index(data, []byte(scan))
		if index == -1 {
			break
		}

		// Extract the filename for debugging
		filenameEnd := index + len(scan)
		filenameStart := index
		for filenameStart > 0 && data[filenameStart-1] != 0x0a && data[filenameStart-1] >= 0x20 && data[filenameStart-1] <= 0x7e {
			filenameStart--
		}
		filename := string(data[filenameStart:filenameEnd])
		debugPrintf("Found '.proto' at offset %d (absolute: %d), possible filename: %q\n",
			index, totalOffset+index, filename)

		// Find the valid start position using the improved algorithm
		start, prefixLen, prefixBytes := findValidStartWithLength(data, index)
		if start == -1 {
			debugPrintf("  No valid start found, skipping\n")
			data = data[index+1:]
			totalOffset += index + 1
			continue
		}

		debugPrintf("  Using start at offset %d, prefixLen=%d, prefixBytes=%d\n", start, prefixLen, prefixBytes)

		// Show some bytes around start for debugging
		contextStart := start
		if contextStart > 10 {
			contextStart = start - 10
		}
		contextEnd := start + 30
		if contextEnd > len(data) {
			contextEnd = len(data)
		}
		debugPrintf("  Bytes around start: %x\n", data[contextStart:contextEnd])

		var length int
		var err error

		// If we have a valid length prefix, use it directly
		if prefixLen > 0 && start+prefixLen <= len(data) {
			length = prefixLen
			debugPrintf("  Using length prefix: %d bytes\n", length)
		} else {
			// Fall back to consumeBytes for older/simpler formats
			length, err = consumeBytes(data, start)
			debugPrintf("  consumeBytes returned length=%d, err=%v\n", length, err)

			if err != nil {
				fmt.Printf("%v\n", err)
				if len(data) > index {
					data = data[index+1:]
					totalOffset += index + 1
					continue
				} else {
					break
				}
			}
		}

		debugPrintf("  Extracted %d bytes from offset %d\n", length, start)
		results = append(results, data[start:start+length])
		data = data[start+length:]
		totalOffset += start + length
	}

	return results
}
