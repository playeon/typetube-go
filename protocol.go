package typetube

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

type protoReader struct {
	buf []byte
	pos int
}

func newProtoReader(buf []byte) *protoReader {
	return &protoReader{buf: buf, pos: 0}
}

func (p *protoReader) hasMore() bool {
	return p.pos < len(p.buf)
}

func (p *protoReader) readVarint() (uint64, error) {
	var val uint64
	var shift uint
	for {
		if p.pos >= len(p.buf) {
			return 0, errors.New("unexpected EOF reading varint")
		}
		b := p.buf[p.pos]
		p.pos++
		val |= uint64(b&0x7F) << shift
		if (b & 0x80) == 0 {
			return val, nil
		}
		shift += 7
		if shift >= 64 {
			return 0, errors.New("varint overflow")
		}
	}
}

func (p *protoReader) readTag() (int, int, error) {
	tag, err := p.readVarint()
	if err != nil {
		return 0, 0, err
	}
	return int(tag >> 3), int(tag & 0x07), nil
}

func (p *protoReader) readBytes() ([]byte, error) {
	length, err := p.readVarint()
	if err != nil {
		return nil, err
	}
	l := int(length)
	if p.pos+l > len(p.buf) {
		return nil, errors.New("unexpected EOF reading bytes")
	}
	data := p.buf[p.pos : p.pos+l]
	p.pos += l
	return data, nil
}

