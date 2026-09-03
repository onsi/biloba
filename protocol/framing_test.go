package protocol_test

import (
	"bytes"
	"encoding/binary"
	"io"
	"strings"

	"github.com/onsi/biloba/protocol"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("length-prefixed JSON framing", func() {
	type message struct {
		ID     uint64 `json:"id"`
		Method string `json:"method"`
		Script string `json:"script"`
	}

	It("round-trips multiline payloads across arbitrarily fragmented reads", func() {
		var wire bytes.Buffer
		writer := protocol.NewFramedWriter(&wire)
		original := message{ID: 17, Method: "evaluate", Script: "return `first\\nsecond`\n"}
		Expect(writer.Write(original)).To(Succeed())

		reader := protocol.NewFramedReader(&oneByteReader{reader: bytes.NewReader(wire.Bytes())})
		var decoded message
		Expect(reader.Read(&decoded)).To(Succeed())
		Expect(decoded).To(Equal(original))
	})

	It("reads consecutive frames without relying on message boundaries", func() {
		var wire bytes.Buffer
		writer := protocol.NewFramedWriter(&wire)
		Expect(writer.Write(message{ID: 1, Method: "first"})).To(Succeed())
		Expect(writer.Write(message{ID: 2, Method: "second"})).To(Succeed())

		reader := protocol.NewFramedReader(&wire)
		var first, second message
		Expect(reader.Read(&first)).To(Succeed())
		Expect(reader.Read(&second)).To(Succeed())
		Expect(first.ID).To(Equal(uint64(1)))
		Expect(second.ID).To(Equal(uint64(2)))
	})

	It("rejects an oversized frame before allocating its payload", func() {
		var header [4]byte
		binary.LittleEndian.PutUint32(header[:], protocol.MaxFrameSize+1)
		var decoded message
		err := protocol.NewFramedReader(bytes.NewReader(header[:])).Read(&decoded)
		Expect(err).To(MatchError(ContainSubstring("maximum")))
	})

	It("reports a truncated payload", func() {
		var header [4]byte
		binary.LittleEndian.PutUint32(header[:], 10)
		var decoded message
		err := protocol.NewFramedReader(io.MultiReader(bytes.NewReader(header[:]), strings.NewReader("{}"))).Read(&decoded)
		Expect(err).To(MatchError(ContainSubstring("unexpected EOF")))
	})
})

type oneByteReader struct {
	reader io.Reader
}

func (r *oneByteReader) Read(buffer []byte) (int, error) {
	if len(buffer) > 1 {
		buffer = buffer[:1]
	}
	return r.reader.Read(buffer)
}
