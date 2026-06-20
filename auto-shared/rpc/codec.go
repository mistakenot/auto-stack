package rpc

import (
	"encoding/json"
	"io"
)

// Encoder writes NDJSON (one JSON object per line) to an io.Writer.
// Each Encode call produces exactly one newline-terminated JSON line.
type Encoder struct {
	enc *json.Encoder
}

// NewEncoder creates an Encoder that writes to w. The underlying
// json.Encoder appends a newline after each value, producing NDJSON.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{enc: json.NewEncoder(w)}
}

// Encode marshals v as a single JSON line followed by a newline.
func (e *Encoder) Encode(v any) error {
	return e.enc.Encode(v)
}

// Decoder reads NDJSON frames from an io.Reader. Each Decode call consumes
// one JSON value from the stream.
type Decoder struct {
	dec *json.Decoder
}

// NewDecoder creates a Decoder that reads from r.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{dec: json.NewDecoder(r)}
}

// Decode reads the next JSON value from the stream into v.
func (d *Decoder) Decode(v any) error {
	return d.dec.Decode(v)
}