func (p *protoReader) readString() (string, error) {
	b, err := p.readBytes()
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (p *protoReader) readDouble() (float64, error) {
	if p.pos+8 > len(p.buf) {
		return 0, errors.New("unexpected EOF reading double")
	}
	bits := binary.LittleEndian.Uint64(p.buf[p.pos : p.pos+8])
	p.pos += 8
	return math.Float64frombits(bits), nil
}

func (p *protoReader) skip(wire int) error {
	switch wire {
	case 0:
		_, err := p.readVarint()
		return err
	case 1:
		if p.pos+8 > len(p.buf) {
			return errors.New("unexpected EOF skipping 64-bit")
		}
		p.pos += 8
		return nil
	case 2:
		_, err := p.readBytes()
		return err
	case 5:
		if p.pos+4 > len(p.buf) {
			return errors.New("unexpected EOF skipping 32-bit")
		}
		p.pos += 4
		return nil
	default:
		return fmt.Errorf("unsupported wire type: %d", wire)
	}
}

func decodeThumbnail(data []byte) (Thumbnail, error) {
	r := newProtoReader(data)
	var thumb Thumbnail
	for r.hasMore() {
		field, wire, err := r.readTag()
		if err != nil {
			return thumb, err
		}
		switch field {
		case 1:
			s, err := r.readString()
			if err != nil {
				return thumb, err
			}
			thumb.URL = s
		case 2:
			v, err := r.readVarint()
			if err != nil {
				return thumb, err
			}
			thumb.Width = int(v)
		case 3:
			v, err := r.readVarint()
			if err != nil {
				return thumb, err
			}
			thumb.Height = int(v)
		default:
			if err := r.skip(wire); err != nil {
				return thumb, err
			}
		}
	}
	return thumb, nil
}

func decodeAudioStream(data []byte) (AudioStream, error) {
	r := newProtoReader(data)
	var stream AudioStream
	for r.hasMore() {
		field, wire, err := r.readTag()
		if err != nil {
			return stream, err
		}
		switch field {
		case 1:
			v, err := r.readVarint()
			if err != nil {
				return stream, err
			}
			stream.ITag = int(v)
		case 2:
			s, err := r.readString()
			if err != nil {
				return stream, err
			}
			stream.Quality = s
		case 3:
			s, err := r.readString()
			if err != nil {
				return stream, err
			}
			stream.MimeType = s
		case 4:
			v, err := r.readVarint()
			if err != nil {
				return stream, err
			}
			stream.Bitrate = int64(v)
		case 5:
			s, err := r.readString()
			if err != nil {
				return stream, err
			}
			stream.URL = s
		case 6:
			v, err := r.readVarint()
			if err != nil {
				return stream, err
			}
			stream.IsHiFi = v == 1
		case 7:
			s, err := r.readString()
			if err != nil {
				return stream, err
			}
			stream.RawURL = s
		default:
			if err := r.skip(wire); err != nil {
				return stream, err
			}
		}
	}
	return stream, nil
}

func decodeVideoStream(data []byte) (VideoStream, error) {
	r := newProtoReader(data)
	var stream VideoStream
	for r.hasMore() {
		field, wire, err := r.readTag()
		if err != nil {
			return stream, err
		}
		switch field {
		case 1:
			v, err := r.readVarint()
			if err != nil {
				return stream, err
			}
			stream.ITag = int(v)
		case 2:
			s, err := r.readString()
			if err != nil {
				return stream, err
			}
			stream.Quality = s
		case 3:
			s, err := r.readString()
			if err != nil {
				return stream, err
			}
			stream.Resolution = s
		case 4:
			s, err := r.readString()
			if err != nil {
				return stream, err
			}
			stream.MimeType = s
		case 5:
			s, err := r.readString()
			if err != nil {
				return stream, err
			}
			stream.URL = s
		default:
			if err := r.skip(wire); err != nil {
				return stream, err
			}
		}
	}
	return stream, nil
}

func decodeTrackData(data []byte, res *TrackResult) error {
	r := newProtoReader(data)
	for r.hasMore() {
		field, wire, err := r.readTag()
		if err != nil {
			return err
		}
		switch field {
		case 1:
			s, err := r.readString()
			if err != nil {
				return err
			}
			res.ID = s
			res.Query = s
		case 2:
			s, err := r.readString()
			if err != nil {
				return err
			}
			res.Title = s
		case 3:
			s, err := r.readString()
			if err != nil {
				return err
			}
			res.Author = s
		case 4:
			s, err := r.readString()
			if err != nil {
				return err
			}
			res.Uploader = s
		case 5:
			s, err := r.readString()
			if err != nil {
				return err
			}
			res.ArtistAvatar = s
		case 6:
			v, err := r.readVarint()
			if err != nil {
				return err
			}
			res.DurationSeconds = int(v)
		case 7:
			s, err := r.readString()
			if err != nil {
				return err
			}
			res.Thumbnail = s
		case 8:
			b, err := r.readBytes()
			if err != nil {
				return err
			}
			th, err := decodeThumbnail(b)
			if err != nil {
				return err
			}
			res.Thumbnails = append(res.Thumbnails, th)
		case 9:
			b, err := r.readBytes()
			if err != nil {
				return err
			}
			as, err := decodeAudioStream(b)
			if err != nil {
				return err
			}
			res.BestAudio = &as
		case 10:
			b, err := r.readBytes()
			if err != nil {
				return err
			}
			vs, err := decodeVideoStream(b)
			if err != nil {
				return err
			}
			res.BestVideo = &vs
		case 11:
			b, err := r.readBytes()
			if err != nil {
				return err
			}
			as, err := decodeAudioStream(b)
			if err != nil {
				return err
			}
			res.AudioStreams = append(res.AudioStreams, as)
		case 12:
			b, err := r.readBytes()
			if err != nil {
				return err
			}
			vs, err := decodeVideoStream(b)
			if err != nil {
				return err
			}
			res.VideoStreams = append(res.VideoStreams, vs)
		default:
			if err := r.skip(wire); err != nil {
				return err
			}
		}
	}
	return nil
}

func decodeMeta(data []byte, res *TrackResult) error {
	r := newProtoReader(data)
	for r.hasMore() {
		field, wire, err := r.readTag()
		if err != nil {
			return err
		}
		switch field {
		case 1:
			if wire == 0 {
				v, _ := r.readVarint()
				res.LatencyMs = float64(v)
			} else {
				d, _ := r.readDouble()
				res.LatencyMs = d
			}
		default:
			if err := r.skip(wire); err != nil {
				return err
			}
		}
	}
	return nil
}

func decodeError(data []byte) (*TypeTubeError, error) {
	r := newProtoReader(data)
	errObj := &TypeTubeError{StatusCode: 500}
	for r.hasMore() {
		field, wire, err := r.readTag()
		if err != nil {
			return nil, err
		}
		switch field {
		case 1:
			s, err := r.readString()
			if err != nil {
				return nil, err
			}
			_ = s
		case 2:
			s, err := r.readString()
			if err != nil {
				return nil, err
			}
			errObj.Message = s
		case 3:
			v, err := r.readVarint()
			if err != nil {
				return nil, err
			}
			errObj.StatusCode = int(v)
		default:
			if err := r.skip(wire); err != nil {
				return nil, err
			}
		}
	}
	return errObj, nil
}

func decodeFlatTrackResult(data []byte) (*TrackResult, error) {
	r := newProtoReader(data)
	res := &TrackResult{
		Success:      true,
		Thumbnails:   make([]Thumbnail, 0),
		AudioStreams: make([]AudioStream, 0),
		VideoStreams: make([]VideoStream, 0),
	}
	for r.hasMore() {
		field, wire, err := r.readTag()
		if err != nil {
			return nil, err
		}
		switch field {
		case 1:
			v, err := r.readVarint()
			if err != nil {
				return nil, err
			}
			res.Success = v == 1
		case 2:
			s, _ := r.readString()
			res.Query = s
		case 3:
			s, _ := r.readString()
			res.ID = s
		case 4:
			s, _ := r.readString()
			res.Title = s
		case 5:
			s, _ := r.readString()
			res.Author = s
		case 6:
			s, _ := r.readString()
			res.Uploader = s
		case 7:
			s, _ := r.readString()
			res.ArtistAvatar = s
		case 8:
			v, _ := r.readVarint()
			res.DurationSeconds = int(v)
		case 9:
			s, _ := r.readString()
			res.Thumbnail = s
		case 10:
			b, _ := r.readBytes()
			th, _ := decodeThumbnail(b)
			res.Thumbnails = append(res.Thumbnails, th)
		case 11:
			if wire == 0 {
				v, _ := r.readVarint()
				res.LatencyMs = float64(v)
			} else {
				d, _ := r.readDouble()
				res.LatencyMs = d
			}
		case 12:
			b, _ := r.readBytes()
			as, _ := decodeAudioStream(b)
			res.BestAudio = &as
		case 13:
			b, _ := r.readBytes()
			vs, _ := decodeVideoStream(b)
			res.BestVideo = &vs
		case 14:
			b, _ := r.readBytes()
			as, _ := decodeAudioStream(b)
			res.AudioStreams = append(res.AudioStreams, as)
		case 15:
			b, _ := r.readBytes()
			vs, _ := decodeVideoStream(b)
			res.VideoStreams = append(res.VideoStreams, vs)
		default:
			_ = r.skip(wire)
		}
	}
	return res, nil
}

func decodeTrackResult(data []byte) (*TrackResult, error) {
	// Probe to see if this is flat format (field 1: varint, field 2: string query, field 3: string id)
	// or TAP envelope (field 1: varint, field 2: message trackData, field 3: message meta)
	pr := newProtoReader(data)
	isFlat := false
	for pr.hasMore() {
		f, w, err := pr.readTag()
		if err != nil {
			break
		}
		if f == 3 && w == 2 {
			// In flat format, field 3 is string id (length is usually <= 64)
			// In TAP envelope, field 3 is meta message (wire type 2)
		}
		if f == 4 && w == 2 {
			// In flat format, field 4 is string title!
			// In envelope, field 4 is error message
			// We can check if field 14 or 15 appears:
			isFlat = true
			break
		}
		if f > 4 {
			break
		}
		_ = pr.skip(w)
	}

	if isFlat {
		return decodeFlatTrackResult(data)
	}

	r := newProtoReader(data)
	res := &TrackResult{
		Success:      false,
		Thumbnails:   make([]Thumbnail, 0),
		AudioStreams: make([]AudioStream, 0),
		VideoStreams: make([]VideoStream, 0),
	}

	for r.hasMore() {
		field, wire, err := r.readTag()
		if err != nil {
			return nil, err
		}
		switch field {
		case 1:
			v, err := r.readVarint()
			if err != nil {
				return nil, err
			}
			res.Success = v == 1
		case 2:
			b, err := r.readBytes()
			if err != nil {
				return nil, err
			}
			if err := decodeTrackData(b, res); err != nil {
				return nil, err
			}
		case 3:
			b, err := r.readBytes()
			if err != nil {
				return nil, err
			}
			_ = decodeMeta(b, res)
		case 4:
			b, err := r.readBytes()
			if err != nil {
				return nil, err
			}
			apiErr, _ := decodeError(b)
			if apiErr != nil {
				return nil, apiErr
			}
		default:
			if err := r.skip(wire); err != nil {
				return nil, err
			}
		}
	}

	return res, nil
}

func decodeSearchResults(data []byte) ([]SearchItem, error) {
	r := newProtoReader(data)
	var items []SearchItem
	for r.hasMore() {
		field, wire, err := r.readTag()
		if err != nil {
			return nil, err
		}
		switch field {
		case 2:
			itemBytes, err := r.readBytes()
			if err != nil {
				return nil, err
			}
			ir := newProtoReader(itemBytes)
			var item SearchItem
			for ir.hasMore() {
				ifield, iwire, err := ir.readTag()
				if err != nil {
					break
				}
				switch ifield {
				case 1:
					item.ID, _ = ir.readString()
				case 2:
					item.Title, _ = ir.readString()
				case 3:
					item.URL, _ = ir.readString()
				case 4:
					v, _ := ir.readVarint()
					item.Duration = int(v)
				case 5:
					item.Uploader, _ = ir.readString()
				default:
					_ = ir.skip(iwire)
				}
			}
			if item.ID != "" || item.Title != "" {
				items = append(items, item)
			}
		default:
			if err := r.skip(wire); err != nil {
				return nil, err
			}
		}
	}
	return items, nil
}
