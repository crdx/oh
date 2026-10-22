package tool

import (
	"bytes"
	"runtime"
	"testing"
)

func TestOutputBufferBoundsItsRetainedAllocationAfterEveryWrite(t *testing.T) {
	for _, test := range []struct {
		name   string
		writes [][]byte
	}{
		{
			name:   "one oversized write",
			writes: [][]byte{bytes.Repeat([]byte("large"), outputBufferBytes)},
		},
		{
			name: "incremental overflow",
			writes: [][]byte{
				bytes.Repeat([]byte("a"), outputBufferBytes-3),
				[]byte("bcdef"),
				bytes.Repeat([]byte("g"), outputBufferBytes/2),
			},
		},
		{
			name: "small then oversized then small",
			writes: [][]byte{
				[]byte("prefix"),
				bytes.Repeat([]byte("middle"), outputBufferBytes),
				[]byte("tail"),
			},
		},
		{name: "empty writes", writes: [][]byte{nil, {}, []byte("x"), nil}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output OutputBuffer
			var expected []byte
			totalBytes := 0
			for index, data := range test.writes {
				count, err := output.Write(data)
				if err != nil || count != len(data) {
					t.Fatalf("write %d accepted %d of %d bytes: %v", index, count, len(data), err)
				}
				expected = append(expected, data...)
				totalBytes += len(data)
				if len(expected) > outputBufferBytes {
					expected = expected[len(expected)-outputBufferBytes:]
				}
				if len(output.contents) > outputBufferBytes || cap(output.contents) > outputBufferBytes {
					t.Fatalf("write %d retained length %d and capacity %d; limit %d", index, len(output.contents), cap(output.contents), outputBufferBytes)
				}
				if !bytes.Equal(output.contents, expected) || output.DroppedBytes() != totalBytes-len(expected) {
					t.Errorf("write %d lost output order or its dropped-byte count", index)
				}
			}
		})
	}
}

func TestOneLargeOutputWriteAllocatesOnlyTheRetainedTail(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 4*outputBufferBytes+17)
	const tolerance = 64 << 10
	var before, after runtime.MemStats
	var output OutputBuffer
	runtime.ReadMemStats(&before)
	if _, err := output.Write(data); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(&output)
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > outputBufferBytes+tolerance {
		t.Errorf("one write allocated %d bytes; the retained tail needs at most %d", allocated, outputBufferBytes+tolerance)
	}
}
