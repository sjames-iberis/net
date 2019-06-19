package codec

import (
	"encoding/xml"
	"io"

	"github.com/damianoneill/net/netconf/common"

	"github.com/damianoneill/net/netconf/common/codec/rfc6242"
)

// Define encoder and decoder that wrap the standard xml Codec (for XML en/decoding)
// and RFC6242-compliant Codec (for netconf message framing)

type Decoder struct {
	*xml.Decoder
	ncDecoder *rfc6242.Decoder
}

type Encoder struct {
	xmlEncoder *xml.Encoder
	ncEncoder  *rfc6242.Encoder
}

func (e *Encoder) Encode(msg interface{}) error {

	err := e.xmlEncoder.Encode(msg)
	if err != nil {
		return err
	}
	return e.ncEncoder.EndOfMessage()
}

func NewDecoder(t io.Reader) *Decoder {
	ncDecoder := rfc6242.NewDecoder(t)
	return &Decoder{Decoder: xml.NewDecoder(ncDecoder), ncDecoder: ncDecoder}
}

func NewEncoder(t io.Writer) *Encoder {
	ncEncoder := rfc6242.NewEncoder(t)
	return &Encoder{xmlEncoder: xml.NewEncoder(ncEncoder), ncEncoder: ncEncoder}
}

func EnableChunkedFraming(d *Decoder, e *Encoder) {
	rfc6242.SetChunkedFraming(d.ncDecoder, e.ncEncoder)
}

func PeerSupportsChunkedFraming(caps []string) bool {
	for _, capability := range caps {
		if capability == common.CapBase11 {
			return true
		}
	}
	return false
}
