package wire

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/klauspost/compress/zstd"
)

const (
	windowBytes         = 32 << 20
	decodedWindowBytes  = 128 << 20
	frameMagic          = 0xFD2FB528
	skippableMagic      = 0x184D2A50
	skippableMagicMask  = 0xFFFFFFF0
	trailerMagic        = skippableMagic
	trailerPayloadBytes = 8
	blockHeaderBytes    = 3
	checksumBytes       = 4
	surveyBufferBytes   = 1 << 20
	markerLineBytes     = 128
	reservedFrameBit    = 1 << 3
	checksumFrameBit    = 1 << 2
	singleSegmentBit    = 1 << 5
	repeatedBlock       = 1
	reservedBlock       = 3
)

var (
	closingBlock      = []byte{1, 0, 0}
	dictionaryIDBytes = [4]int{0, 1, 2, 4}
	contentSizeBytes  = [4]int{0, 2, 4, 8}
)

type layout struct {
	next           int
	wholeBytes     int64
	lastFrameStart int64
	isUnterminated bool
}

type frame struct {
	wholeBytes  int64
	hasHeader   bool
	hasEnded    bool
	hasChecksum bool
}

func resume(file *os.File) (int, bool, error) {
	info, err := file.Stat()
	if err != nil {
		return 0, false, err
	}
	if info.Size() == 0 {
		return 1, true, nil
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return 0, false, err
	}
	found, err := survey(bufio.NewReaderSize(file, surveyBufferBytes))
	if err != nil {
		return 0, false, err
	}

	end := found.wholeBytes
	if end < info.Size() {
		if err := file.Truncate(end); err != nil {
			return 0, false, err
		}
	}
	if found.isUnterminated {
		if _, err := file.Write(closingBlock); err != nil {
			return 0, false, err
		}
		end += int64(len(closingBlock))
	}

	next := max(found.next, 1)
	if found.lastFrameStart >= 0 {
		last, err := lastExchangeNumber(io.NewSectionReader(file, found.lastFrameStart, end-found.lastFrameStart))
		if err != nil {
			return 0, false, err
		}
		next = max(next, last+1)
	}

	return next, end == 0, nil
}

func survey(reader *bufio.Reader) (layout, error) {
	found := layout{lastFrameStart: -1}
	for {
		start := found.wholeBytes
		magic, isWhole, err := readUint32(reader)
		if err != nil || !isWhole {
			return found, err
		}

		switch {
		case magic == frameMagic:
			read, err := readFrame(reader)
			if err != nil {
				return found, fmt.Errorf("the frame at byte %d: %w", start, err)
			}
			if !read.hasHeader {
				return found, nil
			}
			found.wholeBytes = start + 4 + read.wholeBytes
			found.lastFrameStart = start
			if !read.hasEnded {
				if read.hasChecksum {
					return found, fmt.Errorf("the frame at byte %d ends early and carries a checksum", start)
				}
				found.isUnterminated = true
				return found, nil
			}
		case magic&skippableMagicMask == skippableMagic:
			size, isWhole, err := readUint32(reader)
			if err != nil || !isWhole {
				return found, err
			}
			payload := make([]byte, min(size, trailerPayloadBytes))
			isWhole, err = readWhole(reader, payload)
			if err != nil || !isWhole {
				return found, err
			}
			isWhole, err = discardWhole(reader, int64(size)-int64(len(payload)))
			if err != nil || !isWhole {
				return found, err
			}
			found.wholeBytes = start + 8 + int64(size)
			if magic == trailerMagic && size == trailerPayloadBytes {
				found.next = max(found.next, int(binary.LittleEndian.Uint64(payload))) //nolint:gosec // an exchange count
				found.lastFrameStart = -1
			}
		default:
			return found, fmt.Errorf("byte %d does not begin a zstd frame", start)
		}
	}
}

