package tap

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"io"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

var zstdDecoder, _ = zstd.NewReader(nil)

func DecompressPayload(payload []byte) ([]byte, error) {
	// 1. Try Zstandard
	if zstdDecoder != nil {
		out, err := zstdDecoder.DecodeAll(payload, nil)
		if err == nil && len(out) > 0 {
			return out, nil
		}
	}

	// 2. Try Brotli
	brReader := brotli.NewReader(bytes.NewReader(payload))
	out, err := io.ReadAll(brReader)
	if err == nil && len(out) > 0 {
		return out, nil
	}

	// 3. Try Gzip
	gzReader, err := gzip.NewReader(bytes.NewReader(payload))
	if err == nil {
		out, err := io.ReadAll(gzReader)
		gzReader.Close()
		if err == nil && len(out) > 0 {
			return out, nil
		}
	}

	// 4. Try Deflate
	flReader := flate.NewReader(bytes.NewReader(payload))
	out, err = io.ReadAll(flReader)
	flReader.Close()
	if err == nil && len(out) > 0 {
		return out, nil
	}

	return payload, nil
}
