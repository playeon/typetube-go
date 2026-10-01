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

func decodeTrackResult(data []byte) (*TrackResult, error) {
	r := newProtoReader(data)
	res := &TrackResult{
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
			s, err := r.readString()
			if err != nil {
				return nil, err
			}
			res.Query = s
		case 3:
			s, err := r.readString()
			if err != nil {
				return nil, err
			}
			res.ID = s
		case 4:
			s, err := r.readString()
			if err != nil {
				return nil, err
			}
			res.Title = s
		case 5:
			s, err := r.readString()
			if err != nil {
				return nil, err
			}
			res.Author = s
		case 6:
			s, err := r.readString()
			if err != nil {
				return nil, err
			}
			res.Uploader = s
		case 7:
			s, err := r.readString()
			if err != nil {
				return nil, err
			}
			res.ArtistAvatar = s
		case 8:
			v, err := r.readVarint()
			if err != nil {
				return nil, err
			}
			res.DurationSeconds = int(v)
		case 9:
			s, err := r.readString()
			if err != nil {
				return nil, err
			}
			res.Thumbnail = s
		case 10:
			b, err := r.readBytes()
			if err != nil {
				return nil, err
			}
			th, err := decodeThumbnail(b)
			if err != nil {
				return nil, err
			}
			res.Thumbnails = append(res.Thumbnails, th)
		case 11:
			d, err := r.readDouble()
			if err != nil {
				return nil, err
			}
			res.LatencyMs = d
		case 12:
			b, err := r.readBytes()
			if err != nil {
				return nil, err
			}
			as, err := decodeAudioStream(b)
			if err != nil {
				return nil, err
			}
			res.BestAudio = &as
		case 13:
			b, err := r.readBytes()
			if err != nil {
				return nil, err
			}
			vs, err := decodeVideoStream(b)
			if err != nil {
				return nil, err
			}
			res.BestVideo = &vs
		case 14:
			b, err := r.readBytes()
			if err != nil {
				return nil, err
			}
			as, err := decodeAudioStream(b)
			if err != nil {
				return nil, err
			}
			res.AudioStreams = append(res.AudioStreams, as)
		case 15:
			b, err := r.readBytes()
			if err != nil {
				return nil, err
			}
			vs, err := decodeVideoStream(b)
			if err != nil {
				return nil, err
			}
			res.VideoStreams = append(res.VideoStreams, vs)
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
					return nil, err
				}
				switch ifield {
				case 1:
					s, err := ir.readString()
					if err != nil {
						return nil, err
					}
					item.ID = s
				case 2:
					s, err := ir.readString()
					if err != nil {
						return nil, err
					}
					item.Title = s
				case 3:
					s, err := ir.readString()
					if err != nil {
						return nil, err
					}
					item.URL = s
				case 4:
					v, err := ir.readVarint()
					if err != nil {
						return nil, err
					}
					item.Duration = int(v)
				case 5:
					s, err := ir.readString()
					if err != nil {
						return nil, err
					}
					item.Uploader = s
				default:
					if err := ir.skip(iwire); err != nil {
						return nil, err
					}
				}
			}
			items = append(items, item)
		default:
			if err := r.skip(wire); err != nil {
				return nil, err
			}
		}
	}
	return items, nil
}