func readFrame(reader *bufio.Reader) (frame, error) {
	var descriptor [1]byte
	isWhole, err := readWhole(reader, descriptor[:])
	if err != nil || !isWhole {
		return frame{}, err
	}

	flags := descriptor[0]
	if flags&reservedFrameBit != 0 {
		return frame{}, errors.New("its header sets a reserved bit")
	}

	headerBytes := 1 + dictionaryIDBytes[flags&3]
	isSingleSegment := flags&singleSegmentBit != 0
	switch {
	case !isSingleSegment:
		headerBytes += 1 + contentSizeBytes[flags>>6]
	case flags>>6 == 0:
		headerBytes++
	default:
		headerBytes += contentSizeBytes[flags>>6]
	}
	isWhole, err = discardWhole(reader, int64(headerBytes-1))
	if err != nil || !isWhole {
		return frame{}, err
	}

	read := frame{wholeBytes: int64(headerBytes), hasHeader: true, hasChecksum: flags&checksumFrameBit != 0}
	for {
		var header [blockHeaderBytes]byte
		isWhole, err := readWhole(reader, header[:])
		if err != nil || !isWhole {
			return read, err
		}

		value := uint32(header[0]) | uint32(header[1])<<8 | uint32(header[2])<<16
		isLast := value&1 != 0
		size := int64(value >> 3)
		switch (value >> 1) & 3 {
		case repeatedBlock:
			size = 1
		case reservedBlock:
			return read, errors.New("it holds a block of a reserved type")
		}

		isWhole, err = discardWhole(reader, size)
		if err != nil || !isWhole {
			return read, err
		}
		read.wholeBytes += blockHeaderBytes + size

		if isLast {
			if read.hasChecksum {
				isWhole, err := discardWhole(reader, checksumBytes)
				if err != nil || !isWhole {
					return read, err
				}
				read.wholeBytes += checksumBytes
			}
			read.hasEnded = true
			return read, nil
		}
	}
}

func readUint32(reader *bufio.Reader) (uint32, bool, error) {
	var value [4]byte
	isWhole, err := readWhole(reader, value[:])
	return binary.LittleEndian.Uint32(value[:]), isWhole, err
}

func readWhole(reader *bufio.Reader, into []byte) (bool, error) {
	_, err := io.ReadFull(reader, into)
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return false, nil
	}
	return err == nil, err
}

func discardWhole(reader *bufio.Reader, count int64) (bool, error) {
	for count > 0 {
		step := int(min(count, surveyBufferBytes))
		discardedBytes, err := reader.Discard(step)
		count -= int64(discardedBytes)
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
	return true, nil
}

func trailer(next int) []byte {
	frameBytes := binary.LittleEndian.AppendUint32(nil, trailerMagic)
	frameBytes = binary.LittleEndian.AppendUint32(frameBytes, trailerPayloadBytes)
	return binary.LittleEndian.AppendUint64(frameBytes, uint64(next)) //nolint:gosec // an exchange count
}

func lastExchangeNumber(source io.Reader) (int, error) {
	decoder, err := zstd.NewReader(
		source,
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderMaxWindow(decodedWindowBytes),
		zstd.WithDecoderLowmem(false),
	)
	if err != nil {
		return 0, err
	}
	defer decoder.Close()

	scanner := &exchangeScanner{}
	if _, err := decoder.WriteTo(scanner); err != nil {
		return 0, err
	}
	return scanner.last, nil
}

type exchangeScanner struct {
	last int
	line []byte
}

func (self *exchangeScanner) Write(chunk []byte) (int, error) {
	for rest := chunk; len(rest) > 0; {
		lineEnd := bytes.IndexByte(rest, '\n')
		piece := rest
		if lineEnd >= 0 {
			piece = rest[:lineEnd+1]
		}
		if room := markerLineBytes - len(self.line); room > 0 {
			self.line = append(self.line, piece[:min(len(piece), room)]...)
		}
		if lineEnd < 0 {
			break
		}

		self.scan()
		rest = rest[lineEnd+1:]
	}
	return len(chunk), nil
}

func (self *exchangeScanner) scan() {
	defer func() { self.line = self.line[:0] }()
	if !bytes.HasPrefix(self.line, []byte(exchangeMarker)) {
		return
	}

	var sequence int
	if _, err := fmt.Sscanf(string(self.line), exchangeMarker+"%d start", &sequence); err == nil {
		self.last = max(self.last, sequence)
	}
}
