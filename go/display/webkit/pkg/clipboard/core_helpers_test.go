package clipboard

func bytesRepeat(value []byte, count int) []byte {
	if count <= 0 || len(value) == 0 {
		return nil
	}
	out := make([]byte, 0, len(value)*count)
	for range count {
		out = append(out, value...)
	}
	return out
}
