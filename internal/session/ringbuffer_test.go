package session

import (
	"bytes"
	"strings"
	"testing"
)

func TestRingBufferAuffuellen(t *testing.T) {
	r := newRingBuffer(10)
	if _, err := r.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Write([]byte("de")); err != nil {
		t.Fatal(err)
	}
	data, offset, truncated := r.Snapshot()
	if string(data) != "abcde" {
		t.Errorf("Inhalt = %q, erwartet abcde", data)
	}
	if offset != 0 {
		t.Errorf("Offset = %d, erwartet 0", offset)
	}
	if truncated {
		t.Error("ohne Überlauf darf nichts als gekürzt gelten")
	}
	if r.Written() != 5 {
		t.Errorf("Written = %d, erwartet 5", r.Written())
	}
}

func TestRingBufferUeberlauf(t *testing.T) {
	r := newRingBuffer(5)
	r.Write([]byte("abcde"))
	r.Write([]byte("fg"))

	data, offset, truncated := r.Snapshot()
	if string(data) != "cdefg" {
		t.Errorf("Inhalt = %q, erwartet cdefg (älteste Daten verworfen)", data)
	}
	if !truncated {
		t.Error("Kürzung wurde nicht gemeldet")
	}
	if offset != 2 {
		t.Errorf("Offset = %d, erwartet 2", offset)
	}
	if r.Written() != 7 {
		t.Errorf("Written = %d, erwartet 7", r.Written())
	}
}

func TestRingBufferEinzelschreibenGroesserAlsPuffer(t *testing.T) {
	r := newRingBuffer(4)
	r.Write([]byte("0123456789"))
	data, offset, truncated := r.Snapshot()
	if string(data) != "6789" {
		t.Errorf("Inhalt = %q, erwartet 6789", data)
	}
	if !truncated || offset != 6 {
		t.Errorf("truncated = %v, Offset = %d; erwartet true und 6", truncated, offset)
	}
}

func TestRingBufferOffsetSchreibtFort(t *testing.T) {
	r := newRingBuffer(8)
	gesamt := int64(0)
	for i := 0; i < 100; i++ {
		block := []byte(strings.Repeat("x", 3))
		r.Write(block)
		gesamt += int64(len(block))
		if r.Written() != gesamt {
			t.Fatalf("Written = %d, erwartet %d", r.Written(), gesamt)
		}
		data, offset, _ := r.Snapshot()
		if offset+int64(len(data)) != r.Written() {
			t.Fatalf("Offset %d + Länge %d ≠ Written %d", offset, len(data), r.Written())
		}
	}
}

func TestRingBufferUmlaufBleibtInReihenfolge(t *testing.T) {
	r := newRingBuffer(6)
	var erwartet []byte
	for i := byte(0); i < 40; i++ {
		block := []byte{i, i + 1}
		r.Write(block)
		erwartet = append(erwartet, block...)
	}
	data, _, _ := r.Snapshot()
	if !bytes.Equal(data, erwartet[len(erwartet)-6:]) {
		t.Errorf("Inhalt = %v, erwartet %v", data, erwartet[len(erwartet)-6:])
	}
}
