package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

// Wire protocol constants.
const (
	protocolMagic = 0x54554649 // "TUFI"
	// magic(4) + bands(NumBands×4) + peak(4) + progressMs(4) = 268 bytes.
	// Bass/Mid/High are recomputed on decode, not transmitted.
	frameSize = 4 + NumBands*4 + 4 + 4
)

// EncodeFrame writes a FrequencyData as a fixed-size binary frame to w.
func EncodeFrame(w io.Writer, fd *FrequencyData) error {
	var buf [frameSize]byte

	binary.LittleEndian.PutUint32(buf[0:4], protocolMagic)

	for i := range NumBands {
		binary.LittleEndian.PutUint32(buf[4+i*4:4+i*4+4], math.Float32bits(fd.Bands[i]))
	}

	binary.LittleEndian.PutUint32(buf[260:264], math.Float32bits(fd.Peak))
	binary.LittleEndian.PutUint32(buf[264:268], uint32(fd.ProgressMs))

	_, err := w.Write(buf[:])
	return err
}

// DecodeFrame reads a fixed-size binary frame from r into fd.
func DecodeFrame(r io.Reader, fd *FrequencyData) error {
	var buf [frameSize]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return err
	}

	magic := binary.LittleEndian.Uint32(buf[0:4])
	if magic != protocolMagic {
		return fmt.Errorf("invalid frame magic: 0x%08x", magic)
	}

	for i := range NumBands {
		fd.Bands[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[4+i*4 : 4+i*4+4]))
	}

	fd.Peak = math.Float32frombits(binary.LittleEndian.Uint32(buf[260:264]))
	fd.ProgressMs = int32(binary.LittleEndian.Uint32(buf[264:268]))

	// Recompute convenience fields from bands.
	fd.Bass = 0
	for i := 0; i < 8; i++ {
		fd.Bass += fd.Bands[i]
	}
	fd.Bass /= 8

	fd.Mid = 0
	for i := 8; i < 32; i++ {
		fd.Mid += fd.Bands[i]
	}
	fd.Mid /= 24

	fd.High = 0
	for i := 32; i < 64; i++ {
		fd.High += fd.Bands[i]
	}
	fd.High /= 32

	return nil
}
