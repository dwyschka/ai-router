package session

// ringBuffer hält die jüngsten Ausgabebytes einer Session bis zu einer festen
// Obergrenze. Der Byte-Offset wächst monoton weiter, auch wenn älteste Daten
// verworfen werden — daran erkennt ein Client eine Kürzung.
type ringBuffer struct {
	data      []byte
	head      int   // Index des ältesten Bytes
	size      int   // aktuell gehaltene Bytes
	max       int   // Obergrenze
	written   int64 // insgesamt jemals geschriebene Bytes (monoton)
	truncated bool  // es wurden bereits Daten verworfen
}

func newRingBuffer(max int) *ringBuffer {
	if max <= 0 {
		max = 1 << 20
	}
	return &ringBuffer{data: make([]byte, max), max: max}
}

// Write nimmt Ausgabebytes auf und verwirft bei Überlauf die ältesten.
func (r *ringBuffer) Write(p []byte) (int, error) {
	n := len(p)
	r.written += int64(n)

	if n >= r.max {
		// Der neue Block füllt den Puffer allein: nur sein Ende bleibt übrig.
		copy(r.data, p[n-r.max:])
		r.head = 0
		r.size = r.max
		r.truncated = true
		return n, nil
	}

	if r.size+n > r.max {
		drop := r.size + n - r.max
		r.head = (r.head + drop) % r.max
		r.size -= drop
		r.truncated = true
	}

	tail := (r.head + r.size) % r.max
	first := copy(r.data[tail:], p)
	if first < n {
		copy(r.data, p[first:])
	}
	r.size += n
	return n, nil
}

// Snapshot liefert eine Kopie des gehaltenen Verlaufs in Reihenfolge, den Offset des
// ersten gehaltenen Bytes und den Hinweis, ob älterer Verlauf abgeschnitten wurde.
func (r *ringBuffer) Snapshot() (data []byte, offset int64, truncated bool) {
	out := make([]byte, r.size)
	first := copy(out, r.data[r.head:min(r.head+r.size, r.max)])
	if first < r.size {
		copy(out[first:], r.data[:r.size-first])
	}
	return out, r.written - int64(r.size), r.truncated
}

// Written ist der monotone Byte-Offset hinter dem zuletzt geschriebenen Byte.
func (r *ringBuffer) Written() int64 { return r.written }

// Reset gibt den Puffer frei, wenn eine beendete Session entfernt wird.
func (r *ringBuffer) Reset() {
	r.data = nil
	r.head, r.size = 0, 0
}
