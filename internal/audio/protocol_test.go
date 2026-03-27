package audio

import (
	"bytes"
	"math"
	"net"
	"testing"
	"time"
)

func TestEncodeDecodeRoundtrip(t *testing.T) {
	original := FrequencyData{
		Peak:       0.85,
		ProgressMs: 12345,
	}
	for i := range NumBands {
		original.Bands[i] = float32(i) / float32(NumBands)
	}

	var buf bytes.Buffer
	if err := EncodeFrame(&buf, &original); err != nil {
		t.Fatalf("EncodeFrame: %v", err)
	}

	if buf.Len() != frameSize {
		t.Errorf("frame size: got %d, want %d", buf.Len(), frameSize)
	}

	var decoded FrequencyData
	if err := DecodeFrame(&buf, &decoded); err != nil {
		t.Fatalf("DecodeFrame: %v", err)
	}

	for i := range NumBands {
		if decoded.Bands[i] != original.Bands[i] {
			t.Errorf("Band[%d]: got %f, want %f", i, decoded.Bands[i], original.Bands[i])
		}
	}
	if decoded.Peak != original.Peak {
		t.Errorf("Peak: got %f, want %f", decoded.Peak, original.Peak)
	}
	if decoded.ProgressMs != original.ProgressMs {
		t.Errorf("ProgressMs: got %d, want %d", decoded.ProgressMs, original.ProgressMs)
	}

	// ComputeConvenienceFields should have been called
	if decoded.Bass == 0 && decoded.Bands[0] != 0 {
		t.Error("Bass should be computed after decode")
	}
}

func TestEncodeDecodeMultipleFrames(t *testing.T) {
	var buf bytes.Buffer
	frames := make([]FrequencyData, 3)
	for f := range frames {
		frames[f].Peak = float32(f) * 0.3
		frames[f].ProgressMs = int32(f * 1000)
		for i := range NumBands {
			frames[f].Bands[i] = float32(f*NumBands+i) * 0.001
		}
		if err := EncodeFrame(&buf, &frames[f]); err != nil {
			t.Fatalf("EncodeFrame[%d]: %v", f, err)
		}
	}

	for f := range frames {
		var decoded FrequencyData
		if err := DecodeFrame(&buf, &decoded); err != nil {
			t.Fatalf("DecodeFrame[%d]: %v", f, err)
		}
		if decoded.Peak != frames[f].Peak {
			t.Errorf("frame %d Peak: got %f, want %f", f, decoded.Peak, frames[f].Peak)
		}
		if decoded.ProgressMs != frames[f].ProgressMs {
			t.Errorf("frame %d ProgressMs: got %d, want %d", f, decoded.ProgressMs, frames[f].ProgressMs)
		}
	}
}

func TestDecodeFrame_BadMagic(t *testing.T) {
	buf := make([]byte, frameSize)
	// Write wrong magic
	buf[0] = 0xFF
	buf[1] = 0xFF
	buf[2] = 0xFF
	buf[3] = 0xFF

	var fd FrequencyData
	err := DecodeFrame(bytes.NewReader(buf), &fd)
	if err == nil {
		t.Fatal("expected error for bad magic")
	}
}

func TestDecodeFrame_ShortRead(t *testing.T) {
	buf := make([]byte, 10) // too short
	var fd FrequencyData
	err := DecodeFrame(bytes.NewReader(buf), &fd)
	if err == nil {
		t.Fatal("expected error for short read")
	}
}

func TestDecodeFrame_EmptyReader(t *testing.T) {
	var fd FrequencyData
	err := DecodeFrame(bytes.NewReader(nil), &fd)
	if err == nil {
		t.Fatal("expected error for empty reader")
	}
}

func TestEncodeDecodeSpecialValues(t *testing.T) {
	original := FrequencyData{
		Peak:       0,
		ProgressMs: 0,
	}
	// All zeros
	var buf bytes.Buffer
	if err := EncodeFrame(&buf, &original); err != nil {
		t.Fatalf("EncodeFrame: %v", err)
	}
	var decoded FrequencyData
	if err := DecodeFrame(&buf, &decoded); err != nil {
		t.Fatalf("DecodeFrame: %v", err)
	}
	if decoded.Peak != 0 {
		t.Errorf("Peak: got %f, want 0", decoded.Peak)
	}

	// Max values
	original.Peak = math.MaxFloat32
	original.ProgressMs = math.MaxInt32
	for i := range NumBands {
		original.Bands[i] = 1.0
	}
	buf.Reset()
	if err := EncodeFrame(&buf, &original); err != nil {
		t.Fatalf("EncodeFrame: %v", err)
	}
	if err := DecodeFrame(&buf, &decoded); err != nil {
		t.Fatalf("DecodeFrame: %v", err)
	}
	if decoded.Peak != math.MaxFloat32 {
		t.Errorf("Peak: got %f, want MaxFloat32", decoded.Peak)
	}
}

func TestReceiverStartStop(t *testing.T) {
	r := NewReceiver()
	if err := r.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	path := r.SocketPath()
	if path == "" {
		t.Fatal("SocketPath is empty")
	}

	// Latest should return nil before any data
	if fd := r.Latest(); fd != nil {
		t.Errorf("Latest before data: got %+v", fd)
	}

	r.Stop()
	// Stop should be idempotent
	r.Stop()
}

func TestReceiverReceivesData(t *testing.T) {
	r := NewReceiver()
	if err := r.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer r.Stop()

	// Connect and send a frame
	network := "unix"
	conn, err := net.Dial(network, r.SocketPath())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	fd := FrequencyData{Peak: 0.75, ProgressMs: 5000}
	fd.Bands[0] = 0.9
	fd.Bands[1] = 0.8
	if err := EncodeFrame(conn, &fd); err != nil {
		t.Fatalf("EncodeFrame: %v", err)
	}

	// Give the receiver a moment to process
	time.Sleep(50 * time.Millisecond)

	latest := r.Latest()
	if latest == nil {
		t.Fatal("Latest returned nil after sending data")
	}
	if latest.Peak != 0.75 {
		t.Errorf("Peak: got %f, want 0.75", latest.Peak)
	}
	if latest.ProgressMs != 5000 {
		t.Errorf("ProgressMs: got %d, want 5000", latest.ProgressMs)
	}
}

func TestReceiverLatestStale(t *testing.T) {
	r := NewReceiver()

	// Manually set a stale timestamp (200ms ago)
	r.lastUpdate.Store(time.Now().Add(-200 * time.Millisecond).UnixNano())
	fd := &FrequencyData{Peak: 0.5}
	r.latest.Store(fd)

	// Should return nil because data is stale
	if got := r.Latest(); got != nil {
		t.Errorf("Latest should be nil for stale data, got %+v", got)
	}
}
