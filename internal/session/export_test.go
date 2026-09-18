package session

// snapshotForTest greift unter dem Lock auf den Ringpuffer zu.
func (s *Session) snapshotForTest() ([]byte, int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Snapshot()
}
